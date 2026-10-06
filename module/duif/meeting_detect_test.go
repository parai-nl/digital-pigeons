package duif

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// --- testgereedschap voor de kandidaatgenerator ------------------------------

// candidateLLM is de nep-generator: elke kandidaatcall levert het volgende
// gescripte antwoord (het laatste herhaalt zich), zodat een test een hele reeks
// evaluaties kan uitlokken zonder script bij te schrijven. De knop-route krijgt
// een vaste tekst, zodat één nep-LLM zowel de generator als de knop-route kan
// voeden.
//
// Het onderscheid komt uit de systemprompt: alleen de kandidaatprompt legt de
// kopregelafspraak uit. Dat is precies de scheiding die de code zelf maakt.
type candidateLLM struct {
	mu     sync.Mutex
	calls  []llm.CompletionRequest
	querys []llm.CompletionRequest
	steps  []candidateStep
	served int

	turnText string
}

type candidateStep struct {
	body   string
	err    error
	chunks []string // niet-leeg: lever het antwoord in deze stukken
	// stall: de provider neemt de aanvraag aan en stuurt daarna niets meer. De
	// stroom hangt tot de context omvalt — de vorm van een tijdslimiet.
	stall bool
	// stallErr: wat er ná die annulering nog uit het kanaal komt. Leeg is de
	// kale sluiting; gevuld is de foutchunk die de echte adapter soms nog
	// aflevert en soms niet.
	stallErr error
	// stallBuffered: dezelfde foutchunk, maar hij stond al in de buffer toen
	// de tijdslimiet toesloeg. De leeslus vindt dan bij zijn eerste select
	// allebei klaar staan en kiest willekeurig — precies de race waarin één
	// stilstand onder twee diagnostiekcodes belandde.
	stallBuffered bool
	// finish is de finish_reason op de laatste chunk. Leeg betekent "stop":
	// het model was uitgeschreven. "length" en "content_filter" zijn de twee
	// waarop de provider zelf afsloot.
	finish string
	// chunkErr is een echte providerfout midden in een levende stroom: de
	// inhoud hierboven komt door, dan gaat de provider stuk. Geen annulering,
	// geen tijdslimiet — precies de storing waar "stream_failed" voor is, en
	// het tegengeval van stallErr.
	chunkErr error
}

func newCandidateLLM(steps ...candidateStep) *candidateLLM {
	return &candidateLLM{steps: steps, turnText: "de duif zegt iets op de knop"}
}

// speak is de kortste weg naar een geldige SPEAK-kandidaat.
func speak(score float64, topic, text string) candidateStep {
	return candidateStep{body: fmt.Sprintf("SPEAK score=%.2f topic=%s addressed=false\n%s", score, topic, text)}
}

// addressed is een kandidaat waarin iemand de duif rechtstreeks aansprak.
func addressed(score float64, topic, text string) candidateStep {
	return candidateStep{body: fmt.Sprintf("SPEAK score=%.2f topic=%s addressed=true\n%s", score, topic, text)}
}

// pass is het normale antwoord: de duif heeft niets toe te voegen.
func pass() candidateStep { return candidateStep{body: "PASS\n"} }

// raw is een antwoord dat letterlijk zo binnenkomt, voor de rommelige vormen.
func raw(body string) candidateStep { return candidateStep{body: body} }

// closedSpeak is een geldige SPEAK-kandidaat die de provider zelf afsloot: de
// laatste chunk draagt de opgegeven finish_reason en, zoals de echte adapter
// dat doet, geen letter tekst meer.
func closedSpeak(finishReason string, score float64, topic, text string) candidateStep {
	step := speak(score, topic, text)
	step.chunks = []string{step.body, ""}
	step.body, step.finish = "", finishReason
	return step
}

// broken is de provider die een stuk antwoord levert en dan stukgaat, op een
// stroom die verder gewoon leeft: geen deadline, geen annulering.
func broken(body string) candidateStep {
	return candidateStep{chunks: []string{body}, chunkErr: errors.New("test: de provider ging stuk")}
}

// stalled is de provider die de stroom opent en daarna zwijgt tot de
// tijdslimiet hem afkapt, zonder ooit een foutchunk te sturen.
func stalled() candidateStep { return candidateStep{stall: true} }

// stalledWithError is dezelfde stilstand, maar met de foutchunk die de
// adapter ná de annulering nog probeert af te leveren.
func stalledWithError() candidateStep {
	return candidateStep{stall: true, stallErr: errors.New("test: context deadline exceeded")}
}

// stalledWithBufferedError is dezelfde stilstand met de foutchunk al in de
// buffer: de leeslus krijgt de annulering en de fout tegelijk aangeboden.
func stalledWithBufferedError() candidateStep {
	step := stalledWithError()
	step.stallBuffered = true
	return step
}

func isCandidateRequest(req llm.CompletionRequest) bool {
	return len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "SPEAK score=")
}

// isMeetingQueryRequest herkent de korte herformuleringscall voor de
// kennisopzoeking. Zij staat vóór de retrieval en is geen beurt, geen kandidaat
// en geen audit; de doubles beantwoorden hem los van hun script.
func isMeetingQueryRequest(req llm.CompletionRequest) bool {
	if len(req.Messages) == 0 {
		return false
	}
	for _, prompt := range meetingQueryPrompt {
		if req.Messages[0].Content == prompt {
			return true
		}
	}
	return false
}

// testMeetingQueryLine is de zoekvraag die de doubles teruggeven. Hij is
// herkenbaar in een assertie en bevat de woorden waarop de geseede kennisbank
// van de vergadertests aanslaat.
const testMeetingQueryLine = "Wat doen geluid en licht met duiven op daken in de stad?"

