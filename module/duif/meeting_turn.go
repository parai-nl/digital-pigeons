package duif

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/parai-nl/parai-core/llm"
)

// Voice is de TTS-seam van de vergadermodus: een spreekbeurt gaat open, krijgt
// zin voor zin tekst zodra de LLM die afmaakt en levert intussen al audioframes
// — af te breken via de context (stopknop). nil = tekst-only. Frames zijn
// onderweg-data: ze gaan het kanaal op en worden nooit bewaard.
//
// Waarom streaming en niet "hele uiting in, frames uit": de duif begon pas te
// praten als het volledige antwoord er stond, plus de volledige synthese. Bij
// een antwoord van zeshonderd tekens was dat negen seconden stilte na de
// eerste zin op het scherm. Nu opent de beurt de stem bij de eerste
// uitspreekbare zin en klinkt die terwijl de rest nog geschreven wordt.
type Voice interface {
	// Open opent één spreekbeurt met deze stem en taal.
	Open(ctx context.Context, req SpeechRequest) (Speech, error)
	// Format zegt wat er in de frames zit, zodat de client zijn speler kiest
	// vóór het eerste frame binnen is.
	Format() AudioFormat
}

// Speech is één open spreekbeurt. Say en End komen van de beurt-goroutine;
// Frames leest de pomp die de frames het kanaal op zet. Say krijgt tekst die
// al uitspreekbaar is (zie sentenceFeeder) en blokkeert nooit langer dan de
// context of de providersessie leeft.
type Speech interface {
	Say(text string)
	End()
	Frames() <-chan []byte
}

// SpeechRequest is de stem, de taal en het spreektempo van één spreekbeurt.
type SpeechRequest struct {
	VoiceID string
	Lang    string
	// Speed is het spreektempo van deze duif: 1,0 is het normale
	// tempo van de dienst, hoger is sneller. 0 betekent "geen voorkeur" en
	// laat de provider zijn eigen tempo kiezen — dat is ook wat een provider
	// zonder tempoknop hier krijgt. Het is nadrukkelijk een nuance op één
	// gedeelde stem: de stem zelf staat in de providerconfiguratie.
	Speed float64
}

// AudioFormat is wat er in de audioframes zit. De client speelt PCM16 blok
// voor blok af zodra het binnenkomt; MP3 kan hij pas decoderen als de beurt
// compleet is (losse MP3-chunks zijn geen bestand), dus daar wacht hij op
// tts_done — het oude gedrag, en de reden dat de ElevenLabs-stem later klinkt
// dan de Soniox-stem.
type AudioFormat struct {
	Codec      string
	SampleRate int
}

const (
	CodecMP3   = "mp3"
	CodecPCM16 = "pcm16"
)

var (
	// errPigeonDoesNotSpeak: de non-verbale duif kent geen spreekbeurt; haar
	// interventie is de geluidenset.
	errPigeonDoesNotSpeak = errors.New("duif: deze duif spreekt niet")
	// errPigeonBusy: het spreekslot is bezet.
	errPigeonBusy = errors.New("duif: de duif is aan het woord")
	// errPigeonDoesNotSound: deze duif heeft geen geluidenset — voor haar is
	// de spreekknop de handmatige route.
	errPigeonDoesNotSound = errors.New("duif: deze duif maakt geen geluid")
	// errNoSoundAvailable: er viel niets te kiezen uit de geluidenset.
	errNoSoundAvailable = errors.New("duif: geen geluid beschikbaar")
)

// respondNow is de knop "de duif antwoordt nu": de gegarandeerde basis. Hij
// leest het venster rechtstreeks en deelt geen enkel pad met de zelfstandige
// route, zodat hij blijft werken wat er ook met detectie en zelfstandig
// ingrijpen gebeurt.
func (e *meetingEngine) respondNow(s *meetingSession) error {
	if !s.persona.Chat {
		return errPigeonDoesNotSpeak
	}
	gen, ctx, cancel, ok := s.beginTurn()
	if !ok {
		s.publish(MeetingEvent{Type: EventBusy, Message: busyMessage})
		return errPigeonBusy
	}
	go e.runTurn(s, gen, ctx, cancel)
	return nil
}

