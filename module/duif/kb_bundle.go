package duif

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/parai-nl/parai-core/data"
	"github.com/parai-nl/parai-interaction/modules/interaction/bundleimport"
)

// kbMapperVersion is de versie van déze bundellezer. Hij staat in de
// importmarker: verandert de mapping (andere props, andere relatiecanon), dan
// bump je hem en importeert de volgende boot opnieuw, ook bij een gelijke
// content-hash.
//
// 3: duiven/3 slaat de vertaalde velden plat naar een zijspoorprop (`titel_en`)
// en embedt standpunten op de Engelse samenvatting.
const kbMapperVersion = 3

// kbBundleSchema is het bundelformaat voor de bestaande ZIP-route. De
// check is hard: een ander schema betekent andere veldnamen, en die stil
// negeren levert een halfvolle graaf op.
const kbBundleSchema = "mosaic/1"

// Limieten op de bundel. Een zip is invoer van buiten: de zip-index is een
// claim (UncompressedSize64 liegt bij een zipbom), dus elke lezing loopt door
// een io.LimitReader en niets wordt naar schijf uitgepakt.
const (
	maxBundleFileBytes = 128 << 20 // de zip zelf
	// maxBundleEntries begrenst het aantal entries. Een geldige bundel kan naast
	// de twee JSON-bestanden ook afbeeldingen bevatten; die lezen we niet (we
	// pakken niets uit en lezen alleen de twee JSON's), dus een krappe grens
	// beschermt niets. 64 laat ze door en houdt een zip met duizenden entries
	// buiten de deur.
	maxBundleEntries       = 64
	maxKnowledgeBaseBytes  = 64 << 20 // knowledge_base.json (≈ 11 MB in de proef-KB)
	maxProjectDetailsBytes = 64 << 10 // project_details.json ({"name": …})
)

// Reden-codes van de zip-vormcontrole. Ze halen de wire niet — een afgekeurde
// bundel is bundle_invalid — maar staan in de fouttekst die bij een mislukte
// import op Debug landt, zodat "welke vorm klopte er niet" een stabiel woord
// heeft in plaats van een zin die morgen anders luidt.
const (
	reasonBundleTooManyEntries = "bundle_too_many_entries"
	reasonBundleUnsafeEntry    = "bundle_unsafe_entry"
)

// Bestandsnamen in de bundel (op basename, zoals ParseBundle ze zoekt).
const (
	kbFileKnowledgeBase  = "knowledge_base.json"
	kbFileProjectDetails = "project_details.json"
)

// kbNodeProps is per tabel de allowlist van props die meegaan. Alleen wat
// gelezen wordt: `storage_key` hoort hier bewust wél bij (de graaf-endpoints
// strippen hem op de wire), `content_hash` en `embedding` niet.
var kbNodeProps = map[string][]string{
	tableDestillaat:  {"samenvatting", "primary_themes", "secondary_themes", "domain", "scope", "abstraction_level", "persona", "ext_id"},
	tableQuote:       {"text", "speaker", "quote_index", "grounded", "ext_id", "bron_id", "bron_key", "destillaat_id"},
	tableInterview:   {"title", "storage_key", "interview_id", "ext_id", "key", "citatie", "doi", "categorie", "citatie_confidence", "citatie_ontbreekt", "citatie_bron", "type", "source_kind"},
	tableThema:       {"titel", "beschrijving", "kernconcepten", "taxonomy_level", "ext_id"},
	tableSubthema:    {"titel", "beschrijving", "kernconcepten", "taxonomy_level", "destillaat_count", "ext_id"},
	tableDomein:      {"titel", "beschrijving", "kernconcepten", "taxonomy_level", "ext_id"},
	tablePerspective: {"naam", "kernstandpunt", "beschrijving", "ext_id"},
	tablePersona:     {"naam", "voornaam", "kernprofiel", "samenvatting", "age", "ext_id"},
}

// kbRelations vertaalt de relatietypen van de bundel naar core's canon. De
// bundel spelt twee ervan met -PERSPECTIEF; core leest -perspective. Een type
// dat hier niet staat wordt overgeslagen (reden-code), nooit geraden.
var kbRelations = map[string]string{
	"CONTAINS":               data.RelContains,
	"HAS_QUOTE":              data.RelHasQuote,
	"BELONGS_TO":             relBelongsTo,
	"BELONGS_TO_SUBTHEMA":    relBelongsToSubthema,
	"BELONGS_TO_PERSPECTIEF": data.RelBelongsToPerspectief,
	"CONTAINS_THEMA":         relContainsThema,
	"CONTAINS_SUBTHEMA":      relContainsSubthema,
	"CONTAINS_PERSPECTIEF":   data.RelContainsPerspectief,
	"HAS_PERSPECTIVE":        data.RelHasPerspective,
	"RELATES_TO":             data.RelRelatesTo,
}

