// Package soniox spreekt het realtime-protocol van Soniox en levert daarmee
// de twee audio-seams van parai-core: audio.RealtimeTranscriber (spraak naar
// tekst) en audio.Synthesizer (tekst naar spraak).
//
// Waarom dit pakket bestaat: de vergadermodus streamt de stem van echte
// mensen naar een derde partij, en die stroom moet in de EU kunnen blijven.
// Soniox biedt per project een EU-regio met eigen endpoints
// (stt-rt.eu.soniox.com / tts-rt.eu.soniox.com) en regio-gebonden sleutels.
// Dit pakket praat daar rechtstreeks tegenaan, achter dezelfde interfaces, dus
// de vergadermodus zelf verandert geen regel.
//
// Wat hier níét gebeurt, is even belangrijk als wat er wel gebeurt: er wordt
// geen audio gebufferd buiten de kanalen, er wordt niets naar schijf
// geschreven, en geen enkele logregel draagt transcript- of uitingstekst:
// vergaderinhoud blijft vluchtig. De privacytoets in protocol_test.go bewaakt
// dat statisch.
package soniox

import (
	"strings"
)

// Wire-typen van het STT-WebSocket-protocol
// (https://soniox.com/docs/api-reference/stt/websocket-api).

// sttConfig is het eerste bericht op de socket: één JSON-frame met de sleutel
// en de sessie-instellingen. Soniox kent geen auth-header — de sleutel rijdt
// mee in dit bericht, en daarom staat hij nergens in een URL of een logregel.
type sttConfig struct {
	APIKey      string `json:"api_key"`
	Model       string `json:"model"`
	AudioFormat string `json:"audio_format"`
	SampleRate  int    `json:"sample_rate,omitempty"`
	NumChannels int    `json:"num_channels,omitempty"`
	// LanguageHints stuurt het model naar de taal van de sessie zonder hem
	// vast te zetten: samen met EnableLanguageIdentification blijft een
	// vergadering die halverwege naar het Engels omslaat gewoon leesbaar.
	LanguageHints                []string `json:"language_hints,omitempty"`
	EnableLanguageIdentification bool     `json:"enable_language_identification,omitempty"`
	EnableSpeakerDiarization     bool     `json:"enable_speaker_diarization,omitempty"`
	EnableEndpointDetection      bool     `json:"enable_endpoint_detection,omitempty"`
}

// sttControl is een besturingsbericht op dezelfde socket: keepalive tegen de
// inactiviteitstimer, finalize om het lopende segment af te sluiten.
type sttControl struct {
	Type string `json:"type"`
}

const (
	controlKeepalive = "keepalive"
	controlFinalize  = "finalize"
)

// sttToken is één woord(deel) uit het antwoord. `is_final` is de hele
// semantiek: een niet-finale token is een gok die in een volgend bericht
// wordt vervangen, een finale token komt precies één keer langs en wordt
// daarna nooit meer herhaald. De aggregator hieronder leunt op die belofte.
type sttToken struct {
	Text     string  `json:"text"`
	IsFinal  bool    `json:"is_final"`
	Speaker  string  `json:"speaker,omitempty"`
	Language string  `json:"language,omitempty"`
	StartMS  int     `json:"start_ms,omitempty"`
	EndMS    int     `json:"end_ms,omitempty"`
	Conf     float64 `json:"confidence,omitempty"`
}

// sttResponse is één serverbericht.
type sttResponse struct {
	Tokens       []sttToken `json:"tokens"`
	Finished     bool       `json:"finished"`
	ErrorCode    int        `json:"error_code"`
	ErrorMessage string     `json:"error_message"`
}

const (
	// markerEnd is de token die Soniox stuurt zodra endpoint-detectie een
	// uiting afsluit; markerFin doet hetzelfde na een expliciete finalize.
	// Ze dragen geen tekst — ze zijn de knip tussen twee segmenten.
	markerEnd = "<end>"
	markerFin = "<fin>"
)

// defaultMaxSegmentChars is de vangrail onder de endpoint-detectie. Soniox
// knipt een segment af bij een adempauze; wie een kwartier aan één stuk
// doorpraat zou anders nooit een gecommit segment opleveren en dus nooit in
// het vergadervenster belanden. Dan knippen we zelf, op de eerste woordgrens
// voorbij deze lengte.
const defaultMaxSegmentChars = 600

// transcriptEvent is één uitgaand bericht richting de brug: een partial is
// weergave, een final is voer voor het vergadervenster.
type transcriptEvent struct {
	Text    string
	Speaker string
	Final   bool
}

// aggregator vertaalt de tokenstroom van Soniox naar de segmentsemantiek die
// audio.TranscriptionEvent verwacht.
//
// Het verschil tussen de twee providers zit precies hier. ElevenLabs stuurt
// hele zinnen ("partial_transcript", "committed_transcript"); Soniox stuurt
// losse tokens en laat het aan de client om er een zin van te maken. De
// aggregator is die client: hij plakt finale tokens aan elkaar tot een
// markertoken (of de vangrail) zegt dat de uiting klaar is.
//
// Hij houdt bewust maar één ding vast — de lopende zin. Dat is geen opslag
// maar het schuivende venster van één uiting; zodra het segment gecommit is,
// is het weg.
type aggregator struct {
	maxSegmentChars int

	final   strings.Builder
	speaker string
}

func newAggregator() *aggregator {
	return &aggregator{maxSegmentChars: defaultMaxSegmentChars}
}

func (a *aggregator) limit() int {
	if a.maxSegmentChars > 0 {
		return a.maxSegmentChars
	}
	return defaultMaxSegmentChars
}

// consume verwerkt één serverbericht en geeft de events die eruit volgen, in
// volgorde. Een bericht kan er meerdere opleveren: een afgesloten segment en
// meteen daarna de eerste partial van het volgende.
func (a *aggregator) consume(tokens []sttToken) []transcriptEvent {
	// Soniox stuurt ook berichten zonder tokens (voortgang, keepalive-antwoord).
	// Die veranderen niets, en dan hoort er ook niets naar de weergave te gaan:
	// anders herhaalt elke stille tik de lopende zin.
	if len(tokens) == 0 {
		return nil
	}
	var out []transcriptEvent
	var pending strings.Builder

	flush := func() {
		if ev, ok := a.close(); ok {
			out = append(out, ev)
		}
	}

	for _, tok := range tokens {
		if !tok.IsFinal {
			pending.WriteString(tok.Text)
			if a.speaker == "" {
				a.speaker = tok.Speaker
			}
			continue
		}
		if tok.Text == markerEnd || tok.Text == markerFin {
			// Een marker sluit het segment. Wat er in dit bericht nog aan
			// niet-finale tokens stond hoort al bij de volgende uiting.
			flush()
			continue
		}
		a.final.WriteString(tok.Text)
		if a.speaker == "" {
			a.speaker = tok.Speaker
		}
		if a.final.Len() >= a.limit() {
			flush()
		}
	}

	if partial := strings.TrimSpace(a.final.String() + pending.String()); partial != "" {
		out = append(out, transcriptEvent{Text: partial, Speaker: a.speaker, Final: false})
	}
	return out
}

// close sluit het lopende segment af. De tweede returnwaarde is false als er
// niets uitspreekbaars stond — een marker op een leeg segment is geen event.
func (a *aggregator) close() (transcriptEvent, bool) {
	text := strings.TrimSpace(a.final.String())
	speaker := a.speaker
	a.final.Reset()
	a.speaker = ""
	if text == "" {
		return transcriptEvent{}, false
	}
	return transcriptEvent{Text: text, Speaker: speaker, Final: true}, true
}