// runTurn draait één spreekbeurt van de knop: venster → (kennisbank) → prompt
// → LLM-stream → TTS. Elke stap hangt aan de beurt-context; de stopknop
// annuleert die en alles wat daarna nog binnenkomt wordt weggegooid.
//
// Dit is de gegarandeerde basis en ook de enige route die hier
// nog langskomt: een zelfstandige bijdrage is al geschreven en gaat langs
// speakCandidate.
func (e *meetingEngine) runTurn(s *meetingSession, gen uint64, ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	defer s.endTurn(gen)
	// Laatst geregistreerd, dus eerst uitgevoerd: de paniek wordt opgevangen en
	// gemeld terwijl de beurtgeneratie nog geldig is, daarna geeft endTurn het
	// spreekslot vrij. Zonder dit vangnet nam één kapotte beurt de hele server
	// mee — inclusief elke andere vergadering.
	defer e.recoverTurn(s, gen)

	if !s.emitTurn(gen, MeetingEvent{Type: EventPersona, PigeonID: s.pigeonID, Lang: s.lang}) {
		return
	}

	// Eerst de lopende uiting laten afronden, dán pas lezen. Wie op
	// zijn laatste woord drukt, drukt op een venster waar die zin nog niet in
	// staat; hij staat er een paar honderd milliseconden later wel in.
	settle := e.settleSpeech(ctx, s)
	if ctx.Err() != nil {
		// Tijdens dat wachten kan de stopknop gevallen zijn, of de beurtklok.
		e.endExpiredTurn(ctx, s, gen)
		return
	}
	window := s.window()
	// De leegtetoets gaat over het gesprek, niet over haarzelf. Een duif die al
	// gesproken heeft maar niets meer hoort, heeft nog steeds niets om op te
	// reageren; haar eigen bijdragen zijn context, nooit een aanleiding.
	if len(window) == 0 {
		// Leeg venster: een korte gescripte reactie, geen LLM-gok over
		// niet-gehoorde inhoud. Welke van de drie het wordt zegt waaróm er
		// niets staat — dat scheelt de tafel een logboek.
		reason := s.emptyWindowReason(settle)
		e.speakScripted(ctx, s, gen, meetingEmptyWindowReply(reason, s.lang), "vergaderbeurt zonder venster",
			"oorzaak", reason, "afronding_gevraagd", settle.requested, "wacht_ms", settle.waited.Milliseconds())
		return
	}

	// Er staat wel iets in het venster, maar is er sinds haar vorige bijdrage
	// ook iets nieuws gezegd? Zo niet, dan beantwoordt ze het venster
	// niet nog eens: één verdieping op haar eigen punt met de bronnen van toen,
	// of de vaste zin zonder modelronde.
	quiet, basis := s.quietSinceOwnPoint(window)
	if quiet != quietNo && settle.pending && !settle.landed {
		// Iemand was nog aan het woord toen de knop viel en die zin landde niet
		// op tijd: "niets nieuws gehoord" is dan onwaar, en een verdieping gaat
		// aan die spreker voorbij. Dezelfde zin als bij een leeg venster.
		e.speakScripted(ctx, s, gen, meetingEmptyWindowReply(emptyWindowUnfinished, s.lang), "vergaderbeurt zonder nieuwe inbreng",
			"oorzaak", emptyWindowUnfinished, "segmenten", len(window), "wacht_ms", settle.waited.Milliseconds())
		return
	}
	if quiet == quietNothingNew {
		e.speakScripted(ctx, s, gen, meetingNothingNewReply[s.lang], "vergaderbeurt zonder nieuwe inbreng",
			"oorzaak", "niets_nieuw", "segmenten", len(window), "wacht_ms", settle.waited.Milliseconds())
		return
	}

	status := KBPersonaOnly
	var sources []Source
	var ragSection string
	var dropped *SourceDrops
	if quiet == quietDeepen {
		// Haar eigen bronnen, in de nummering van haar vorige punt: het
		// menselijke venster is niet veranderd, dus opnieuw zoeken kost een
		// herformuleringsronde voor dezelfde zoekvraag.
		status, sources, ragSection = basis.Status, basis.Sources, basis.RAGSection
	} else if s.persona.Kennisbank {
		res, err := e.retrieve(ctx, s, window)
		if err != nil && turnExpired(ctx) {
			// Niet de kennisbank viel weg, de beurtklok liep af terwijl zij
			// zocht: dat is de melding die de tafel hoort te zien.
			e.endExpiredTurn(ctx, s, gen)
			return
		}
		if err != nil {
			// De kennisbank was weg: geen beurt. Een duif die dan tóch praat,
			// praat uit het niets — en geen kennisbank betekent geen antwoord.
			e.log().Error("vergaderbeurt zonder kennisbank", "component", "duif",
				"sessie", s.id, "reason", ReasonKBUnavailable)
			e.emitFailure(s, gen, ReasonKBUnavailable)
			return
		}
		status, sources, ragSection = res.Status, res.Sources, res.RAGSection
		dropped = res.Dropped
	}
	if ctx.Err() != nil {
		e.endExpiredTurn(ctx, s, gen)
		return
	}
	// De bronnen gaan het kanaal op zodra ze er zijn, niet pas bij
	// response_done: de client heeft ze nodig om de eerste "[1]" al tijdens het
	// streamen als verwijzing te tekenen. Dezelfde volgorde als in de chat.
	if !s.emitTurn(gen, MeetingEvent{Type: EventSources, KBStatus: status, Sources: sources, Dropped: dropped}) {
		return
	}
	messages := []llm.Message{
		{Role: "system", Content: BuildSystemPrompt(PromptInput{
			Persona: s.persona, Personas: e.personas,
			Lang: s.lang, Status: status, RAGSection: ragSection,
			Meeting: true, Deepen: quiet == quietDeepen,
		})},
		// Het venster is het gesprek plus wat de duif er zelf al aan toevoegde:
		// zonder dat laatste ziet ze bij een tweede knopdruk binnen
		// anderhalve minuut dezelfde menselijke zinnen en nooit haar eigen
		// antwoord, en beantwoordt ze alles opnieuw.
		{Role: "user", Content: renderTranscriptWindow(s.lang, s.markToken, s.pigeonID, window, s.ownContributionSegments())},
	}

	// De stem loopt mee met de LLM-stream: elke afgemaakte zin gaat meteen
	// naar de provider, en de audioframes stromen intussen al het kanaal op.
	sp := e.newSpeaker(ctx, s, gen)
	// Welke route deze beurt ook verlaat, de stem gaat mee dicht. Zonder dit
	// bleef de stemgoroutine op elke afgebroken beurt in haar leeslus staan.
	defer sp.close()
	// Wat de mensen aan tafel zeiden, los van de duiven. Een regel van een duif
	// die hier eerder zat is context voor de prompt, geen menselijke uitspraak,
	// en telt hier dus niet mee. Haar eigen bijdragen evenmin: die komen niet uit
	// het venster en dragen sowieso een duif-id.
	var humanContext []string
	for _, segment := range window {
		if segment.PigeonID == "" {
			humanContext = append(humanContext, segment.Text)
		}
	}
	full, stopReason, ok := e.streamTurn(ctx, s, gen, messages, sp, sources, humanContext)
	if !ok {
		// De beurt is afgebroken. Eerst de context door: daarop stoppen de
		// stemsessie en de pomp, zodat er geen frames meer het kanaal op gaan
		// nadat deze beurt al is afgesloten. Pas daarna sluit de uitgestelde
		// close() het zinnenkanaal.
		cancel()
		// Wat er wél geleverd is blijft op het scherm staan, afgebroken en al.
		return
	}
	// Het geschreven antwoord is hier af. sp.finish() wacht daarna op de laatste
	// audioframes; dat kan seconden duren en zegt niets meer over de tekst. Dus
	// eerst text_done — de cursor verdwijnt en de kaart gaat naar "spreekt" —
	// en pas ná de stem response_done, dat de beurt afsluit.
	s.emitTurn(gen, MeetingEvent{Type: EventTextDone, KBStatus: status, Sources: sources, StopReason: stopReason})
	sp.finish()
	// Wat de duif zei blijft in het geheugen van de sessie staan: bij een
	// duifwissel gaat het als háár bijdrage mee, en de duif zelf leest het
	// terug om zichzelf niet te herhalen — in haar kandidaatprompt en in het
	// venster van de volgende knopdruk.
	s.noteContribution(full, lastHumanSeq(window), ownGrounding{Status: status, Sources: sources, RAGSection: ragSection})
	s.finishTurn(gen, MeetingEvent{Type: EventResponseDone, KBStatus: status, Sources: sources, StopReason: stopReason})
	// Deze regel blijft ná finishTurn staan: TestMeetingRespondNow_ResponseDoneMeansTheSlotIsFree
	// houdt de beurt op precies dit bericht vast om te bewijzen dat het slot dan al vrij is.
	e.log().Info("vergaderbeurt klaar", "component", "duif", "sessie", s.id,
		"duif", s.pigeonID, "aanleiding", "knop", "segmenten", len(window),
		"bronnen", len(sources), "tekens", len(full), "stop", stopReason,
		"verdieping", quiet == quietDeepen, "wacht_ms", settle.waited.Milliseconds())
}

