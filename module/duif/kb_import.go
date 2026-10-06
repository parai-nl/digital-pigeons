package duif

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/parai-nl/parai-core/data"
	"github.com/parai-nl/parai-interaction/modules/interaction/bundleimport"
)

// Reden-codes van de import. Ze staan op de wire (`reason` in de status) en in
// het log; de onderliggende fouttekst gaat nooit mee — providers echoën soms
// hun invoer, en die invoer is hier kennisbanktekst.
const (
	ReasonImportBundleInvalid     = "bundle_invalid"
	ReasonImportKBIDMismatch      = "kb_id_mismatch"
	ReasonImportEmbeddingMismatch = "embedding_mismatch"
	ReasonImportCountMismatch     = "count_mismatch"
	ReasonImportWriteFailed       = "write_failed"
	ReasonImportReplaceFailed     = "replace_failed"
	ReasonImportMarkerUnreadable  = "marker_unreadable"
	ReasonImportShutdown          = "shutdown"
	ReasonImportUpdateCheckFailed = "update_check_failed"
)

const (
	// defaultImportWorkers is het aantal parallelle schrijvers. Elke
	// standpunt-schrijving kost een embed-call bij de provider; zes is de
	// afweging tussen doorlooptijd (≈ 4100 calls) en 429's.
	defaultImportWorkers = 6
	// kbWriteAttempts is het totale aantal pogingen per schrijfoperatie.
	kbWriteAttempts = 3
	// kbWriteBackoff is de wachttijd vóór poging 2; daarna verdubbelt hij.
	kbWriteBackoff = 500 * time.Millisecond
	// kbProgressEvery bepaalt hoe vaak de voortgang gelogd wordt — als
	// telling, nooit als inhoud.
	kbProgressEvery = 500
	// kbListPage is de paginagrootte van ListNodes (core's plafond is 200).
	kbListPage = 200
)

// knowledgeWriter is de schrijfseam van de import: precies de vier methoden
// die de importer aanraakt, vervuld door data.KnowledgeRepo. De vergadermodus
// krijgt hem nooit te zien (interface-segregatie).
type knowledgeWriter interface {
	knowledgeLister
	CreateNodeWithID(ctx context.Context, id data.NodeID, props map[string]any, embedText string) (data.NodeID, error)
	Relate(ctx context.Context, from, to data.NodeID, relation string, props map[string]any) error
	Delete(ctx context.Context, id data.NodeID) error
}

// kbImporter draait de kennisbank-import: één tegelijk (CAS-gate), in de
// achtergrond, met de status als enige gedeelde toestand.
type kbImporter struct {
	writer             knowledgeWriter
	logger             *slog.Logger
	workers            int
	force              bool
	embeddingModel     string
	embeddingDimension int
	// sleep is de wachtseam tussen twee pogingen; als veld zodat een test niet
	// echt een halve seconde hoeft te wachten. Eén knop is genoeg: wie de
	// wachttijd wegneemt, heeft aan de duur niets meer.
	sleep func(ctx context.Context, d time.Duration) bool
	// onImportDone wordt aangeroepen zodra een run klaar is, geslaagd of niet.
	// De graaf hangt eraan: hij bewaart de kinderlijst van een ouder kort, en
	// een import (zeker een vervangende) schrijft of wist juist de rijen
	// waaruit die lijst geteld is. Ook een mislukte run kan al geschreven
	// hebben, dus de haak gaat af op elke uitkomst.
	onImportDone func()

	// running is de gate: twee imports tegelijk zouden dezelfde ids upserten.
	running atomic.Bool
	// cancel stopt de lopende import bij Shutdown; done sluit wanneer de
	// achtergrond-goroutine werkelijk weg is.
	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.Mutex
	status importStatus

	countMu sync.Mutex
	// progressEvery is de logdrempel van de voortgang; als veld, zodat een test
	// hem kan verlagen zonder duizend knopen te schrijven.
	progressEvery int
	nodeCounts    map[string]int
	written       int
	nextLog       int
	// Immutable after the preflight; workers only read this set.
	reuseEmbedding map[data.NodeID]bool
}

// newKBImporter bouwt de importer. workers ≤ 0 valt terug op de default.
func newKBImporter(w knowledgeWriter, logger *slog.Logger, workers int, force bool, embeddingModel string, embeddingDimension int) *kbImporter {
	if workers <= 0 {
		workers = defaultImportWorkers
	}
	return &kbImporter{
		writer:             w,
		logger:             logger,
		workers:            workers,
		force:              force,
		embeddingModel:     embeddingModel,
		embeddingDimension: embeddingDimension,
		sleep:              sleepCtx,
		status:             importStatus{State: ImportEmpty},
		progressEvery:      kbProgressEvery,
		nodeCounts:         map[string]int{},
		nextLog:            kbProgressEvery,
	}
}

