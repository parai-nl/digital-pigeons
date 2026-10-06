package duif

import (
	"context"
	"sort"
	"strings"

	"github.com/parai-nl/parai-core/data"
)

// Het schema van de kennisbank zoals deze module hem kent: welke tabellen en
// relaties er zijn, hoe een node-id gelezen wordt en wat de importmarker
// vertelt. Drie lagen delen dit — de import die schrijft, de graaf die toont en
// de gate die citeert — en ze horen niet elk een eigen kopie te hebben: een
// tabelnaam die op één plek verandert en op de andere twee blijft staan, is een
// halve kennisbank zonder dat er iets faalt.

// De acht tabellen van de kennisbank in core's gedeelde kennisgraaf, plus de
// markertabel van deze module. Let op `perspective`: core's typed reads
// (GetPerspectief, GetWithContext) lezen die tabelnaam, en omdat DeriveID de
// tabel in de sleutel stopt is dat niet achteraf te corrigeren.
const (
	tableDomein      = "domein"
	tableThema       = "thema"
	tableSubthema    = "subthema"
	tablePerspective = "perspective"
	tablePersona     = "persona"
	tableInterview   = "interview"
	tableDestillaat  = "destillaat"
	tableQuote       = "quote"
	// tableImportMarker draagt één marker-node per kennisbank.
	tableImportMarker = "duif_import"
)

// Relaties die deze kennisbank buiten core's canon legt (core kent contains,
// has_quote, belongs_to_perspective, contains_perspective en has_perspective
// al als constante).
const (
	relBelongsTo         = "belongs_to"
	relBelongsToSubthema = "belongs_to_subthema"
	relContainsThema     = "contains_thema"
	relContainsSubthema  = "contains_subthema"
)

// kbLabelTables mapt de labels uit de bundel op de tabellen hierboven. De
// Bron is de duiven/2-naam voor dezelfde interne interviewtabel. Die naam
// blijft behouden om bestaande afgeleide IDs en frameworkrelaties te bewaren.
var kbLabelTables = map[string]string{
	"Destillaat":  tableDestillaat,
	"Quote":       tableQuote,
	"Interview":   tableInterview,
	"Bron":        tableInterview,
	"Thema":       tableThema,
	"Subthema":    tableSubthema,
	"Domein":      tableDomein,
	"Perspectief": tablePerspective,
	"Persona":     tablePersona,
}

// kbTables is de vaste lijst tabellen die deze module beheert, plus de
// markertabel. Gesorteerd, zodat vervangen en tellen deterministisch lopen.
func kbTables() []string {
	out := make([]string, 0, len(kbLabelTables)+1)
	out = append(out, tableImportMarker)
	seen := map[string]bool{tableImportMarker: true}
	for _, table := range kbLabelTables {
		if seen[table] {
			continue
		}
		seen[table] = true
		out = append(out, table)
	}
	sort.Strings(out)
	return out
}

// tableOf haalt de tabel uit een node-id van de vorm `tabel:uuid`.
func tableOf(id string) string {
	table, _, _ := strings.Cut(id, ":")
	return table
}

// --- de importstand ---------------------------------------------------------

// ImportState is de stand van de kennisbank-import. Een ander begrip dan
// KBStatus (persona_only|sources|gap): die gaat over één turn, dit over de
// database.
type ImportState string

const (
	// ImportEmpty: geen marker en geen standpunten — de graaf is leeg.
	ImportEmpty ImportState = "empty"
	// ImportImporting: de achtergrond-goroutine is bezig.
	ImportImporting ImportState = "importing"
	// ImportReady: er staat een marker; de kennisbank is compleet geladen.
	ImportReady ImportState = "ready"
	// ImportIncomplete: er staan standpunten zónder marker. Een eerdere import
	// is halverwege gestopt; een volgende boot met DUIF_IMPORT_KB maakt hem af.
	ImportIncomplete ImportState = "incomplete"
	// ImportFailed: de laatste poging viel om; `reason` zegt waarom.
	ImportFailed ImportState = "failed"
)

// importStatus is de stand van de import: achter de mutex van de importer én,
// in precies dezelfde vorm, de body van GET /v1/knowledge/status. Eén type,
// want twee die uit elkaar kunnen lopen zijn twee waarheden. camelCase op de
// wire, snake_case in de database; `reason` is altijd een code, nooit een
// fouttekst.
type importStatus struct {
	State  ImportState    `json:"state"`
	KBID   string         `json:"kbId,omitempty"`
	Counts map[string]int `json:"counts,omitempty"`
	// BundleSchema, Languages en SourceLanguage zeggen wélk formaat er binnen
	// is gekomen. De marker legt het al vast; zonder deze drie kon
	// je van buiten niet zien of een ready-bank eentalig is of vertaald. Een
	// duiven/2-import draagt geen talen, dus blijven die velden weg — dat is
	// het verschil tussen "eentalig corpus" en "vertaling ontbreekt". Ze staan
	// op omitempty: een oudere client die ze niet kent, ziet precies de wire
	// die hij altijd zag.
	BundleSchema   string   `json:"bundleSchema,omitempty"`
	Languages      []string `json:"languages,omitempty"`
	SourceLanguage string   `json:"sourceLanguage,omitempty"`
	ImportedAt     string   `json:"importedAt,omitempty"`
	Skipped        bool     `json:"skipped,omitempty"`
	Reason         string   `json:"reason,omitempty"`
}