// speakScripted spreekt een vaste zin uit, zonder modelronde: bij een leeg
// venster en bij een knopdruk zonder nieuwe inbreng. De
// zin gaat niet in het geheugen van eigen bijdragen: hij is geen punt waarop
// een volgende beurt hoort voort te bouwen.
func (e *meetingEngine) speakScripted(ctx context.Context, s *meetingSession, gen uint64, reply, logMsg string, logArgs ...any) {
	s.emitTurn(gen, MeetingEvent{Type: EventText, Delta: reply})
	s.emitTurn(gen, MeetingEvent{Type: EventTextDone, KBStatus: KBPersonaOnly})
	sp := e.newSpeaker(ctx, s, gen)
	defer sp.close()
	sp.feed(reply)
	sp.finish()
	// Eerst het log, dan response_done: wie de beurt af ziet, vindt de
	// oorzaak ook al in het log.
	e.log().Info(logMsg, append([]any{"component", "duif", "sessie", s.id}, logArgs...)...)
	s.finishTurn(gen, MeetingEvent{Type: EventResponseDone, KBStatus: KBPersonaOnly})
}

// turnDelivery houdt bij wat er van een beurt werkelijk bij de mensen aan tafel
// aankwam.
//
// De volgorde in send is het hele punt: eerst leveren, dan pas boeken. emitTurn
// weigert elke chunk van een verlopen beurtgeneratie en van een gesloten sessie
// (stopknop), en zo'n delta bereikte dus geen scherm en geen stem. Stond hij
// tóch al in de tekst, dan zou de sessie een zin onthouden die niemand gezien
// heeft.
type turnDelivery struct {
	s    *meetingSession
	gen  uint64
	sp   *speaker
	full strings.Builder
}