// fixedStream levert één chunk en sluit — de vorm van elk kort, niet-gescript
// antwoord in deze doubles.
func fixedStream(text string) <-chan llm.StreamChunk {
	out := make(chan llm.StreamChunk, 1)
	out <- llm.StreamChunk{Content: text, FinishReason: "stop"}
	close(out)
	return out
}

func (c *candidateLLM) Complete(_ context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, req)
	return llm.CompletionResponse{Content: "{}", FinishReason: "stop"}, nil
}

func (c *candidateLLM) CompleteStream(ctx context.Context, req llm.CompletionRequest) (<-chan llm.StreamChunk, error) {
	if isMeetingQueryRequest(req) {
		c.mu.Lock()
		c.querys = append(c.querys, req)
		c.mu.Unlock()
		return fixedStream(testMeetingQueryLine), nil
	}
	c.mu.Lock()
	c.calls = append(c.calls, req)
	var step candidateStep
	if isCandidateRequest(req) {
		if len(c.steps) == 0 {
			c.mu.Unlock()
			return nil, errors.New("test: geen kandidaatscript")
		}
		i := min(c.served, len(c.steps)-1)
		c.served++
		step = c.steps[i]
	} else {
		step = candidateStep{body: c.turnText}
	}
	c.mu.Unlock()
	if step.err != nil {
		return nil, step.err
	}
	if step.stall {
		if step.stallBuffered {
			// De fout staat er al in vóór de lezer ook maar begint, en de
			// stroom komt pas terug als de tijdslimiet voorbij is.
			out := make(chan llm.StreamChunk, 1)
			out <- llm.StreamChunk{Err: step.stallErr}
			close(out)
			<-ctx.Done()
			return out, nil
		}
		// Buffer 1: de foutchunk wordt geschreven nadat de context al om is,
		// precies zoals de adapter dat doet — en of de lezer hem nog ophaalt
		// is dan juist de vraag.
		out := make(chan llm.StreamChunk, 1)
		go func() {
			defer close(out)
			<-ctx.Done()
			if step.stallErr != nil {
				out <- llm.StreamChunk{Err: step.stallErr}
			}
		}()
		return out, nil
	}
	pieces := step.chunks
	if len(pieces) == 0 {
		pieces = []string{step.body}
	}
	out := make(chan llm.StreamChunk)
	go func() {
		defer close(out)
		for i, piece := range pieces {
			chunk := llm.StreamChunk{Content: piece}
			if i == len(pieces)-1 && step.chunkErr == nil {
				chunk.FinishReason = "stop"
				if step.finish != "" {
					chunk.FinishReason = step.finish
				}
			}
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
		if step.chunkErr == nil {
			return
		}
		// De stroom leeft nog: dit is een storing, geen afkapping.
		select {
		case out <- llm.StreamChunk{Err: step.chunkErr}:
		case <-ctx.Done():
		}
	}()
	return out, nil
}

// queryCalls zijn de herformuleringsaanroepen voor de kennisopzoeking.
func (c *candidateLLM) queryCalls() []llm.CompletionRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]llm.CompletionRequest(nil), c.querys...)
}

func (c *candidateLLM) candidateCalls() []llm.CompletionRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]llm.CompletionRequest, 0, len(c.calls))
	for _, r := range c.calls {
		if isCandidateRequest(r) {
			out = append(out, r)
		}
	}
	return out
}

// generationCalls zijn alle stroomaanroepen die de duif zelf voortbrengen: de
// kandidaatcall en de knop-beurt.
func (c *candidateLLM) generationCalls() []llm.CompletionRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]llm.CompletionRequest, 0, len(c.calls))
	for _, r := range c.calls {
		if r.Stream && len(r.Messages) > 0 {
			out = append(out, r)
		}
	}
	return out
}

// candidates haalt de kandidaatgenerator van een lopende sessie op.
func (rig *meetingRig) candidates(t *testing.T, sessionID string) *candidateEngine {
	t.Helper()
	s := rig.session(t, sessionID)
	if s.candidates == nil {
		t.Fatal("deze sessie draait zonder kandidaatgenerator")
	}
	return s.candidates
}

func (rig *meetingRig) session(t *testing.T, sessionID string) *meetingSession {
	t.Helper()
	s, ok := rig.engine.session(sessionID, testUser.ID)
	if !ok {
		t.Fatalf("sessie %q bestaat niet", sessionID)
	}
	return s
}

// openMeeting opent een sessie met vrije instellingen.
func (rig *meetingRig) openMeeting(t *testing.T, body map[string]any) string {
	t.Helper()
	status, body2 := rig.do(t, http.MethodPost, "/v1/meeting/sessions", body)
	if status != http.StatusCreated {
		t.Fatalf("sessie openen: status = %d, body = %s", status, body2)
	}
	return sessionIDOf(t, body2)
}

// eagerRig is de standaardopstelling van de generatortests: cadans zonder
// wachttijd, zodat elke gecommitte zin meteen een evaluatie oplevert, en een
// kennisbank die werkelijk antwoordt — anders slaat de kennispartner elke
// evaluatie over op haar eigen bronregel.
func eagerRig(t *testing.T, llmSvc llm.Service, tune func(c *detectConfig)) *meetingRig {
	t.Helper()
	return newMeetingRig(t, func(m *Module) {
		m.llm = llmSvc
		m.embedder = &llmtesting.MockEmbedder{}
		m.knowledge = seededKnowledge(t)
	}, func(e *meetingEngine) {
		e.cfg.Detect.MinWords = 3
		e.cfg.Detect.Interval = 0
		if tune != nil {
			tune(&e.cfg.Detect)
		}
	})
}