// kbMarker is de importmarker: de enige plek waar "deze kennisbank staat er
// compleet in" vastligt.
type kbMarker struct {
	ID          data.NodeID
	KBID        string
	ContentHash string
	// BundleSchema, Languages en SourceLanguage leggen vast wélk formaat er
	// geïmporteerd is. Een duiven/2-marker draagt geen talen; dat is het
	// verschil tussen "eentalig corpus" en "vertaling ontbreekt".
	BundleSchema       string
	Languages          []string
	SourceLanguage     string
	MapperVersion      int
	EmbeddingModel     string
	EmbeddingDimension int
	ImportedAt         string
	Counts             map[string]int
}

func markerFromProps(row map[string]any) kbMarker {
	kbID, _ := row["kb_id"].(string)
	hash, _ := row["content_hash"].(string)
	model, _ := row["embedding_model"].(string)
	at, _ := row["imported_at"].(string)
	schema, _ := row["bundle_schema"].(string)
	sourceLang, _ := row["source_language"].(string)
	return kbMarker{
		ID:                 data.NodeID(rowID(row)),
		KBID:               kbID,
		ContentHash:        hash,
		BundleSchema:       schema,
		Languages:          kbLanguagesFromProps(row["languages"]),
		SourceLanguage:     sourceLang,
		MapperVersion:      asInt(row["mapper_version"]),
		EmbeddingModel:     model,
		EmbeddingDimension: asInt(row["embedding_dimension"]),
		ImportedAt:         at,
		Counts:             countsFromProps(row["counts"]),
	}
}

// kbLanguageProp schrijft de talenlijst zó weg dat een lege lijst geen lege
// jsonb-array wordt: een duiven/2-marker draagt gewoon geen talen.
func kbLanguageProp(langs []string) any {
	if len(langs) == 0 {
		return nil
	}
	out := make([]any, 0, len(langs))
	for _, lang := range langs {
		out = append(out, lang)
	}
	return out
}

// kbLanguagesFromProps leest de talenlijst terug; hij komt als []any uit jsonb
// en als []string uit een in-memory schrijver.
func kbLanguagesFromProps(v any) []string {
	switch list := v.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// asInt leest een getal dat via jsonb terugkomt (float64) of rechtstreeks uit
// een in-memory fake (int).
func asInt(v any) int {
	n, _ := numberOf(v)
	return n
}

// countsFromProps leest de tellingen van de marker robuust: ze komen als JSON
// uit de database terug (float64), maar een in-memory schrijver kan er ints in
// hebben gezet. Wat geen getal is telt niet mee — een nul die er niet stond zou
// als "tabel leeg" lezen.
func countsFromProps(v any) map[string]int {
	raw, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]int, len(raw))
	for k, v := range raw {
		if n, ok := numberOf(v); ok {
			out[k] = n
		}
	}
	return out
}

// numberOf leest een getal uit props, ongeacht of het via jsonb (float64) of
// rechtstreeks uit een in-memory schrijver (int) komt.
func numberOf(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

// knowledgeLister is de smalste leesseam die de status nodig heeft.
type knowledgeLister interface {
	ListNodes(ctx context.Context, table string, opts data.ListOpts) ([]map[string]any, error)
}

// readImportStatus leest de stand uit de graaf met twee begrensde queries.
// Nooit paginerend tellen: ListNodes sleept `embedding` mee (≈ 25 KB per
// standpunt), dus één rij is precies genoeg om "is er iets" te beantwoorden.
//
// De tweede query is streng: een standpunt zonder `import_hash` is niet door
// deze import geschreven en zegt dus niets over een afgebroken poging.
func readImportStatus(ctx context.Context, lister knowledgeLister) (importStatus, error) {
	rows, err := lister.ListNodes(ctx, tableImportMarker, data.ListOpts{Limit: 1})
	if err != nil {
		return importStatus{}, err
	}
	if len(rows) > 0 {
		mk := markerFromProps(rows[0])
		return readyStatus(mk, false), nil
	}
	rows, err = lister.ListNodes(ctx, tableDestillaat, data.ListOpts{Limit: 1})
	if err != nil {
		return importStatus{}, err
	}
	if len(rows) > 0 && rows[0]["import_hash"] != nil {
		return importStatus{State: ImportIncomplete}, nil
	}
	return importStatus{State: ImportEmpty}, nil
}

// readyStatus is de ready-stand van één marker: dezelfde velden, of de import
// nu net gedraaid heeft, overgeslagen is of alleen bij het opstarten gelezen
// werd. Eén plek, zodat het formaat niet op de ene route wél en op de andere
// niet op de wire komt.
func readyStatus(mk kbMarker, skipped bool) importStatus {
	return importStatus{
		State:          ImportReady,
		KBID:           mk.KBID,
		Counts:         mk.Counts,
		BundleSchema:   mk.BundleSchema,
		Languages:      mk.Languages,
		SourceLanguage: mk.SourceLanguage,
		ImportedAt:     mk.ImportedAt,
		Skipped:        skipped,
	}
}
