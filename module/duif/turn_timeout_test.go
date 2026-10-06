package duif

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
	"github.com/parai-nl/parai-core/pkg/agent"
)

// Een beurt die de harde tijdgrens haalt hoort zichtbaar te eindigen: een
// error-frame met een reden en daarna done, allebei nog vóór de
// schrijfdeadline van de verbinding. Verlopen de beurt-context en de
// schrijfdeadline op dezelfde seconde, dan sluit de stream zonder één van
// beide frames — en blijft de lezer op de statusregel hangen.
//
// Deze tests draaien daarom op een échte verbinding (httptest.NewServer): een
// ResponseRecorder kent geen deadlines en zou juist het gedrag wegnemen dat
// hier bewezen moet worden.

// stallingLLM is een trage proxy, maar betrouwbaar: hij opent
// netjes een stream en levert er nooit iets op. Hij laat pas los als de
// context omvalt, precies zoals een provider die blijft hangen.
type stallingLLM struct{ *llmtesting.MockService }

func (stallingLLM) CompleteStream(ctx context.Context, _ llm.CompletionRequest) (<-chan llm.StreamChunk, error) {
	out := make(chan llm.StreamChunk)
	go func() {
		<-ctx.Done()
		close(out)
	}()
	return out, nil
}

// shortBudget verkort een van de twee grenzen voor de duur van één test.
func shortBudget(t *testing.T, budget *time.Duration, d time.Duration) {
	t.Helper()
	original := *budget
	*budget = d
	t.Cleanup(func() { *budget = original })
}

// timedTurnServer draait de echte chatroute achter de echte grens van een
// beurt. De middleware is niet nagebouwd: hij roept applyTurnBudget aan, exact
// zoals accessMiddleware dat doet — alleen de toegangscontrole eromheen (met
// haar database) blijft weg.
func timedTurnServer(t *testing.T, m *Module, budget time.Duration) *httptest.Server {
	t.Helper()
	r := chi.NewRouter()
	withTestUser(r)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, rq *http.Request) {
			rq, done := applyTurnBudget(w, rq, budget)
			defer done()
			next.ServeHTTP(w, rq)
		})
	})
	m.Routes(r, nil, nil)
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return server
}