// seededKnowledge is een kennisbank met één bruikbaar standpunt erin.
func seededKnowledge(t *testing.T) *fakeKnowledge {
	t.Helper()
	knowledge := &fakeKnowledge{}
	seedDestillaat(t, knowledge, kbSeed{
		id: "meeting", samenvatting: "Noise and light change how city pigeons use rooftops.",
		score: .9, quote: answerSourcesFixture[0].Ref.Passages[0].Text, paper: "synthetic-study",
	})
	return knowledge
}

// waitForEvaluations wacht tot de generator n afgeronde evaluaties heeft.
func waitForEvaluations(t *testing.T, d *candidateEngine, n int) {
	t.Helper()
	waitFor(t, func() bool {
		c := d.counters()
		return c.Completed+c.Skipped >= n
	}, fmt.Sprintf("de generator rondde geen %d evaluatie(s) af (stand: %+v)", n, d.counters()))
}

func waitForCandidate(t *testing.T, s *meetingSession) *meetingCandidate {
	t.Helper()
	var c *meetingCandidate
	waitFor(t, func() bool {
		c = s.heldCandidate()
		return c != nil
	}, "de duif kreeg geen kandidaat klaarliggen")
	return c
}

// openHolding opent een vergadering waarin een klaarliggende kandidaat blijft
// liggen: de duif "zei net iets", dus haar eigen rust loopt nog. De klok
// wacht nergens anders meer op — geen stilte-eis, geen zinseinde — dus
// die rust is de enige rem waarmee een test een kandidaat op de plank houdt.
// Eén tik voorbij `hold` laat hem alsnog klinken.
func openHolding(t *testing.T, rig *meetingRig, hold time.Duration, settings map[string]any) (string, *meetingSession) {
	t.Helper()
	settings["cooldownSeconds"] = int(hold / time.Second)
	id := rig.openMeeting(t, settings)
	s := rig.session(t, id)
	s.noteUtterance()
	return id, s
}

// --- het venster -------------------------------------------------------------

func TestRollingTranscript_Since_SplitsFreshFromContextAndCapsWords(t *testing.T) {
	base := meetingEpoch
	r := NewRollingTranscript(40, time.Hour)
	for i, text := range []string{"een twee drie", "vier vijf zes", "zeven acht negen", "tien elf twaalf"} {
		r.Append(TranscriptSegment{Seq: uint64(i + 1), Text: text, At: base.Add(time.Duration(i) * time.Second)})
	}

	win := r.Since(2, 40, 300)
	if win.Fresh != 2 || len(win.Segments) != 4 {
		t.Fatalf("venster = %d segmenten met %d nieuw, wil 4 met 2", len(win.Segments), win.Fresh)
	}
	if win.Words != 12 {
		t.Errorf("woorden = %d, wil 12", win.Words)
	}

	// Een woordplafond snoeit eerst de context weg, daarna de oudste nieuwe
	// segmenten — nooit het laatste segment.
	capped := r.Since(2, 40, 4)
	if capped.Fresh != 1 || len(capped.Segments) != 1 {
		t.Fatalf("geknipt venster = %d segmenten met %d nieuw, wil 1 met 1", len(capped.Segments), capped.Fresh)
	}
	if capped.Segments[0].Text != "tien elf twaalf" {
		t.Errorf("laatste segment = %q", capped.Segments[0].Text)
	}
}

func TestRollingTranscript_Since_ExcludesPigeonWindow(t *testing.T) {
	base := meetingEpoch
	r := NewRollingTranscript(40, time.Hour)
	r.Append(TranscriptSegment{Seq: 1, Text: "mens zegt iets", At: base})
	r.Append(TranscriptSegment{Seq: 2, Text: "de duif zegt iets", At: base.Add(time.Second), PigeonWindow: true})
	r.Append(TranscriptSegment{Seq: 3, Text: "mens zegt nog iets", At: base.Add(2 * time.Second)})

	win := r.Since(1, 40, 300)
	if win.Fresh != 1 || len(win.Segments) != 2 {
		t.Fatalf("venster = %d segmenten met %d nieuw, wil 2 met 1", len(win.Segments), win.Fresh)
	}
	for _, s := range win.Segments {
		if s.PigeonWindow {
			t.Error("het duif-spreekvenster hoort niet in het detectievenster")
		}
	}
}

// --- de kandidaat ------------------------------------------------------------