func (i *kbImporter) log() *slog.Logger { return logOr(i.logger) }

// snapshot geeft een kopie van de status; de counts-map gaat mee als kopie
// zodat een lezer hem niet onder de importer vandaan kan muteren.
func (i *kbImporter) snapshot() importStatus {
	i.mu.Lock()
	defer i.mu.Unlock()
	st := i.status
	if st.Counts != nil {
		counts := make(map[string]int, len(st.Counts))
		for k, v := range st.Counts {
			counts[k] = v
		}
		st.Counts = counts
	}
	if st.Languages != nil {
		st.Languages = append([]string(nil), st.Languages...)
	}
	return st
}

func (i *kbImporter) setStatus(st importStatus) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.status = st
}

// stop cancelt een lopende import en wacht tot de goroutine weg is. De marker
// wordt dan niet geschreven, zodat de volgende boot de import gewoon herhaalt.
// Het wachten is begrensd door de meegegeven context: een import die in een
// trage schrijfactie hangt mag het afsluiten niet gijzelen, maar een schrijver
// die na Shutdown nog doorloopt schrijft in een database die het proces al
// opgeeft.
func (i *kbImporter) stop(ctx context.Context) {
	i.mu.Lock()
	cancel, done := i.cancel, i.done
	i.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// loadBootStatus bepaalt de stand zonder te schrijven: marker → ready,
// standpunten zonder marker → incomplete, niets → empty.
func (i *kbImporter) loadBootStatus(ctx context.Context) {
	st, err := readImportStatus(ctx, i.writer)
	if err != nil {
		i.log().Warn("kennisbankstatus onleesbaar", "component", "duif", "reason", ReasonImportMarkerUnreadable)
		st = importStatus{State: ImportFailed, Reason: ReasonImportMarkerUnreadable}
	}
	i.setStatus(st)
}

// start zet de afsluitgrendel klaar en draait de import in de achtergrond.
// De cancel staat er vóór de goroutine: een Shutdown die meteen op Init volgt
// moet de import kunnen stoppen, ook als die nog niet begonnen is.
func (i *kbImporter) start(ctx context.Context, path string) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	i.mu.Lock()
	i.cancel, i.done = cancel, done
	i.mu.Unlock()
	go func() {
		defer cancel()
		defer close(done)
		i.run(ctx, path)
	}()
}

// run draait één import. De CAS-gate is de hele grendel: een tweede aanroep
// (dubbele Init, of een handmatige trigger later) valt er stil op af.
func (i *kbImporter) run(ctx context.Context, path string) {
	if !i.running.CompareAndSwap(false, true) {
		return
	}
	defer i.running.Store(false)
	// De tellers horen bij déze run. Een tweede import op dezelfde importer zou
	// anders met de knopen van de eerste beginnen en de telling vóór de marker
	// laten kloppen op werk dat hij niet gedaan heeft.
	i.resetCounts()

	i.setStatus(importStatus{State: ImportImporting})
	st, _ := i.importBundle(ctx, path)
	i.setStatus(st)
	if i.onImportDone != nil {
		i.onImportDone()
	}
	if st.State == ImportReady {
		i.log().Info("kennisbank-import klaar", "component", "duif",
			"kb_id", st.KBID, "skipped", st.Skipped, "nodes", i.writtenCount())
		return
	}
	i.log().Error("kennisbank-import mislukt", "component", "duif", "reason", st.Reason)
	// Parser/provider errors may contain private corpus text or paths. The
	// fixed reason remains diagnostic at every log level, including Debug.
}

// resetCounts zet de tellers van een run terug op nul.
func (i *kbImporter) resetCounts() {
	i.countMu.Lock()
	defer i.countMu.Unlock()
	i.nodeCounts = map[string]int{}
	i.written = 0
	i.nextLog = i.progressEvery
	i.reuseEmbedding = nil
}

