package duif

import (
	"context"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/parai-nl/parai-core/audio"
)

// De STT-brug: continu meeluisteren achter de
// audio.RealtimeTranscriber-interface van parai-core.
//
// Drie afwijkingen van het push-to-talk-patroon sturen dit bestand:
//
//  1. De STT-sessie blijft open, óók terwijl de duif zelf aan het woord is.
//     Er wordt nooit gesuspend en nooit gecommit — VAD-commits komen van de
//     provider en zijn voer, geen startschot: niets start vanzelf een antwoord.
//  2. Er is geen resume-handshake. De brug is haar eigen supervisor: valt de
//     provider weg, dan herstart ze zelf met exponentiële backoff.
//  3. Audio stroomt door of gaat verloren; ze wordt nooit bewaard. Tijdens een
//     gat is er niets om naartoe te streamen, dus worden frames geteld en
//     weggegooid — niet gebufferd, en al helemaal niet naar schijf
//     (streaming-only).
//
// Alles wat hier langskomt — frames, partials, gecommitte segmenten — leeft in
// kanalen en in het schuivende venster van de sessie, en nergens anders.

const (
	// sttBackoffMin/Max is de band waarin de brug zich herstelt.
	sttBackoffMin = 500 * time.Millisecond
	sttBackoffMax = 8 * time.Second
	// sttJitterSpread is hoeveel de jitter de wachttijd mag oprekken: genoeg om
	// te voorkomen dat alle sessies tegelijk opnieuw aankloppen.
	sttJitterSpread = 0.25
	// sttQueueFrames is de doorstroomruimte tussen de HTTP-kant en de
	// provider-kant. Bewust klein: dit is een sluis, geen opslag.
	sttQueueFrames = 64
	// sttEventType is het providerlabel van een definitief segment.
	sttEventCommitted = "committed"
	// defaultCommitAfter: groeit één uiting zo lang door zonder dat de
	// provider hem afsluit, dan vraagt de brug zelf om een commit. De
	// VAD-commit van de provider komt pas bij een adempauze; wie tien seconden
	// doorpraat houdt het scherm grijs en de duif doof, want alleen definitieve
	// tekst bereikt het venster en de detector. Override:
	// DUIF_STT_COMMIT_SECONDS.
	defaultCommitAfter = 10 * time.Second
)

// sttBackoff is de wachttijd vóór herstelpoging `attempt` (0-based), met een
// jitterfactor in [0,1]. Verdubbelt tot het plafond en blijft daar.
//
// De basis groeit niet tot sttBackoffMax maar tot de waarde die mét maximale
// jitter precies op sttBackoffMax uitkomt. Anders werd elke jitter boven het
// plafond weggeknipt en klopte elke langdurig herstellende sessie in exact
// dezelfde tred bij de provider aan — precies de kudde die de jitter moet
// voorkomen.
func sttBackoff(attempt int, jitter float64) time.Duration {
	ceiling := time.Duration(float64(sttBackoffMax) / (1 + sttJitterSpread))
	base := sttBackoffMin
	for range attempt {
		base *= 2
		if base >= ceiling {
			base = ceiling
			break
		}
	}
	// De min() is een vangrail op de rekenkunde hierboven, niet de plafondlogica
	// zelf: die zit in `ceiling`.
	return min(base+time.Duration(float64(base)*sttJitterSpread*clampFloat(jitter, 0, 1)), sttBackoffMax)
}

// sleepCtx is de standaard wachtseam tussen twee herstelpogingen. Hij geeft
// false zodra de sessie sluit, zodat de brug meteen stopt.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// sttBridge koppelt de microfoonstroom van één vergadersessie aan de
// streaming-STT. Merk op wat er níét in staat: geen enkel veld kan audio
// verzamelen — `frames` is een begrensd kanaal, geen buffer (toets in
// meeting_stt_test.go).
type sttBridge struct {
	engine  *meetingEngine
	session *meetingSession
	tr      audio.RealtimeTranscriber
	frames  chan []byte
	sleep   func(context.Context, time.Duration) bool
	jitter  func() float64

	mu      sync.Mutex
	live    bool
	dropped int
	// sess is de provider-sessie die nu open staat, of nil tijdens een gat. Zij
	// staat hier omdat de knop erbij moet: die vraagt om een afronding van de
	// lopende uiting voordat hij het venster leest, en dat gebeurt
	// buiten de pomp om. Wat hierin zit zijn kanalen, geen audio.
	sess *audio.RealtimeSTTSession
	// partialSince is het moment waarop de lopende uiting begon te groeien;
	// nul zolang er geen open uiting is. Alleen de pomp raakt hem aan.
	partialSince time.Time
	forced       int
	finalized    int
}

