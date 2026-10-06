package duif

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parai-nl/parai-core/data"
)

// findNode zoekt een knoop in de structuurlijst.
func findNode(t *testing.T, nodes []kbNode, id data.NodeID) kbNode {
	t.Helper()
	for _, n := range nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("knoop %s ontbreekt", id)
	return kbNode{}
}

func findDestillaat(t *testing.T, b *kbBundle, id data.NodeID) kbDestillaat {
	t.Helper()
	for _, d := range b.Destillaten {
		if d.Node.ID == id {
			return d
		}
	}
	t.Fatalf("standpunt %s ontbreekt", id)
	return kbDestillaat{}
}

func TestLoadKBBundle_MapsEveryLabelToItsCoreTable(t *testing.T) {
	path := newMosaicFixture().writeZip(t, "kb.zip")
	b, err := loadKBBundle(path)
	if err != nil {
		t.Fatalf("loadKBBundle: %v", err)
	}
	if b.KBID != "duiven-proef" {
		t.Errorf("kb-id = %q, wil de naam uit project_details.json", b.KBID)
	}
	if len(b.ContentHash) != 64 {
		t.Errorf("content-hash %q is geen sha256-hex", b.ContentHash)
	}

	want := map[string]int{
		tableDestillaat: 2, tableQuote: 3, tableInterview: 1,
		tableThema: 1, tableSubthema: 1, tableDomein: 1,
		tablePerspective: 2, tablePersona: 1,
	}
	for table, n := range want {
		if b.Counts[table] != n {
			t.Errorf("counts[%s] = %d, wil %d", table, b.Counts[table], n)
		}
	}
	// Het perspectief hoort in core's tabel `perspective`, niet `perspectief`:
	// GetPerspectief/GetWithContext lezen die naam, en DeriveID zet hem vast.
	if _, ok := b.Counts["perspectief"]; ok {
		t.Error("de bundel mag geen tabel `perspectief` opleveren")
	}
	if len(b.Destillaten) != 2 {
		t.Fatalf("%d standpunten, wil 2", len(b.Destillaten))
	}
	if len(b.Skipped) != 0 {
		t.Errorf("niets hoorde overgeslagen te worden: %v", b.Skipped)
	}
}

func TestLoadKBBundle_NodePropsFollowCoreTypedReads(t *testing.T) {
	path := newMosaicFixture().writeZip(t, "kb.zip")
	b, err := loadKBBundle(path)
	if err != nil {
		t.Fatal(err)
	}
	kb := b.KBID

	d := findDestillaat(t, b, derived(kb, tableDestillaat, "des-1"))
	for key, want := range map[string]any{
		"samenvatting": "Duiven voeren vergroot de overlast.",
		"domain":       "Stad",
		"scope":        "lokaal",
		"persona":      "Ria",
		"ext_id":       "DS1",
		// Gedenormaliseerd uit BELONGS_TO: de turn hoeft er geen extra query
		// voor te doen (data.Destillaat laat extra props vallen).
		"thema_titel": "Overlast",
		"kb_id":       kb,
		"import_hash": b.ContentHash,
	} {
		if got := d.Node.Props[key]; got != want {
			t.Errorf("standpunt-prop %s = %v, wil %v", key, got, want)
		}
	}

	persona := findNode(t, b.Structure, derived(kb, tablePersona, "pers-1"))
	if persona.Props["published"] != false {
		t.Error("een persona-knoop uit de bundel hoort published:false te zijn (blijft uit de personalijst)")
	}
	if persona.EmbedText != "" {
		t.Error("alleen standpunten krijgen een embed-tekst")
	}

	interview := findNode(t, b.Structure, derived(kb, tableInterview, "int-1"))
	if interview.Props["title"] != "lewald1987" || interview.Props["storage_key"] != "kb/lewald1987.pdf" {
		t.Errorf("bronpaper-props onvolledig: %v", interview.Props)
	}
	if _, ok := interview.Props["content_hash"]; ok {
		t.Error("content_hash hoort niet in de graaf")
	}

	quote := findNode(t, b.Structure, derived(kb, tableQuote, "quo-2"))
	if quote.Props["grounded"] != true || quote.Props["text"] != "Bij de bakker liggen broodkorsten." {
		t.Errorf("citaat-props onvolledig: %v", quote.Props)
	}
	if _, ok := quote.Props["destillaat_id"]; ok {
		t.Error("destillaat_id hoort niet mee: de relatie draagt die koppeling")
	}
}

