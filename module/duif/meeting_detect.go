package duif

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/parai-nl/parai-core/llm"
)

// De kandidaatgenerator: de laag die doorlopend meeschrijft met de
// vergadering en de duif een klaarliggende bijdrage geeft.
//
// Vier ontwerpregels sturen dit bestand:
//
//  1. Eén model, één call. Er is geen aparte classificatie meer die eerst
//     beslist of het onderwerp raakt en daarna een tweede call die de bijdrage
//     schrijft. Het model leest persona, spreeklat, bronnen, venster en de
//     eigen eerdere bijdragen van de duif, en begint met `PASS` of `SPEAK`.
//     Bij `PASS` breken we de stroom direct af: dat is de goedkope uitkomst en
//     tegelijk de normale.
//  2. De generator is een zijtak, geen schakel. Hij raakt de knop-route nooit
//     aan; valt hij om, dan degradeert de sessie zichtbaar naar alleen-knop.
//  3. Rommelige uitvoer is één overgeslagen evaluatie, nooit een sessiebrede
//     uitschakeling. Een kopregel met een spatie te veel, een decimale komma of
//     een ontbrekend veld levert een `PASS` met een diagnosecode op; de
//     volgende evaluatie probeert het gewoon opnieuw.
//  4. Vergaderinhoud blijft vluchtig: venster, bijdrage en kopregel gaan het
//     sessiekanaal op en verder nergens heen — nooit naar een log, nooit naar
//     de diagnostiek.

// detectorCounters zijn de tellers waarmee de generator zich verantwoordt:
// aantallen, nooit inhoud.
type detectorCounters struct {
	Started      int
	Completed    int
	Passed       int
	Held         int
	Malformed    int
	Skipped      int
	Panicked     int
	ScoreBuckets [5]int // vaste relevantiebanden, geen thema's en geen inhoud
}

// candidateEngine is de kandidaatgenerator van één sessie.
type candidateEngine struct {
	engine  *meetingEngine
	session *meetingSession
	gov     *governor
	cfg     detectConfig
	topics  []Topic

	mu       sync.Mutex
	enabled  bool
	reason   string
	running  bool
	lastSeq  uint64 // laatste geëvalueerde segment
	lastSeen uint64
	lastEval time.Time // start van de vorige evaluatie (de cadans)
	pending  int       // ongeëvalueerde woorden
	counts   detectorCounters
	closed   bool
	failures int
	retryAt  time.Time
	// lastVerdict onthoudt wat de klok de vorige tik zei, zodat een kandidaat
	// die een minuut op stilte wacht niet 240 regels diagnostiek kost.
	lastVerdict governorVerdict
	// queries onthoudt de laatste geslaagde queryherformulering van deze
	// sessie. Hij heeft zijn eigen slot: de evaluatie draait in een
	// eigen goroutine naast de klok.
	queries *queryMemory
}

func newCandidateEngine(e *meetingEngine, s *meetingSession) *candidateEngine {
	d := &candidateEngine{
		engine:   e,
		session:  s,
		queries:  &queryMemory{},
		gov:      newGovernor(s.persona.Spreeklat, s.cfg.Detect.Quiet),
		cfg:      s.cfg.Detect,
		topics:   s.persona.Topics,
		enabled:  true,
		lastEval: s.startedAt.Add(-s.cfg.Detect.Interval),
	}
	if !s.persona.Chat && !s.persona.signalResearchReady {
		d.enabled = false
		d.reason = reasonSignalResearchUnavailable
	}
	return d
}

func (d *candidateEngine) counters() detectorCounters {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.counts
}

func (d *candidateEngine) status() DetectorStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.statusLocked(d.engine.clock.Now())
}

// onSegment voedt de cadans-teller. Wat de duif zelf zei telt niet mee: dat
// zou een zelfversterkende lus zijn: de duif die op haar eigen echo reageert.
func (d *candidateEngine) onSegment(seg TranscriptSegment) {
	if seg.PigeonWindow {
		return
	}
	d.mu.Lock()
	if d.closed || seg.Seq <= d.lastSeq {
		d.mu.Unlock()
		return
	}
	d.lastSeen = max(d.lastSeen, seg.Seq)
	d.pending += countWords(seg.Text)
	due := d.pending >= d.cfg.MinWords
	d.mu.Unlock()
	if due {
		d.maybeEvaluate(seg.At)
	}
}