// kbIDPattern begrenst de kb-id: hij zit in elke node-id (via DeriveID), in
// elke prop en straks in de allowlist van de graaf-endpoints.
var kbIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// mosaicBundle is de eigen lezer van knowledge_base.json, met precies de
// labels en props die deze module leest.
type mosaicBundle struct {
	Schema string `json:"bundle_schema"`
	// Languages en SourceLanguage bestaan alleen in duiven/3. Bij elk ander
	// schema horen ze afwezig te zijn; validateKBLanguages weigert ze daar.
	Languages      []string                    `json:"languages"`
	SourceLanguage string                      `json:"source_language"`
	ExportDate     string                      `json:"export_date"`
	Nodes          map[string][]map[string]any `json:"nodes"`
	Relationships  map[string][]mosaicRel      `json:"relationships"`
}

// mosaicRel is één relatie; de bundel nest de volledige eindknopen, waarvan
// alleen de graaf-id ons interesseert.
type mosaicRel struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	StartNode  mosaicRelNode  `json:"start_node"`
	EndNode    mosaicRelNode  `json:"end_node"`
}

type mosaicRelNode struct {
	Labels     []string `json:"labels"`
	Properties struct {
		ID string `json:"id"`
	} `json:"properties"`
}

// kbNode is één te schrijven knoop: id, props en de tekst waarop core
// server-side embedt (leeg = geen embedding).
type kbNode struct {
	ID        data.NodeID
	Table     string
	Props     map[string]any
	EmbedText string
}

// kbEdge is één te schrijven relatie.
type kbEdge struct {
	From     data.NodeID
	To       data.NodeID
	Relation string
	Props    map[string]any
}

// kbDestillaat koppelt een standpunt aan zijn eigen relaties, zodat de
// importer node en relaties in dezelfde worker kan schrijven: een chat die
// tijdens de import zoekt vindt nooit een standpunt zonder citaat of bron.
type kbDestillaat struct {
	Node  kbNode
	Edges []kbEdge
}

// kbBundle is de gemapte bundel, klaar om te schrijven. De volgorde van de
// velden is de volgorde van de importfasen.
type kbBundle struct {
	Schema string
	// Languages en SourceLanguage komen uit de bundelkop en gaan mee naar de
	// importmarker, zodat vóór en ná een herimport vastligt welk formaat en
	// welke talen er binnengekomen zijn.
	Languages      []string
	SourceLanguage string
	KBID           string
	ContentHash    string

	Structure      []kbNode
	StructureEdges []kbEdge
	Destillaten    []kbDestillaat
	RelatesTo      []kbEdge

	// Counts telt per tabel en per relatie wat de bundel belooft; de importer
	// vergelijkt zijn eigen tellingen ermee vóór hij de marker schrijft.
	Counts map[string]int
	// Skipped telt per reden-code wat de mapping heeft laten liggen. Alleen
	// codes en aantallen — nooit een id of een stuk tekst.
	Skipped map[string]int
}

// edgeSources verzamelt per relatie de knopen waar deze bundel edges vandaan
// laat lopen, ontdubbeld. De telling na de import leest de relaties daarmee
// gericht terug: ListEdges krijgt altijd een begrensde bronlijst mee, nooit de
// hele relatietabel van een graaf die met andere modules gedeeld wordt.
func (b *kbBundle) edgeSources() map[string][]data.NodeID {
	out := map[string][]data.NodeID{}
	seen := map[string]map[data.NodeID]bool{}
	add := func(e kbEdge) {
		s := seen[e.Relation]
		if s == nil {
			s = map[data.NodeID]bool{}
			seen[e.Relation] = s
		}
		if s[e.From] {
			return
		}
		s[e.From] = true
		out[e.Relation] = append(out[e.Relation], e.From)
	}
	for _, e := range b.StructureEdges {
		add(e)
	}
	for _, d := range b.Destillaten {
		for _, e := range d.Edges {
			add(e)
		}
	}
	for _, e := range b.RelatesTo {
		add(e)
	}
	return out
}