func TestLoadKBBundle_EmbedTextPrefersTheGroundedQuote(t *testing.T) {
	f := newMosaicFixture()
	path := f.writeZip(t, "kb.zip")
	b, err := loadKBBundle(path)
	if err != nil {
		t.Fatal(err)
	}
	d1 := findDestillaat(t, b, derived(b.KBID, tableDestillaat, "des-1"))
	want := "Duiven voeren vergroot de overlast.\n\nBij de bakker liggen broodkorsten."
	if d1.Node.EmbedText != want {
		t.Errorf("embed-tekst = %q, wil samenvatting + het gegronde citaat", d1.Node.EmbedText)
	}
	// des-2 heeft alleen een niet-gegrond citaat: dan telt het eerste citaat.
	d2 := findDestillaat(t, b, derived(b.KBID, tableDestillaat, "des-2"))
	if d2.Node.EmbedText != "Duiven horen bij de stad.\n\nZe horen erbij." {
		t.Errorf("embed-tekst zonder gegrond citaat = %q", d2.Node.EmbedText)
	}
}

func TestLoadKBBundle_EmbedTextWithoutQuotesIsTheSummary(t *testing.T) {
	f := newMosaicFixture()
	f.Rels["HAS_QUOTE"] = nil
	path := f.writeZip(t, "kb.zip")
	b, err := loadKBBundle(path)
	if err != nil {
		t.Fatal(err)
	}
	d := findDestillaat(t, b, derived(b.KBID, tableDestillaat, "des-1"))
	if d.Node.EmbedText != "Duiven voeren vergroot de overlast." {
		t.Errorf("zonder citaat = alleen de samenvatting, kreeg %q", d.Node.EmbedText)
	}
}

