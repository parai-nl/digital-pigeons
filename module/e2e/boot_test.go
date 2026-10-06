//go:build e2e

// Package e2e boot de duif-server als echt proces tegen een wegwerp-Postgres
// en een OpenAI-compatibele stub-LLM, en controleert het contract op de wire.
// Draaien via `make e2e` (scripts/e2e.sh regelt Postgres, build en opruimen).
package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// duifServer is de gebootte server plus de handvatten om hem te bevragen.
type duifServer struct {
	baseURL string
	llm     *stubLLM
	logs    *syncBuffer
	// stop sluit het serverproces netjes af (SIGINT, coverage weggeschreven).
	// Idempotent: de opruiming van de test roept hem hoe dan ook aan.
	stop func()
}

type syncBuffer struct {
	mu  chan struct{}
	buf bytes.Buffer
}

func newSyncBuffer() *syncBuffer {
	b := &syncBuffer{mu: make(chan struct{}, 1)}
	b.mu <- struct{}{}
	return b
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	<-b.mu
	defer func() { b.mu <- struct{}{} }()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	<-b.mu
	defer func() { b.mu <- struct{}{} }()
	return b.buf.String()
}

func env(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("%s is niet gezet — draai de boot-e2e via `make e2e`", key)
	}
	return v
}

// freePort claimt en laat direct los; de server pakt de poort daarna op.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("vrije poort zoeken: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// serverConfig zijn de knoppen waarmee een scenario een eigen server start:
// een andere database, extra omgevingsvariabelen, of een stub-LLM die van een
// vorige boot wordt hergebruikt (zodat tellers over een herstart heen lopen).
type serverConfig struct {
	// dbURL vervangt DUIF_E2E_DB_URL. Leeg = de gedeelde bootdatabase.
	dbURL string
	// env zijn extra KEY=VALUE-regels voor het serverproces.
	env []string
	// llm hergebruikt een draaiende stub in plaats van een nieuwe te starten.
	llm *stubLLM
}

// bootServer start de echte binary op de gedeelde bootdatabase.
func bootServer(t *testing.T) *duifServer { return bootServerWith(t, serverConfig{}) }

// bootServerWith start de echte binary en wacht tot de healthcheck groen is.
func bootServerWith(t *testing.T, cfg serverConfig) *duifServer {
	t.Helper()
	binary := env(t, "DUIF_E2E_BINARY")
	workdir := env(t, "DUIF_E2E_WORKDIR")
	dbURL := cfg.dbURL
	if dbURL == "" {
		dbURL = env(t, "DUIF_E2E_DB_URL")
	}

	llm := cfg.llm
	if llm == nil {
		llm = startStubLLM(t)
	}
	port := freePort(t)
	logs := newSyncBuffer()

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(),
		"PARAI_DB_URL="+dbURL,
		"PARAI_LLM_BASE_URL="+llm.URL,
		"PARAI_LLM_API_KEY=synthetic-loopback-only",
		"PORT="+fmt.Sprint(port),
		"PARAI_LOG_LEVEL=info",
	)
	cmd.Env = append(cmd.Env, cfg.env...)
	// Een nooit geïmporteerde kennisbank is een storing en geen
	// kennisgat: zonder bundel zou elke duifbeurt hieronder op kb_unavailable
	// eindigen en zou deze contracttest niets meer over chat of vergadering
	// zeggen. Elke boot krijgt daarom dezelfde kleine fixturebundel. De
	// scenario's die hier een gat verwachten stellen vragen die er niet in
	// staan; de tweede boot op dezelfde database slaat de import over.
	importKB := !slices.ContainsFunc(cfg.env, func(kv string) bool {
		return strings.HasPrefix(kv, "DUIF_IMPORT_KB=")
	})
	if importKB {
		cmd.Env = append(cmd.Env, "DUIF_IMPORT_KB="+writeKBFixture(t))
	}
	if dir := os.Getenv("DUIF_E2E_COVERDIR"); dir != "" {
		cmd.Env = append(cmd.Env, "GOCOVERDIR="+dir)
	}
	cmd.Stdout = logs
	cmd.Stderr = logs
	// SIGINT laat de server netjes afsluiten en zijn coverageprofiel wegschrijven.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 15 * time.Second

	if err := cmd.Start(); err != nil {
		t.Fatalf("server starten: %v", err)
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			_ = cmd.Wait()
		})
	}
	t.Cleanup(func() {
		stop()
		if t.Failed() {
			t.Logf("serverlog:\n%s", logs.String())
		}
	})

	srv := &duifServer{baseURL: fmt.Sprintf("http://127.0.0.1:%d", port), llm: llm, logs: logs, stop: stop}
	srv.waitHealthy(t)
	status, raw := srv.get(t, "/api/duif/v1/access/notice")
	var notice struct {
		Version string `json:"version"`
	}
	if status != 200 || json.Unmarshal(raw, &notice) != nil || notice.Version == "" {
		t.Fatalf("notice unavailable: %d", status)
	}
	if status, _ := srv.postJSON(t, http.MethodPost, "/api/duif/v1/access/notice", map[string]string{"version": notice.Version}); status != 200 {
		t.Fatalf("notice acknowledgement: %d", status)
	}
	if importKB {
		// Wachten hoort erbij: zolang de import loopt is de stand `importing`,
		// en dat is dezelfde storing als een lege bank. Zonder dit wachten zou
		// de eerste beurt na een koude boot willekeurig omvallen.
		srv.waitImportReady(t, 60*time.Second)
	}
	return srv
}