// De kern van de kandidaatgenerator: één call levert de bijdrage al mee, dus
// tussen "de duif mag" en de eerste woorden zit geen tweede generatie meer.
func TestCandidate_SpeakBecomesAHeldContributionAndThenSpeaks(t *testing.T) {
	svc := newCandidateLLM(speak(0.9, "urban-ecology", "Vuurwerk is voor duiven vooral licht en klap [1]."))
	rig := eagerRig(t, svc, nil)
	// Haar rust loopt nog vijf seconden: alleen zo ligt de kandidaat lang
	// genoeg stil om hem te kunnen bekijken (anders spreekt zij meteen).
	id, s := openHolding(t, rig, 5*time.Second, map[string]any{"pigeonId": "biological", "autonomous": true, "eagerness": "eager"})
	stream := rig.events(t, id)

	rig.post(t, id, "/transcript", map[string]any{"text": "Een centrale show geeft heel harde knallen tegen het stationsdak."})
	waitForEvaluations(t, rig.candidates(t, id), 1)

	cand := waitForCandidate(t, s)
	if cand.Text != "Vuurwerk is voor duiven vooral licht en klap [1]." {
		t.Fatalf("kandidaattekst = %q", cand.Text)
	}
	if cand.TopicID != "urban-ecology" || cand.Score != 0.9 {
		t.Fatalf("kandidaat = %s/%.2f, wil urban-ecology/0.90", cand.TopicID, cand.Score)
	}

	ev := stream.wait(t, EventCandidate)
	if ev.Candidate == nil || ev.Candidate.Reason != candidateReasonTopic {
		t.Fatalf("kandidaat-event = %+v", ev.Candidate)
	}
	if ev.Candidate.Topic == nil || ev.Candidate.Topic.ID != "urban-ecology" {
		t.Errorf("kandidaat-event mist het thema: %+v", ev.Candidate)
	}

	// Zodra haar rust om is laat de klok hem door; de bijdrage klinkt zonder
	// nieuwe call.
	before := len(svc.candidateCalls())
	rig.clock.advance(t, 6*time.Second)
	stream.wait(t, EventIntervention)
	stream.wait(t, EventResponseDone)
	if got := textOf(stream.collected()); got != cand.Text {
		t.Errorf("uitgesproken tekst = %q, wil %q", got, cand.Text)
	}
	for _, call := range svc.candidateCalls()[before:] {
		if !call.Stream {
			t.Error("spreken kostte een extra niet-streamende call")
		}
	}
}

// Een kandidaat draagt geen letter transcript en geen letter bijdrage de wire
// over: de vergadering ziet dát de duif iets heeft, niet wat.
func TestCandidate_EventCarriesNoContribution(t *testing.T) {
	secret := "Dit is de geheime bijdrage van de duif [1]."
	svc := newCandidateLLM(speak(0.9, "urban-ecology", secret))
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true, "eagerness": "eager"})
	stream := rig.events(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "We hebben het over het stationsdak en vuurwerk."})

	ev := stream.wait(t, EventCandidate)
	if ev.Text != "" || ev.Delta != "" {
		t.Fatalf("kandidaat-event draagt tekst: %+v", ev)
	}
	if strings.Contains(rig.logs(), secret) || strings.Contains(rig.logs(), "stationsdak") {
		t.Error("de bijdrage of het transcript staat in het log")
	}
}

// PASS is de normale uitkomst en kost niets: geen kandidaat, geen beurt.
func TestCandidate_PassLeavesTheMeetingAlone(t *testing.T) {
	svc := newCandidateLLM(pass())
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true, "eagerness": "eager"})
	s := rig.session(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "De vergunning voor de fietsenberging loopt in april af."})
	waitForEvaluations(t, rig.candidates(t, id), 1)
	rig.clock.advance(t, 3*time.Second)

	if c := s.heldCandidate(); c != nil {
		t.Fatalf("PASS leverde alsnog een kandidaat: %+v", c)
	}
	if counts := rig.candidates(t, id).counters(); counts.Passed != 1 || counts.Held != 0 {
		t.Errorf("tellers = %+v, wil één PASS en geen kandidaat", counts)
	}
}

// Een PASS ná een SPEAK vervangt de kandidaat: de duif las de zaal opnieuw en
// heeft nu niets. Verouderen is vervangen, geen afkapping.
func TestCandidate_PassReplacesAnOlderCandidate(t *testing.T) {
	svc := newCandidateLLM(speak(0.9, "urban-ecology", "Iets over daken [1]."), pass())
	rig := eagerRig(t, svc, nil)
	id, s := openHolding(t, rig, 10*time.Minute, map[string]any{
		"pigeonId": "biological", "autonomous": true, "eagerness": "eager",
	})
	rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat over het stationsdak."})
	waitForCandidate(t, s)

	rig.post(t, id, "/transcript", map[string]any{"text": "Nu gaat het weer over de begroting."})
	waitFor(t, func() bool { return s.heldCandidate() == nil }, "de PASS verving de oude kandidaat niet")
}

// De klok wacht niet meer op een stilte. Een halve seconde na de laatste
// gesproken woorden, ruim binnen de 1,5 s die vroeger gold, klinkt de duif.
func TestCandidate_BreaksInWithoutWaitingForQuiet(t *testing.T) {
	svc := newCandidateLLM(speak(0.9, "urban-ecology", "Een korte bijdrage [1]."))
	rig := eagerRig(t, svc, func(c *detectConfig) { c.Quiet = 1500 * time.Millisecond })
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true, "eagerness": "eager"})
	stream := rig.events(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat over het stationsdak."})

	// Geen tik van de klok en geen milliseconde stilte: de bijdrage klinkt op
	// het moment dat zij klaar is.
	stream.wait(t, EventIntervention)
	if got := rig.candidates(t, id).gov.verdictCounts()[string(verdictWaitQuiet)]; got != 0 {
		t.Errorf("de klok hield haar %d keer tegen om de stilte", got)
	}
}