func TestLoadKBBundle_CanonicalisesRelationsAndCarriesProps(t *testing.T) {
	path := newMosaicFixture().writeZip(t, "kb.zip")
	b, err := loadKBBundle(path)
	if err != nil {
		t.Fatal(err)
	}
	// De bundel spelt -PERSPECTIEF, core leest -perspective.
	for relation, want := range map[string]int{
		data.RelBelongsToPerspectief: 2,
		data.RelContainsPerspectief:  2,
		data.RelHasPerspective:       2,
		data.RelContains:             2,
		data.RelHasQuote:             3,
		data.RelRelatesTo:            1,
		"belongs_to":                 2,
		"belongs_to_subthema":        2,
		"contains_thema":             1,
		"contains_subthema":          1,
	} {
		if b.Counts[relation] != want {
			t.Errorf("counts[%s] = %d, wil %d", relation, b.Counts[relation], want)
		}
	}
	if b.Counts["BELONGS_TO_PERSPECTIEF"] != 0 {
		t.Error("de ruwe bundelnaam mag niet in de tellingen belanden")
	}

	d := findDestillaat(t, b, derived(b.KBID, tableDestillaat, "des-1"))
	var seen []string
	for _, e := range d.Edges {
		seen = append(seen, e.Relation)
		if e.Props["kb_id"] != b.KBID || e.Props["import_hash"] != b.ContentHash {
			t.Errorf("relatie %s mist kb_id/import_hash: %v", e.Relation, e.Props)
		}
		if e.Relation == data.RelBelongsToPerspectief && e.Props["uitleg"] != "meet en telt" {
			t.Errorf("relatieprops gaan niet mee: %v", e.Props)
		}
	}
	// Het standpunt draagt zijn eigen relaties, inclusief de inkomende
	// contains van het bronpaper — die worden in dezelfde worker geschreven.
	for _, want := range []string{data.RelContains, data.RelHasQuote, "belongs_to", "belongs_to_subthema", data.RelBelongsToPerspectief} {
		if !contains(seen, want) {
			t.Errorf("relatie %s hoort bij het standpunt, kreeg %v", want, seen)
		}
	}
	if len(b.RelatesTo) != 1 {
		t.Errorf("relates_to hoort in de eigen fase te staan: %d", len(b.RelatesTo))
	}
	for _, e := range b.StructureEdges {
		if tableOf(string(e.From)) == tableDestillaat || tableOf(string(e.To)) == tableDestillaat {
			t.Errorf("structuurrelatie %s raakt een standpunt", e.Relation)
		}
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func TestLoadKBBundle_SkipsUnknownLabelsAndRelations(t *testing.T) {
	f := newMosaicFixture()
	f.Nodes["Sausage"] = []map[string]any{{"id": "s-1"}}
	f.Nodes["Thema"] = append(f.Nodes["Thema"], map[string]any{"ext_id": "T2", "titel": "Zonder id"})
	f.Nodes["Domein"] = append(f.Nodes["Domein"], map[string]any{"id": "dom-1", "titel": "Dubbel"})
	f.Rels["FLIES_WITH"] = []map[string]any{rel("FLIES_WITH", "des-1", "des-2", nil)}
	f.Rels["BELONGS_TO"] = append(f.Rels["BELONGS_TO"], rel("BELONGS_TO", "des-1", "the-99", nil))

	b, err := loadKBBundle(f.writeZip(t, "kb.zip"))
	if err != nil {
		t.Fatal(err)
	}
	for reason, want := range map[string]int{
		ReasonKBUnknownLabel:        1,
		ReasonKBNodeWithoutID:       1,
		ReasonKBDuplicateNodeID:     1,
		ReasonKBUnknownRelation:     1,
		ReasonKBEdgeEndpointMissing: 1,
	} {
		if b.Skipped[reason] != want {
			t.Errorf("skipped[%s] = %d, wil %d (%v)", reason, b.Skipped[reason], want, b.Skipped)
		}
	}
	// De dubbele knoop telt één keer mee, niet twee.
	if b.Counts[tableDomein] != 1 {
		t.Errorf("counts[domein] = %d, wil 1", b.Counts[tableDomein])
	}
}

func TestLoadKBBundle_KBIDFallsBackToTheFilename(t *testing.T) {
	f := newMosaicFixture()
	f.NoProjectDetails = true
	b, err := loadKBBundle(f.writeZip(t, "duif-kb_2026.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if b.KBID != "duif-kb_2026" {
		t.Errorf("kb-id = %q, wil de bestandsnaam zonder extensie", b.KBID)
	}
}

func TestLoadKBBundle_RejectsAnUnusableKBID(t *testing.T) {
	f := newMosaicFixture()
	f.Project = "duiven proef!"
	if _, err := loadKBBundle(f.writeZip(t, "kb.zip")); err == nil {
		t.Fatal("een kb-id met spaties en leestekens hoort geweigerd te worden")
	}
}

func TestLoadKBBundle_RejectsAnotherSchema(t *testing.T) {
	f := newMosaicFixture()
	f.Schema = "mosaic/2"
	_, err := loadKBBundle(f.writeZip(t, "kb.zip"))
	if err == nil || !strings.Contains(err.Error(), "bundle_schema") {
		t.Fatalf("een ander schema hoort hard te falen, kreeg %v", err)
	}
}

func TestLoadKBBundle_RejectsUnsafeZips(t *testing.T) {
	kb := newMosaicFixture().knowledgeBaseJSON(t)
	pd := []byte(`{"name":"duiven-proef"}`)

	cases := map[string]map[string][]byte{
		"pad omhoog":  {kbFileKnowledgeBase: kb, "../ontsnapt.json": pd},
		"absoluut":    {kbFileKnowledgeBase: kb, "/etc/passwd": pd},
		"backslash":   {kbFileKnowledgeBase: kb, `map\bestand.json`: pd},
		"geen kb":     {kbFileProjectDetails: pd},
		"te veel":     manyEntries(kb),
		"te grote pd": {kbFileKnowledgeBase: kb, kbFileProjectDetails: bytes.Repeat([]byte("x"), maxProjectDetailsBytes+1)},
	}
	for naam, entries := range cases {
		t.Run(naam, func(t *testing.T) {
			if _, err := loadKBBundle(writeZipFile(t, "kb.zip", entries)); err == nil {
				t.Fatal("deze zip hoort geweigerd te worden")
			}
		})
	}
}

func manyEntries(kb []byte) map[string][]byte {
	entries := map[string][]byte{kbFileKnowledgeBase: kb}
	for i := range maxBundleEntries {
		entries["vul-"+string(rune('a'+i))+".txt"] = []byte("x")
	}
	return entries
}

func TestLoadKBBundle_RejectsAMissingOrIrregularFile(t *testing.T) {
	if _, err := loadKBBundle(filepath.Join(t.TempDir(), "bestaat-niet.zip")); err == nil {
		t.Error("een ontbrekend bestand hoort een fout te geven")
	}
	dir := t.TempDir()
	if _, err := loadKBBundle(dir); err == nil {
		t.Error("een map is geen bundel")
	}
}

func TestLoadKBBundle_RejectsALyingZipIndex(t *testing.T) {
	// Een zip-entry mag over zijn maat liegen (UncompressedSize64 is een
	// claim); de LimitReader is wat er werkelijk telt.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(kbFileProjectDetails)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(bytes.Repeat([]byte("y"), maxProjectDetailsBytes+64)); err != nil {
		t.Fatal(err)
	}
	w2, err := zw.Create(kbFileKnowledgeBase)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w2.Write(newMosaicFixture().knowledgeBaseJSON(t)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "kb.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKBBundle(path); err == nil {
		t.Fatal("een te grote entry hoort geweigerd te worden")
	}
}

func TestLoadKBBundle_RejectsBrokenJSON(t *testing.T) {
	entries := map[string][]byte{
		kbFileKnowledgeBase:  []byte(`{"bundle_schema": "mosaic/1"`),
		kbFileProjectDetails: []byte(`{"name":"duiven-proef"}`),
	}
	if _, err := loadKBBundle(writeZipFile(t, "kb.zip", entries)); err == nil {
		t.Fatal("kapotte JSON hoort te falen")
	}
}

func TestLoadKBBundle_ContentHashFollowsTheRawBytes(t *testing.T) {
	f := newMosaicFixture()
	first, err := loadKBBundle(f.writeZip(t, "kb.zip"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := loadKBBundle(f.writeZip(t, "kb.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if first.ContentHash != again.ContentHash {
		t.Error("dezelfde bundel hoort dezelfde hash te geven — daar hangt het overslaan van aan")
	}
	f.Nodes["Domein"][0]["titel"] = "Dorp"
	changed, err := loadKBBundle(f.writeZip(t, "kb.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if changed.ContentHash == first.ContentHash {
		t.Error("een gewijzigde bundel hoort een andere hash te geven")
	}
}

func TestDeriveID_IsStableAndScopedToTheKB(t *testing.T) {
	a := derived("duiven-proef", tableDestillaat, "des-1")
	if a != derived("duiven-proef", tableDestillaat, "des-1") {
		t.Error("dezelfde sleutel hoort dezelfde id te geven (idempotente upsert)")
	}
	if a == derived("andere-kb", tableDestillaat, "des-1") {
		t.Error("een andere kennisbank hoort een andere id te geven")
	}
	if tableOf(string(a)) != tableDestillaat {
		t.Errorf("id %q hoort met de tabel te beginnen", a)
	}
}

func TestLoadKBBundle_RejectsJSONThatOnlyTheOwnReaderSees(t *testing.T) {
	// ParseBundle kent alleen vijf labels en laat Interview links liggen; onze
	// eigen lezer leest álle labels en struikelt hier wél. De poortwachter
	// vervangt de eigen decode dus niet.
	entries := map[string][]byte{
		kbFileKnowledgeBase:  []byte(`{"bundle_schema":"mosaic/1","nodes":{"Interview":"geen lijst"}}`),
		kbFileProjectDetails: []byte(`{"name":"duiven-proef"}`),
	}
	_, err := loadKBBundle(writeZipFile(t, "kb.zip", entries))
	if err == nil || !strings.Contains(err.Error(), kbFileKnowledgeBase) {
		t.Fatalf("de eigen decode hoort dit te weigeren, kreeg %v", err)
	}
}

func TestLoadKBBundle_RejectsAFileThatIsNoZip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kb.zip")
	if err := os.WriteFile(path, []byte("dit is geen zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKBBundle(path); err == nil {
		t.Fatal("een bestand dat geen zip is hoort te falen")
	}
}

func TestLoadKBBundle_RejectsAnOversizedArchive(t *testing.T) {
	// Sparse: het bestand kost geen schijfruimte, maar os.Stat ziet de maat.
	path := filepath.Join(t.TempDir(), "groot.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxBundleFileBytes + 1); err != nil {
		f.Close()
		t.Skipf("sparse bestand niet te maken: %v", err)
	}
	f.Close()
	if _, err := loadKBBundle(path); err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("een te grote bundel hoort geweigerd te worden, kreeg %v", err)
	}
}

func TestLoadKBBundle_RejectsAnOversizedKnowledgeBase(t *testing.T) {
	// Sterk comprimeerbaar: de zip blijft klein, de uitgepakte stroom niet.
	entries := map[string][]byte{
		kbFileKnowledgeBase:  bytes.Repeat([]byte("x"), maxKnowledgeBaseBytes+1),
		kbFileProjectDetails: []byte(`{"name":"duiven-proef"}`),
	}
	_, err := loadKBBundle(writeZipFile(t, "kb.zip", entries))
	if err == nil || !strings.Contains(err.Error(), kbFileKnowledgeBase) {
		t.Fatalf("een te grote knowledge_base.json hoort geweigerd te worden, kreeg %v", err)
	}
}

func TestUnsafeZipEntryName_RejectsANamelessEntry(t *testing.T) {
	if !unsafeZipEntryName("") {
		t.Fatal("een naamloze entry hoort geweigerd te worden")
	}
	if unsafeZipEntryName("persona_images/ria.png") {
		t.Fatal("een gewone entry-naam hoort door te mogen")
	}
}

func TestLoadKBBundle_EmbedTextFallsBackToTheQuoteAlone(t *testing.T) {
	f := newMosaicFixture()
	// Geen samenvatting, en het eerste citaat is leeg: de tekst hoort dan het
	// tweede citaat te zijn.
	delete(f.Nodes["Destillaat"][0], "samenvatting")
	f.Nodes["Quote"][0]["text"] = ""
	f.Nodes["Quote"][1]["grounded"] = false

	b, err := loadKBBundle(f.writeZip(t, "kb.zip"))
	if err != nil {
		t.Fatal(err)
	}
	d := findDestillaat(t, b, derived(b.KBID, tableDestillaat, "des-1"))
	if d.Node.EmbedText != "Bij de bakker liggen broodkorsten." {
		t.Errorf("embed-tekst = %q, wil alleen het citaat", d.Node.EmbedText)
	}
}

func TestLoadKBBundle_RelationTypeFallsBackToTheMapKey(t *testing.T) {
	f := newMosaicFixture()
	// Sommige exports laten `type` op de relatie zelf weg; de sleutel van de
	// map is dan de enige bron.
	zonderType := rel("", "des-1", "des-2", nil)
	delete(zonderType, "type")
	f.Rels["RELATES_TO"] = []map[string]any{zonderType}

	b, err := loadKBBundle(f.writeZip(t, "kb.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.RelatesTo) != 1 || b.Skipped[ReasonKBUnknownRelation] != 0 {
		t.Errorf("relatie zonder type niet herkend: %d / %v", len(b.RelatesTo), b.Skipped)
	}
}