func (s *duifServer) waitHealthy(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(s.baseURL + "/api/duif/v1/health")
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && strings.Contains(string(body), `"ok"`) {
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("server werd niet gezond binnen de deadline; log:\n%s", s.logs.String())
}

func (s *duifServer) get(t *testing.T, path string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(s.baseURL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s lezen: %v", path, err)
	}
	return resp.StatusCode, body
}

// sseFrame is één geparsed SSE-frame van de turn-route.
type sseFrame map[string]any

// postTurn stuurt een turn en leest de SSE-stream uit: de frames die de lezer
// iets te lezen geven, plus de ruwe body.
//
// De `phase`-frames zitten niet in de lijst. Zij zijn de stand van
// de beurt en geen inhoud; ze meetellen zou elke volgorde-assertie hier
// dezelfde vijf statusframes laten herhalen. Hun eigen volgorde komt uit de
// ruwe body (phaseSequence in answer_contract_test.go).
func (s *duifServer) postTurn(t *testing.T, body map[string]any) (int, []sseFrame, string) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(s.baseURL+"/api/duif/v1/chat/turn", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST turn: %v", err)
	}
	defer resp.Body.Close()

	all, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("turn-body lezen: %v", err)
	}
	var frames []sseFrame
	sc := bufio.NewScanner(bytes.NewReader(all))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var f sseFrame
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &f); err != nil {
			t.Fatalf("ongeldig SSE-frame %q: %v", line, err)
		}
		if f["type"] == "phase" {
			continue
		}
		frames = append(frames, f)
	}
	return resp.StatusCode, frames, string(all)
}

func frameTypes(frames []sseFrame) []string {
	out := make([]string, len(frames))
	for i, f := range frames {
		out[i], _ = f["type"].(string)
	}
	return out
}

func lastFrame(t *testing.T, frames []sseFrame, typ string) sseFrame {
	t.Helper()
	for i := len(frames) - 1; i >= 0; i-- {
		if frames[i]["type"] == typ {
			return frames[i]
		}
	}
	t.Fatalf("geen %s-frame in %v", typ, frameTypes(frames))
	return nil
}

// frameAt geeft het frame op stap n van de gecoalesceerde reeks: opeenvolgende
// frames van dezelfde soort tellen als één stap. Zo blijft een assertie over de
// volgorde staan of de duif nu in twee of in twintig stukjes streamt.
func frameAt(t *testing.T, frames []sseFrame, n int) sseFrame {
	t.Helper()
	step := -1
	vorige := ""
	for _, f := range frames {
		typ, _ := f["type"].(string)
		if typ != vorige {
			step++
			vorige = typ
		}
		if step == n {
			return f
		}
	}
	t.Fatalf("geen stap %d in %v", n, coalescedFrameTypes(frames))
	return nil
}

// artifactText plakt de delta's van alle duif-blokken aan elkaar.
func artifactText(frames []sseFrame) string {
	var b strings.Builder
	for _, f := range frames {
		if f["type"] == "artifact_delta" {
			d, _ := f["delta"].(string)
			b.WriteString(d)
		}
	}
	return b.String()
}

func streamedText(frames []sseFrame) string {
	var b strings.Builder
	for _, f := range frames {
		if f["type"] == "text" {
			d, _ := f["delta"].(string)
			b.WriteString(d)
		}
	}
	return b.String()
}