// De knop "laat horen" is de snelweg: hij wacht niet op de tik van de klok,
// maar laat de klaarliggende bijdrage meteen klinken. Daarna ligt er niets
// meer, en dat zegt de server ook (409 in plaats van een stille 202).
func TestCandidate_SpeakNowIsTheShortcutPastTheClock(t *testing.T) {
	svc := newCandidateLLM(speak(0.9, "urban-ecology", "Een korte bijdrage [1]."))
	rig := eagerRig(t, svc, nil)
	id, s := openHolding(t, rig, time.Hour, map[string]any{"pigeonId": "biological", "autonomous": true, "eagerness": "eager"})
	stream := rig.events(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat over het stationsdak."})
	waitForCandidate(t, s)

	if status, body := rig.post(t, id, "/candidate/speak", nil); status != http.StatusAccepted {
		t.Fatalf("laat horen: status = %d, body = %s", status, body)
	}
	stream.wait(t, EventIntervention)
	stream.wait(t, EventResponseDone)

	// Zonder kandidaat is er niets te laten horen: 409, geen stille 202.
	if status, _ := rig.post(t, id, "/candidate/speak", nil); status != http.StatusConflict {
		t.Errorf("tweede keer laat horen: status = %d, wil 409", status)
	}
}

// Aangesproken worden gaat langs de cooldown: wie de duif iets vraagt hoort
// geen stilte omdat zij drie minuten geleden ook al iets zei.
func TestCandidate_AddressedPassesTheCooldown(t *testing.T) {
	svc := newCandidateLLM(addressed(0.1, "pest-framing", "Ik geef antwoord."))
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{
		"pigeonId": "individualist", "autonomous": true, "cooldownSeconds": 3600,
	})
	s := rig.session(t, id)
	// De duif zei net iets: de rust loopt nog een uur.
	s.noteUtterance()
	stream := rig.events(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "Wat vindt de duif hiervan?"})

	// Aangesproken worden wacht alleen op het zinseinde, niet op de rust: de
	// bijdrage klinkt zodra de zin gecommit is.
	ev := stream.wait(t, EventIntervention)
	if !ev.Addressed {
		t.Error("de interventie meldt niet dat de duif aangesproken was")
	}
}

// De cooldown loopt per duif en loopt op: de rem in plaats van een sessieplafond.
func TestCandidate_CooldownHoldsTheSecondContribution(t *testing.T) {
	svc := newCandidateLLM(speak(0.9, "urban-ecology", "Eerste bijdrage [1]."))
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true, "eagerness": "eager"})
	s := rig.session(t, id)
	stream := rig.events(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat over het stationsdak."})
	stream.wait(t, EventResponseDone)

	// Meteen daarna een nieuwe kandidaat: die blijft liggen tot de rust om is.
	rig.post(t, id, "/transcript", map[string]any{"text": "En nog iets over datzelfde dak."})
	waitForCandidate(t, s)
	rig.clock.advance(t, 30*time.Second)
	if counts := rig.candidates(t, id).gov.verdictCounts(); counts[string(verdictCooldown)] == 0 {
		t.Errorf("de cooldown hield niets tegen: %v", counts)
	}
}

// Onder de lat van de duif blijft zij stil — maar de kandidaat blijft liggen,
// zodat "laat horen" hem alsnog kan laten klinken.
func TestCandidate_BelowTheBarStaysHeld(t *testing.T) {
	svc := newCandidateLLM(speak(0.2, "urban-ecology", "Iets kleins [1]."))
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true, "eagerness": "reserved"})
	s := rig.session(t, id)
	stream := rig.events(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat over het stationsdak."})
	waitForCandidate(t, s)
	rig.clock.advance(t, 5*time.Second)

	if hasEvent(stream.collected(), EventIntervention) {
		t.Fatal("een kandidaat onder de lat viel toch in")
	}
	if s.heldCandidate() == nil {
		t.Fatal("de kandidaat is weggegooid in plaats van vastgehouden")
	}
	if status, _ := rig.post(t, id, "/candidate/speak", nil); status != http.StatusAccepted {
		t.Error("laat horen werkt niet voor een kandidaat onder de lat")
	}
}

// De duif krijgt haar eigen eerdere bijdragen mee, zodat zij zich niet herhaalt.
func TestCandidate_PromptCarriesHerOwnEarlierContributions(t *testing.T) {
	svc := newCandidateLLM(speak(0.9, "urban-ecology", "Duiven horen die klap veel scherper [1]."), pass())
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true, "eagerness": "eager"})
	s := rig.session(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat over het stationsdak."})
	waitFor(t, func() bool { return len(s.ownContributionSegments()) == 1 }, "de bijdrage werd niet onthouden")

	rig.post(t, id, "/transcript", map[string]any{"text": "En hoe zit dat met de daken zelf?"})
	waitFor(t, func() bool { return len(svc.candidateCalls()) >= 2 }, "er kwam geen tweede kandidaatcall")
	calls := svc.candidateCalls()
	last := calls[len(calls)-1].Messages[1].Content
	if !strings.Contains(last, "Duiven horen die klap veel scherper") {
		t.Errorf("de tweede prompt draagt haar eigen bijdrage niet mee:\n%s", last)
	}
}

// Zonder nieuwe gecommitte woorden gaat er geen call uit: stilte is gratis.
func TestCandidate_SilenceCostsNoCalls(t *testing.T) {
	svc := newCandidateLLM(pass())
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true})
	for range 8 {
		rig.clock.advance(t, time.Second)
	}
	if n := len(svc.candidateCalls()); n != 0 {
		t.Fatalf("stilte kostte %d call(s)", n)
	}
	if got := rig.candidates(t, id).counters().Started; got != 0 {
		t.Errorf("gestarte evaluaties = %d, wil 0", got)
	}
}