// Reden-codes van de mapping (log-only; ze halen de wire niet).
const (
	ReasonKBUnknownLabel        = "unknown_label"
	ReasonKBUnknownRelation     = "unknown_relation"
	ReasonKBNodeWithoutID       = "node_without_id"
	ReasonKBDuplicateNodeID     = "duplicate_node_id"
	ReasonKBEdgeEndpointMissing = "edge_endpoint_missing"
)

// loadKBBundle leest de bundelzip van schijf en levert de gemapte graaf. De
// volgorde is de veiligheidsvolgorde: eerst de maat van het bestand, dan de
// zip-index, dan begrensde lezingen, dan pas parsen.
func loadKBBundle(path string) (*kbBundle, error) {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return loadKBV2JSON(path)
	}
	raw, kbID, hash, err := readBundleFiles(path)
	if err != nil {
		return nil, err
	}
	var mb mosaicBundle
	if err := json.Unmarshal(raw, &mb); err != nil {
		return nil, fmt.Errorf("duif: %s ontleden: %w", kbFileKnowledgeBase, err)
	}
	if mb.Schema != kbBundleSchema {
		return nil, fmt.Errorf("duif: bundle_schema %q wordt niet ondersteund (verwacht %q)", mb.Schema, kbBundleSchema)
	}
	return mapMosaic(kbID, hash, &mb), nil
}

