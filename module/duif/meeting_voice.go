package duif

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/parai-nl/ai-maps-duiven/module/duif/ttsresult"
	"github.com/parai-nl/parai-core/audio"
)

// De echte stem achter de Voice-seam: audio.Synthesizer van parai-core
// (ElevenLabs) of de Soniox-synthesizer, allebei een streaming WebSocket. Eén
// spreekbeurt is één sessie — de socket valt na twintig seconden stilte om,
// dus openhouden heeft geen zin.
//
// De adapter gebruikt Text, Audio, Result, Done en Close. Result bevestigt
// succesvolle synthese; het sluiten van Audio of Done op zichzelf doet dat niet.
// Een oudere API zonder Result blijft compileerbaar via ttsresult, maar meldt
// ontbrekende bevestiging in plaats van voltooiing.
//
// Streaming-only loopt hier dwars doorheen: de uiting gaat wél naar de provider
// (dat is het hele punt), maar nooit naar een logregel, een bestand of een
// foutmelding. Wat er in de logs terechtkomt zijn redenen en tellingen.

const (
	// defaultCoalesceBytes: frames worden opgespaard tot een blok van deze
	// grootte (MP3 44,1 kHz 128 kbps ≈ 1 s; PCM16 24 kHz ≈ 0,34 s), zodat een
	// beurt tientallen events kost in plaats van honderden.
	defaultCoalesceBytes = 16 * 1024
	// defaultFlushAfter: blijft een blok zo lang half gevuld, dan gaat het
	// toch. Zonder deze klok wachtte de eerste zin op genoeg audio voor een
	// vol blok, en dat is precies de latentie die streaming moet wegnemen.
	defaultFlushAfter = 120 * time.Millisecond
	// defaultIdleTimeout: zwijgt de provider zo lang, dan is de sessie stuk.
	defaultIdleTimeout = 15 * time.Second
)

var (
	// errNoVoice: deze duif heeft geen bruikbare stem-id (zie resolveVoiceID).
	errNoVoice = errors.New("duif: deze duif heeft geen stem")
	// errNothingToSay: na het strippen bleef er niets uitspreekbaars over.
	errNothingToSay = errors.New("duif: niets uitspreekbaars in deze uiting")
	// errSpeechOpenTimeout: de stemhost antwoordde niet binnen de eigen
	// deadline van het openen (speakerOpenTimeout). De beurt blijft tekst-only.
	errSpeechOpenTimeout = errors.New("duif: de stem opende niet op tijd")
)

// ttsSession is de leeskant van de publieke providersessie inclusief resultaat.
type ttsSession struct {
	text   chan<- string
	audio  <-chan []byte
	done   <-chan struct{}
	result <-chan error
	close  func() error
}

// wrapSession is de enige plek die audio.SynthesizeSession kent.
func wrapSession(s *audio.SynthesizeSession) ttsSession {
	return ttsSession{text: s.Text, audio: s.Audio, done: s.Done, result: ttsresult.Read(s), close: s.Close}
}

// synthesizerVoice is de Voice-implementatie op een audio.Synthesizer.
type synthesizerVoice struct {
	synth  audio.Synthesizer
	format AudioFormat
	logger *slog.Logger

	// coalesce, flush en idle zijn de knoppen; 0 betekent de default hierboven.
	coalesce int
	flush    time.Duration
	idle     time.Duration
}

// newSynthesizerVoice wired een synthesizer achter de Voice-seam. format is
// wat die synthesizer levert; de seam geeft het door aan de client.
func newSynthesizerVoice(synth audio.Synthesizer, format AudioFormat, log *slog.Logger) *synthesizerVoice {
	return &synthesizerVoice{synth: synth, format: format, logger: log}
}

func (v *synthesizerVoice) Format() AudioFormat { return v.format }

func (v *synthesizerVoice) log() *slog.Logger { return logOr(v.logger) }

func (v *synthesizerVoice) coalesceSize() int {
	if v.coalesce > 0 {
		return v.coalesce
	}
	return defaultCoalesceBytes
}