func (d *turnDelivery) send(delta string) bool {
	if delta == "" {
		return true
	}
	if !d.s.emitTurn(d.gen, MeetingEvent{Type: EventText, Delta: delta}) {
		return false
	}
	d.full.WriteString(delta)
	d.sp.feed(delta)
	return true
}

// text is wat er geleverd is: precies de deltas die het kanaal op gingen.
func (d *turnDelivery) text() string { return d.full.String() }

// length is dezelfde tekst in tekens, voor de WARN-regel van een afgebroken
// beurt — die telt tekens en noemt de tekst zelf nooit.
func (d *turnDelivery) length() int { return d.full.Len() }

// maxQueryRunes is het venster dat de vraag aan de kennisbank meekrijgt. Een
// vergadervenster kan honderden zinnen lang zijn; een embedding van dat geheel
// middelt elk onderwerp weg. De laatste ~1500 tekens zijn waar het gesprek nú
// over gaat.
const maxQueryRunes = 1500

// retrieve draait de gedeelde retrieval op het vergadervenster.
// Zonder gewirede kennisbank volgt kb_unavailable, net als in chat.
//
// Dit is de zoekopdracht van de vergadertafel (runMeeting: topK 24, lat
// meetingMinSimilarity 0,65), dezelfde als die van de kandidaatgenerator in
// meeting_detect.go. Een transcriptvenster is geen chatvraag: het is een reeks
// gesproken zinnen waar het onderwerp verspreid in zit, en de chatlat 0,73 liet
// daar bij acht segmenten nul bronnen over terwijl de zelfstandige route op
// hetzelfde gesprek wél bronnen vond (zie de constante in retrieval.go). De
// chat houdt zijn eigen lat; alleen de vergadertafel zoekt lager.
//
// Er gaat één korte modelronde aan vooraf: het venster wordt
// eerst tot één zoekvraag herleid (meeting_query.go). De knop herformuleert
// elke keer — hij wordt per beurt één keer ingedrukt, en de lezer wacht toch al
// op deze beurt. Mislukt de herformulering, dan zoekt hij met het ruwe venster,
// precies zoals voorheen.
func (e *meetingEngine) retrieve(ctx context.Context, s *meetingSession, window []TranscriptSegment) (retrievalResult, error) {
	q := e.reformulateQuery(ctx, s.lang, routeMeetingQuery, window)
	e.logQuery(s.id, routeMeetingQuery, q)
	return e.retrieval.runMeeting(ctx, q.Query)
}

