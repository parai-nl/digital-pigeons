//go:build live

package duif

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/parai-nl/parai-core/data"
	datapostgres "github.com/parai-nl/parai-core/data/postgres"
	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/litellm"
)

// De live-kalibratie van de bronnendrempel. Hij draait niet in de gewone suite:
// buildtag `live`, en zonder database of proxy in de omgeving slaat hij over.
// `make kb-eval` laadt .env en start hem.
//
// Wat hij meet is bewust smal: de retrieval-scores van elke vraag uit
// config/eval/kb-eval.yaml. Dat is goedkoop (één embed-aanroep en één
// vectorzoekopdracht per vraag), herhaalbaar zonder dat een model iets kiest,
// en het is precies het getal waar DUIF_MIN_SIMILARITY op werkt. De duurdere
// vraag — praat de duif ook echt binnen zijn bronnen? — staat apart in
// TestLive_KBEval_Answers, achter een eigen omgevingsvlag.

const (
	// kbEvalTimeout begrenst de hele run: ~23 vragen × (embed + zoek).
	kbEvalTimeout = 10 * time.Minute
	// kbEvalUserID is de eigenaar van de chatsessies die de antwoordtoets
	// aanmaakt. Ze worden na afloop weer verwijderd.
	kbEvalUserID = "kb-eval"
	// De defaults van core's knowledge-config, zodat de eval dezelfde
	// embedder gebruikt als de import zonder dat .env ze hoeft te herhalen.
	kbEvalDefaultEmbeddingModel = "vertex/gemini-embedding-001"
	kbEvalDefaultEmbeddingDim   = 3072
)

// kbEvalRig is de minimale core-wiring die de kalibratie nodig heeft: de pool,
// de LiteLLM-provider (embedder én chatmodel) en core's Postgres-kennisadapter.
// Het is dezelfde constructie als in core's boot, maar zonder server, auth,
// queue of migraties — die heeft `make dev` al gedraaid.
type kbEvalRig struct {
	pool           *pgxpool.Pool
	provider       *litellm.Provider
	knowledge      *datapostgres.KnowledgeAdapter
	logger         *slog.Logger
	embeddingModel string
	embeddingDim   int
}

// kbEvalProviderConfig applies the same language-model policy before the
// standalone calibration can open its database or construct a provider.
// Embeddings keep their configured model; this does not migrate vectors.
func kbEvalProviderConfig() (litellm.Config, error) {
	if err := validateModelOverrides(); err != nil {
		return litellm.Config{}, err
	}
	baseURL, apiKey := os.Getenv("PARAI_LLM_BASE_URL"), os.Getenv("PARAI_LLM_API_KEY")
	if baseURL != "" {
		if err := validateModelEndpoint(baseURL, apiKey); err != nil {
			return litellm.Config{}, err
		}
	}
	model := os.Getenv("PARAI_KNOWLEDGE_EMBEDDING_MODEL")
	if model == "" {
		model = kbEvalDefaultEmbeddingModel
	}
	return litellm.Config{BaseURL: baseURL, APIKey: apiKey, EmbeddingModel: model}, nil
}