// De cadans houdt hoogstens één call per interval aan, ook bij doorpraten.
func TestCandidate_CadenceKeepsOneCallPerInterval(t *testing.T) {
	svc := newCandidateLLM(pass())
	rig := newMeetingRig(t, func(m *Module) {
		m.llm = svc
		m.embedder = &llmtesting.MockEmbedder{}
		m.knowledge = seededKnowledge(t)
	}, func(e *meetingEngine) {
		e.cfg.Detect.MinWords = 3
		e.cfg.Detect.Interval = 6 * time.Second
	})
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true})
	d := rig.candidates(t, id)

	for i := range 5 {
		rig.post(t, id, "/transcript", map[string]any{"text": fmt.Sprintf("zin nummer %d met genoeg woorden erin", i)})
		rig.clock.advance(t, time.Second)
	}
	waitForEvaluations(t, d, 1)
	if got := d.counters().Started; got != 1 {
		t.Fatalf("gestarte evaluaties na 5 s doorpraten = %d, wil 1", got)
	}
	rig.clock.advance(t, 6*time.Second)
	rig.post(t, id, "/transcript", map[string]any{"text": "en nog een zin met genoeg woorden"})
	waitFor(t, func() bool { return d.counters().Started == 2 }, "na het interval kwam er geen tweede evaluatie")
}

// Zolang "Ben je er nog?" openstaat bedenkt de duif niets nieuws: een
// kandidaat die niet mag klinken is een modelcall voor een misschien lege
// zaal. De woorden blijven geteld en komen na het antwoord alsnog mee.
func TestCandidate_NoEvaluationWhilePresenceCheckIsOpen(t *testing.T) {
	svc := newCandidateLLM(pass())
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true})
	s := rig.session(t, id)
	d := rig.candidates(t, id)
	s.mu.Lock()
	s.presenceAt = rig.clock.Now()
	s.mu.Unlock()

	rig.post(t, id, "/transcript", map[string]any{"text": "een zin met genoeg woorden voor een evaluatie"})
	rig.clock.advanceProcessed(t, time.Second)
	if got := d.counters().Started; got != 0 {
		t.Fatalf("de generator evalueerde terwijl de vraag openstond: %d evaluatie(s)", got)
	}
	s.noteUserActivity()
	rig.clock.advanceProcessed(t, time.Second)
	waitFor(t, func() bool { return d.counters().Started == 1 }, "na het antwoord kwam de evaluatie niet alsnog")
}

// Wat de duif zelf zei telt niet mee voor de cadans. De echo bestaat
// alleen op de microfoonroute — daar staat de luidspreker in dezelfde kamer.
// Getypte inbreng draagt geen echo (appendTypedSegment) en is dus niet de
// route waarop deze regel te toetsen valt.
func TestCandidate_IgnoresItsOwnWords(t *testing.T) {
	svc := newCandidateLLM(pass())
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true})
	s := rig.session(t, id)
	d := rig.candidates(t, id)
	s.mu.Lock()
	s.speakUntil = rig.clock.Now().Add(time.Minute)
	s.mu.Unlock()

	s.appendSegment("dit komt uit de speaker van de duif zelf", "")
	// De teller gaat synchroon omhoog in maybeEvaluate, de call zelf pas een
	// goroutine verder. Toetsen op de call leest dus een race; toetsen op de
	// teller leest wat er besloten is.
	rig.clock.advanceProcessed(t, time.Second)
	if got := d.counters().Started; got != 0 {
		t.Fatalf("de duif evalueerde haar eigen woorden: %d evaluatie(s)", got)
	}
	if n := len(svc.candidateCalls()); n != 0 {
		t.Fatalf("de duif evalueerde haar eigen woorden: %d call(s)", n)
	}
}

// Zelfstandig meedoen staat standaard uit en kost dan geen enkele call.
func TestCandidate_AutonomyOffCostsNothing(t *testing.T) {
	svc := newCandidateLLM(speak(0.9, "urban-ecology", "iets"))
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological"})
	rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat over het stationsdak en vuurwerk."})
	rig.clock.advance(t, 2*time.Second)
	if n := len(svc.candidateCalls()); n != 0 {
		t.Fatalf("met autonomie uit gingen er %d call(s) uit", n)
	}
}

// Een kapotte provider zet de generator niet meteen uit: hij probeert het
// opnieuw, en pas na een reeks storingen degradeert hij zichtbaar.
func TestCandidate_ProviderFailure_RetriesThenDegradesVisibly(t *testing.T) {
	svc := newCandidateLLM(candidateStep{err: errors.New("test: 503 upstream")})
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true})
	d := rig.candidates(t, id)
	stream := rig.events(t, id)

	for i := range maxDetectorFailures {
		rig.post(t, id, "/transcript", map[string]any{"text": fmt.Sprintf("zin %d met voldoende woorden erin", i)})
		waitForEvaluations(t, d, i+1)
		rig.clock.advance(t, 10*time.Second)
	}
	// Ook hier op de melding zelf wachten: disable zet de stand om en publiceert
	// daarna, dus "uit" is er eerder dan het bericht erover.
	waitFor(t, func() bool { return !d.status().Enabled }, "de generator degradeerde niet na herhaalde storingen")
	waitFor(t, func() bool { return hasEvent(stream.collected(), EventDetectorStatus) },
		"de degradatie is niet gemeld")
	// De knop blijft werken.
	if status, body := rig.post(t, id, "/respond", nil); status != http.StatusAccepted {
		t.Errorf("de knop werkt niet meer: status = %d, body = %s", status, body)
	}
}

// Een kostenlimiet is meteen einde verhaal, zichtbaar, en de knop blijft werken.
func TestCandidate_BudgetError_DisablesItselfAtOnce(t *testing.T) {
	svc := newCandidateLLM(candidateStep{err: errBudgetUpstream})
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true})
	d := rig.candidates(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat over het stationsdak en vuurwerk."})
	waitFor(t, func() bool { return !d.status().Enabled }, "de generator ging niet uit bij een kostenlimiet")
	if got := d.status().State; got != "budget_stopped" {
		t.Errorf("status = %q, wil budget_stopped", got)
	}
}