// truncateQuery houdt de staart van de tekst over, afgekapt op een zinsgrens
// zodat de vraag niet middenin een woord begint. Past de tekst al, dan komt hij
// ongewijzigd terug.
func truncateQuery(text string) string {
	rs := []rune(text)
	if len(rs) <= maxQueryRunes {
		return text
	}
	tail := rs[len(rs)-maxQueryRunes:]
	// Zoek het eerste zinseinde in de staart en begin daarna; vinden we er
	// geen, dan is de knip op teken-niveau het beste wat er is.
	for i := 0; i < len(tail)-1; i++ {
		if sentenceEnd[tail[i]] {
			return strings.TrimSpace(string(tail[i+1:]))
		}
	}
	return strings.TrimSpace(string(tail))
}

// streamTurn pompt de LLM-stream naar tekstdelta's. Eerste returnwaarde is de
// tekst die werkelijk bij de tafel aankwam — ook wanneer de beurt afbrak, want
// die tekst blijft op het scherm staan en hoort haar brondekkingslabel te
// krijgen. Tweede is de stopreden: leeg bij een normaal einde,
// "truncated" als de provider de stream op de tokengrens sloot en "filtered"
// als zijn inhoudsfilter hem sloot. Derde is false wanneer de beurt is
// afgebroken: dan volgt er geen text_done en geen response_done meer van
// runTurn. Liep de beurtklok af, dan heeft streamTurn de beurt zelf al met een
// eigen foutmelding afgesloten; bij een stopknop of een gesloten
// sessie volgt er niets, want daar weet de tafel al waarom het stil is.
func (e *meetingEngine) streamTurn(ctx context.Context, s *meetingSession, gen uint64, messages []llm.Message, sp *speaker, sources []Source, humanContext []string) (string, string, bool) {
	if ctx.Err() != nil {
		e.endExpiredTurn(ctx, s, gen)
		return "", "", false
	}
	if e.llm == nil {
		e.log().Error("vergaderbeurt zonder llm-service", "component", "duif", "sessie", s.id)
		e.emitFailure(s, gen, ReasonLLMUnavailable)
		return "", "", false
	}
	ch, err := openAnswerStream(withLLMRoute(ctx, routeMeeting), e.llm, llm.CompletionRequest{Model: e.model, Messages: messages, Stream: true, MaxTokens: answerMeetingMaxTokens, ReasoningEffort: meetingReasoning()}, sources, answerPolicy{Persona: s.persona, Lang: s.lang, Meeting: true, HumanContext: humanContext}, e.answerStream)
	if err != nil && turnExpired(ctx) {
		e.endExpiredTurn(ctx, s, gen)
		return "", "", false
	}
	if err != nil {
		reason := classifyLLMError(err)
		e.log().Error("vergaderbeurt: llm-stream start faalde", "component", "duif",
			"sessie", s.id, "reason", reason)
		e.emitFailure(s, gen, reason)
		return "", "", false
	}

	delivery := &turnDelivery{s: s, gen: gen, sp: sp}
	stopReason := ""
	started := time.Now()
	// De beurt kan eindigen vóórdat het antwoord af is: de stopknop, een
	// nieuwe aanleiding, het einde van de sessie, of gewoon een modelcall die
	// zo lang duurde dat de beurtklok hem inhaalde. Eén WARN-regel maakt het
	// achteraf verklaarbaar: waarom, in welke fase, hoe lang hij al liep en
	// hoeveel tekens er waren. Nooit de tekst zelf.
	//
	// Op het scherm verloopt alleen de beurtklok niet stil meer. Bij de
	// stopknop, een nieuwe aanleiding of een gesloten sessie weet de tafel al
	// waarom de duif zweeg; bij de beurtklok wist niemand het, en bleef de kaart
	// op "denkt" staan tot iemand zelf op stop drukte.
	cutShort := func() (string, string, bool) {
		reason := s.stopReasonFor(gen)
		if reason == "" {
			reason = "reader_gone"
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				reason = ReasonTurnTimeout
			}
		}
		e.log().Warn("vergaderbeurt eindigde vóór het antwoord af was", "component", "duif",
			"sessie", s.id, "reden", reason, "fase", routeMeeting,
			"ms", time.Since(started).Milliseconds(), "tekens", delivery.length())
		if reason == ReasonTurnTimeout {
			e.emitTurnTimeout(s, gen)
		}
		// De geleverde tekst gaat wél mee terug: zij staat op het scherm en
		// verdient haar label. `false` blijft "er volgt niets meer
		// van runTurn", niet "er is niets geweest".
		return delivery.text(), "", false
	}
	filter := citationFilter(sources)
	send := delivery.send
	for {
		select {
		case <-ctx.Done():
			return cutShort()
		case chunk, ok := <-ch:
			// Na een stopknop komt hier niets meer door: emitTurn hieronder
			// weigert elke chunk van een verlopen beurtgeneratie.
			//
			// Een stream die sluit of een fout geeft nádat de context afliep,
			// sloot omdát die afliep: de select kiest willekeurig tussen twee
			// klare kanalen, en zonder deze toets ging een afgekapt antwoord
			// soms als compleet de deur uit, of als providerfout.
			if (!ok || chunk.Err != nil) && ctx.Err() != nil {
				return cutShort()
			}
			if !ok {
				if !send(filter.flush()) {
					return delivery.text(), "", false
				}
				return delivery.text(), stopReason, true
			}
			if chunk.Err != nil {
				reason := classifyLLMError(chunk.Err)
				e.log().Error("vergaderbeurt: llm-stream brak af", "component", "duif",
					"sessie", s.id, "reason", reason)
				e.emitFailure(s, gen, reason)
				return delivery.text(), "", false
			}
			if reason := providerStop(chunk.FinishReason); reason != "" {
				stopReason = reason
			}
			if chunk.Content == "" {
				continue
			}
			if !send(filter.write(chunk.Content)) {
				return delivery.text(), "", false
			}
		}
	}
}