// importBundle is de import zelf. Hij geeft de eindstand terug plus de fout
// die eronder zat: de stand draagt een reden-code (en gaat de wire op), de
// fout blijft binnen; ook Debug krijgt uitsluitend de vaste reden-code.
func (i *kbImporter) importBundle(ctx context.Context, path string) (importStatus, error) {
	b, err := loadKBBundle(path)
	if err != nil {
		return failedImport(ReasonImportBundleInvalid, ""), err
	}
	return i.importMappedBundle(ctx, b)
}

// Operate on one validated in-memory snapshot throughout the write/check
// phases, so a caller that already holds a checked snapshot never rereads its
// input.
func (i *kbImporter) importMappedBundle(ctx context.Context, b *kbBundle) (importStatus, error) {
	log := i.log()
	for _, reason := range sortedKeys(b.Skipped) {
		log.Warn("bundelonderdeel overgeslagen", "component", "duif", "reason", reason, "aantal", b.Skipped[reason])
	}
	log.Info("kennisbank-import gestart", "component", "duif", "kb_id", b.KBID,
		"structuur", len(b.Structure), "standpunten", len(b.Destillaten), "workers", i.workers)

	marker, reason, err := i.checkMarker(ctx, b)
	switch {
	case reason != "":
		return failedImport(reason, b.KBID), err
	case marker != nil && !i.force && marker.ContentHash == b.ContentHash && marker.MapperVersion == kbMapperVersion:
		// Dezelfde bundel, dezelfde mapper, dezelfde embedder: nul schrijfacties.
		return readyStatus(*marker, true), nil
	}
	if marker != nil && !i.force {
		// Read the old evidence before structure writes replace any quote.
		// A changed bibliography does not require thousands of new vectors.
		i.reuseEmbedding, err = i.planEmbeddingReuse(ctx, b)
		if err != nil {
			return i.abort(ctx, ReasonImportUpdateCheckFailed, b.KBID), err
		}
		log.Info("kennisbank zoekindex gecontroleerd", "component", "duif", "behouden", len(i.reuseEmbedding), "nieuw", len(b.Destillaten)-len(i.reuseEmbedding))
	}
	if b.Schema == "duiven/2" {
		if _, ok := i.writer.(kbUpdateWriter); !ok {
			return failedImport(ReasonImportUpdateCheckFailed, b.KBID), nil
		}
	}
	// Never leave the old success marker behind after a partial update. A
	// restart must see an incomplete import until all writes/checks finish.
	if marker != nil && !i.force {
		if err := i.writer.Delete(ctx, marker.ID); err != nil {
			return i.abort(ctx, ReasonImportWriteFailed, b.KBID), err
		}
	}

	if i.force {
		// Vervangen in plaats van bijwerken: een upsert wist niets, dus zonder
		// dit blijven verweesde knopen van de vorige bundel staan en kunnen
		// belongs_to_*-relaties dubbel lopen (relaties naar een verweesde knoop
		// blijven gewoon staan).
		deleted, err := i.replace(ctx)
		if err != nil {
			return i.abort(ctx, ReasonImportReplaceFailed, b.KBID), err
		}
		log.Info("kennisbank vervangen", "component", "duif", "kb_id", b.KBID, "verwijderd", deleted)
	}

	if err := i.writeGraph(ctx, b); err != nil {
		return i.abort(ctx, ReasonImportWriteFailed, b.KBID), err
	}
	if b.Schema == "duiven/2" {
		if err := i.reconcileBundle(ctx, b); err != nil {
			return i.abort(ctx, ReasonImportWriteFailed, b.KBID), err
		}
	}
	if !i.countsMatch(ctx, b) {
		return failedImport(ReasonImportCountMismatch, b.KBID), nil
	}

	st, err := i.writeMarker(ctx, b)
	if err != nil {
		return i.abort(ctx, ReasonImportWriteFailed, b.KBID), err
	}
	return st, nil
}

// abort vertaalt "de context is weg" naar shutdown; alles daarbuiten houdt de
// meegegeven reden.
func (i *kbImporter) abort(ctx context.Context, reason, kbID string) importStatus {
	if ctx.Err() != nil {
		return failedImport(ReasonImportShutdown, kbID)
	}
	return failedImport(reason, kbID)
}

func failedImport(reason, kbID string) importStatus {
	return importStatus{State: ImportFailed, Reason: reason, KBID: kbID}
}