// turnOverTheWire doet één echte POST en leest de stream tot hij sluit.
func turnOverTheWire(t *testing.T, server *httptest.Server, body map[string]any) ([]sseFrame, time.Duration) {
	t.Helper()
	started := time.Now()
	res, err := server.Client().Post(server.URL+"/v1/chat/turn", "application/json", turnBody(t, body))
	if err != nil {
		t.Fatalf("POST /v1/chat/turn: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("de stream brak af in plaats van te sluiten: %v", err)
	}
	return parseSSE(t, string(raw)), time.Since(started)
}

// assertTimeoutTail toetst de vereiste staart: een error met de
// reden, en daarna done.
func assertTimeoutTail(t *testing.T, frames []sseFrame, elapsed time.Duration, budget time.Duration) {
	t.Helper()
	if len(frames) < 2 {
		t.Fatalf("frames = %v, want minstens error + done", frameTypes(frames))
	}
	failure, closing := frames[len(frames)-2], frames[len(frames)-1]
	if failure["type"] != "error" {
		t.Fatalf("voorlaatste frame = %v, want error (%v)", failure["type"], frameTypes(frames))
	}
	if failure["error"] != ReasonTurnTimeout {
		t.Errorf("error.error = %v, want %q", failure["error"], ReasonTurnTimeout)
	}
	message, _ := failure["message"].(map[string]any)
	if message["nl"] != errorMessages[ReasonTurnTimeout]["nl"] || message["en"] != errorMessages[ReasonTurnTimeout]["en"] {
		t.Errorf("error.message = %v, want de tweetalige catalogustekst", message)
	}
	if closing["type"] != "done" {
		t.Fatalf("laatste frame = %v, want done (%v)", closing["type"], frameTypes(frames))
	}
	if elapsed < budget {
		t.Errorf("de beurt eindigde na %v, vóór de grens van %v", elapsed, budget)
	}
	if elapsed > budget+turnWriteGrace {
		t.Errorf("de frames kwamen na %v, ná de schrijfdeadline op %v", elapsed, budget+turnWriteGrace)
	}
}

// De agent-motor: de facilitator wacht op een provider die niets stuurt.
func TestAgentTurn_TurnBudgetReached_WireGetsErrorAndDone(t *testing.T) {
	budget := 300 * time.Millisecond
	m, repo := newTestModule(t, func(m *Module) { m.llm = stallingLLM{llmtesting.NewMockService()} })
	server := timedTurnServer(t, m, budget)

	frames, elapsed := turnOverTheWire(t, server, map[string]any{"pigeonId": "individualist", "message": "hoi", "lang": "nl"})

	assertTimeoutTail(t, frames, elapsed, budget)
	if got := frames[len(frames)-1]["stopReason"]; got != "error" {
		t.Errorf("done.stopReason = %v, want error", got)
	}
	// De vraag van de mens blijft staan: een afgekapte beurt verliest hem niet.
	if got := len(repo.messagesWithRole("user")); got != 1 {
		t.Errorf("mensberichten = %d, want 1", got)
	}
}

// De pipeline-motor komt op hetzelfde uit; de bug zat in beide paden.
func TestPipelineTurn_TurnBudgetReached_WireGetsErrorAndDone(t *testing.T) {
	budget := 300 * time.Millisecond
	m, _ := newTestModule(t, func(m *Module) { m.llm = stallingLLM{llmtesting.NewMockService()} })
	usePipeline(t, m, "individualist")
	server := timedTurnServer(t, m, budget)

	frames, elapsed := turnOverTheWire(t, server, map[string]any{"pigeonId": "individualist", "message": "hoi", "lang": "nl"})

	assertTimeoutTail(t, frames, elapsed, budget)
	if got := frames[len(frames)-1]["stopReason"]; got != "error" {
		t.Errorf("done.stopReason = %v, want error", got)
	}
}

// afterTheBudget draait één verzoek waarin de handler op de grens wacht en er
// dán nog een regel uit probeert te krijgen. Hij geeft terug wat de lezer
// ontving en met welke reden de context omviel.
//
// De vorm van het verzoek staat erin omdat zij ertoe doet. Een POST met een
// gelezen body is de vorm van /v1/chat/turn; een verzoek zonder body laat
// net/http al een achtergrondlezer op de verbinding staan, en dat is het geval
// waarin een te vroege leesdeadline de request-context zou annuleren. Beide
// horen op de tijdgrens uit te komen en niet op een annulering.
func afterTheBudget(t *testing.T, mount func(chi.Router, http.HandlerFunc), path string, withBody bool) (string, error) {
	t.Helper()
	var reason error
	klaar := make(chan struct{})
	r := chi.NewRouter()
	mount(r, func(w http.ResponseWriter, rq *http.Request) {
		defer close(klaar)
		if withBody {
			var body map[string]any
			if err := json.NewDecoder(rq.Body).Decode(&body); err != nil {
				t.Errorf("body lezen: %v", err)
			}
		}
		w.WriteHeader(http.StatusOK)
		<-rq.Context().Done()
		reason = rq.Context().Err()
		// Dit is wat zonder de marge niet lukt: schrijven ná de grens.
		_, _ = io.WriteString(w, "na de grens")
	})
	server := httptest.NewServer(r)
	defer server.Close()

	var res *http.Response
	var err error
	if withBody {
		res, err = server.Client().Post(server.URL+path, "application/json", strings.NewReader(`{"vraag":"hoi"}`))
	} else {
		res, err = server.Client().Get(server.URL + path)
	}
	if err != nil {
		t.Fatalf("verzoek: %v", err)
	}
	defer res.Body.Close()
	got, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("de verbinding sloot vóór de laatste byte: %v", err)
	}
	<-klaar
	return string(got), reason
}

// De marge is het hele mechanisme: de context van de beurt valt om terwijl de
// verbinding nog schrijfbaar is. Zonder dat verschil kan geen enkele motor zijn
// annulering nog melden.
func TestApplyTurnBudget_ConnectionOutlivesTheContext(t *testing.T) {
	budget := 200 * time.Millisecond
	for naam, metBody := range map[string]bool{"POST met body (de vorm van de chatroute)": true, "verzoek zonder body": false} {
		t.Run(naam, func(t *testing.T) {
			mount := func(r chi.Router, handler http.HandlerFunc) {
				r.Use(func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, rq *http.Request) {
						rq, done := applyTurnBudget(w, rq, budget)
						defer done()
						next.ServeHTTP(w, rq)
					})
				})
				r.Handle("/v1/probe", handler)
			}
			body, reason := afterTheBudget(t, mount, "/v1/probe", metBody)
			if body != "na de grens" {
				t.Errorf("body = %q, want de tekst die ná de contextgrens geschreven werd", body)
			}
			if !errors.Is(reason, context.DeadlineExceeded) {
				t.Errorf("ctx.Err() = %v, want DeadlineExceeded — een andere reden leest als een weggelopen lezer", reason)
			}
		})
	}
}

// Dezelfde marge, maar dan zoals accessMiddleware hem in productie legt, en op
// de vorm van de chatroute: een POST met een body. Het pad is bewust een ander
// (de chatroute leest eerst haar kennisgeving uit Postgres); de deadlines
// komen uit dezelfde aanroep en verschillen alleen in het getal.
func TestAccessMiddleware_WriteWindowOutlivesTheRequestBudget(t *testing.T) {
	shortBudget(t, &requestBudget, 200*time.Millisecond)
	m, _ := newTestModule(t, nil)
	m.access = newAccessPolicy(nil)
	mount := func(r chi.Router, handler http.HandlerFunc) {
		withTestUser(r)
		r.Use(m.accessMiddleware)
		r.Handle("/v1/probe", handler)
	}
	body, reason := afterTheBudget(t, mount, "/v1/probe", true)
	if body != "na de grens" || !errors.Is(reason, context.DeadlineExceeded) {
		t.Errorf("body = %q, ctx.Err() = %v; want een schrijfbare verbinding na een verlopen context", body, reason)
	}
}