// recoverTurn vangt een paniek in de spreekbeurt-goroutine op. De vergadering
// merkt er precies één ding van: een nette foutmelding en een vrijgegeven
// spreekknop. De paniekwaarde blijft uit het log — die kan de uiting van de
// duif of een transcriptfragment bevatten (streaming-only).
func (e *meetingEngine) recoverTurn(s *meetingSession, gen uint64) {
	if r := recover(); r != nil {
		e.log().Error("vergaderbeurt paniekte, spreekslot vrijgegeven",
			"component", "duif", "sessie", s.id, "duif", s.pigeonID)
		e.emitFailure(s, gen, ReasonInternal)
	}
}

// emitFailure meldt een mislukte beurt op het kanaal en sluit hem netjes af,
// zodat de UI het spreekslot weer vrijgeeft: geen stille storing.
func (e *meetingEngine) emitFailure(s *meetingSession, gen uint64, reason string) {
	body := errorBody(reason)
	s.emitTurn(gen, MeetingEvent{Type: EventError, Error: body.Error, Message: body.Message})
	s.finishTurn(gen, MeetingEvent{Type: EventResponseDone})
}

// meetingTurnTimeoutMessage is wat de tafel ziet als de beurtklok een beurt
// stopt. De code is die van de chat (ReasonTurnTimeout), de zin niet:
// aan tafel stelt niemand een vraag die korter kan, er is een knop.
var meetingTurnTimeoutMessage = map[string]string{
	"en": "The pigeon took too long and was stopped. Whatever is already on screen stays there. The button still works — press it to try again.",
	"nl": "De duif deed er te lang over en is gestopt. Wat al op het scherm staat, blijft staan. De knop werkt nog — druk erop om het opnieuw te proberen.",
}