// TestBootE2E draait alle scenario's tegen één gebootte server: opnieuw
// opstarten per subtest kost tientallen seconden aan migraties en zegt niets
// extra's.
func TestBootE2E(t *testing.T) {
	srv := bootServer(t)

	var sessionID string
	var sessionAnswer string
	var sessionProvenance map[string]string

	t.Run("healthcheck", func(t *testing.T) {
		status, body := srv.get(t, "/api/duif/v1/health")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", status, body)
		}
		var out map[string]string
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("health-body %s: %v", body, err)
		}
		if out["status"] != "ok" {
			t.Errorf("status = %q, want ok", out["status"])
		}
	})

	t.Run("duivenlijst zonder prompts", func(t *testing.T) {
		status, body := srv.get(t, "/api/duif/v1/duiven")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", status, body)
		}
		var out struct {
			Duiven []struct {
				ID            string            `json:"id"`
				Name          map[string]string `json:"name"`
				Chat          bool              `json:"chat"`
				Engine        string            `json:"engine"`
				KnowledgeBase bool              `json:"knowledgeBase"`
				// De startstand van zelfstandig meedoen hoort bij de duif:
				// de opzetkaart leest hem hieruit.
				AutonomousDefault bool `json:"autonomousDefault"`
			} `json:"duiven"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("duiven-body %s: %v", body, err)
		}
		if len(out.Duiven) != 3 {
			t.Fatalf("aantal duiven = %d, want 3", len(out.Duiven))
		}
		// De motor staat in de lijst: de client weet daarmee vanaf het eerste
		// teken van een beurt wie er praat — de facilitator of de duif zelf.
		motoren := map[string]string{"biological": "agent", "individualist": "agent", "nonverbal": "pipeline"}
		for _, d := range out.Duiven {
			if want := d.ID != "nonverbal"; d.KnowledgeBase != want || d.Chat != want {
				t.Errorf("duif %q: knowledgeBase=%v, chat=%v, want beide %v", d.ID, d.KnowledgeBase, d.Chat, want)
			}
			if d.Engine != motoren[d.ID] {
				t.Errorf("duif %q: engine = %q, want %q", d.ID, d.Engine, motoren[d.ID])
			}
			// Zelfstandig meedoen staat aan bij de duif zonder woorden en uit
			// bij de twee die praten: zonder dit veld zou de opzetkaart die
			// stand moeten raden.
			if want := d.ID == "nonverbal"; d.AutonomousDefault != want {
				t.Errorf("duif %q: autonomousDefault = %v, want %v", d.ID, d.AutonomousDefault, want)
			}
		}
		// En het veld staat er werkelijk onder deze naam op de wire; een
		// ontbrekend veld zou hierboven als een keurige false doorkomen.
		if !strings.Contains(string(body), `"autonomousDefault":true`) {
			t.Errorf("de duivenlijst draagt geen autonomousDefault: %s", body)
		}
		// Systemprompts verlaten de server nooit: geen enkel fragment van de
		// promptbestanden mag in de duivenlijst opduiken.
		for _, fragment := range []string{"digitale duifpersona", "digital pigeon persona", "eis:R"} {
			if strings.Contains(strings.ToLower(string(body)), strings.ToLower(fragment)) {
				t.Errorf("promptfragment %q lekt in de duivenlijst: %s", fragment, body)
			}
		}
	})

	t.Run("chatturn: facilitator en duif als twee stemmen", func(t *testing.T) {
		beforeRequests := len(srv.llm.requests())
		status, frames, raw := srv.postTurn(t, map[string]any{
			"pigeonId": "individualist",
			"message":  "Wat vind jij van de Coolsingel?",
			"lang":     "nl",
		})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", status, raw)
		}
		// De volgorde van de agent-motor rond de individualist: chip → kader →
		// duif-blok → chip dicht → bemiddeling → done. De done sluit de beurt.
		want := []string{"tool", "text", "artifact", "artifact_delta", "artifact", "tool_result", "text", "done"}
		if got := coalescedFrameTypes(frames); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("frames = %v,\nwant %v", got, want)
		}
		if frames[0]["name"] != "ask_pigeon" {
			t.Errorf("tool-frame = %+v, want ask_pigeon", frames[0])
		}
		// En naast die stemmen loopt de stapregel mee: vraag
		// formuleren, bronnen lezen, antwoord schrijven, afronden.
		assertPhaseOrder(t, raw, true)
		open := frameAt(t, frames, 2)
		if open["type"] != "artifact" || open["kind"] != "pigeon_answer" || open["persona"] != "individualist" {
			t.Errorf("artifact-open = %+v", open)
		}
		if !strings.Contains(artifactText(frames), "Roekoe") {
			t.Errorf("duif-blok = %q, want het stub-antwoord van de duif", artifactText(frames))
		}
		// Het kader staat vóór het duif-blok, de bemiddeling erna: dat is de
		// volgorde die de lezer beleeft en die het herladen moet reproduceren.
		if got := frameAt(t, frames, 1); got["type"] != "text" || got["delta"] != fixtureRouterFraming {
			t.Errorf("kaderzin = %+v, want %q vóór het artifact", got, fixtureRouterFraming)
		}
		if got := frameAt(t, frames, 6); got["type"] != "text" || got["delta"] != fixtureRouterMediation {
			t.Errorf("bemiddeling = %+v, want %q na het artifact", got, fixtureRouterMediation)
		}
		if streamedText(frames) != fixtureRouterFraming+fixtureRouterMediation {
			t.Errorf("facilitator-tekst = %q", streamedText(frames))
		}

		done := lastFrame(t, frames, "done")
		if done["kbStatus"] != "gap" {
			t.Errorf("kbStatus = %v, want gap (ook de individualist raadpleegt de bereikbare, lege kennisbank)", done["kbStatus"])
		}
		if done["stopReason"] != "natural" {
			t.Errorf("stopReason = %v, want natural", done["stopReason"])
		}
		id, _ := done["sessionId"].(string)
		if id == "" {
			t.Fatal("done-frame zonder sessionId")
		}
		sessionID = id

		assertAnswerRequests(t, srv.llm.requests()[beforeRequests:], "individualist", "nl", false, false, []string{"Wat vind jij van de Coolsingel?"}, artifactText(frames))
		if !strings.Contains(artifactText(frames), fixturePositionNL) {
			t.Fatal("individualistic position was not appended")
		}
		for _, frame := range frames {
			if frame["type"] == "artifact" || frame["type"] == "artifact_delta" {
				if frame["id"] != open["id"] {
					t.Fatal("pigeon answer changed artifact identity")
				}
			}
		}
		closed := lastFrame(t, frames, "artifact")
		if closed["done"] != true || closed["error"] != nil || closed["persona"] != "individualist" {
			t.Fatal("pigeon artifact did not close under the original author")
		}
		result := lastFrame(t, frames, "tool_result")
		if result["id"] != frames[0]["id"] || result["name"] != "ask_pigeon" || result["error"] != nil {
			t.Fatal("pigeon tool result lost its original identity or failed")
		}
		var metadata struct {
			Provenance map[string]string `json:"provenance"`
		}
		reMarshal(t, done, &metadata)
		assertProvenance(t, metadata.Provenance, false)
		sessionProvenance = metadata.Provenance
		sessionAnswer = artifactText(frames)

	})

	t.Run("sessie en berichten zijn persistent", func(t *testing.T) {
		if sessionID == "" {
			t.Fatal("voorwaarde ontbreekt: de vorige chatturn leverde geen sessie op")
		}
		status, body := srv.get(t, "/api/duif/v1/chat/sessions")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", status, body)
		}
		var sessions struct {
			Sessions []struct {
				ID       string `json:"id"`
				PigeonID string `json:"pigeonId"`
				Lang     string `json:"lang"`
			} `json:"sessions"`
		}
		if err := json.Unmarshal(body, &sessions); err != nil {
			t.Fatalf("sessies-body %s: %v", body, err)
		}
		var gevonden bool
		for _, s := range sessions.Sessions {
			if s.ID == sessionID {
				gevonden = true
				if s.PigeonID != "individualist" || s.Lang != "nl" {
					t.Errorf("sessie kwam verminkt terug: %+v", s)
				}
			}
		}
		if !gevonden {
			t.Fatalf("sessie %s staat niet in de lijst: %s", sessionID, body)
		}
		if strings.Contains(string(body), "userId") || strings.Contains(string(body), "user_id") {
			t.Errorf("de user-id hoort niet op de wire: %s", body)
		}

		status, body = srv.get(t, "/api/duif/v1/chat/sessions/"+sessionID+"/messages")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", status, body)
		}
		var msgs struct {
			Messages []struct {
				Role       string            `json:"role"`
				Content    string            `json:"content"`
				KBStatus   string            `json:"kbStatus"`
				Kind       string            `json:"kind"`
				Persona    string            `json:"persona"`
				Provenance map[string]string `json:"provenance"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(body, &msgs); err != nil {
			t.Fatalf("berichten-body %s: %v", body, err)
		}
		// Vier berichten: de vraag, het kader, het duif-blok en de bemiddeling,
		// in de volgorde waarin ze op het scherm stonden.
		if len(msgs.Messages) != 4 {
			t.Fatalf("aantal berichten = %d, want 4 (vraag, kader, duif, bemiddeling): %s", len(msgs.Messages), body)
		}
		if msgs.Messages[0].Role != "user" || !strings.Contains(msgs.Messages[0].Content, "Coolsingel") {
			t.Errorf("eerste bericht = %+v, want de gestelde vraag", msgs.Messages[0])
		}
		if msgs.Messages[0].Kind != "text" {
			t.Errorf("het userbericht hoort kind text te dragen: %+v", msgs.Messages[0])
		}
		if msgs.Messages[1].Persona != "facilitator" || msgs.Messages[1].Content != fixtureRouterFraming {
			t.Errorf("tweede bericht = %+v, want de kaderrij vóór de duif", msgs.Messages[1])
		}
		if msgs.Messages[3].Persona != "facilitator" || msgs.Messages[3].Content != fixtureRouterMediation {
			t.Errorf("vierde bericht = %+v, want de bemiddeling ná de duif", msgs.Messages[3])
		}
		duif := msgs.Messages[2]
		if duif.Role != "assistant" || duif.Kind != "pigeon_answer" || duif.Persona != "individualist" ||
			!strings.Contains(duif.Content, "Roekoe") {
			t.Errorf("tweede bericht = %+v, want het duif-blok", duif)
		}
		if duif.Content != sessionAnswer || !reflect.DeepEqual(duif.Provenance, sessionProvenance) {
			t.Fatal("reloaded pigeon lost final text or turn provenance")
		}
		if duif.KBStatus != "gap" {
			t.Errorf("kbStatus = %q, want gap", duif.KBStatus)
		}
		if msgs.Messages[0].Provenance != nil {
			t.Fatal("provenance was reassigned to a human")
		}
		// De vraag die de facilitator aan de duif stelde blijft server-side.
		if strings.Contains(string(body), "question") {
			t.Errorf("de facilitator-vraag hoort niet op de wire: %s", body)
		}
	})

	t.Run("vervolgturn voert de historie uit Postgres mee", func(t *testing.T) {
		beforeRequests := len(srv.llm.requests())
		if sessionID == "" {
			t.Fatal("voorwaarde ontbreekt: de vorige chatturn leverde geen sessie op")
		}
		status, frames, raw := srv.postTurn(t, map[string]any{
			"sessionId": sessionID,
			"pigeonId":  "individualist",
			"message":   "En de Erasmusbrug?",
			"lang":      "nl",
		})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", status, raw)
		}
		done := lastFrame(t, frames, "done")
		if done["sessionId"] != sessionID {
			t.Errorf("sessionId = %v, want %s (dezelfde sessie)", done["sessionId"], sessionID)
		}
		// Beide historiesporen moeten uit Postgres komen: de facilitator ziet
		// zijn eigen gesprek terug als messages, en het eerdere duif-antwoord
		// verbatim in de ambient-context (nooit als zijn eigen assistant-tekst).
		reqs := srv.llm.requests()[beforeRequests:]
		assertAnswerRequests(t, reqs, "individualist", "nl", false, false, []string{"Wat vind jij van de Coolsingel?", "En de Erasmusbrug?"}, artifactText(frames))
		facilitator := reqs[0]
		var zagVraag, zagDuifAmbient bool
		for _, m := range facilitator.Messages {
			if m.Role == "user" && strings.Contains(m.Content, "Coolsingel") {
				zagVraag = true
			}
			if m.Role != "user" && m.Role != "tool" && strings.Contains(m.Content, "Roekoe") {
				zagDuifAmbient = true
			}
			if m.Role == "assistant" && strings.Contains(m.Content, "Roekoe") {
				t.Error("een duif-antwoord mag niet als assistant-bericht in de facilitator-historie staan")
			}
		}
		if !zagVraag {
			t.Error("de eerdere vraag uit Postgres ontbreekt in de facilitator-historie")
		}
		if !zagDuifAmbient {
			t.Error("het eerdere duif-antwoord ontbreekt in de ambient-context")
		}
	})

	t.Run("kennisgat zonder bronnen", func(t *testing.T) {
		beforeRequests := len(srv.llm.requests())
		voor := srv.llm.embedCallCount()
		status, frames, raw := srv.postTurn(t, map[string]any{
			"pigeonId": "biological",
			"message":  "Wat zegt het onderzoek over duiven op de kade?",
		})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", status, raw)
		}
		types := frameTypes(frames)
		for _, tp := range types {
			if tp == "sources" {
				t.Fatalf("zonder bron mag er geen sources-frame zijn: %v", types)
			}
		}
		done := lastFrame(t, frames, "done")
		if done["kbStatus"] != "gap" {
			t.Errorf("kbStatus = %v, want gap (lege kennisbank)", done["kbStatus"])
		}
		if srv.llm.embedCallCount() <= voor {
			t.Errorf("de KB-duif moet de vraag embedden (embed-aanroepen: %d → %d)", voor, srv.llm.embedCallCount())
		}
		if done["stopReason"] != "natural" || artifactText(frames) == "" {
			t.Fatal("knowledge gap completed without a verified answer")
		}
		assertAnswerRequests(t, srv.llm.requests()[beforeRequests:], "biological", "en", false, false, []string{"Wat zegt het onderzoek over duiven op de kade?"}, artifactText(frames))

	})

	t.Run("capfout levert llm_budget_exceeded", func(t *testing.T) {
		// A budget rejection deliberately pauses this process. Isolate its provider brake.
		srv := bootServer(t)
		_, frames, raw := srv.postTurn(t, map[string]any{
			"pigeonId": "individualist",
			"message":  "Vertel eens iets. " + budgetTrigger,
			"lang":     "nl",
		})
		fout := lastFrame(t, frames, "error")
		if fout["error"] != "llm_budget_exceeded" {
			t.Fatalf("error = %v, want llm_budget_exceeded (frames %v)", fout["error"], frameTypes(frames))
		}
		msg, _ := fout["message"].(map[string]any)
		nl, _ := msg["nl"].(string)
		en, _ := msg["en"].(string)
		if !strings.Contains(nl, "modelbudget") || en == "" {
			t.Errorf("melding = %+v, want de nette tweetalige capmelding", msg)
		}
		// De upstream-fouttekst mag nooit op de wire staan.
		for _, lek := range []string{"BudgetExceededError", "9.87654", "litellm"} {
			if strings.Contains(raw, lek) {
				t.Errorf("upstream-detail %q lekt naar de client:\n%s", lek, raw)
			}
		}
		if frameTypes(frames)[len(frames)-1] != "done" {
			t.Errorf("frames = %v, want afgesloten met done", frameTypes(frames))
		}
	})

	t.Run("onbekende duif is een nette 400", func(t *testing.T) {
		status, _, raw := srv.postTurn(t, map[string]any{"pigeonId": "flamingo", "message": "hoi"})
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %s)", status, raw)
		}
		if !strings.Contains(raw, "invalid_request") {
			t.Errorf("body = %s, want de invalid_request-code", raw)
		}
	})

	t.Run("niet-chattende duif mag niet chatten", func(t *testing.T) {
		status, _, raw := srv.postTurn(t, map[string]any{"pigeonId": "nonverbal", "message": "hoi"})
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %s)", status, raw)
		}
	})

	t.Run("onbekende sessie is 404", func(t *testing.T) {
		status, body := srv.get(t, "/api/duif/v1/chat/sessions/bestaat-niet/messages")
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (body %s)", status, body)
		}
	})

	// De basis van de vergadermodus op de echte server: sessiekanaal, knop,
	// stopknop en sluiting — plus de harde eis dat een vergadering niets
	// opslaat: van de vergaderzin blijft nergens iets achter.
	t.Run("vergadering: kanaal, knop en niets-persisteren", func(t *testing.T) {
		const vergaderzin = "sentinelzin-e2e-4412: wij willen de duiven van het plein weren"

		chatSessiesVoor := len(srv.chatSessions(t))

		status, body := srv.postJSON(t, http.MethodPost, "/api/duif/v1/meeting/sessions",
			map[string]any{"pigeonId": "individualist", "lang": "nl"})
		if status != http.StatusCreated {
			t.Fatalf("vergadersessie openen: status = %d, body = %s", status, body)
		}
		var geopend struct {
			SessionID string `json:"sessionId"`
			Listening string `json:"listening"`
			Voice     string `json:"voice"`
		}
		if err := json.Unmarshal([]byte(body), &geopend); err != nil || geopend.SessionID == "" {
			t.Fatalf("sessiebody = %s (%v)", body, err)
		}
		if geopend.Listening != "listening" {
			t.Errorf("luister-indicator = %q, want listening", geopend.Listening)
		}
		// De UI hoort meteen te weten of deze duif klinkt. Zonder
		// providersleutel is dat hier "off"; nooit een leeg veld.
		if geopend.Voice != "ready" && geopend.Voice != "off" {
			t.Errorf("stemstatus = %q, want ready of off", geopend.Voice)
		}
		basis := "/api/duif/v1/meeting/sessions/" + geopend.SessionID
		srv.cleanupMeeting(t, geopend.SessionID)

		events, stopStream := srv.openMeetingStream(t, geopend.SessionID)
		defer stopStream()
		if eerste := nextMeetingEvent(t, events, "state"); eerste["listening"] != "listening" {
			t.Errorf("statusframe = %+v", eerste)
		}

		if status, body := srv.postJSON(t, http.MethodPost, basis+"/transcript",
			map[string]any{"text": vergaderzin, "speaker": "voorzitter"}); status != http.StatusAccepted {
			t.Fatalf("transcriptsegment: status = %d, body = %s", status, body)
		}
		if ev := nextMeetingEvent(t, events, "transcript"); ev["text"] != vergaderzin {
			t.Errorf("het kanaal hoort het transcript live te tonen: %+v", ev)
		}

		beforeRequests := len(srv.llm.requests())
		if status, body := srv.postJSON(t, http.MethodPost, basis+"/respond", nil); status != http.StatusAccepted {
			t.Fatalf("de knop: status = %d, body = %s", status, body)
		}
		nextMeetingEvent(t, events, "persona")
		var gesproken strings.Builder
		for {
			ev := nextMeetingEvent(t, events, "")
			if ev["type"] == "text" {
				d, _ := ev["delta"].(string)
				gesproken.WriteString(d)
			}
			if ev["type"] == "error" {
				t.Fatal("manual meeting returned a terminal error")
			}
			if ev["type"] == "response_done" {
				if ev["kbStatus"] != "gap" {
					t.Fatal("meeting did not complete its verified gap answer")
				}
				break
			}
		}
		if !strings.Contains(gesproken.String(), "Roekoe") {
			t.Errorf("gesproken tekst = %q, want het stub-antwoord", gesproken.String())
		}

		assertAnswerRequests(t, srv.llm.requests()[beforeRequests:], "individualist", "nl", true, false, []string{vergaderzin}, gesproken.String())
		if !strings.Contains(gesproken.String(), fixturePositionNL) {
			t.Fatal("meeting lost the appended designed position")
		}

		if status, body := srv.postJSON(t, http.MethodPost, basis+"/stop", nil); status != http.StatusAccepted {
			t.Fatalf("stopknop: status = %d, body = %s", status, body)
		}
		nextMeetingEvent(t, events, "stopped")

		// De knoppen die het vergaderscherm verder gebruikt (de instellingen en
		// de bevestiging) doen het op de echte server ook.
		status, body = srv.postJSON(t, http.MethodPost, basis+"/settings",
			map[string]any{"autonomous": true, "eagerness": "eager"})
		if status != http.StatusAccepted {
			t.Fatalf("instellingen: status = %d, body = %s", status, body)
		}
		var instellingen struct {
			Autonomous bool   `json:"autonomous"`
			Eagerness  string `json:"eagerness"`
		}
		if err := json.Unmarshal([]byte(body), &instellingen); err != nil {
			t.Fatalf("instellingenbody = %s (%v)", body, err)
		}
		if !instellingen.Autonomous || instellingen.Eagerness != "eager" {
			t.Errorf("instellingen kwamen niet aan: %+v", instellingen)
		}
		if status, body := srv.postJSON(t, http.MethodPost, basis+"/presence", nil); status != http.StatusAccepted {
			t.Fatalf("aanwezigheid bevestigen: status = %d, body = %s", status, body)
		}

		if status, body := srv.postJSON(t, http.MethodDelete, basis, nil); status != http.StatusNoContent {
			t.Fatalf("sessie sluiten: status = %d, body = %s", status, body)
		}
		if ev := nextMeetingEvent(t, events, "session_closed"); ev["reason"] != "ended_by_user" {
			t.Errorf("sluitreden = %+v", ev)
		}
		if status, _ := srv.postJSON(t, http.MethodPost, basis+"/respond", nil); status != http.StatusNotFound {
			t.Errorf("een gesloten vergadersessie bestaat niet meer (status %d)", status)
		}

		// Niets opslaan, op de echte server: geen chatsessie erbij, en de
		// vergaderzin staat nergens in het serverlog.
		if na := len(srv.chatSessions(t)); na != chatSessiesVoor {
			t.Errorf("aantal opgeslagen sessies %d → %d: vergaderdata hoort nergens te landen", chatSessiesVoor, na)
		}
		if strings.Contains(srv.logs.String(), vergaderzin) {
			t.Error("de vergaderzin staat in het serverlog")
		}
	})

	// De non-verbale duif heeft geen spreekbeurt maar een geluid: de handmatige
	// knop moet op de echte server een afhaalbaar audiobestand opleveren.
	// De huidige bestanden zijn tijdelijke geluiden, geen gevalideerde veldopnames.
	t.Run("vergadering: de non-verbale duif laat zich horen", func(t *testing.T) {
		status, body := srv.postJSON(t, http.MethodPost, "/api/duif/v1/meeting/sessions",
			map[string]any{"pigeonId": "nonverbal", "lang": "en"})
		if status != http.StatusCreated {
			t.Fatalf("vergadersessie openen: status = %d, body = %s", status, body)
		}
		var geopend struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal([]byte(body), &geopend); err != nil || geopend.SessionID == "" {
			t.Fatalf("sessiebody = %s (%v)", body, err)
		}
		basis := "/api/duif/v1/meeting/sessions/" + geopend.SessionID
		srv.cleanupMeeting(t, geopend.SessionID)
		events, stopStream := srv.openMeetingStream(t, geopend.SessionID)
		defer stopStream()
		// Controleer ook tussenliggende frames: signaling geeft geen menselijke
		// relevantie-/emotieduiding mee op het deelnemerskanaal.
		nextNeutral := func(typ string) sseFrame {
			for {
				ev := nextMeetingEvent(t, events, "")
				if ev["type"] == "arousal" || ev["type"] == "topic_hint" || ev["arousal"] != nil || ev["hit"] != nil {
					t.Fatalf("signaling lekt betekenis op het sessiekanaal: %+v", ev)
				}
				if ev["type"] == typ {
					return ev
				}
			}
		}
		nextNeutral("state")

		if status, body := srv.postJSON(t, http.MethodPost, basis+"/intervene", nil); status != http.StatusAccepted {
			t.Fatalf("de ingrijpknop: status = %d, body = %s", status, body)
		}
		geluid := nextNeutral("sound")
		if geluid["eventId"] == nil || geluid["turnId"] == nil {
			t.Fatalf("sound-event mist deduplicatie/beurtidentiteit: %+v", geluid)
		}
		ref, _ := geluid["sound"].(map[string]any)
		url, _ := ref["url"].(string)
		if url == "" {
			t.Fatalf("sound-event zonder url: %+v", geluid)
		}
		if status, mp3 := srv.get(t, url); status != http.StatusOK || len(mp3) == 0 {
			t.Fatalf("geluidsbestand %s: status = %d, %d bytes", url, status, len(mp3))
		}
		if status, body := srv.postJSON(t, http.MethodPost, basis+"/stop", nil); status != http.StatusAccepted {
			t.Fatalf("signaling stoppen: status = %d, body = %s", status, body)
		}
		if ev := nextNeutral("stopped"); ev["autonomyPaused"] != true {
			t.Fatalf("stop pauzeert automatische signaling niet: %+v", ev)
		}

		if status, body := srv.postJSON(t, http.MethodDelete, basis, nil); status != http.StatusNoContent {
			t.Fatalf("sessie sluiten: status = %d, body = %s", status, body)
		}
		if ev := nextNeutral("session_closed"); ev["reason"] != "ended_by_user" {
			t.Fatalf("onverwachte sluitreden: %+v", ev)
		}
	})

	// De module levert haar frontend als één bundle die de core-shell mount
	// (/modules/duif/*). Zonder gebouwde bundle valt er niets te toetsen: dan
	// slaat de e2e deze controle over in plaats van te falen — externe
	// bijdragers zonder bun (en zonder @parai/ui) houden een bruikbare e2e.
	t.Run("module-bundle wordt geserveerd", func(t *testing.T) {
		bundle := filepath.Join(env(t, "DUIF_E2E_WORKDIR"), "duif", "frontend", "dist", "bundle.js")
		if _, err := os.Stat(bundle); err != nil {
			t.Skipf("geen gebouwde bundle (%s): draai `make bundle` om deze controle mee te nemen", bundle)
		}

		resp, err := http.Get(srv.baseURL + "/modules/duif/bundle.js")
		if err != nil {
			t.Fatalf("bundle ophalen: %v", err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("bundle lezen: %v", err)
		}
		if resp.StatusCode != http.StatusOK || len(raw) == 0 {
			t.Fatalf("bundle: status = %d, %d bytes", resp.StatusCode, len(raw))
		}
		// De koppen (Content-Type, Cache-Control) zet de core-shell; dat is
		// zijn contract, niet dat van deze module. Hier telt alleen dat de
		// module zijn bundle aanlevert.
		// Het losse vergaderscherm van slice 1 bestaat niet meer: de module
		// leeft nu in de shell.
		if status, _ := srv.get(t, "/modules/duif/meeting.html"); status != http.StatusNotFound {
			t.Errorf("meeting.html hoort weg te zijn (status %d)", status)
		}
	})
}