// onTick doet het tijdgebonden werk: de klok over een klaarliggende kandidaat,
// en de cadans van de generator. Stilte kost hier niets — zonder nieuwe
// gecommitte woorden gaat er geen call uit.
func (d *candidateEngine) onTick(now time.Time) {
	// Deze lus is de hartslag van de sessie en die van de inactiviteits-
	// bewaking. Een paniek in de klok of op het spreekpad mag hem niet
	// meenemen: vangen, degraderen, doorkloppen.
	defer d.guard("tick")
	if d.session.isClosed() {
		return
	}
	d.mu.Lock()
	enabled := d.enabled
	due := d.pending >= d.cfg.MinWords
	d.mu.Unlock()
	// De klok draait ook met een uitgevallen generator: een kandidaat die er
	// al ligt mag alsnog klinken.
	d.resolveCandidate(now)
	if enabled && due {
		d.maybeEvaluate(now)
	}
}

// maybeEvaluate start één evaluatie, tenzij er al een loopt, de cadans nog
// niet om is, of zelfstandig meedoen uitstaat. De teller gaat synchroon
// omhoog: een test kan daarmee hard vaststellen dat er géén call is begonnen.
func (d *candidateEngine) maybeEvaluate(now time.Time) {
	if d.session.isClosed() {
		return
	}
	st := d.session.governorState()
	// Zolang "Ben je er nog?" openstaat, bedenkt de duif ook niets nieuws: een
	// kandidaat die niet mag klinken is een modelcall voor een misschien lege
	// zaal. De woorden blijven geteld en komen na het antwoord mee.
	if !st.Autonomous || st.Paused || st.PresencePending {
		return
	}
	d.mu.Lock()
	if d.closed || !d.enabled || d.running || now.Sub(d.lastEval) < d.cfg.Interval || now.Before(d.retryAt) {
		d.mu.Unlock()
		return
	}
	d.running = true
	d.lastEval = now
	d.counts.Started++
	d.mu.Unlock()
	d.session.diagnostics.mark(now, "evaluation_started", "", 0)
	go d.evaluate(now)
}