// newKBEvalRig bouwt de wiring of slaat de test over. De foutteksten van de
// pool blijven bewust buiten de uitvoer: een DSN draagt een wachtwoord.
func newKBEvalRig(ctx context.Context, t *testing.T) *kbEvalRig {
	t.Helper()
	providerConfig, err := kbEvalProviderConfig()
	if err != nil {
		t.Fatal(err)
	}
	dbURL := os.Getenv("PARAI_DB_URL")
	if dbURL == "" || providerConfig.BaseURL == "" {
		t.Skip("kalibratie vereist PARAI_DB_URL en PARAI_LLM_BASE_URL (make kb-eval laadt module/.env)")
	}
	model := providerConfig.EmbeddingModel
	dim := kbEvalDefaultEmbeddingDim
	if v := os.Getenv("PARAI_KNOWLEDGE_EMBEDDING_DIMENSION"); v != "" {
		d, err := strconv.Atoi(v)
		if err != nil || d <= 0 {
			t.Fatalf("PARAI_KNOWLEDGE_EMBEDDING_DIMENSION = %q, want een positief getal", v)
		}
		dim = d
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	pool, err := datapostgres.NewPool(ctx, dbURL)
	if err != nil {
		t.Skip("kennisbank niet bereikbaar; controleer PARAI_DB_URL en of Postgres draait")
	}
	t.Cleanup(pool.Close)

	provider := litellm.New(providerConfig, litellm.WithLogger(logger))
	knowledge := datapostgres.NewKnowledgeAdapter(pool, provider, datapostgres.KnowledgeConfig{
		EmbeddingDimension: dim,
		EmbeddingModel:     model,
	}, logger)

	return &kbEvalRig{pool: pool, provider: provider, knowledge: knowledge, logger: logger, embeddingModel: model, embeddingDim: dim}
}

// requireImportedKB slaat over zolang er niets geïmporteerd is: op een lege
// graaf scoort elke vraag nul en zou de "kalibratie" een mooi ogende,
// betekenisloze tabel opleveren.
func (r *kbEvalRig) requireImportedKB(ctx context.Context, t *testing.T) {
	t.Helper()
	d, err := r.knowledge.ListDestillaten(ctx, data.ListOpts{Limit: 1})
	if err != nil {
		t.Skip("standpunten niet leesbaar; draait de duif-migratie en is de kennisbank geïmporteerd?")
	}
	if len(d) == 0 {
		t.Skip("geen standpunten in de kennisbank (draai eerst make import-kb KB=…)")
	}
}

func TestLive_KBEval(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), kbEvalTimeout)
	defer cancel()

	rig := newKBEvalRig(ctx, t)
	rig.requireImportedKB(ctx, t)

	cases, err := kbEvalSet()
	if err != nil {
		t.Fatalf("evaluatieset: %v", err)
	}

	results := make([]kbEvalResult, 0, len(cases))
	for _, c := range cases {
		vec, err := rig.provider.Embed(ctx, c.Question)
		if err != nil {
			t.Fatalf("vraag %s embedden: %v", c.ID, err)
		}
		if len(vec) != rig.embeddingDim {
			// Een andere dimensie dan de import gebruikte betekent dat de
			// vectoren niet vergelijkbaar zijn; elke score hierna is onzin.
			t.Fatalf("vraag %s: vectorlengte %d, want %d (PARAI_KNOWLEDGE_EMBEDDING_DIMENSION)", c.ID, len(vec), rig.embeddingDim)
		}
		// Rechtstreeks op de zoekseam, niet via retrieval.run: dit is precies
		// het getal waar de drempel op werkt, zonder gate, hernummering of
		// graafverrijking ertussen.
		hits, err := rig.knowledge.SemanticSearch(ctx, tableDestillaat, vec, retrieveTopK)
		if err != nil {
			t.Fatalf("vraag %s zoeken: %v", c.ID, err)
		}
		scores := make([]float32, 0, len(hits))
		for _, h := range hits {
			scores = append(scores, h.Score)
		}
		results = append(results, kbEvalResultFor(c, scores))
	}

	// De task_type-controle: core's llm.Embedder is
	// Embed(ctx, text) — één ingang, zonder onderscheid tussen corpus en
	// vraag. Corpus én vraag gaan dus door hetzelfde model met dezelfde
	// (afwezige) task type. Zou dat aan de proxykant veranderen, dan
	// verschuift de hele scoreverdeling en vervalt deze kalibratie.
	t.Logf("embedder: model=%s dimensie=%d, geen task_type (corpus en vraag delen één Embed-aanroep)", rig.embeddingModel, rig.embeddingDim)

	out, closeOut := kbEvalOutput(t)
	defer closeOut()
	if err := writeKBEvalJSON(out, results); err != nil {
		t.Fatalf("JSON-regels schrijven: %v", err)
	}
	t.Logf("topscore-histogrammen\n%s", histogramText(results))
	t.Logf("per drempel (positieve klasse = kennisgat)\n%s", summaryText(results))
}