func (v *synthesizerVoice) flushAfter() time.Duration {
	if v.flush > 0 {
		return v.flush
	}
	return defaultFlushAfter
}

func (v *synthesizerVoice) idleTimeout() time.Duration {
	if v.idle > 0 {
		return v.idle
	}
	return defaultIdleTimeout
}

// Open opent één spreekbeurt bij de provider. De beurt schrijft zelf de
// zinnen (Say) en sluit af (End); de pomp die hier start bezit de uitvoer en
// de sessie.
func (v *synthesizerVoice) Open(ctx context.Context, req SpeechRequest) (Speech, error) {
	if req.VoiceID == "" {
		return nil, errNoVoice
	}
	sess, err := v.synthesize(ctx, req)
	if err != nil {
		// %w houdt de providerfout onder de melding; er staat geen tekst in.
		return nil, fmt.Errorf("duif: tts-sessie openen faalde: %w", err)
	}
	if sess == nil {
		return nil, errSpeechUnconfirmed
	}
	s := wrapSession(sess)
	if s.result == nil {
		v.closeSession(s)
		return nil, errSpeechUnconfirmed
	}
	out := make(chan []byte)
	result := make(chan error, 1)
	go v.pump(ctx, s, out, result)
	return &synthSpeech{v: v, ctx: ctx, s: s, out: out, result: result}, nil
}

// speedSynthesizer is de uitbreiding op audio.Synthesizer voor providers die
// een spreektempo per aanroep kennen. Soniox kent dat (`speed`, 0,7–1,3),
// ElevenLabs in de huidige core-interface niet — vandaar een optionele
// interface in plaats van een wijziging aan parai-core: wie hem niet
// implementeert, spreekt gewoon op het tempo van de dienst.
type speedSynthesizer interface {
	SynthesizeAtSpeed(ctx context.Context, voiceID, language string, speed float64) (*audio.SynthesizeSession, error)
}

// synthesize kiest de aanroep: met tempo als de provider het kent én deze duif
// een tempo heeft, anders de gewone.
func (v *synthesizerVoice) synthesize(ctx context.Context, req SpeechRequest) (*audio.SynthesizeSession, error) {
	if s, ok := v.synth.(speedSynthesizer); ok && req.Speed > 0 {
		return s.SynthesizeAtSpeed(ctx, req.VoiceID, req.Lang, req.Speed)
	}
	return v.synth.Synthesize(ctx, req.VoiceID, req.Lang)
}

// synthSpeech is één open beurt: de schrijfkant van de sessie plus de uitvoer
// van de pomp.
type synthSpeech struct {
	v      *synthesizerVoice
	ctx    context.Context
	s      ttsSession
	out    <-chan []byte
	result <-chan error
	ended  sync.Once
}

// Say schrijft één zin. ElevenLabs wil elk stuk tekst met precies één
// afsluitende spatie; Soniox trekt zich er niets van aan. Valt de sessie of de
// beurt weg, dan verdwijnt de zin — er is niemand meer om hem te horen. Een
// paniek op de schrijfkant (een kanaal dat de provider onder ons vandaan
// sloot) blijft hier: de beurt loopt door als tekst.
func (sp *synthSpeech) Say(text string) {
	defer sp.v.recoverGoroutine("tts_feed_panic")
	chunk := strings.TrimSpace(text)
	if chunk == "" {
		return
	}
	select {
	case sp.s.text <- chunk + " ":
	case <-sp.ctx.Done():
	case <-sp.s.done:
	}
}

// End sluit het tekstkanaal: het teken voor de provider dat de uiting
// compleet is. Meer dan één keer aanroepen is onschuldig.
func (sp *synthSpeech) End() {
	defer sp.v.recoverGoroutine("tts_feed_panic")
	sp.ended.Do(func() { close(sp.s.text) })
}

func (sp *synthSpeech) Frames() <-chan []byte { return sp.out }
func (sp *synthSpeech) Result() <-chan error  { return sp.result }

var errSpeechUnconfirmed = errors.New("speech completion unconfirmed")
var errSpeechEmpty = errors.New("speech contained no audio")
var errSpeechStalled = errors.New("speech stalled")