// postJSON stuurt een besturingsbericht naar de module-API.
func (s *duifServer) postJSON(t *testing.T, method, path string, body any) (int, string) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, s.baseURL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s lezen: %v", method, path, err)
	}
	return resp.StatusCode, string(raw)
}

// chatSessions geeft de opgeslagen chatsessies — de teller waarmee de e2e
// aantoont dat een vergadering níets aan de database toevoegt.
func (s *duifServer) chatSessions(t *testing.T) []map[string]any {
	t.Helper()
	status, body := s.get(t, "/api/duif/v1/chat/sessions")
	if status != http.StatusOK {
		t.Fatalf("sessies opvragen: status = %d (body %s)", status, body)
	}
	var out struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("sessies-body %s: %v", body, err)
	}
	return out.Sessions
}

// openMeetingStream abonneert op het sessiekanaal (SSE) van een vergadering.
func (s *duifServer) openMeetingStream(t *testing.T, sessionID string) (<-chan sseFrame, func()) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, s.baseURL+"/api/duif/v1/meeting/sessions/"+sessionID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		cancel()
		t.Fatalf("sessiekanaal openen: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		t.Fatalf("sessiekanaal status = %d", resp.StatusCode)
	}
	ch := make(chan sseFrame, 256)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var f sseFrame
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &f); err != nil {
				return
			}
			ch <- f
		}
	}()
	return ch, cancel
}