// checkMarker leest de bestaande markers. Een marker van een ándere
// kennisbank blokkeert: twee kb's in dezelfde graaf zouden elkaars status en
// tellingen overschrijven.
func (i *kbImporter) checkMarker(ctx context.Context, b *kbBundle) (*kbMarker, string, error) {
	markers, err := i.readMarkers(ctx)
	if err != nil {
		return nil, ReasonImportMarkerUnreadable, err
	}
	var current *kbMarker
	for idx := range markers {
		if markers[idx].KBID != b.KBID {
			if !i.force {
				return nil, ReasonImportKBIDMismatch, nil
			}
			continue
		}
		current = &markers[idx]
	}
	if current != nil && !i.force {
		if current.EmbeddingModel != i.embeddingModel || current.EmbeddingDimension != i.embeddingDimension {
			// De vectoren van de vorige import zijn met een andere embedder
			// gemaakt; ze mengen zou de scores onvergelijkbaar maken.
			return nil, ReasonImportEmbeddingMismatch, nil
		}
	}
	return current, "", nil
}

func (i *kbImporter) readMarkers(ctx context.Context) ([]kbMarker, error) {
	var out []kbMarker
	for offset := 0; ; offset += kbListPage {
		rows, err := i.writer.ListNodes(ctx, tableImportMarker, data.ListOpts{Limit: kbListPage, Offset: offset})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			out = append(out, markerFromProps(row))
		}
		if len(rows) < kbListPage {
			return out, nil
		}
	}
}

// replace wist de tabellen van deze module vóór een FORCE-import — álle rijen,
// ook die van een andere kb_id. De acht kennisbanktabellen plus de marker zijn
// van de duif alleen, en een standpunt van een vórige kennisbank dat blijft
// staan is met embedding en al doorzoekbaar terwijl de graaf hem als 404
// verbergt: de chat citeert dan bronnen die de kennisgraaf ontkent. Delete
// neemt de relaties van een knoop mee, dus verweesde edges blijven niet achter.
//
// Dit is de dure kant van FORCE: ± 8 900 losse Delete's, elk met een pagina die
// de embeddings meesleept. Core mist er twee poorten voor — een DeleteWhere op
// een prop-filter en een GetNodes(ids) zonder vector — en zolang die er niet
// zijn, is parallel verwijderen wat het snelst kan.
func (i *kbImporter) replace(ctx context.Context) (int, error) {
	deleted := 0
	for _, table := range kbTables() {
		for {
			rows, err := i.writer.ListNodes(ctx, table, data.ListOpts{Limit: kbListPage})
			if err != nil {
				return deleted, err
			}
			ids := make([]data.NodeID, 0, len(rows))
			for _, row := range rows {
				if id := rowID(row); id != "" {
					ids = append(ids, data.NodeID(id))
				}
			}
			if len(ids) == 0 {
				// Niets meer te wissen in deze tabel (of alleen rijen zonder
				// leesbare id, en die kan Delete toch niet aanwijzen).
				break
			}
			if err := i.parallel(ctx, len(ids), func(gctx context.Context, n int) error {
				return i.retry(gctx, func() error { return i.writer.Delete(gctx, ids[n]) })
			}); err != nil {
				return deleted, err
			}
			deleted += len(ids)
			if len(rows) < kbListPage {
				break
			}
		}
	}
	return deleted, nil
}