// evaluate draait één cyclus: venster → bronnen → één streaming call →
// kopregel → kandidaat. Hij leeft in een eigen goroutine, hangt aan de
// sessiecontext en blokkeert het transcriptpad nooit.
func (d *candidateEngine) evaluate(at time.Time) {
	defer func() {
		d.mu.Lock()
		d.running = false
		d.mu.Unlock()
	}()
	// Het vangnet staat als laatste defer geregistreerd en draait dus als
	// eerste: eerst de paniek opvangen, dan pas het slot vrijgeven.
	defer d.guard("evaluate")

	d.mu.Lock()
	since, consumed, through := d.lastSeq, d.pending, d.lastSeen
	d.mu.Unlock()
	win := d.session.detectionWindow(since).through(through)
	if win.Fresh == 0 {
		d.finishWindow(win, consumed)
		d.skip("no_new_content")
		return
	}
	if d.engine.llm == nil {
		d.disable(ReasonLLMUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(d.session.ctx, d.cfg.Timeout)
	defer cancel()

	ground, ok := d.groundCandidate(ctx, at, win, consumed)
	if !ok {
		return
	}
	started := d.engine.clock.Now()
	cand, reason, err := d.generate(ctx, win, ground)
	now := d.engine.clock.Now()
	// De diagnostiekregel wil altijd een woord; de teller wil weten of er iets
	// mis was. Dat zijn twee vragen, en ze delen niet één waarde: een schone
	// PASS heet in het log "pass" en is in de telling geen misvorming.
	d.session.diagnostics.mark(now, "evaluation_done", evaluationCode(reason, cand), now.Sub(started))
	if d.session.isClosed() {
		return
	}
	if err != nil {
		// De woorden blijven staan: een mislukte call heeft dit stuk gesprek
		// niet gezien, en de volgende poging hoort het alsnog te lezen.
		d.noteFailure(now, classifyDetectorFailure(err))
		return
	}
	d.finishWindow(win, consumed)
	d.noteSuccess()
	d.mu.Lock()
	d.counts.Completed++
	if reason != headerOK {
		d.counts.Malformed++
	}
	if cand == nil {
		d.counts.Passed++
	} else {
		d.counts.Held++
		d.counts.ScoreBuckets[min(int(cand.Score*5), 4)]++
	}
	d.mu.Unlock()

	// Een `PASS` vervangt een oudere kandidaat: de duif heeft de zaal opnieuw
	// gelezen en heeft nu niets. Verouderen is vervangen, geen afkapping.
	d.session.setCandidate(cand)
	if cand != nil {
		arousal := d.session.arousal.bump(at, cand.Score)
		if d.session.persona.Chat {
			d.session.publish(MeetingEvent{Type: EventArousal, Arousal: &arousal})
		}
		d.resolveCandidate(d.engine.clock.Now())
	}
}

// groundCandidate haalt de bronnen op waarop een kandidaat mag rusten. Voor een
// kennisbank-duif is dit ook de plek waar een weggevallen kennisbank zichtbaar
// wordt: is de kennisbank weg, dan is er niets onderzocht en komt er geen
// kandidaat — zichtbaar, niet stil.
//
// Zoekt op de lat van de vergadertafel (meetingMinSimilarity), niet op die van
// de chat: een gespreksvenster drukt elke cosinusscore. De kalibratie staat bij
// de constante in retrieval.go.
func (d *candidateEngine) groundCandidate(ctx context.Context, at time.Time, win transcriptWindow, consumed int) (candidateGrounding, bool) {
	if !d.session.persona.Kennisbank {
		return candidateGrounding{Status: KBPersonaOnly}, true
	}
	// Dezelfde herformulering als de knop-route, maar met geheugen:
	// deze route evalueert op cadans, en een gesprek dat nog over hetzelfde
	// gaat hoort geen tweede modelronde te kosten. De onthouden vraag vervalt
	// zodra het venster genoeg ververst is (queryMemory.reuse).
	raw := truncateQuery(transcriptQuery(win.Segments))
	q, hergebruikt := d.queries.reuse(raw)
	if !hergebruikt {
		q = d.engine.reformulateQuery(ctx, d.session.lang, routeCandidateQuery, win.Segments)
		d.queries.remember(raw, q)
	}
	d.engine.logQuery(d.session.id, routeCandidateQuery, q)
	res, err := d.engine.retrieval.runMeeting(ctx, q.Query)
	if err != nil {
		d.engine.log().Error("kandidaat zonder kennisbank", "component", "duif",
			"sessie", d.session.id, "reason", ReasonKBUnavailable)
		d.noteFailure(d.engine.clock.Now(), ReasonKBUnavailable)
		return candidateGrounding{}, false
	}
	// Geen bron én deze duif mag niets zonder bron zeggen: dat is een PASS,
	// geen call. Het model zou hier op zijn eigen instructie toch PASS zeggen,
	// en dat kost een hele generatie.
	if res.Status != KBSources && !d.session.persona.Spreeklat.WithoutSources() {
		// Dit stuk gesprek is beoordeeld: zonder bron valt er niets te zeggen.
		// De woorden gaan van de teller af, zodat hetzelfde venster niet elke
		// cadans opnieuw dezelfde zoekopdracht kost.
		d.finishWindow(win, consumed)
		d.session.diagnostics.mark(at, "candidate_pass", "no_sources", 0)
		d.mu.Lock()
		d.counts.Passed++
		d.mu.Unlock()
		d.session.setCandidate(nil)
		return candidateGrounding{}, false
	}
	return candidateGrounding{
		Status: res.Status, Sources: res.Sources, RAGSection: res.RAGSection, Dropped: res.Dropped,
	}, true
}

// candidateGrounding is waar één kandidaat op rust: de stand van de
// kennisbank, de bronnen, de RAG-sectie voor de prompt, en wat er onderweg
// wegviel. Bij elkaar in één waarde, zodat de generatiestap en de
// spreekbeurt niet elk hun eigen deelverzameling doorgeven.
type candidateGrounding struct {
	Status     KBStatus
	Sources    []Source
	RAGSection string
	Dropped    *SourceDrops
}

// generate doet de ene streaming call van een evaluatie en leest de kopregel
// terwijl hij binnenkomt. Bij `PASS` breekt hij de stroom direct af.
func (d *candidateEngine) generate(ctx context.Context, win transcriptWindow, ground candidateGrounding) (*meetingCandidate, string, error) {
	status, sources, ragSection := ground.Status, ground.Sources, ground.RAGSection
	s := d.session
	messages := []llm.Message{
		{Role: "system", Content: buildCandidatePrompt(candidatePromptInput{
			Persona: s.persona, Personas: d.engine.personas,
			Lang: s.lang, Status: status, RAGSection: ragSection,
		})},
		{Role: "user", Content: renderCandidateWindow(s.lang, s.markToken, s.pigeonID, win, s.ownContributionSegments(), s.persona.SignalsOnly())},
	}
	// De plafonds rekenen nog op denkruimte (answerReasoningHeadroom), ook nu
	// deze call op "none" staat (candidateReasoningEffort). Dat is bewust:
	// "none" is een verzoek aan de provider, geen garantie (een eerder model
	// zette het denken níét uit), en ruimte die het model niet gebruikt kost
	// niets. Op het huidige model gebruikte de detectie met "none" hoogstens
	// 157 tokens.
	maxTokens := answerMeetingMaxTokens
	if s.persona.SignalsOnly() {
		// De signalerende duif schrijft geen bijdrage: haar hele antwoord is de
		// kopregel. Alleen de denkruimte blijft staan.
		maxTokens = answerReasoningHeadroom + 48
	}
	streamCtx, stop := context.WithCancel(withLLMRoute(ctx, routeCandidate))
	defer stop()
	// aborted onthoudt dat wíj de stroom afbraken (een PASS, of een duif die
	// genoeg heeft aan de kopregel). Alleen dan is een geannuleerde streamCtx
	// een normaal einde; zie de controle onder de leeslus.
	aborted := false
	// Temperatuur 0: de kopregel is een vormafspraak, geen stijlkeuze. Wat
	// erachter komt is één bijdrage die de duif hoe dan ook maar één keer
	// schrijft, dus er valt niets te winnen met variatie — wel iets te
	// verliezen als "SPEAK score=…" af en toe anders uitvalt. Geen nieuwe
	// knop: de waarde staat vast in de code.
	ch, err := s.engine.llm.CompleteStream(streamCtx, llm.CompletionRequest{
		Model: s.engine.detectModel, Messages: messages, Stream: true,
		MaxTokens: maxTokens, Temperature: llm.Float64(0),
		ReasoningEffort: candidateReasoning(),
	})
	if err != nil {
		return nil, "stream_start_failed", err
	}

	var split headerSplitter
	var header candidateHeader
	var body strings.Builder
	filter := citationFilter(sources)
	reason := headerOK
	decided := false
	// stopReason blijft leeg bij een normaal einde, en draagt "truncated" of
	// "filtered" zodra de provider zelf afsloot — dezelfde afspraak als op de
	// knop-route (streamTurn) en in de chat.
	stopReason := ""
	accept := func(chunk string) bool {
		line, rest, ready := split.feed(chunk)
		if ready {
			header, reason = parseCandidateHeader(line)
			decided = true
			if !header.Speak || !s.persona.Chat {
				return false // PASS, of een duif die alleen de kopregel nodig heeft
			}
			chunk = rest
		} else if !decided {
			return true
		}
		body.WriteString(filter.write(chunk))
		return true
	}

	for open := true; open; {
		select {
		case <-streamCtx.Done():
			open = false
		case chunk, more := <-ch:
			if !more {
				open = false
				break
			}
			if chunk.Err != nil {
				// Staat de context al uit, dan is dit de stilstand van
				// hieronder en niet een tweede soort storing: de adapter
				// schrijft bij een afgekapte stroom vaak nog een foutchunk
				// weg, en of de lezer die nog ophaalt is een race. Dezelfde
				// gebeurtenis hoort onder dezelfde diagnostiekcode.
				if streamCtx.Err() != nil {
					return nil, reasonCandidateTimeout, chunk.Err
				}
				return nil, "stream_failed", chunk.Err
			}
			// Eerst de reden van de provider, dan pas de sprong over lege
			// inhoud. De afsluitende chunk draagt vrijwel altijd allebei: een
			// finish_reason en geen letter tekst meer. Stond de sprong ervóór,
			// dan las deze route de reden nooit — en sprak de duif een op het
			// tokenplafond afgekapte halve zin uit alsof zij was uitgesproken.
			if r := providerStop(chunk.FinishReason); r != "" {
				stopReason = r
			}
			if chunk.Content == "" {
				continue
			}
			if !accept(chunk.Content) {
				aborted = true
				stop()
				open = false
			}
		}
	}
	// Een stroom die op de tijdslimiet stukliep ziet er van hier af precies zo
	// uit als een stroom die netjes eindigde: het kanaal gaat dicht en de
	// foutchunk van de provider haalt het lang niet altijd — die verliest de
	// race met de geannuleerde context. Zonder deze controle boekte evaluate()
	// zo'n call als een geslaagde PASS: de storingsteller ging terug naar nul,
	// de generator degradeerde nooit naar alleen-knop, en het venster schoof op
	// over gesprek dat de duif nooit gelezen heeft.
	if !aborted {
		if err := streamCtx.Err(); err != nil {
			return nil, reasonCandidateTimeout, err
		}
	}
	if !decided {
		if line, ready := split.finish(); ready {
			header, reason = parseCandidateHeader(line)
		}
	}
	body.WriteString(filter.flush())

	if !header.Speak {
		// headerOK blijft headerOK: een nette PASS is geen misvorming.
		return nil, reason, nil
	}
	text := strings.TrimSpace(body.String())
	if s.persona.Chat && text == "" {
		return nil, "empty_body", nil
	}
	return d.buildCandidate(win, header, text, ground, stopReason), reason, nil
}

// buildCandidate maakt de kandidaat die de sessie vasthoudt. Een thema-id dat
// deze duif niet kent kost de bijdrage niet: hij hangt dan aan geen thema, en
// de vergadering ziet dat de duif iets heeft over wat er net gezegd is.
func (d *candidateEngine) buildCandidate(win transcriptWindow, header candidateHeader, text string, ground candidateGrounding, stopReason string) *meetingCandidate {
	c := &meetingCandidate{
		ID:         newID(),
		Text:       text,
		Status:     ground.Status,
		Sources:    ground.Sources,
		RAGSection: ground.RAGSection,
		Dropped:    ground.Dropped,
		StopReason: stopReason,
		Sound:      !d.session.persona.Chat,
		Score:      header.Score,
		Addressed:  header.Addressed,
		At:         d.engine.clock.Now(),
		Window:     append([]TranscriptSegment(nil), win.Segments...),
	}
	if n := len(win.Segments); n > 0 {
		c.At = win.Segments[n-1].At
		c.Seq = win.Segments[n-1].Seq
	}
	if topic, ok := d.session.persona.Topic(header.TopicID); ok {
		c.TopicID, c.Label = topic.ID, topic.Label
	} else {
		d.session.diagnostics.mark(c.At, "candidate_header", headerUnknownTopic, 0)
	}
	return c
}

// resolveCandidate legt de klaarliggende kandidaat aan de klok voor en laat
// hem klinken zodra die het toestaat.
func (d *candidateEngine) resolveCandidate(now time.Time) {
	c := d.session.heldCandidate()
	if c == nil {
		return
	}
	v := d.gov.assess(now, c, d.sensitivity(c), d.session.governorState())
	d.mu.Lock()
	changed := v != d.lastVerdict
	d.lastVerdict = v
	d.mu.Unlock()
	if changed {
		d.session.diagnostics.mark(now, "governor_decision", string(v), now.Sub(c.At))
		d.gov.note(v)
	}
	if v != verdictAdmit {
		return
	}
	d.speak(now, c)
}

// speak laat één kandidaat klinken. De sessie geeft hem maar één keer af, dus
// twee tikken tegelijk kunnen de duif niet twee keer aan het woord helpen.
func (d *candidateEngine) speak(now time.Time, c *meetingCandidate) {
	taken := d.session.takeCandidate(c.ID)
	if taken == nil {
		return
	}
	d.gov.admit(now, taken)
	d.mu.Lock()
	d.lastVerdict = verdictNone
	d.mu.Unlock()
	if d.session.persona.Chat {
		d.session.publish(MeetingEvent{
			Type:      EventIntervention,
			Topic:     topicRefOf(taken),
			Score:     taken.Score,
			Addressed: taken.Addressed,
		})
	} else {
		d.session.publish(MeetingEvent{Type: EventIntervention})
	}
	if d.engine.intervene != nil {
		d.engine.intervene(d.session, taken)
	}
}

// speakNow is de knop "laat horen" onder het kandidaatblok: de gebruiker vraagt
// er zelf om, dus de stilte-eis, de lat en de cooldown gelden niet. Alleen een
// duif die al aan het woord is kan hem tegenhouden.
func (d *candidateEngine) speakNow() bool {
	// Stop en de uit-stand gelden ook hier. "Laat horen" gaat langs de stilte,
	// de lat en de rusttijd — dat zijn wachtregels — maar niet langs een
	// gebruiker die net gezegd heeft dat de duif moet zwijgen.
	if st := d.session.governorState(); !st.Autonomous || st.Paused {
		return false
	}
	c := d.session.heldCandidate()
	if c == nil || d.session.isSpeaking() {
		return false
	}
	now := d.engine.clock.Now()
	d.session.diagnostics.mark(now, "governor_decision", "requested", now.Sub(c.At))
	d.speak(now, c)
	return true
}

func (d *candidateEngine) sensitivity(c *meetingCandidate) float64 {
	if t, ok := d.session.persona.Topic(c.TopicID); ok {
		return t.Gevoeligheid
	}
	return 0.5
}

func topicRefOf(c *meetingCandidate) *TopicRef {
	if c == nil || c.TopicID == "" {
		return nil
	}
	return &TopicRef{ID: c.TopicID, Label: c.Label}
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// evaluationCode is het woord dat in de diagnostiek terechtkomt. Een lege
// reden betekent "de kopregel klopte"; wat er dan gebeurde staat in de
// uitkomst, niet in de reden.
func evaluationCode(reason string, cand *meetingCandidate) string {
	if reason != headerOK {
		return reason
	}
	if cand == nil {
		return "pass"
	}
	return "speak"
}

// finishWindow schuift de cadans-teller op: deze woorden zijn gezien.
func (d *candidateEngine) finishWindow(win transcriptWindow, consumed int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	if n := len(win.Segments); n > 0 {
		d.lastSeq = max(d.lastSeq, win.Segments[n-1].Seq)
	}
	d.pending = max(d.pending-consumed, 0)
}

// skip telt een overgeslagen evaluatie. De reden is een vaste code, nooit
// transcript- of antwoordtekst: vergaderinhoud komt nooit in het log.
func (d *candidateEngine) skip(reason string) {
	d.mu.Lock()
	d.counts.Skipped++
	n := d.counts.Skipped
	d.mu.Unlock()
	d.engine.log().Debug("kandidaatevaluatie overgeslagen", "component", "duif",
		"sessie", d.session.id, "reden", reason, "overgeslagen_totaal", n)
}

// guard is het vangnet van de generator-goroutines. Een paniek in deze keten —
// de LLM-client, de parser, de klok, het spreekpad — nam zonder dit vangnet het
// hele serverproces mee, en daarmee elke andere vergadering én elke chat. Nu is
// hij wat hij hoort te zijn: het einde van deze zijtak, met een zichtbare
// degradatie naar alleen-knop.
//
// De paniekwaarde zelf gaat níét het log in: hij kan transcriptinhoud dragen,
// en vergaderinhoud komt nooit in het log. Wat er wél in staat is een plek en een teller.
func (d *candidateEngine) guard(where string) {
	r := recover()
	if r == nil {
		return
	}
	d.mu.Lock()
	d.counts.Panicked++
	n := d.counts.Panicked
	d.mu.Unlock()
	d.engine.log().Error("kandidaatgenerator paniekte, gedegradeerd naar alleen-knop",
		"component", "duif", "sessie", d.session.id, "plek", where, "paniek_totaal", n)
	d.disable(ReasonDetectorFailed)
}

// disable degradeert naar alleen-knop, één keer en zichtbaar.
func (d *candidateEngine) disable(reason string) {
	d.mu.Lock()
	if !d.enabled {
		d.mu.Unlock()
		return
	}
	d.enabled = false
	d.reason = reason
	status := d.statusLocked(d.engine.clock.Now())
	d.mu.Unlock()
	d.session.publish(MeetingEvent{
		Type:     EventDetectorStatus,
		Detector: &status,
		Reason:   reason,
		Message:  detectorPausedMessage(reason),
	})
}