// kbEvalOutput opent de bestemming van de JSON-regels. Default is stdout; een
// pad komt uit DUIF_EVAL_OUT, want een vaste locatie in de repo zou van machine
// tot machine iets anders betekenen.
func kbEvalOutput(t *testing.T) (io.Writer, func()) {
	t.Helper()
	path := os.Getenv("DUIF_EVAL_OUT")
	if path == "" || path == "/dev/stdout" {
		return os.Stdout, func() {}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("DUIF_EVAL_OUT openen: %v", err)
	}
	t.Logf("JSON-regels gaan naar %s", path)
	return f, func() {
		if err := f.Close(); err != nil {
			t.Errorf("DUIF_EVAL_OUT sluiten: %v", err)
		}
	}
}

// TestLive_KBEval_Answers is de dure helft, achter DUIF_EVAL_ANSWERS=1: draait
// een echte chatbeurt over het gewone beurtpad en toetst de belofte "geen bron,
// geen antwoord" aan het antwoord zelf. Vijf vragen — drie die bronnen horen op
// te leveren, twee die een gat horen te geven — want elke beurt kost een
// LLM-aanroep.
func TestLive_KBEval_Answers(t *testing.T) {
	if os.Getenv("DUIF_EVAL_ANSWERS") != "1" {
		t.Skip("antwoordtoets alleen met DUIF_EVAL_ANSWERS=1 (kost een LLM-beurt per vraag)")
	}
	ctx, cancel := context.WithTimeout(t.Context(), kbEvalTimeout)
	defer cancel()

	rig := newKBEvalRig(ctx, t)
	rig.requireImportedKB(ctx, t)
	m := rig.module(t)
	pigeon := kbEvalPigeon(t, m.personas)
	t.Cleanup(func() { rig.cleanupEvalSessions(t) })

	cases, err := kbEvalSet()
	if err != nil {
		t.Fatalf("evaluatieset: %v", err)
	}
	for _, c := range pickKBEvalAnswerCases(cases, 3, 2) {
		pt, terr := m.prepareTurn(ctx, kbEvalUserID, turnRequest{PigeonID: pigeon, Message: c.Question, Lang: c.Lang})
		if terr != nil {
			t.Fatalf("beurt %s voorbereiden: status %d", c.ID, terr.status)
		}
		resp, err := m.llm.Complete(ctx, llm.CompletionRequest{Model: m.model, Messages: pt.messages})
		if err != nil {
			t.Fatalf("beurt %s: %v", c.ID, err)
		}
		// Bewust het rauwe modelantwoord, niet de gefilterde stream: in
		// gap-modus haalt pumpTurn de [n]'s er zelf uit, en dan zou deze toets
		// altijd slagen zonder iets over de prompt te zeggen.
		cits := citationNumbers(resp.Content)
		// Het antwoord zelf is kennisbankinhoud: alleen met DUIF_EVAL_VERBOSE=1
		// erbij, voor wie tijdens het kalibreren meeleest.
		t.Logf("%s (%s, %s): status=%s bronnen=%d citaten=%v%s",
			c.ID, c.Lang, c.Expect, pt.status, len(pt.sources), cits, kbEvalAnswerLog(resp.Content))

		switch c.Expect {
		case KBSources:
			if pt.status != KBSources {
				t.Logf("kalibratiebevinding: %s gaf een kennisgat bij drempel %.2f", c.ID, m.minSimilarity)
				continue
			}
			for _, n := range cits {
				if n < 1 || n > len(pt.sources) {
					t.Errorf("%s: het antwoord noemt [%d], maar de beurt had %d bronnen", c.ID, n, len(pt.sources))
				}
			}
		case KBGap:
			if pt.status != KBGap {
				t.Logf("kalibratiebevinding: %s haalde bronnen op bij drempel %.2f", c.ID, m.minSimilarity)
				continue
			}
			if len(cits) > 0 {
				t.Errorf("%s: gap-antwoord noemt bronnen %v terwijl er geen zijn", c.ID, cits)
			}
		}
	}
}