// Een paniek in deze zijtak neemt de sessie niet mee.
func TestCandidate_Panic_DegradesInsteadOfTakingTheProcessDown(t *testing.T) {
	rig := eagerRig(t, panickingLLM{}, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true})
	d := rig.candidates(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat over het stationsdak en vuurwerk."})
	waitFor(t, func() bool { return d.counters().Panicked > 0 }, "de paniek is niet opgevangen")
	// Wachten op de stand zelf, niet op de teller ervoor: guard telt de paniek
	// en degradeert daarna, met een logregel ertussen. Onder belasting is dat
	// genoeg om een directe lezing te laten winnen van het besluit.
	waitFor(t, func() bool { return !d.status().Enabled }, "na een paniek hoort de generator uit te staan")
	if status, _ := rig.post(t, id, "/respond", nil); status != http.StatusAccepted {
		t.Error("de knop werkt niet meer na een paniek in de generator")
	}
}

// Zonder LLM-dienst zet de generator zichzelf één keer uit.
func TestCandidate_WithoutLLMService_DisablesItselfOnce(t *testing.T) {
	rig := newMeetingRig(t, func(m *Module) {
		m.llm = nil
		m.embedder = &llmtesting.MockEmbedder{}
		m.knowledge = seededKnowledge(t)
	}, func(e *meetingEngine) {
		e.cfg.Detect.MinWords = 3
		e.cfg.Detect.Interval = 0
	})
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true})
	d := rig.candidates(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat over het stationsdak en vuurwerk."})
	waitFor(t, func() bool { return !d.status().Enabled }, "de generator ging niet uit zonder LLM")
	if got := d.status().Reason; got != ReasonLLMUnavailable {
		t.Errorf("reden = %q, wil %q", got, ReasonLLMUnavailable)
	}
}

// De taal van de sessie stuurt de prompt.
func TestCandidate_Language_FollowsTheSession(t *testing.T) {
	for _, tc := range []struct{ lang, marker string }{
		{"en", "Theme ids you may use"},
		{"nl", "Thema-id's die je in de kopregel mag gebruiken"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			svc := newCandidateLLM(pass())
			rig := eagerRig(t, svc, nil)
			id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true, "lang": tc.lang})
			rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat over het stationsdak en vuurwerk."})
			waitFor(t, func() bool { return len(svc.candidateCalls()) > 0 }, "er ging geen call uit")
			if got := svc.candidateCalls()[0].Messages[0].Content; !strings.Contains(got, tc.marker) {
				t.Errorf("de %s-prompt mist %q", tc.lang, tc.marker)
			}
		})
	}
}

// Het sluiten van een sessie stopt de generator.
func TestCandidate_SessionClose_StopsGeneration(t *testing.T) {
	svc := newCandidateLLM(pass())
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true})
	d := rig.candidates(t, id)
	rig.do(t, http.MethodDelete, "/v1/meeting/sessions/"+id, nil)
	before := len(svc.candidateCalls())
	d.onTick(rig.clock.Now())
	if n := len(svc.candidateCalls()); n != before {
		t.Errorf("een gesloten sessie evalueerde alsnog: %d → %d", before, n)
	}
}

// De meter stijgt met de score van de kandidaat en zakt op de sessieklok.
func TestCandidate_Arousal_RisesWithScoreAndDecaysOverTime(t *testing.T) {
	svc := newCandidateLLM(speak(0.9, "urban-ecology", "iets [1]."))
	rig := eagerRig(t, svc, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true, "eagerness": "eager"})
	s := rig.session(t, id)
	rig.post(t, id, "/transcript", map[string]any{"text": "Het gaat over het stationsdak."})
	waitFor(t, func() bool { return s.arousal.level() > 0 }, "de meter kwam niet omhoog")
	high := s.arousal.level()
	if high <= 0 {
		t.Fatalf("de meter bleef op %.2f staan", high)
	}
	rig.clock.skip(10 * time.Minute)
	if level, _ := s.arousal.decay(rig.clock.Now()); level >= high {
		t.Errorf("de meter zakte niet: %.2f → %.2f", high, level)
	}
}

func TestDecayArousal_IsBoundedAndMonotone(t *testing.T) {
	half := 2 * time.Minute
	if got := decayArousal(0.8, 0, half); got != 0.8 {
		t.Errorf("zonder verstreken tijd = %.3f, wil 0.8", got)
	}
	if got := decayArousal(0.8, half, half); got > 0.41 || got < 0.39 {
		t.Errorf("na één halveringstijd = %.3f, wil ongeveer 0.4", got)
	}
	if got := decayArousal(0.8, time.Hour, half); got < 0 || got > 1 {
		t.Errorf("na een uur = %.3f, wil binnen [0,1]", got)
	}
	if got := decayArousal(1.5, time.Second, half); got > 1 {
		t.Errorf("een te hoge stand wordt niet geklemd: %.3f", got)
	}
}

// --- de paniekende provider ---------------------------------------------------

type panickingLLM struct{}

func (panickingLLM) Complete(context.Context, llm.CompletionRequest) (llm.CompletionResponse, error) {
	panic("test: de provider paniekt")
}

func (panickingLLM) CompleteStream(context.Context, llm.CompletionRequest) (<-chan llm.StreamChunk, error) {
	panic("test: de provider paniekt")
}