// turnExpired zegt of de beurtklok (cfg.TurnTimeout) deze beurt heeft gestopt.
// Een stopknop, een nieuwe aanleiding of een gesloten sessie annuleert de
// context; alleen de klok laat hem verlopen.
func turnExpired(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.DeadlineExceeded)
}

// emitTurnTimeout sluit een beurt af die de beurtklok haalde: een fout met een
// eigen reden en daarna response_done, net als emitFailure. Zonder dit bleef de
// kaart in "denkt" staan. Is de beurt intussen gestopt of de sessie
// dicht, dan weigert emitTurn beide: daar hoort geen melding meer bij.
func (e *meetingEngine) emitTurnTimeout(s *meetingSession, gen uint64) {
	s.emitTurn(gen, MeetingEvent{Type: EventError, Error: ReasonTurnTimeout, Message: meetingTurnTimeoutMessage})
	s.finishTurn(gen, MeetingEvent{Type: EventResponseDone, StopReason: ReasonTurnTimeout})
}

// endExpiredTurn is de afsluiting van een beurt die vóór de LLM-stream al op
// een verlopen context stuitte. Alleen de beurtklok krijgt een melding; de
// WARN-regel staat erbij zodat het log dezelfde reden noemt als het scherm.
func (e *meetingEngine) endExpiredTurn(ctx context.Context, s *meetingSession, gen uint64) {
	if !turnExpired(ctx) || s.stopReasonFor(gen) != "" {
		return
	}
	e.log().Warn("vergaderbeurt eindigde vóór het antwoord af was", "component", "duif",
		"sessie", s.id, "reden", ReasonTurnTimeout, "fase", "voorbereiding", "tekens", 0)
	e.emitTurnTimeout(s, gen)
}

// logSpeechFailure meldt waarom er geen geluid komt. Een duif zonder stem is
// een configuratiestand, geen incident: die gaat als Info het log in. Alles
// wat wél mis is krijgt alleen een vaste reason-code. Ook debuglogs bewaren
// geen ruwe providerfouten: die kunnen verzoekinhoud of URLs bevatten.
func (e *meetingEngine) logSpeechFailure(s *meetingSession, err error) {
	switch {
	case errors.Is(err, errNoVoice):
		e.log().Info("vergaderbeurt blijft tekst-only", "component", "duif",
			"sessie", s.id, "duif", s.pigeonID, "reason", "no_voice")
	case errors.Is(err, errNothingToSay):
		e.log().Info("vergaderbeurt blijft tekst-only", "component", "duif",
			"sessie", s.id, "duif", s.pigeonID, "reason", "nothing_to_say")
	case errors.Is(err, errSpeechOpenTimeout):
		e.log().Error("vergaderbeurt: de stem opende niet op tijd, tekst-only", "component", "duif",
			"sessie", s.id, "duif", s.pigeonID, "reason", "tts_open_timeout")
	default:
		e.log().Error("vergaderbeurt: tts-start faalde, tekst-only", "component", "duif",
			"sessie", s.id, "duif", s.pigeonID, "reason", "tts_start_failed")
	}
}