func newSTTBridge(e *meetingEngine, s *meetingSession) *sttBridge {
	b := &sttBridge{
		engine:  e,
		session: s,
		tr:      e.transcriber,
		frames:  make(chan []byte, sttQueueFrames),
		sleep:   e.sttSleep,
		jitter:  e.sttJitter,
	}
	if b.sleep == nil {
		b.sleep = sleepCtx
	}
	if b.jitter == nil {
		b.jitter = rand.Float64
	}
	return b
}

// push biedt één blok microfoonaudio aan. Is er geen open STT-sessie, of is de
// sluis vol, dan verdwijnt het blok — geteld, nooit bewaard.
func (b *sttBridge) push(frame []byte) bool {
	b.mu.Lock()
	live := b.live
	b.mu.Unlock()
	if !live {
		b.dropFrame()
		return false
	}
	select {
	case b.frames <- frame:
		return true
	default:
		b.dropFrame()
		return false
	}
}

func (b *sttBridge) dropFrame() {
	b.mu.Lock()
	b.dropped++
	b.mu.Unlock()
}

func (b *sttBridge) droppedFrames() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dropped
}

// queuedFrames is wat er op dit moment in de sluis staat — de enige plek waar
// audio zich kan ophopen, en die is begrensd op sttQueueFrames.
func (b *sttBridge) queuedFrames() int { return len(b.frames) }

func (b *sttBridge) setLive(live bool) {
	b.mu.Lock()
	b.live = live
	b.mu.Unlock()
}

// setSession legt de open provider-sessie neer, of haalt hem weg zodra de pomp
// eindigt. Alleen de supervisor schrijft hem; finalize() leest hem.
func (b *sttBridge) setSession(sess *audio.RealtimeSTTSession) {
	b.mu.Lock()
	b.sess = sess
	b.mu.Unlock()
}

// finalize vraagt de provider de lopende uiting nú af te sluiten, zodat wat er
// gezegd is als definitief segment binnenkomt in plaats van als partial te
// blijven staan. Dit is hetzelfde kanaal als dat van forceCommitIfLong — daar
// is de aanleiding tien seconden doorpraten, hier een knopdruk.
//
// Niet-blokkerend: staat er al een aanvraag in de wacht, dan is die genoeg, en
// dan is het antwoord nog steeds "ja, er wordt afgerond". False betekent: er is
// niemand die kan afronden (geen open sessie, of een provider zonder
// commit-kanaal) — de knop wacht dan nergens op.
func (b *sttBridge) finalize() bool {
	b.mu.Lock()
	sess := b.sess
	b.mu.Unlock()
	if sess == nil || sess.Commit == nil {
		return false
	}
	select {
	case sess.Commit <- struct{}{}:
		b.mu.Lock()
		b.finalized++
		b.mu.Unlock()
	default:
	}
	return true
}

// finalizeRequests is het aantal keer dat de knop om een afronding vroeg.
func (b *sttBridge) finalizeRequests() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.finalized
}