// nextMeetingEvent wacht op het volgende event (van dit type, of het eerste
// het beste als typ leeg is).
func nextMeetingEvent(t *testing.T, ch <-chan sseFrame, typ string) sseFrame {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("sessiekanaal ging dicht vóór een %q-event", typ)
			}
			if typ == "" || ev["type"] == typ {
				return ev
			}
		case <-deadline:
			t.Fatalf("geen %q-event binnen de deadline", typ)
		}
	}
}

// Een vergeten module/.env is lokaal de meest voorkomende bootfout. De binary
// moet daar met exitcode 1 en een bruikbare melding op reageren, niet met een
// core-interne configfout of een half opgestarte server.
func TestBoot_WithoutDatabaseURL_ExitsWithUsefulMessage(t *testing.T) {
	binary := env(t, "DUIF_E2E_BINARY")
	workdir := env(t, "DUIF_E2E_WORKDIR")

	cmd := exec.Command(binary)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), "PARAI_DB_URL=", "PORT="+fmt.Sprint(freePort(t)))
	if dir := os.Getenv("DUIF_E2E_COVERDIR"); dir != "" {
		cmd.Env = append(cmd.Env, "GOCOVERDIR="+dir)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run zonder PARAI_DB_URL = %v, want een exitcode ≠ 0 (uitvoer: %s)", err, out.String())
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("exitcode = %d, want 1", exitErr.ExitCode())
	}
	if !strings.Contains(out.String(), "PARAI_DB_URL") || !strings.Contains(out.String(), ".env") {
		t.Errorf("uitvoer = %q; moet PARAI_DB_URL én .env noemen", out.String())
	}
}