// Een stroom die tot de tijdslimiet stilvalt is een storing, geen geslaagde
// PASS. Hij telt mee voor de degradatie naar alleen-knop en laat het venster
// staan: dit stuk gesprek is niet gelezen.
func TestCandidate_StalledStream_CountsAsFailureAndKeepsItsWindow(t *testing.T) {
	for _, tc := range []struct {
		name string
		step candidateStep
	}{
		{"kale sluiting", stalled()},
		{"met foutchunk", stalledWithError()},
		{"met foutchunk in de buffer", stalledWithBufferedError()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newCandidateLLM(tc.step)
			rig := eagerRig(t, svc, func(c *detectConfig) { c.Timeout = 50 * time.Millisecond })
			id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true})
			d := rig.candidates(t, id)
			stream := rig.events(t, id)

			for i := range maxDetectorFailures {
				rig.post(t, id, "/transcript", map[string]any{"text": fmt.Sprintf("zin %d met voldoende woorden erin", i)})
				waitFor(t, func() bool { return d.counters().Skipped > i },
					"de stilgevallen evaluatie is niet als storing geboekt")
				rig.clock.advance(t, 10*time.Second)
			}
			if counts := d.counters(); counts.Completed != 0 || counts.Passed != 0 {
				t.Errorf("een stilgevallen stroom telde als afgeronde evaluatie: %+v", counts)
			}
			// Beide sub-gevallen zijn dezelfde stilstand. Of de foutchunk van
			// de provider de race met de afgekapte context wint verandert
			// niets aan wat er gebeurde, dus horen ze onder één
			// diagnostiekcode te vallen.
			marks := rig.session(t, id).diagnostics.snapshot().Recent
			var seen int
			for _, mark := range marks {
				if mark.Stage != "evaluation_done" {
					continue
				}
				seen++
				if mark.Reason != reasonCandidateTimeout {
					t.Errorf("evaluation_done reason = %q, wil %q", mark.Reason, reasonCandidateTimeout)
				}
			}
			if seen == 0 {
				t.Error("geen enkele evaluation_done-markering gevonden")
			}
			d.mu.Lock()
			seq := d.lastSeq
			d.mu.Unlock()
			if seq != 0 {
				t.Errorf("lastSeq = %d, wil 0: ongelezen gesprek hoort herleesbaar te blijven", seq)
			}
			waitFor(t, func() bool { return !d.status().Enabled },
				"de generator degradeerde niet na herhaalde tijdslimieten")
			if !hasEvent(stream.collected(), EventDetectorStatus) {
				t.Error("de degradatie is niet gemeld")
			}
			// De knop blijft de gegarandeerde basis.
			if status, body := rig.post(t, id, "/respond", nil); status != http.StatusAccepted {
				t.Errorf("de knop werkt niet meer: status = %d, body = %s", status, body)
			}
		})
	}
}

// De tegenproef bij de tijdslimiet hierboven: gaat de provider werkelijk stuk
// op een stroom die verder leeft — geen tijdslimiet, geen annulering — dan
// blijft dat "stream_failed" heten. Zonder deze toets zou het samenvoegen van
// de stilstandcodes ongemerkt élke gebroken stroom als tijdslimiet kunnen boeken, en dan
// zegt het log niet meer wat er gebeurde.
func TestCandidate_GenuineProviderError_StillReportsStreamFailed(t *testing.T) {
	svc := newCandidateLLM(broken("SPEAK score=0.99 topic=pigeon-rights addressed=false\nHalve "))
	rig := eagerRig(t, svc, func(c *detectConfig) { c.Timeout = 10 * time.Second })
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true})
	d := rig.candidates(t, id)
	_ = rig.events(t, id)

	rig.post(t, id, "/transcript", map[string]any{"text": "een zin met voldoende woorden erin"})
	waitFor(t, func() bool { return d.counters().Skipped > 0 }, "de kapotte stroom is niet als storing geboekt")

	marks := rig.session(t, id).diagnostics.snapshot().Recent
	var seen int
	for _, mark := range marks {
		if mark.Stage != "evaluation_done" {
			continue
		}
		seen++
		if mark.Reason != "stream_failed" {
			t.Errorf("evaluation_done reason = %q, wil %q", mark.Reason, "stream_failed")
		}
	}
	if seen == 0 {
		t.Error("geen enkele evaluation_done-markering gevonden")
	}
	// En de storing telt mee, net als elke andere: een halve bijdrage is geen
	// afgeronde evaluatie.
	if counts := d.counters(); counts.Completed != 0 || counts.Passed != 0 {
		t.Errorf("een kapotte stroom telde als afgeronde evaluatie: %+v", counts)
	}
}

// Eén tijdslimiet wist een lopende storingsreeks niet uit: fout, stilstand,
// fout is drie storingen achter elkaar.
func TestCandidate_StalledStream_DoesNotResetTheFailureStreak(t *testing.T) {
	upstream := candidateStep{err: errors.New("test: 503 upstream")}
	svc := newCandidateLLM(upstream, stalled(), upstream)
	rig := eagerRig(t, svc, func(c *detectConfig) { c.Timeout = 50 * time.Millisecond })
	id := rig.openMeeting(t, map[string]any{"pigeonId": "biological", "autonomous": true})
	d := rig.candidates(t, id)

	for i := range maxDetectorFailures {
		rig.post(t, id, "/transcript", map[string]any{"text": fmt.Sprintf("zin %d met voldoende woorden erin", i)})
		waitFor(t, func() bool { return d.counters().Skipped > i }, "de storing is niet geboekt")
		rig.clock.advance(t, 10*time.Second)
	}
	waitFor(t, func() bool { return !d.status().Enabled },
		"de tijdslimiet in het midden zette de storingsteller terug")
}