// pump verzamelt de audioframes en levert ze als blokken af. Hij is de enige
// eigenaar van out en van het sluiten van de sessie: welke weg de beurt ook
// aflegt (klaar, afgebroken, stilgevallen, gepaniekeerd), de socket gaat dicht
// en de client krijgt een gesloten kanaal.
func (v *synthesizerVoice) pump(ctx context.Context, s ttsSession, out chan []byte, result chan error) {
	terminal := errSpeechUnconfirmed
	defer close(out)
	defer func() {
		if recover() != nil {
			terminal = errSpeechUnconfirmed
			v.log().Error("speech pump failed", "component", "duif", "reason", "tts_pump_panic")
		}
		if errors.Is(terminal, errSpeechEmpty) {
			v.log().Warn("speech returned no audio", "component", "duif", "reason", "no_audio")
		}
		if errors.Is(terminal, errSpeechStalled) {
			v.log().Warn("speech stalled", "component", "duif", "reason", "tts_stalled")
		}
		result <- terminal
		close(result)
	}()
	defer v.closeSession(s)
	terminal = v.pumpFrames(ctx, s, out)
}

func (v *synthesizerVoice) pumpFrames(ctx context.Context, s ttsSession, out chan []byte) error {
	timer := time.NewTimer(v.idleTimeout())
	defer timer.Stop()
	flush := time.NewTimer(time.Hour)
	flush.Stop()
	defer flush.Stop()
	var buf []byte
	total := 0
	result := s.result
	confirmed := false
	send := func() bool {
		if len(buf) == 0 {
			return true
		}
		flush.Stop()
		select {
		case out <- buf:
			buf = nil
			return true
		case <-ctx.Done():
			return false
		}
	}
	finish := func() error {
		if !send() {
			return ctx.Err()
		}
		if !confirmed {
			select {
			case err, ok := <-result:
				if !ok {
					return errSpeechUnconfirmed
				}
				if err != nil {
					return errSpeechUnconfirmed
				}
			case <-timer.C:
				return errSpeechStalled
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if total == 0 {
			return errSpeechEmpty
		}
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errSpeechStalled
		case err, ok := <-result:
			if !ok || err != nil {
				return errSpeechUnconfirmed
			}
			confirmed = true
			result = nil
		case <-flush.C:
			if !send() {
				return ctx.Err()
			}
		case frame, ok := <-s.audio:
			if !ok {
				return finish()
			}
			if len(frame) == 0 {
				continue
			}
			timer.Reset(v.idleTimeout())
			if len(buf) == 0 {
				flush.Reset(v.flushAfter())
			}
			buf = append(buf, frame...)
			total += len(frame)
			if len(buf) >= v.coalesceSize() && !send() {
				return ctx.Err()
			}
		case <-s.done:
			tail, n := drain(s.audio)
			buf = append(buf, tail...)
			total += n
			return finish()
		}
	}
}

// drain haalt op wat er zonder wachten nog uit het audiokanaal komt, plus
// hoeveel bytes dat waren.
func drain(frames <-chan []byte) ([]byte, int) {
	var tail []byte
	for {
		select {
		case frame, ok := <-frames:
			if !ok {
				return tail, len(tail)
			}
			tail = append(tail, frame...)
		default:
			return tail, len(tail)
		}
	}
}

// closeSession sluit de providersessie. Core sluit de WebSocket niet zelf, dus
// dit is het enige pad waarlangs hij dichtgaat.
func (v *synthesizerVoice) closeSession(s ttsSession) {
	if s.close == nil {
		return
	}
	if err := s.close(); err != nil {
		v.log().Debug("vergaderbeurt: tts-sessie sluiten gaf een fout",
			"component", "duif", "reason", "tts_close_failed")
	}
}

// recoverGoroutine vangt een paniek op zonder de paniekwaarde te loggen: die
// kan de uiting van de duif bevatten (streaming-only). Alleen de reden gaat
// mee.
func (v *synthesizerVoice) recoverGoroutine(reason string) {
	if r := recover(); r != nil {
		v.log().Error("tts-goroutine paniekte", "component", "duif", "reason", reason)
	}
}