// De grens kan ook vallen terwijl de beurt al aan het afronden is: handleDone
// heeft zeven takken en elke tak schrijft zijn eigen error. Liet emitFrame een
// verlopen context de deur dichtdoen, dan verdwenen die error én de done die
// erop volgt, en sloot de stroom alsnog stil — hetzelfde stille einde als
// hierboven, maar via een ander pad.
func TestAgentRun_DeadlinePassedWhileFinishing_StillWritesErrorAndDone(t *testing.T) {
	afronding := map[string]func(r *agentRun, ctx context.Context){
		// De kale volgorde van runAgentTurn na een tak van handleDone.
		"emitError en finish": func(r *agentRun, ctx context.Context) {
			r.emitError(ctx, ReasonInternal)
			r.persist(ctx)
			r.finish(ctx)
		},
		// En dezelfde stand via handleDone zelf, zodat het pad meetelt en niet
		// alleen zijn losse stappen.
		"handleDone met een provider-fout": func(r *agentRun, ctx context.Context) {
			r.handleDone(ctx, agent.Done{Reason: agent.StopReasonError, Err: errors.New("transport dicht")})
			r.persist(ctx)
			r.finish(ctx)
		},
	}
	for naam, afronden := range afronding {
		t.Run(naam, func(t *testing.T) {
			m, _ := newTestModule(t, func(m *Module) { m.llm = llmtesting.NewMockService() })
			pt, terr := m.prepareTurn(t.Context(), testUser.ID, turnRequest{PigeonID: "individualist", Message: "hoi"})
			if terr != nil {
				t.Fatalf("prepareTurn: %+v", terr)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
			defer cancel()
			var types []string
			r := &agentRun{
				m: m, at: m.newAgentTurn(pt), pt: pt, cancel: cancel, started: time.Now(),
				emit: func(frame any) error {
					switch frame.(type) {
					case errorFrame:
						types = append(types, "error")
					case doneFrame:
						types = append(types, "done")
					case textFrame:
						types = append(types, "text")
					}
					return nil
				},
			}
			time.Sleep(5 * time.Millisecond) // de grens is nu verlopen

			afronden(r, ctx)

			if len(types) < 2 || types[len(types)-2] != "error" || types[len(types)-1] != "done" {
				t.Fatalf("frames = %v, want een error gevolgd door done", types)
			}
			if r.aborted {
				t.Error("de beurt gold als weggelopen lezer; de grens verliep, de lezer zat er nog")
			}
		})
	}
}

// De keerzijde blijft staan: een lezer die zélf wegloopt krijgt geen done. Die
// twee gevallen zijn nu uit elkaar gehaald, en dat mag het oude gedrag niet
// meegenomen hebben.
func TestAgentTurn_ClientGoneIsNotATimeout_StillNoDone(t *testing.T) {
	m, _ := newTestModule(t, func(m *Module) { m.llm = stallingLLM{llmtesting.NewMockService()} })
	ctx, cancel := context.WithCancel(t.Context())
	pt, terr := m.prepareTurn(ctx, testUser.ID, turnRequest{PigeonID: "individualist", Message: "hoi"})
	if terr != nil {
		t.Fatalf("prepareTurn: %+v", terr)
	}
	var types []string
	klaar := make(chan struct{})
	go func() {
		defer close(klaar)
		m.runAgentTurn(ctx, pt, func(frame any) error {
			switch frame.(type) {
			case doneFrame:
				types = append(types, "done")
			case errorFrame:
				types = append(types, "error")
			}
			return nil
		})
	}()
	time.AfterFunc(50*time.Millisecond, cancel)
	select {
	case <-klaar:
	case <-time.After(5 * time.Second):
		t.Fatal("runAgentTurn hangt na een weggelopen lezer")
	}
	if len(types) != 0 {
		t.Errorf("frames = %v, want niets: er is niemand meer om het aan te vertellen", types)
	}
}

// De reden hoort in de tweetalige catalogus te staan en niet stil op de
// terugval van errorBody te leunen.
func TestTurnTimeout_ReasonHasItsOwnBilingualMessage(t *testing.T) {
	body := errorBody(ReasonTurnTimeout)
	if body.Error != ReasonTurnTimeout {
		t.Fatalf("errorBody.Error = %q", body.Error)
	}
	for _, lang := range []string{"nl", "en"} {
		if strings.TrimSpace(body.Message[lang]) == "" {
			t.Errorf("melding in %q ontbreekt", lang)
		}
	}
	if body.Message["nl"] == errorMessages[ReasonLLMUnavailable]["nl"] {
		t.Error("turn_timeout valt terug op llm_unavailable; dan zegt de melding het verkeerde")
	}
}