// module bouwt de duif-module met de live services eronder — dezelfde velden
// die Init uit de core haalt, zodat prepareTurn het gewone pad loopt.
func (r *kbEvalRig) module(t *testing.T) *Module {
	t.Helper()
	personas, err := LoadEmbeddedPersonas()
	if err != nil {
		t.Fatalf("persona-config: %v", err)
	}
	m := New()
	m.personas = personas
	m.llm = r.provider
	m.embedder = r.provider
	m.knowledge = r.knowledge
	m.logger = r.logger
	m.repo = newPostgresRepo(r.pool)
	m.model = defaultChatModel
	if v := os.Getenv("DUIF_MIN_SIMILARITY"); v != "" {
		f, err := strconv.ParseFloat(v, 32)
		if err != nil || f < 0 || f > 1 {
			t.Fatalf("DUIF_MIN_SIMILARITY = %q, want een waarde in [0,1]", v)
		}
		m.minSimilarity = float32(f)
	}
	ret, err := newRetrieval(m.embedder, m.knowledge, m.minSimilarity, m.knowledgeStatus, m.logger)
	if err != nil {
		t.Fatalf("retrieval bouwen: %v", err)
	}
	m.retrieval = ret
	return m
}

// cleanupEvalSessions ruimt de chatsessies van de eval op: een kalibratierun
// hoort niet in de gesprekshistorie van de app achter te blijven. Berichten
// gaan mee via ON DELETE CASCADE.
func (r *kbEvalRig) cleanupEvalSessions(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := r.pool.Exec(ctx, `DELETE FROM duif.chat_sessions WHERE user_id = $1`, kbEvalUserID); err != nil {
		t.Errorf("eval-sessies opruimen: %v", err)
	}
}

// kbEvalPigeon kiest de duif die chat én op de kennisbank leunt.
func kbEvalPigeon(t *testing.T, set *PersonaSet) string {
	t.Helper()
	for _, p := range set.Personas {
		if p.Chat && p.Kennisbank {
			return p.ID
		}
	}
	t.Skip("geen chattende kennisbank-duif in personas.yaml")
	return ""
}

// pickKBEvalAnswerCases neemt de eerste n in-KB-vragen en de eerste m
// gat-vragen uit de set — in bestandsvolgorde, zodat twee runs dezelfde vragen
// stellen.
func pickKBEvalAnswerCases(cases []kbEvalCase, wantSources, wantGap int) []kbEvalCase {
	out := make([]kbEvalCase, 0, wantSources+wantGap)
	for _, c := range cases {
		if c.Expect == KBSources && wantSources > 0 {
			out = append(out, c)
			wantSources--
		}
	}
	for _, c := range cases {
		if c.Expect == KBGap && wantGap > 0 {
			out = append(out, c)
			wantGap--
		}
	}
	return out
}

// kbEvalAnswerLog geeft het antwoordfragment alleen als de kalibrator er
// expliciet om vraagt (DUIF_EVAL_VERBOSE=1). Zonder die knop blijft de
// testuitvoer een rij cijfers: het antwoord is kennisbankinhoud, en die hoort
// niet standaard in een logboek dat later ergens geplakt wordt.
func kbEvalAnswerLog(s string) string {
	if os.Getenv("DUIF_EVAL_VERBOSE") != "1" {
		return ""
	}
	return " | " + kbEvalSnippet(s)
}

// kbEvalSnippet houdt de logregel leesbaar; het volledige antwoord hoort in de
// notities, niet in de testuitvoer.
func kbEvalSnippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= 160 {
		return s
	}
	return string(r[:160]) + "…"
}