// run is de supervisor: starten, pompen, en na een gat opnieuw starten tot de
// sessie sluit. De teller loopt alleen op zolang het herstel niet lukt — een
// sessie die daadwerkelijk transcript leverde begint weer onderaan de band.
func (b *sttBridge) run(ctx context.Context) {
	var attempt int
	for {
		if ctx.Err() != nil {
			return
		}
		sess, err := b.tr.Start(ctx, b.session.lang)
		if err != nil {
			b.engine.log().Warn("stt-sessie starten faalde", "component", "duif",
				"sessie", b.session.id, "poging", attempt+1, "reason", "stt_start_failed")
			b.session.setListenState(ListenReconnecting)
			if !b.sleep(ctx, sttBackoff(attempt, b.jitter())) {
				return
			}
			attempt++
			continue
		}

		b.setLive(true)
		b.setSession(sess)
		b.session.setListenState(ListenActive)
		heard := b.pump(ctx, sess)
		b.setSession(nil)
		b.setLive(false)
		if ctx.Err() != nil {
			return
		}

		// Het gat: de luisterindicator vertelt de waarheid en de audio die
		// intussen binnenkomt gaat verloren.
		b.session.setListenState(ListenReconnecting)
		b.engine.log().Warn("stt-sessie viel weg, brug herstelt", "component", "duif",
			"sessie", b.session.id, "gedropte_frames", b.droppedFrames())
		if heard {
			attempt = 0
		}
		if !b.sleep(ctx, sttBackoff(attempt, b.jitter())) {
			return
		}
		attempt++
	}
}

// pump verbindt de sluis met de provider en het providerantwoord met het
// venster. Hij eindigt zodra de sessie sterft of de vergadersessie sluit; de
// returnwaarde zegt of deze STT-sessie werkelijk transcript leverde.
func (b *sttBridge) pump(ctx context.Context, sess *audio.RealtimeSTTSession) bool {
	defer func() {
		if sess.Close != nil {
			_ = sess.Close()
		}
	}()
	var heard bool
	for {
		select {
		case <-ctx.Done():
			return heard
		case <-sess.Done:
			return heard
		case frame := <-b.frames:
			select {
			case sess.Audio <- frame:
			case <-ctx.Done():
				return heard
			default:
				// De provider houdt het niet bij: liever een gat in het geluid
				// dan een groeiende buffer (streaming-only).
				b.dropFrame()
			}
		case ev, open := <-sess.Events:
			if !open {
				return heard
			}
			heard = true
			b.forceCommitIfLong(ev, sess)
			if !b.onEvent(ev) {
				return heard
			}
		}
	}
}

// onEvent verwerkt één providerbericht: een partial is louter weergave, een
// commit is voer voor het venster. False betekent: de sessie is dicht.
func (b *sttBridge) onEvent(ev audio.TranscriptionEvent) bool {
	text := strings.TrimSpace(ev.Text)
	if text == "" {
		return true
	}
	if !ev.IsFinal && ev.Type != sttEventCommitted {
		b.session.publishPartial(text, ev.SpeakerID)
		return true
	}
	return b.session.appendSegment(text, ev.SpeakerID)
}

// forceCommitIfLong sluit een uiting af die langer dan CommitAfter doorgroeit.
// Een commit-event zet de klok terug; een partial start hem of, als hij al
// lang genoeg loopt, vraagt de provider om af te sluiten. Beide providers
// beginnen daarna een vers segment zonder context te verliezen (ElevenLabs:
// commit-vlag op een leeg audioblok; Soniox: finalize). De aanvraag is
// niet-blokkerend: staat er al een in de wacht, dan is die genoeg.
func (b *sttBridge) forceCommitIfLong(ev audio.TranscriptionEvent, sess *audio.RealtimeSTTSession) {
	after := b.session.cfg.CommitAfter
	if after <= 0 || sess.Commit == nil {
		return
	}
	if ev.IsFinal || ev.Type == sttEventCommitted {
		b.partialSince = time.Time{}
		return
	}
	if strings.TrimSpace(ev.Text) == "" {
		return
	}
	now := b.session.clock.Now()
	if b.partialSince.IsZero() {
		b.partialSince = now
		return
	}
	if now.Sub(b.partialSince) < after {
		return
	}
	select {
	case sess.Commit <- struct{}{}:
		b.forced++
		b.engine.log().Debug("stt: lange uiting afgesloten", "component", "duif",
			"sessie", b.session.id, "na", now.Sub(b.partialSince).Round(time.Second))
	default:
	}
	b.partialSince = time.Time{}
}

// forcedCommits is het aantal keer dat de brug zelf om een commit vroeg.
func (b *sttBridge) forcedCommits() int { return b.forced }