// readBundleFiles levert de rauwe bytes van knowledge_base.json, de kb-id en
// de content-hash. Niets wordt uitgepakt naar schijf.
func readBundleFiles(zipPath string) (kbJSON []byte, kbID, contentHash string, err error) {
	info, err := os.Stat(zipPath)
	if err != nil {
		return nil, "", "", fmt.Errorf("duif: bundel openen: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, "", "", fmt.Errorf("duif: bundel %s is geen gewoon bestand", filepath.Base(zipPath))
	}
	if info.Size() > maxBundleFileBytes {
		return nil, "", "", fmt.Errorf("duif: bundel is %d bytes, meer dan het maximum van %d", info.Size(), maxBundleFileBytes)
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, "", "", fmt.Errorf("duif: bundel lezen: %w", err)
	}
	defer zr.Close()
	if len(zr.File) > maxBundleEntries {
		return nil, "", "", fmt.Errorf("duif: %s: bundel heeft %d entries, meer dan het maximum van %d", reasonBundleTooManyEntries, len(zr.File), maxBundleEntries)
	}

	var kbEntry, pdEntry *zip.File
	for _, f := range zr.File {
		if unsafeZipEntryName(f.Name) {
			return nil, "", "", fmt.Errorf("duif: %s: bundel bevat een entry-naam die buiten de bundel wijst", reasonBundleUnsafeEntry)
		}
		switch path.Base(f.Name) {
		case kbFileKnowledgeBase:
			kbEntry = f
		case kbFileProjectDetails:
			pdEntry = f
		}
	}
	if kbEntry == nil {
		return nil, "", "", fmt.Errorf("duif: bundel mist %s", kbFileKnowledgeBase)
	}

	// De hash loopt mee met de lezing: hij dekt de rauwe bytes zoals ze in de
	// zip staan, niet de geparste vorm. Zo herkent de marker "dezelfde bundel"
	// ook als de lezer verandert.
	sum := sha256.New()
	kbJSON, err = readZipEntry(kbEntry, maxKnowledgeBaseBytes, sum)
	if err != nil {
		return nil, "", "", err
	}
	contentHash = hex.EncodeToString(sum.Sum(nil))

	var pdJSON []byte
	if pdEntry != nil {
		if pdJSON, err = readZipEntry(pdEntry, maxProjectDetailsBytes, nil); err != nil {
			return nil, "", "", err
		}
	}

	// bundleimport.ParseBundle is de poortwachter op de zip-layout: hij zegt of
	// dit een bundel van de verwachte vorm is. Hij krijgt een in-memory zip met
	// uitsluitend de twee JSON-entries, die we hierboven zelf begrensd hebben.
	// De veldmapping doen we zelf.
	gate, err := zipOf(map[string][]byte{kbFileKnowledgeBase: kbJSON, kbFileProjectDetails: pdJSON})
	if err != nil {
		return nil, "", "", err
	}
	if _, err := bundleimport.ParseBundle(gate); err != nil {
		return nil, "", "", fmt.Errorf("duif: bundel-layout afgekeurd: %w", err)
	}

	// De projectnaam lezen we uit onze eigen (begrensde) bytes. De poortwachter
	// weet hem ook, maar hem daarvoor gebruiken zou betekenen dat de 11 MB
	// knowledge_base.json een tweede keer ontleed moet worden voor één string.
	var pd struct {
		Name string `json:"name"`
	}
	if len(pdJSON) > 0 {
		if err := json.Unmarshal(pdJSON, &pd); err != nil {
			return nil, "", "", fmt.Errorf("duif: %s ontleden: %w", kbFileProjectDetails, err)
		}
	}
	kbID = strings.TrimSpace(pd.Name)
	if kbID == "" {
		// Fallback: de bestandsnaam zonder extensie. Bewust geen pad — de
		// kb-id staat straks in elke node-prop en in elke afgeleide id.
		base := filepath.Base(zipPath)
		kbID = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if !kbIDPattern.MatchString(kbID) {
		return nil, "", "", fmt.Errorf("duif: kb-id voldoet niet aan %s", kbIDPattern.String())
	}
	return kbJSON, kbID, contentHash, nil
}

// unsafeZipEntryName herkent een entry-naam die buiten de bundel wijst:
// naamloos, absoluut, met een backslash of met een stap omhoog. Er wordt niets
// uitgepakt, dus dit beschermt geen schijf — het zegt iets over de bundel zelf,
// en een bundel met zulke namen is niet de bundel die we verwachten.
func unsafeZipEntryName(name string) bool {
	return name == "" || path.IsAbs(name) || strings.Contains(name, `\`) ||
		slices.Contains(strings.Split(name, "/"), "..")
}

// readZipEntry leest één entry met een harde bovengrens. De +1 is de truc:
// leest hij er één te veel, dan loog de zip-index over de maat.
func readZipEntry(f *zip.File, limit int64, tee io.Writer) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("duif: %s openen: %w", path.Base(f.Name), err)
	}
	defer rc.Close()
	var r io.Reader = io.LimitReader(rc, limit+1)
	if tee != nil {
		r = io.TeeReader(r, tee)
	}
	buf, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("duif: %s lezen: %w", path.Base(f.Name), err)
	}
	if int64(len(buf)) > limit {
		return nil, fmt.Errorf("duif: %s is groter dan het maximum van %d bytes", path.Base(f.Name), limit)
	}
	return buf, nil
}

// zipOf bouwt een zip in het geheugen met alleen de meegegeven bestanden.
func zipOf(files map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range sortedKeys(files) {
		body := files[name]
		if body == nil {
			continue
		}
		w, err := zw.Create(name)
		if err != nil {
			return nil, fmt.Errorf("duif: bundel herverpakken: %w", err)
		}
		if _, err := w.Write(body); err != nil {
			return nil, fmt.Errorf("duif: bundel herverpakken: %w", err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("duif: bundel herverpakken: %w", err)
	}
	return buf.Bytes(), nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// kbCloneProps kopieert een propmap ondiep; nil wordt een lege map, zodat er
// nooit een nil-props de schrijfseam in gaat.
func kbCloneProps(p map[string]any) map[string]any {
	out := make(map[string]any, len(p)+2)
	for k, v := range p {
		out[k] = v
	}
	return out
}

// mosaicIndexEntry koppelt een export-id aan de afgeleide node-id en de rauwe
// props, zodat de tweede pas (thema-titel, citaten) niets hoeft te zoeken.
type mosaicIndexEntry struct {
	table string
	id    data.NodeID
	raw   map[string]any
}

// mapMosaic vertaalt de bundel naar knopen en relaties. Deterministisch: de
// labels en relatietypen lopen op alfabetische volgorde, de knopen binnen een
// label in bundelvolgorde.
func mapMosaic(kbID, hash string, mb *mosaicBundle) *kbBundle {
	out := &kbBundle{
		Schema:         mb.Schema,
		Languages:      mb.Languages,
		SourceLanguage: mb.SourceLanguage,
		KBID:           kbID,
		ContentHash:    hash,
		Counts:         map[string]int{},
		Skipped:        map[string]int{},
	}
	index := map[string]mosaicIndexEntry{}

	// Pas 1 — elke knoop een stabiele id. DeriveID zet de tabel in de sleutel:
	// hetzelfde standpunt levert bij elke import dezelfde id, en dat is wat de
	// upsert idempotent maakt.
	labels := sortedKeys(mb.Nodes)
	for _, label := range labels {
		table, ok := kbLabelTables[label]
		if !ok {
			out.Skipped[ReasonKBUnknownLabel] += len(mb.Nodes[label])
			continue
		}
		for _, raw := range mb.Nodes[label] {
			exportID, _ := raw["id"].(string)
			if exportID == "" {
				out.Skipped[ReasonKBNodeWithoutID]++
				continue
			}
			if _, dup := index[exportID]; dup {
				out.Skipped[ReasonKBDuplicateNodeID]++
				continue
			}
			index[exportID] = mosaicIndexEntry{
				table: table,
				id:    data.NodeID(bundleimport.DeriveID(kbID, table, exportID)),
				raw:   raw,
			}
		}
	}

	// Pas 2 — relaties. Onderweg vangen we op wat de standpunt-props nodig
	// hebben: de thema-titel (gedenormaliseerd, zodat de turn er geen extra
	// query voor hoeft) en de citaten (voor de embed-tekst).
	themaOf := map[string]string{}
	quotesOf := map[string][]string{}
	edgesOf := map[data.NodeID][]kbEdge{}

	for _, relType := range sortedKeys(mb.Relationships) {
		for _, rel := range mb.Relationships[relType] {
			typ := rel.Type
			if typ == "" {
				typ = relType
			}
			relation, ok := kbRelations[strings.ToUpper(typ)]
			if !ok {
				out.Skipped[ReasonKBUnknownRelation]++
				continue
			}
			from, okFrom := index[rel.StartNode.Properties.ID]
			to, okTo := index[rel.EndNode.Properties.ID]
			if !okFrom || !okTo {
				out.Skipped[ReasonKBEdgeEndpointMissing]++
				continue
			}
			switch {
			case relation == relBelongsTo && to.table == tableThema:
				themaOf[rel.StartNode.Properties.ID] = rel.EndNode.Properties.ID
			case relation == data.RelHasQuote:
				quotesOf[rel.StartNode.Properties.ID] = append(quotesOf[rel.StartNode.Properties.ID], rel.EndNode.Properties.ID)
			}

			props := kbCloneProps(rel.Properties)
			props["kb_id"] = kbID
			props["import_hash"] = hash
			edge := kbEdge{From: from.id, To: to.id, Relation: relation, Props: props}
			out.Counts[relation]++

			switch {
			case relation == data.RelRelatesTo:
				out.RelatesTo = append(out.RelatesTo, edge)
			case from.table == tableDestillaat:
				edgesOf[from.id] = append(edgesOf[from.id], edge)
			case to.table == tableDestillaat:
				edgesOf[to.id] = append(edgesOf[to.id], edge)
			default:
				out.StructureEdges = append(out.StructureEdges, edge)
			}
		}
	}

	// Pas 3 — de props. Standpunten gaan apart: zij dragen de embedding en
	// worden samen met hun eigen relaties geschreven. `done` houdt de dubbele
	// export-ids buiten: de index hield de eerste, de tweede is in pas 1 al
	// als overgeslagen geteld.
	done := map[string]bool{}
	for _, label := range labels {
		table, ok := kbLabelTables[label]
		if !ok {
			continue
		}
		for _, raw := range mb.Nodes[label] {
			exportID, _ := raw["id"].(string)
			entry, ok := index[exportID]
			if !ok || done[exportID] {
				continue
			}
			done[exportID] = true
			node := kbNode{ID: entry.id, Table: table, Props: nodeProps(mb.Schema, table, raw, kbID, hash)}
			if mb.Schema == kbV2Schema || mb.Schema == kbV3Schema {
				node.Props["bundle_schema"] = mb.Schema
				if mb.SourceLanguage != "" {
					node.Props["source_language"] = mb.SourceLanguage
				}
			} else if table == tableQuote {
				// Legacy exports use relationships alone for these links. A v2
				// bundle preserves them only after its strict ancestry validation.
				delete(node.Props, "bron_id")
				delete(node.Props, "bron_key")
				delete(node.Props, "destillaat_id")
			}
			out.Counts[table]++
			if table != tableDestillaat {
				out.Structure = append(out.Structure, node)
				continue
			}
			// Het gedenormaliseerde thema draagt dezelfde twee talen als het
			// thema zelf: de bronkaart en het leespaneel tonen het, dus een
			// Engelse lezer hoort er geen Nederlandse themanaam te zien.
			if titel, english := bundleThemeTitle(mb.Schema, index, themaOf[exportID]); titel != "" {
				node.Props["thema_titel"] = titel
				if english != "" {
					node.Props[kbLocalizedProp("thema_titel")] = english
				}
			}
			node.EmbedText = destillaatEmbedText(mb.Schema, index, raw, quotesOf[exportID])
			out.Destillaten = append(out.Destillaten, kbDestillaat{Node: node, Edges: edgesOf[entry.id]})
		}
	}
	return out
}

// nodeProps filtert de rauwe knoop op de allowlist en zet er de twee
// herkomstvelden bij die élke geïmporteerde rij draagt.
func nodeProps(schema, table string, raw map[string]any, kbID, hash string) map[string]any {
	props := make(map[string]any, len(kbNodeProps[table])+3)
	for _, key := range kbNodeProps[table] {
		v, ok := raw[key]
		if !ok || v == nil {
			continue
		}
		// Een vertaald veld wordt platgeslagen: de brontaal houdt de bestaande
		// kolom, het Engels krijgt de zijspoorprop ernaast. Een object in de
		// bestaande kolom zou core's getypeerde reads, inspectiequery's op de opslag
		// en het graaflabel in één klap breken.
		if kind, localized := kbLocalizedKind(key); localized && schema == kbV3Schema {
			source, english, valid := kbLocalizedValue(kind, schema, v)
			if !valid {
				continue
			}
			props[key] = source
			props[kbLocalizedProp(key)] = english
			continue
		}
		props[key] = v
	}
	if table == tablePersona {
		// Persona-knopen uit de bundel zijn metadata, geen duiven:
		// published=false houdt ze uit de personalijst van core, en zonder
		// embedding blijven ze uit het semantisch zoeken.
		props["published"] = false
	}
	props["kb_id"] = kbID
	props["import_hash"] = hash
	return props
}

// bundleThemeTitle leest de themanaam uit de bundelindex — de tegenhanger van
// themeTitle in de gate, die hem uit de rauwe graafprops leest. Twee bronnen,
// twee namen: verwarren betekent de titel van het verkeerde moment tonen.
func bundleThemeTitle(schema string, index map[string]mosaicIndexEntry, exportID string) (source, english string) {
	// De index is per definitie gevuld: themaOf krijgt alleen sleutels van
	// relaties waarvan beide eindknopen opgelost zijn. Een lege string is dus
	// "geen BELONGS_TO", niet "onbekend thema".
	raw := index[exportID].raw
	if raw == nil {
		return "", ""
	}
	if schema == kbV3Schema {
		source, english, _ = kbLocalizedText(schema, raw["titel"])
		return source, english
	}
	titel, _ := raw["titel"].(string)
	return titel, ""
}

// kbLocalizedValue slaat één vertaald veld plat op zijn twee talen, ongeacht of
// het een tekst of een lijst is.
func kbLocalizedValue(kind, schema string, value any) (source, english any, ok bool) {
	if kind == "localized-list" {
		src, en, valid := kbLocalizedList(schema, value)
		return src, en, valid
	}
	src, en, valid := kbLocalizedText(schema, value)
	return src, en, valid
}

// kbEmbedSummary is de samenvatting waarop geëmbed wordt: in duiven/3 de
// Engelse, want de citaten die ernaast in de embed-tekst staan zijn al Engels.
// In duiven/2 is er maar één taal en is dat de Nederlandse.
func kbEmbedSummary(schema string, raw map[string]any) string {
	if schema == kbV3Schema {
		_, english, ok := kbLocalizedText(schema, raw["samenvatting"])
		if ok {
			return english
		}
		return ""
	}
	samenvatting, _ := raw["samenvatting"].(string)
	return samenvatting
}

// destillaatEmbedText is de tekst waarop core server-side embedt: de
// samenvatting plus het eerste gegronde citaat. De samenvatting is abstract,
// het citaat draagt de specifieke bewoording — samen vindt een vraag het
// standpunt ook als ze de woorden van de spreker gebruikt.
func destillaatEmbedText(schema string, index map[string]mosaicIndexEntry, raw map[string]any, quoteIDs []string) string {
	samenvatting := kbEmbedSummary(schema, raw)
	var first, grounded string
	for _, qid := range quoteIDs {
		text, _ := index[qid].raw["text"].(string)
		if text == "" {
			continue
		}
		if first == "" {
			first = text
		}
		if g, _ := index[qid].raw["grounded"].(bool); g && grounded == "" {
			grounded = text
		}
	}
	quote := grounded
	if quote == "" {
		quote = first
	}
	if quote == "" {
		return samenvatting
	}
	if samenvatting == "" {
		return quote
	}
	return samenvatting + "\n\n" + quote
}