// writeGraph schrijft de graaf in vier fasen. De volgorde is er voor de
// chatgebruiker tijdens de import: een standpunt verschijnt pas mét zijn
// citaten, perspectief en bronpaper.
func (i *kbImporter) writeGraph(ctx context.Context, b *kbBundle) error {
	// 1. Structuur (domein, thema, subthema, perspectief, persona,
	//    bronpaper, citaat) en de relaties daartussen.
	if err := i.phase(ctx, "structuur", len(b.Structure), func(gctx context.Context, n int) error {
		return i.writeNode(gctx, b.Structure[n])
	}); err != nil {
		return err
	}
	if err := i.phase(ctx, "structuurrelaties", len(b.StructureEdges), func(gctx context.Context, n int) error {
		return i.writeEdge(gctx, b.StructureEdges[n])
	}); err != nil {
		return err
	}
	// 2. Standpunten: node en eigen relaties in dezelfde worker.
	if err := i.phase(ctx, "standpunten", len(b.Destillaten), func(gctx context.Context, n int) error {
		d := b.Destillaten[n]
		if err := i.writeNode(gctx, d.Node); err != nil {
			return err
		}
		for _, e := range d.Edges {
			if err := i.writeEdge(gctx, e); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	// 3. De onderlinge verwijzingen tussen standpunten, als alles er staat.
	return i.phase(ctx, "verwijzingen", len(b.RelatesTo), func(gctx context.Context, n int) error {
		return i.writeEdge(gctx, b.RelatesTo[n])
	})
}

// phase draait één importfase en logt hoe lang hij duurde. De standpuntenfase
// kost een embed-call per knoop en is daarmee de fase die de doorlooptijd
// bepaalt; zonder die maat is DUIF_IMPORT_WORKERS een knop zonder meter.
// Aantallen en milliseconden, nooit inhoud.
func (i *kbImporter) phase(ctx context.Context, name string, n int, do func(gctx context.Context, idx int) error) error {
	start := time.Now()
	if err := i.parallel(ctx, n, do); err != nil {
		return err
	}
	i.log().Info("kennisbank-import fase klaar", "component", "duif",
		"fase", name, "aantal", n, "ms", time.Since(start).Milliseconds())
	return nil
}

// parallel draait n taken over de errgroup met het ingestelde plafond.
func (i *kbImporter) parallel(ctx context.Context, n int, do func(ctx context.Context, idx int) error) error {
	if n == 0 {
		return ctx.Err()
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(i.workers)
	for idx := range n {
		g.Go(func() error { return do(gctx, idx) })
	}
	return g.Wait()
}

func (i *kbImporter) writeNode(ctx context.Context, n kbNode) error {
	if err := i.retry(ctx, func() error {
		if i.reuseEmbedding[n.ID] {
			props := kbCloneProps(n.Props)
			// UpdateNode merges JSON props. Explicit nulls retire old imported
			// fields (notably persona) without erasing the existing vector.
			for _, key := range kbNodeProps[n.Table] {
				if _, present := props[key]; !present {
					props[key] = nil
				}
			}
			if _, present := props["thema_titel"]; !present {
				props["thema_titel"] = nil
			}
			return i.writer.(kbUpdateWriter).UpdateNode(ctx, n.ID, props)
		}
		_, err := i.writer.CreateNodeWithID(ctx, n.ID, n.Props, n.EmbedText)
		return err
	}); err != nil {
		return err
	}
	i.countNode(n.Table)
	return nil
}

func (i *kbImporter) writeEdge(ctx context.Context, e kbEdge) error {
	return i.retry(ctx, func() error {
		return i.writer.Relate(ctx, e.From, e.To, e.Relation, e.Props)
	})
}

// countNode telt de geschreven knopen en logt de voortgang per 500 — als
// tellingen, want een voortgangsregel met inhoud zou de kennisbank lekken.
func (i *kbImporter) countNode(table string) {
	i.countMu.Lock()
	i.nodeCounts[table]++
	i.written++
	var milestone int
	if i.written >= i.nextLog {
		milestone = i.written
		i.nextLog += i.progressEvery
	}
	i.countMu.Unlock()
	if milestone > 0 {
		i.log().Info("kennisbank-import voortgang", "component", "duif", "nodes", milestone)
	}
}

func (i *kbImporter) writtenCount() int {
	i.countMu.Lock()
	defer i.countMu.Unlock()
	return i.written
}

// retry probeert een schrijfoperatie drie keer met verdubbelende backoff. De
// embed-fout van CreateNodeWithID is niet te onderscheiden van een gewone
// schrijffout, en het reële geval is een 429 van de embedprovider.
func (i *kbImporter) retry(ctx context.Context, op func() error) error {
	wait := kbWriteBackoff
	var err error
	for attempt := 1; attempt <= kbWriteAttempts; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err = op(); err == nil {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if attempt == kbWriteAttempts || !retryableWrite(err) {
			break
		}
		if !i.sleep(ctx, wait) {
			// De wachtseam stopt alleen als de context weg is; abort() maakt er
			// dan `shutdown` van in plaats van een schrijffout.
			break
		}
		wait *= 2
	}
	return err
}

// retryableWrite zegt of een schrijffout kans maakt bij een tweede poging. Het
// reële geval is een 429 of een gevallen verbinding bij de embedprovider, en
// die trekt bij. Een id dat al onder een andere tabel bestaat en een antwoord
// dat niet te ontleden is, gaan bij poging drie precies zo mis — en kosten dan
// alleen tijd en nog twee embed-calls.
func retryableWrite(err error) bool {
	if err == nil {
		return false
	}
	var syntax *json.SyntaxError
	var typ *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &typ) {
		return false
	}
	return !strings.Contains(err.Error(), "already exists")
}

// countsMatch vergelijkt wat er geschreven is met wat de bundel belooft. Een
// verschil betekent dat de graaf half gevuld is: geen marker, en de volgende
// boot doet het over.
//
// De relaties worden geteld op `import_hash`, niet op `kb_id`. Een herimport
// van een gekrompen bundel laat edges van de vórige bundel staan (upsert wist
// niets), en op kb_id geteld zou élke volgende boot op count_mismatch vallen —
// een import die zichzelf permanent blokkeert. Op de hash geteld tellen alleen
// de edges van déze bundel mee. Wat er nog van de vorige staat, verdwijnt pas
// bij een import met DUIF_IMPORT_KB_FORCE=1: is de bundel gekrompen, dan is
// FORCE de weg.
func (i *kbImporter) countsMatch(ctx context.Context, b *kbBundle) bool {
	ok := true
	i.countMu.Lock()
	written := make(map[string]int, len(i.nodeCounts))
	for k, v := range i.nodeCounts {
		written[k] = v
	}
	i.countMu.Unlock()

	for _, table := range kbTables() {
		want := b.Counts[table]
		if want == 0 && written[table] == 0 {
			continue
		}
		if written[table] != want {
			ok = false
			i.log().Error("kennisbank-import telling wijkt af", "component", "duif",
				"reason", ReasonImportCountMismatch, "tabel", table, "verwacht", want, "geschreven", written[table])
		}
	}

	// Relaties zijn alleen te tellen als de repository de optionele
	// EdgeLister-poort vervult; zonder die poort blijft de check bij de knopen.
	lister, ok2 := i.writer.(data.EdgeLister)
	if !ok2 {
		return ok
	}
	sources := b.edgeSources()
	for _, relation := range sortedKeys(kbRelations) {
		canon := kbRelations[relation]
		want := b.Counts[canon]
		got, err := i.countEdges(ctx, lister, canon, sources[canon], b.ContentHash)
		if err != nil {
			i.log().Warn("relaties niet te tellen", "component", "duif", "relatie", canon)
			return false
		}
		if got != want {
			ok = false
			i.log().Error("kennisbank-import telling wijkt af", "component", "duif",
				"reason", ReasonImportCountMismatch, "relatie", canon, "verwacht", want, "geschreven", got)
		}
	}
	return ok
}

// countEdges telt de edges van één relatie die déze import geschreven heeft.
// Nooit een ongefilterde ListEdges: die leest de hele relatietabel van een
// gedeelde graaf. De bronknopen komen uit de bundel en gaan er in blokken van
// 200 in, zodat elke query begrensd is.
func (i *kbImporter) countEdges(ctx context.Context, lister data.EdgeLister, relation string, from []data.NodeID, hash string) (int, error) {
	got := 0
	for start := 0; start < len(from); start += kbListPage {
		end := min(start+kbListPage, len(from))
		edges, err := lister.ListEdges(ctx, relation, from[start:end], nil)
		if err != nil {
			return 0, err
		}
		for _, e := range edges {
			if h, _ := e.Props["import_hash"].(string); h == hash {
				got++
			}
		}
	}
	return got, nil
}

// writeMarker sluit de import af. De marker draagt meer dan de hash: mapper-
// versie en embedder bepalen mee of een volgende boot mag overslaan.
func (i *kbImporter) writeMarker(ctx context.Context, b *kbBundle) (importStatus, error) {
	id := data.NodeID(bundleimport.DeriveID(b.KBID, tableImportMarker, b.KBID))
	importedAt := time.Now().UTC().Format(time.RFC3339)
	counts := make(map[string]any, len(b.Counts))
	for k, v := range b.Counts {
		counts[k] = v
	}
	props := map[string]any{
		"kb_id":               b.KBID,
		"bundle_schema":       b.Schema,
		"content_hash":        b.ContentHash,
		"source_language":     b.SourceLanguage,
		"languages":           kbLanguageProp(b.Languages),
		"mapper_version":      kbMapperVersion,
		"embedding_model":     i.embeddingModel,
		"embedding_dimension": i.embeddingDimension,
		"imported_at":         importedAt,
		"counts":              counts,
	}
	if err := i.retry(ctx, func() error {
		_, err := i.writer.CreateNodeWithID(ctx, id, props, "")
		return err
	}); err != nil {
		return importStatus{}, err
	}
	return readyStatus(kbMarker{
		KBID:           b.KBID,
		BundleSchema:   b.Schema,
		Languages:      b.Languages,
		SourceLanguage: b.SourceLanguage,
		ImportedAt:     importedAt,
		Counts:         b.Counts,
	}, false), nil
}
