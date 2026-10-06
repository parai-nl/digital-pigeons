// Gedragstests van de vergaderstore: kanaal, microfoon, beurten, stoppen,
// instellingen en meldingen, aangevuld met de race-guards rond late
// antwoorden en sessiewissels.
import { describe, expect, it, vi } from 'vitest'
// Baseline transport fixtures are independent of locally installed media.
// The native-media suite covers selected field recordings separately.
vi.mock('virtual:duif-media', () => ({ default: { signalSounds: null } }))
import { BLOCK_MS, BLOCK_SAMPLES } from '../audio/pcm'
import { jsonResponse, type Route, scriptedFetch } from '../testing/fetch'
import { type FakePortsOptions, meetingHarness, meetingRoutes, OPEN_SESSION_ROUTE } from '../testing/meeting'
import { INTERACTION_THROTTLE_MS, MIC_FLUSH_MS, PRESENCE_FALLBACK_MS, SETTINGS_DEBOUNCE_MS } from './meetingStore.svelte'
import { LISTEN } from './protocol'

// gate houdt een antwoord vast tot de test het loslaat: zo is "een trage lijn"
// een gewone regel in plaats van een timing-truc.
function gate() {
  let open!: () => void
  const waiting = new Promise<void>((resolve) => {
    open = resolve
  })
  return { waiting, release: () => open() }
}

const tick = () => new Promise((resolve) => setTimeout(resolve, 0))

async function live(options: { pigeon?: string; routes?: Route[]; ports?: FakePortsOptions; lang?: 'en' | 'nl' } = {}) {
  const scripted = scriptedFetch([...(options.routes ?? []), ...meetingRoutes()])
  const harness = meetingHarness(scripted.fetch, options.ports)
  // Een taal uit de test is een keuze van de gebruiker, geen startwaarde: de
  // configuratiedefault mag hem niet meer overschrijven.
  if (options.lang) harness.locale.choose(options.lang)
  await harness.store.loadPigeons()
  if (options.pigeon) harness.store.pigeonId = options.pigeon
  await harness.store.start()
  await tick()
  return { ...harness, calls: scripted.calls }
}

const posts = (calls: { url: string; init?: RequestInit }[], part: string, method = 'POST') =>
  calls.filter((c) => (c.init?.method ?? 'GET').toUpperCase() === method && c.url.includes(part))

describe('MeetingStore', () => {
  it('lists only the pigeons that may sit at the table', async () => {
    const scripted = scriptedFetch(meetingRoutes())
    const { store } = meetingHarness(scripted.fetch)
    await store.loadPigeons()

    expect(store.meetingPigeons.map((p) => p.id)).toEqual(['individualist', 'nonverbal'])
    expect(store.pigeonId).toBe('individualist')
    expect(store.settings.eagerness).toBe('normal')
    expect(store.settings.cooldownSeconds).toBe(120)
  })

  it('opens the channel, the microphone and the controls on start', async () => {
    const { store, ports, calls } = await live({ lang: 'nl' })

    const open = posts(calls, '/meeting/sessions').at(-1)
    expect(open).toBeTruthy()
    expect(JSON.parse(String(open?.init?.body))).toMatchObject({ pigeonId: 'individualist', lang: 'nl' })
    expect(ports.channel().url).toContain('/meeting/sessions/sessie-1/events')
    expect(store.live).toBe(true)
    expect(store.phase).toBe('live')
    expect(store.localMic).toBe('open')
    expect(store.voice).toBe('ready')
  })

  it('derives the listen indicator from the real state', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'state', sessionId: 'sessie-1', listening: 'listening' })
    expect(store.listen).toBe(LISTEN.listening)

    ports.channel().emit({ type: 'listening_heartbeat', listening: 'reconnecting' })
    expect(store.listen).toBe(LISTEN.reconnecting)

    ports.channel().emit({ type: 'listening_heartbeat', listening: 'off' })
    expect(store.listen).toBe(LISTEN.off)

    // Een wegvallend kanaal is geen groen lampje: liever vals-negatief.
    ports.channel().emit({ type: 'listening_heartbeat', listening: 'listening' })
    ports.channel().fail(false)
    expect(store.listen).toBe(LISTEN.reconnecting)
  })

  // Een aangenomen audioblok zegt alleen dat het geluid de server haalde; het
  // antwoord van de duif komt via het kanaal, en dat ligt eruit. Het lampje
  // blijft dus op herstellen tot het kanaal weer spreekt.
  it('keeps the indicator on reconnecting while the channel is down, whatever /audio says', async () => {
    const { store, ports, calls } = await live()
    ports.channel().emit({ type: 'state', sessionId: 'sessie-1', listening: 'listening' })
    ports.channel().fail(false)
    expect(store.listen).toBe(LISTEN.reconnecting)

    const before = posts(calls, '/audio').length
    ports.mic().push()
    await tick()
    await tick()
    expect(posts(calls, '/audio').length).toBeGreaterThan(before)
    expect(store.listen).toBe(LISTEN.reconnecting)

    // Het kanaal is terug: de server zegt eerst zijn stand, en vanaf dan volgen
    // ook de audioantwoorden hem weer.
    ports.channel().emit({ type: 'state', sessionId: 'sessie-1', listening: 'off' })
    expect(store.listen).toBe(LISTEN.off)
    ports.mic().push()
    await tick()
    await tick()
    expect(store.listen).toBe(LISTEN.listening)
  })

  it('never lets a heartbeat turn a muted microphone green', async () => {
    const { store, ports } = await live()

    store.setMuted(true)
    ports.channel().emit({ type: 'listening_heartbeat', listening: 'listening' })
    expect(store.localMic).toBe('muted')
    expect(store.listen).toBe(LISTEN.off)
    expect(ports.mic().enabled).toBe(false)
  })

  it('keeps the transcript on screen and lets it go when the session closes', async () => {
    const { store, ports } = await live()
    const line = 'sentinelzin-4711: wij willen de duiven van het plein weren'

    ports.channel().emit({ type: 'transcript', text: 'wij willen de dui', speaker: 'voorzitter', final: false })
    expect(store.partial?.text).toBe('wij willen de dui')

    ports.channel().emit({ type: 'transcript', text: line, speaker: 'voorzitter', final: true })
    expect(store.partial).toBeNull()
    expect(store.transcript).toHaveLength(1)
    expect(store.transcript[0].text).toBe(line)

    ports.channel().emit({ type: 'transcript', text: 'roekoe', final: true, pigeonWindow: true })
    expect(store.transcript[1].own).toBe(true)

    ports.channel().emit({ type: 'session_closed', reason: 'ended_by_user' })
    expect(store.transcript).toEqual([])
  })

  it('caps the transcript at 80 lines', async () => {
    const { store, ports } = await live()
    for (let i = 0; i < 140; i++) ports.channel().emit({ type: 'transcript', text: 'regel ' + i, final: true })

    expect(store.transcript.length).toBe(80)
    expect(store.transcript.at(-1)?.text).toBe('regel 139')
  })

  it('lets the pigeon respond and stays busy while it speaks', async () => {
    const { store, ports, calls } = await live()

    await store.respond()
    expect(posts(calls, '/respond')).toHaveLength(1)

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist', lang: 'en' })
    expect(store.speaking).toBe(true)

    ports.channel().emit({ type: 'text', delta: 'Roekoe. ' })
    ports.channel().emit({ type: 'text', delta: 'Ik zat te luisteren.' })
    expect(store.answers.at(-1)?.text).toBe('Roekoe. Ik zat te luisteren.')

    ports.channel().emit({ type: 'tts_done' })
    ports.channel().emit({ type: 'response_done', kbStatus: 'persona_only' })
    expect(store.speaking).toBe(false)
  })

  it('keeps the sources of the last turn in the shared source DTO', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'persona', pigeonId: 'biological', lang: 'en' })
    ports.channel().emit({
      type: 'response_done',
      kbStatus: 'sources',
      sources: [
        { number: 1, type: 'destillaat', id: 'destillaat:9f1c', text: 'Pigeons nest on ledges.', ref: { paper: 'lewald1987' } },
        { type: 'destillaat', text: 'geen nummer' },
      ],
    })
    expect(store.kbStatus).toBe('sources')
    expect(store.sources).toEqual([
      { number: 1, type: 'destillaat', id: 'destillaat:9f1c', text: 'Pigeons nest on ledges.', ref: { paper: 'lewald1987' } },
    ])

    // Een nieuwe beurt begint schoon: de bronnen van de vorige horen er niet bij.
    ports.channel().emit({ type: 'persona', pigeonId: 'biological', lang: 'en' })
    expect(store.sources).toEqual([])
    expect(store.kbStatus).toBeUndefined()
  })

  it('shows a civil busy message on a second press', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'busy', message: { en: 'The pigeon is speaking.', nl: 'De duif is aan het woord.' } })
    expect(store.busyText).toMatch(/The pigeon is speaking/)
  })

  it('ignores an SSE busy while its own respond is in flight', async () => {
    const held = gate()
    const { store, ports } = await live({
      routes: [{ method: 'POST', match: '/respond', respond: async () => (await held.waiting, jsonResponse({ accepted: true }, 202)) }],
    })

    const pending = store.respond()
    ports.channel().emit({ type: 'busy', message: { en: 'The pigeon is speaking.' } })
    expect(store.busyText).toBe('')
    held.release()
    await pending
  })

  it('stops locally first and reports afterwards', async () => {
    const held = gate()
    const { store, ports, calls } = await live({
      routes: [{ method: 'POST', match: '/stop', respond: async () => (await held.waiting, jsonResponse({ stopped: true }, 202)) }],
    })

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'tts_audio', audio: 'QUJD', seq: 1 })
    ports.channel().emit({ type: 'tts_done', frames: 1 })
    await tick()
    const source = ports.context().played.at(-1)
    expect(source).toBeTruthy()

    const pending = store.stop()
    // Nog vóór de server antwoordt is het stil: 0 ms waarneembare latency.
    expect(source?.stopped).toBe(true)
    expect(store.stopped).toBe(true)
    expect(store.speaking).toBe(false)

    held.release()
    await pending
    expect(posts(calls, '/stop')).toHaveLength(1)

    // Stoppen is geen sessie-einde: er wordt doorgeluisterd.
    ports.channel().emit({ type: 'stopped', listening: 'listening' })
    expect(store.live).toBe(true)
    expect(ports.mic().stopped).toBe(false)

    // Een frame dat na de stop nog binnenkomt hoort niet meer te klinken.
    const played = ports.context().played.length
    ports.channel().emit({ type: 'tts_audio', audio: 'REVG', seq: 1 })
    ports.channel().emit({ type: 'tts_done', frames: 1 })
    await tick()
    expect(ports.context().played.length).toBe(played)
  })

  // Eén vlag droeg twee verhalen: een stop die de server niet haalde werd als
  // afgekapt geluid gemeld. De bediening heeft nu haar eigen melding.
  it('reports a Stop that never reached the server as a control failure', async () => {
    const { store, ports } = await live({
      routes: [{ method: 'POST', match: '/stop', respond: () => Promise.reject(new Error('offline')) }],
    })

    await store.stop()

    expect(store.stopped).toBe(true)
    expect(store.stopSyncFailed).toBe(true)
    expect(store.playbackSyncFailed).toBe(false)
    expect(store.notice?.id).toBe('stop-sync')
    expect(store.notice?.text).toMatch(/did not confirm/i)
    expect(store.notice?.text).not.toMatch(/sound/i)

    // De stopbarrière van de server is het bewijs dat de bediening alsnog
    // aankwam; dan heeft de melding afgedaan.
    ports.channel().emit({ type: 'stopped', sessionId: 'sessie-1', turnId: '1', listening: 'listening' })
    expect(store.stopSyncFailed).toBe(false)
  })

  // Een stop die de server met een foutantwoord afwijst, is dezelfde storing in
  // de bediening als een stop die hem niet haalde: hier stil, daar onbevestigd.
  // De algemene foutmelding mag de eigen stopmelding niet verdringen.
  it('reports a Stop the server rejected as a control failure, not as a generic error', async () => {
    const { store } = await live({
      routes: [{ method: 'POST', match: '/stop', respond: () => jsonResponse({ error: 'internal' }, 500) }],
    })

    await store.stop()
    await tick()

    expect(store.stopped).toBe(true)
    expect(store.stopSyncFailed).toBe(true)
    expect(store.error).toBeNull()
    expect(store.notice?.id).toBe('stop-sync')
    expect(store.notice?.text).toMatch(/did not confirm/i)
  })

  // "Druk nog een keer op Stop inbreng" helpt niet bij een verlopen login, een
  // geweigerd verzoek of een sessie die al dicht is. Die krijgen de gewone
  // foutroute, of (404) de sluitmelding die onderweg is.
  it.each([401, 403])('gives a Stop rejected with %i the ordinary error, not the press-again notice', async (status) => {
    const { store } = await live({
      routes: [{ method: 'POST', match: '/stop', respond: () => jsonResponse({ error: 'unauthorized' }, status) }],
    })

    await store.stop()
    await tick()

    expect(store.stopped).toBe(true)
    expect(store.stopSyncFailed).toBe(false)
    expect(store.stopPending).toBe(false)
    expect(store.notice?.id).toBe('error')
  })

  it('leaves a Stop rejected with 404 to session_closed', async () => {
    const { store } = await live({
      routes: [{ method: 'POST', match: '/stop', respond: () => jsonResponse({ error: 'not_found' }, 404) }],
    })

    await store.stop()
    await tick()

    expect(store.stopped).toBe(true)
    expect(store.stopSyncFailed).toBe(false)
    expect(store.stopPending).toBe(false)
    expect(store.error).toBeNull()
    expect(store.notice?.id).not.toBe('stop-sync')
  })

  // Een tekst-only opstelling verwacht nergens geluid: de afsluiting van zo'n
  // beurt mag niet als afgekapt geluid op het scherm komen.
  it('closes a text-only turn without claiming the sound was cut off', async () => {
    const { store, ports, calls } = await live()
    ports.channel().emit({
      type: 'state', sessionId: 'sessie-1', turnId: '0', voice: 'off',
      listening: 'listening', speaking: false, playbackTracking: true, settings: { autonomous: false },
    })
    ports.channel().emit({ type: 'persona', sessionId: 'sessie-1', turnId: '1', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'text', turnId: '1', delta: 'alleen tekst' })
    ports.channel().emit({ type: 'text_done', turnId: '1' })
    ports.channel().emit({ type: 'response_done', turnId: '1' })
    await tick()

    expect(posts(calls, '/playback').map((c) => JSON.parse(String(c.init?.body)).state)).toEqual(['ended'])
    expect(store.playbackSyncFailed).toBe(false)
    expect(store.notice?.id).not.toBe('sound')
  })

  it('ignores a heartbeat that still claims speaking while a stop is pending', async () => {
    const held = gate()
    const { store, ports } = await live({
      routes: [{ method: 'POST', match: '/stop', respond: async () => (await held.waiting, jsonResponse({ stopped: true }, 202)) }],
    })

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    const pending = store.stop()
    ports.channel().emit({ type: 'listening_heartbeat', listening: 'listening', speaking: true })
    expect(store.speaking).toBe(false)

    held.release()
    await pending
    // De 202 zegt alleen "aangenomen": ook daarna telt een hartslag die nog
    // "spreekt" roept niet, tot het stopped-bericht de beurt echt sluit.
    expect(store.stopPending).toBe(true)
    ports.channel().emit({ type: 'listening_heartbeat', listening: 'listening', speaking: true })
    expect(store.speaking).toBe(false)

    ports.channel().emit({ type: 'stopped', listening: 'listening' })
    expect(store.stopPending).toBe(false)
    expect(store.speaking).toBe(false)
  })

  it('decodes the frames of one turn as a single utterance', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'tts_audio', audio: 'QUJD', seq: 1 }) // "ABC"
    ports.channel().emit({ type: 'tts_audio', audio: 'REVG', seq: 2 }) // "DEF"
    expect(ports.context().played).toHaveLength(0)

    ports.channel().emit({ type: 'tts_done', frames: 2 })
    await tick()
    expect(ports.context().decoded).toEqual([6])
    expect(ports.context().played).toHaveLength(1)

    // Een volgende beurt begint met een schone lei en wacht op de vorige.
    ports.channel().emit({ type: 'response_done' })
    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'tts_audio', audio: 'R0hJ', seq: 1 })
    ports.channel().emit({ type: 'tts_done', frames: 1 })
    await tick()
    expect(ports.context().played).toHaveLength(1)

    ports.context().finish(0)
    expect(ports.context().played).toHaveLength(2)
    expect(store.audioGap).toBe(false)
  })

  it('throws away a turn with a gap in its frames and says so', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'tts_audio', audio: 'QUJD', seq: 1 })
    ports.channel().emit({ type: 'tts_audio', audio: 'R0hJ', seq: 3 })
    ports.channel().emit({ type: 'tts_done', frames: 3 })
    await tick()

    expect(ports.context().played).toHaveLength(0)
    expect(store.audioGap).toBe(true)
  })

  it('reports a silent turn when the session was supposed to have a voice', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'tts_done', frames: 0 })
    await tick()
    expect(store.audioGap).toBe(true)
  })

  it('starts nothing when the stop lands while the audio is still decoding', async () => {
    const { store, ports } = await live()
    ports.context().holdDecode = true

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'tts_audio', audio: 'QUJD', seq: 1 })
    ports.channel().emit({ type: 'tts_done', frames: 1 })
    await tick()
    expect(ports.context().decodesPending).toBe(1)

    await store.stop()
    ports.context().releaseDecode()
    await tick()
    expect(ports.context().played).toHaveLength(0)
  })

  it('survives an unmount while the audio is still decoding', async () => {
    const { store, ports } = await live()
    const ctx = ports.context()
    ctx.holdDecode = true

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'tts_audio', audio: 'QUJD', seq: 1 })
    ports.channel().emit({ type: 'tts_done', frames: 1 })
    await tick()

    store.dispose()
    ctx.releaseDecode()
    await tick()
    expect(ctx.played).toHaveLength(0)
    expect(ctx.closed).toBe(true)
  })

  it('silences the non-verbal pigeon as well', async () => {
    const { store, ports } = await live({ pigeon: 'nonverbal' })

    ports.channel().emit({ type: 'sound', sound: { id: 'koeren-1', band: 'opgewonden', url: '/api/duif/v1/sounds/koeren-1' } })
    const first = ports.soundPlayers.at(-1)
    expect(first?.url).toBe('/api/duif/v1/sounds/koeren-1')

    await store.stop()
    expect(first?.stopped).toBe(true)

    // Stop pauses automatic contributions; an explicit invitation allows one.
    await store.intervene()
    ports.channel().emit({ type: 'sound', sound: { id: 'koeren-2', band: 'rustig', url: '/api/duif/v1/sounds/koeren-2' } })
    const second = ports.soundPlayers.at(-1)
    expect(second?.url).toBe('/api/duif/v1/sounds/koeren-2')
    expect(second?.stopped).toBe(false)

    ports.channel().emit({ type: 'session_closed', reason: 'ended_by_user' })
    expect(second?.stopped).toBe(true)
  })

  it('surfaces arousal, the held candidate and the moment it speaks', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'arousal', arousal: 0.42 })
    expect(store.arousal).toBeCloseTo(0.42)

    // De kandidaat draagt geen letter van de bijdrage: score, thema, reden.
    ports.channel().emit({
      type: 'candidate',
      candidate: {
        id: 'c1', reason: 'topic', score: 0.61, sources: 2,
        topic: { id: 'overlast', label: { en: 'Nuisance', nl: 'Overlast' } },
      },
    })
    expect(store.candidate?.topic?.label?.en).toBe('Nuisance')
    expect(store.candidate?.score).toBeCloseTo(0.61)
    expect(store.candidate?.sources).toBe(2)

    // Een leeg kandidaatbericht wist het blok: de duif heeft nu niets meer.
    ports.channel().emit({ type: 'candidate' })
    expect(store.candidate).toBeNull()

    // Onbruikbare berichten leveren geen blok op: liever niets dan een leugen.
    ports.channel().emit({ type: 'candidate', candidate: { id: 'c2' } })
    expect(store.candidate).toBeNull()

    ports.channel().emit({
      type: 'candidate',
      candidate: { id: 'c3', reason: 'addressed', score: 0.9, addressed: true },
    })
    expect(store.candidate?.reason).toBe('addressed')

    ports.channel().emit({
      type: 'intervention',
      topic: { id: 'overlast', label: { en: 'Nuisance', nl: 'Overlast' } },
      addressed: true,
    })
    expect(store.trigger?.addressed).toBe(true)
    expect(store.candidate).toBeNull()
  })

  it('lets the user hear the held candidate right away', async () => {
    const { store, ports, calls } = await live()
    ports.channel().emit({ type: 'candidate', candidate: { id: 'c1', reason: 'topic', score: 0.7 } })
    expect(store.candidate?.id).toBe('c1')

    await store.speakCandidate()
    expect(posts(calls, '/candidate/speak')).toHaveLength(1)
    expect(store.candidateInFlight).toBe(false)
  })

  it('changes the settings live during the meeting', async () => {
    const { store, ports, calls } = await live()

    // De knop reageert meteen; de POST volgt zodra de hand stilligt.
    store.updateSettings({ autonomous: true })
    expect(store.settings.autonomous).toBe(true)
    expect(posts(calls, '/settings')).toHaveLength(0)
    ports.advance(SETTINGS_DEBOUNCE_MS)
    await tick()
    expect(JSON.parse(String(posts(calls, '/settings').at(-1)?.init?.body))).toMatchObject({ autonomous: true })

    // Een schuif die beweegt is één bedoeling: dertig stapjes, één POST.
    for (let s = 30; s <= 900; s += 30) store.updateSettings({ cooldownSeconds: s })
    store.updateSettings({ eagerness: 'eager' })
    expect(posts(calls, '/settings')).toHaveLength(1)
    // Een echo van de server terwijl er nog iets van ons onderweg is mag de
    // knoppen niet terugzetten.
    ports.channel().emit({ type: 'settings', settings: { autonomous: true, eagerness: 'normal', cooldownSeconds: 0 } })
    expect(store.settings).toEqual({ autonomous: true, eagerness: 'eager', cooldownSeconds: 900 })
    ports.advance(SETTINGS_DEBOUNCE_MS)
    await tick()
    expect(posts(calls, '/settings')).toHaveLength(2)
    expect(JSON.parse(String(posts(calls, '/settings').at(-1)?.init?.body))).toEqual({
      autonomous: true,
      eagerness: 'eager',
      cooldownSeconds: 900,
    })

    // De server is de baas: zodra er niets meer onderweg is zet zijn
    // bevestiging de knoppen goed.
    ports.channel().emit({ type: 'settings', settings: { autonomous: false, eagerness: 'reserved', cooldownSeconds: 180 } })
    expect(store.settings).toEqual({ autonomous: false, eagerness: 'reserved', cooldownSeconds: 180 })
  })

  it('clamps the cooldown to the range the server accepts', async () => {
    const { store } = await live()

    store.updateSettings({ cooldownSeconds: 99999 })
    expect(store.settings.cooldownSeconds).toBe(3600)
    store.updateSettings({ cooldownSeconds: -5 })
    expect(store.settings.cooldownSeconds).toBe(0)
  })

  it('reports a paused detector and leaves the button alone', async () => {
    const { store, ports } = await live()

    ports.channel().emit({
      type: 'detector_status',
      detector: { enabled: false, reason: 'llm_budget_exceeded' },
      message: { en: 'Joining in on its own is paused — cost limit reached. The button still works.', nl: '…' },
    })
    expect(store.detector?.enabled).toBe(false)
    expect(store.detector?.message?.en).toMatch(/cost limit/i)
    expect(store.live).toBe(true)

    ports.channel().emit({ type: 'detector_status', detector: { enabled: true } })
    expect(store.detector?.enabled).toBe(true)
  })

  it('renders a budget error as a civil bilingual message', async () => {
    const { store, ports } = await live({ lang: 'nl' })

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit({
      type: 'error',
      error: 'llm_budget_exceeded',
      message: { en: 'The configured model budget has been reached.', nl: 'Het ingestelde modelbudget is bereikt.' },
    })
    expect(store.errorText).toMatch(/modelbudget/)
    expect(store.errorText).not.toMatch(/BudgetExceededError/)

    ports.channel().emit({ type: 'response_done' })
    expect(store.speaking).toBe(false)
  })

  // De aftelling komt uit de klok, niet uit een teller die zelf aftelt: één
  // sprong van negentig seconden (een achtergrondtabblad met geremde timers)
  // staat in één keer op dertig seconden.
  it('raises and answers the presence check', async () => {
    const { store, ports, calls } = await live()
    expect(store.presence).toBeNull()

    ports.channel().emit({ type: 'presence_check', message: { en: 'Nothing has been clicked.', nl: 'Er is niets aangeklikt.' }, graceMs: 120_000 })
    expect(store.presence?.en).toMatch(/Nothing has been clicked/)
    expect(store.presenceRemainingMs).toBe(120_000)

    ports.advance(90_000)
    expect(store.presenceRemainingMs).toBe(30_000)
    // De hartslag met resterende tijd laat de vraag staan.
    ports.channel().emit({ type: 'listening_heartbeat', listening: 'listening', graceMs: 30_000 })
    expect(store.presence).not.toBeNull()

    await store.acknowledgePresence()
    expect(posts(calls, '/presence')).toHaveLength(1)
    expect(store.presence).toBeNull()
    expect(store.presenceRemainingMs).toBeNull()
    // De lokale terugval is met het antwoord opgeruimd.
    ports.advance(PRESENCE_FALLBACK_MS + 60_000)
    expect(store.live).toBe(true)
  })

  // Een gehoorde zin is geen antwoord: de microfoon hoort ook de zaal
  // en de echo van de duif. Op de conferentie sloot elke zin de vraag weer.
  it('keeps the presence check open while people (or the pigeon) speak', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'presence_check', message: { en: 'Are you still there?' }, graceMs: 120_000 })
    ports.channel().emit({ type: 'transcript', text: 'roekoe', final: true, pigeonWindow: true })
    ports.channel().emit({ type: 'transcript', text: 'we zijn er nog', final: true })
    expect(store.presence).not.toBeNull()
  })

  // Antwoordt iemand in een ander tabblad (of klikt hij daar), dan draagt de
  // volgende hartslag geen resterende tijd meer, en verdwijnt de vraag hier ook.
  // Na een herverbinding brengt het state-event de vraag met haar tijd terug.
  it('follows the server about whether the question is still open', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'presence_check', message: { en: 'Are you still there?' }, graceMs: 120_000 })
    ports.channel().emit({ type: 'listening_heartbeat', listening: 'listening' })
    expect(store.presence).toBeNull()

    ports.channel().emit({ type: 'state', sessionId: 'sessie-1', listening: 'listening', graceMs: 45_000, message: { en: 'Still there?' } })
    expect(store.presence?.en).toBe('Still there?')
    expect(store.presenceRemainingMs).toBe(45_000)
    ports.channel().emit({ type: 'state', sessionId: 'sessie-1', listening: 'listening' })
    expect(store.presence).toBeNull()
  })

  // Klikken en toetsen melden de server dat er iemand is, maar hoogstens eens
  // per drossel; wat er tijdens de drossel gebeurde gaat er aan het eind als
  // één signaal achteraan. Staat de vraag open, dan antwoordt alleen de dialoog.
  it('reports clicks and keys as a throttled sign of life', async () => {
    const { store, ports, calls } = await live()
    const presence = () => posts(calls, '/presence').length

    globalThis.dispatchEvent(new Event('pointerdown'))
    expect(presence()).toBe(1)
    globalThis.dispatchEvent(new KeyboardEvent('keydown', { key: 'a' }))
    globalThis.dispatchEvent(new Event('pointerdown'))
    expect(presence()).toBe(1)
    ports.advance(INTERACTION_THROTTLE_MS)
    expect(presence()).toBe(2)
    ports.advance(INTERACTION_THROTTLE_MS)
    expect(presence()).toBe(2)

    // Een klik vlak vóór de vraag hangt nog in de drossel; hij beantwoordt de
    // vraag niet die daarna kwam.
    globalThis.dispatchEvent(new Event('pointerdown'))
    expect(presence()).toBe(3)
    globalThis.dispatchEvent(new Event('pointerdown'))
    ports.channel().emit({ type: 'presence_check', message: { en: 'Are you still there?' }, graceMs: 120_000 })
    ports.advance(INTERACTION_THROTTLE_MS)
    globalThis.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab' }))
    expect(presence()).toBe(3)
    expect(store.presence).not.toBeNull()

    store.end()
    globalThis.dispatchEvent(new Event('pointerdown'))
    expect(presence()).toBe(3)
  })

  // De lokale terugval: is de aftelling ruim voorbij en kwam er geen
  // session_closed, dan is het netwerk weg. De browser legt de vergadering dan
  // zelf neer, langs hetzelfde pad als de rode knop.
  it('lays the meeting down itself when the countdown is long past and no close arrives', async () => {
    const { store, ports, calls } = await live()

    ports.channel().emit({ type: 'presence_check', message: { en: 'Are you still there?' }, graceMs: 120_000 })
    ports.advance(120_000 + PRESENCE_FALLBACK_MS - 1)
    expect(store.live).toBe(true)
    ports.advance(1)
    expect(store.live).toBe(false)
    expect(store.closedReason).toBe('inactivity')
    expect(ports.mic().stopped).toBe(true)
    expect(ports.channel().closed).toBe(true)
    expect(ports.context().closed).toBe(true)
    const deleted = posts(calls, '/meeting/sessions/sessie-1', 'DELETE')
    expect(deleted).toHaveLength(1)
    expect(deleted[0].init?.keepalive).toBe(true)
  })

  // Valt het kanaal weg terwijl de aftelling al op nul stond, dan was dat niet
  // de reden: de vergadering stopte omdat niemand antwoordde.
  it('names inactivity, not the connection, when the channel drops after the countdown', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'presence_check', message: { en: 'Are you still there?' }, graceMs: 1_000 })
    ports.advance(2_000)
    ports.channel().fail(true)
    expect(store.closedReason).toBe('inactivity')
  })

  // Haalt "ja, ga door" de server niet, dan komt de vraag terug met de tijd die
  // er nog was: de server stopt de vergadering anders toch.
  it('brings the question back when the answer did not reach the server', async () => {
    const { store, ports } = await live({
      routes: [{ method: 'POST', match: '/presence', respond: () => { throw new TypeError('offline') } }],
    })

    ports.channel().emit({ type: 'presence_check', message: { en: 'Are you still there?' }, graceMs: 120_000 })
    ports.advance(20_000)
    await store.acknowledgePresence()
    expect(store.presence?.en).toBe('Are you still there?')
    expect(store.presenceRemainingMs).toBe(100_000)
    expect(store.error).toBeNull()
  })

  // Een "ja" dat de server met een fout beantwoordt, is niet
  // aangekomen: de server stopt anders toch op de deadline. Alleen een 404
  // (de sessie is al weg) sluit de vraag; de sluitmelding is dan onderweg.
  it('brings the question back when the server refuses the answer, but not on a 404', async () => {
    let status = 503
    const { store, ports } = await live({
      routes: [{ method: 'POST', match: '/presence', respond: () => jsonResponse({ error: 'unavailable' }, status) }],
    })

    ports.channel().emit({ type: 'presence_check', message: { en: 'Are you still there?' }, graceMs: 120_000 })
    ports.advance(20_000)
    await store.acknowledgePresence()
    expect(store.presence?.en).toBe('Are you still there?')
    expect(store.presenceRemainingMs).toBe(100_000)

    status = 404
    await store.acknowledgePresence()
    expect(store.presence).toBeNull()
  })

  // Staat de vraag op de server nog open terwijl hij hier
  // dicht is (een eerdere hartslag zonder tijd, een verloren antwoord), dan
  // brengt de volgende hartslag mét tijd haar terug. Niet zolang er een "ja"
  // onderweg is: dat antwoord kan de hartslag nog inhalen.
  it('reopens the question from a heartbeat that still carries time', async () => {
    const held = gate()
    const { store, ports } = await live({
      routes: [{ method: 'POST', match: '/presence', respond: async () => (await held.waiting, jsonResponse({ acknowledged: true }, 202)) }],
    })

    ports.channel().emit({ type: 'presence_check', message: { en: 'Are you still there?' }, graceMs: 120_000 })
    ports.channel().emit({ type: 'listening_heartbeat', listening: 'reconnecting' })
    expect(store.presence).toBeNull()
    ports.advance(5_000)
    ports.channel().emit({ type: 'listening_heartbeat', listening: 'listening', graceMs: 115_000 })
    expect(store.presence?.en).toBe('Are you still there?')
    expect(store.presenceRemainingMs).toBe(115_000)

    const answering = store.acknowledgePresence()
    ports.channel().emit({ type: 'listening_heartbeat', listening: 'listening', graceMs: 114_000 })
    expect(store.presence).toBeNull()
    held.release()
    await answering
    expect(store.presence).toBeNull()
  })

  // De hartslag stelt de aftelling bij, maar alleen naar
  // voren: een trage hartslag mag de deadline niet later zetten dan de server.
  it('lets the heartbeat pull the countdown forward, never back', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'presence_check', message: { en: 'Are you still there?' }, graceMs: 120_000 })
    ports.advance(5_000)
    ports.channel().emit({ type: 'listening_heartbeat', listening: 'listening', graceMs: 110_000 })
    expect(store.presenceRemainingMs).toBe(110_000)
    ports.channel().emit({ type: 'listening_heartbeat', listening: 'listening', graceMs: 119_000 })
    expect(store.presenceRemainingMs).toBe(110_000)
    // De lokale terugval schuift mee met de vroegere deadline.
    ports.advance(110_000 + PRESENCE_FALLBACK_MS)
    expect(store.live).toBe(false)
    expect(store.closedReason).toBe('inactivity')
  })

  // AC8 — een achtergrondtabblad remt de timers af, maar de klok loopt door.
  // Komt het tabblad terug, dan klopt de aftelling meteen weer; is zij ruim
  // voorbij, dan stopt de vergadering daar ook.
  it('recomputes the countdown when the tab comes back from the background', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'presence_check', message: { en: 'Are you still there?' }, graceMs: 120_000 })
    ports.jump(60_000)
    expect(store.presenceRemainingMs).toBe(120_000)
    globalThis.dispatchEvent(new Event('visibilitychange'))
    expect(store.presenceRemainingMs).toBe(60_000)
    expect(store.live).toBe(true)

    ports.jump(60_000 + PRESENCE_FALLBACK_MS)
    globalThis.dispatchEvent(new Event('visibilitychange'))
    expect(store.live).toBe(false)
    expect(store.closedReason).toBe('inactivity')
  })

  // Een "ja" is zelf al het teken van leven: een klik vlak daarna hoeft niet
  // nog een keer naar de server, alleen aan het eind van de drossel.
  it('counts the answer as the start of the throttle', async () => {
    const { store, ports, calls } = await live()

    ports.channel().emit({ type: 'presence_check', message: { en: 'Are you still there?' }, graceMs: 120_000 })
    await store.acknowledgePresence()
    expect(posts(calls, '/presence')).toHaveLength(1)
    globalThis.dispatchEvent(new Event('pointerdown'))
    expect(posts(calls, '/presence')).toHaveLength(1)
    ports.advance(INTERACTION_THROTTLE_MS)
    expect(posts(calls, '/presence')).toHaveLength(2)
  })

  // AC12 — "Ben je er nog?" ligt boven elke andere vraag. Staat de vraag om te
  // beëindigen of om van duif te wisselen open als zij verschijnt, dan gaat
  // die dicht: anders ligt die er bovenop en zit de focus op een knop die je
  // niet ziet.
  it('closes the other open questions when the presence check appears', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist', turnId: '1' })
    store.askEnd()
    expect(store.requestPigeonSwitch('nonverbal')).toBe(true)
    ports.channel().emit({ type: 'presence_check', message: { en: 'Are you still there?' }, graceMs: 120_000 })
    expect(store.endConfirm).toBe(false)
    expect(store.pendingSwitch).toBe('')
    expect(store.presence).not.toBeNull()
  })

  // Sluit de server de vergadering terwijl er nog een andere vraag openstaat
  // ("toch beëindigen?", "andere duif aan tafel?"), dan gaat die mee: anders
  // staat hij bij de volgende vergadering meteen open.
  it('clears the other open questions when the meeting stops by itself', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist', turnId: '1' })
    store.askEnd()
    expect(store.endConfirm).toBe(true)
    expect(store.requestPigeonSwitch('nonverbal')).toBe(true)
    ports.channel().emit({ type: 'session_closed', reason: 'inactivity' })
    expect(store.endConfirm).toBe(false)
    expect(store.pendingSwitch).toBe('')
    expect(store.speaking).toBe(false)
  })

  it('streams microphone blocks and collects nothing', async () => {
    const { ports, calls } = await live()

    ports.mic().push()
    await tick()
    const first = posts(calls, '/audio').at(-1)
    expect(first).toBeTruthy()
    expect(Object.prototype.toString.call(first?.init?.body)).toBe('[object ArrayBuffer]')
    expect((first?.init?.body as ArrayBuffer).byteLength).toBe(BLOCK_SAMPLES * 2)
    expect((first?.init?.headers as Record<string, string>)['Content-Type']).toBe('application/octet-stream')
  })

  it('holds blocks in the sluice while a slow POST is in flight', async () => {
    const held = gate()
    const { store, ports, calls } = await live({
      routes: [
        {
          method: 'POST',
          match: '/audio',
          respond: async () => (await held.waiting, jsonResponse({ streamed: 1, dropped: 0, listening: 'listening' }, 202)),
        },
      ],
    })

    ports.mic().push()
    await tick()
    expect(posts(calls, '/audio')).toHaveLength(1)

    for (let i = 0; i < 5; i++) ports.mic().push()
    await tick()
    expect(posts(calls, '/audio')).toHaveLength(1)
    expect(store.dropped).toBe(0)

    held.release()
    await tick()
    const audio = posts(calls, '/audio')
    expect(audio).toHaveLength(2)
    expect((audio[1].init?.body as ArrayBuffer).byteLength).toBe(5 * BLOCK_SAMPLES * 2)
  })

  // De knop leest aan de serverkant het venster, dus het laatste
  // geluid moet er eerst zijn. De sluis mag tot drie seconden audio onderweg
  // hebben, en dat is precies de zin waarop geantwoord moet worden.
  it('empties the microphone sluice before asking for a turn', async () => {
    const held = gate()
    const { store, ports, calls } = await live({
      routes: [
        {
          method: 'POST',
          match: '/audio',
          respond: async () => (await held.waiting, jsonResponse({ streamed: 1, dropped: 0, listening: 'listening' }, 202)),
        },
      ],
    })

    ports.mic().push() // deze POST staat in de lucht
    await tick()
    ports.mic().push() // en dit blok wacht in de sluis
    await tick()

    const pending = store.respond()
    await tick()
    expect(posts(calls, '/respond')).toHaveLength(0)
    // De knop voelt wél meteen ingedrukt: de duif "denkt" al.
    expect(store.respondInFlight).toBe(true)

    held.release()
    await pending
    expect(posts(calls, '/audio')).toHaveLength(2)
    expect(posts(calls, '/respond')).toHaveLength(1)
    // En in die volgorde: het laatste blok ging de lijn op vóór de vraag.
    const lastAudio = calls.findLastIndex((c) => c.url.includes('/audio'))
    expect(lastAudio).toBeLessThan(calls.findIndex((c) => c.url.includes('/respond')))
  })

  // Dat legen duurt hooguit MIC_FLUSH_MS, maar in dat venster past een
  // stopknop. Stop is de gegarandeerde stilte: de vraag die nog in de
  // wacht stond gaat dan niet alsnog de deur uit.
  it('drops the pending turn when Stop falls while the sluice is emptying', async () => {
    const held = gate()
    const { store, ports, calls } = await live({
      routes: [
        {
          method: 'POST',
          match: '/audio',
          respond: async () => (await held.waiting, jsonResponse({ streamed: 1, dropped: 0, listening: 'listening' }, 202)),
        },
      ],
    })

    ports.mic().push()
    await tick()
    ports.mic().push()
    await tick()

    const pending = store.respond()
    await store.stop()
    held.release()
    await pending

    expect(posts(calls, '/respond')).toHaveLength(0)
    expect(posts(calls, '/stop')).toHaveLength(1)
    // En de knop blijft niet in "de duif denkt" hangen.
    expect(store.respondInFlight).toBe(false)
    expect(store.stopped).toBe(true)
  })

  // Ligt de lijn net stil na een mislukte verzending, dan valt er niets meer te
  // legen dat deze beurt nog haalt: de knop wacht niet op die herkansing.
  it('does not wait for a resting sluice after a failed audio POST', async () => {
    const { store, ports, calls } = await live({
      routes: [{ method: 'POST', match: '/audio', respond: () => { throw new Error('lijn weg') } }],
    })

    ports.mic().push()
    await tick()
    expect(store.dropped).toBe(1) // de verzending mislukte; de sluis rust nu
    ports.mic().push()
    await tick()

    await store.respond()
    expect(posts(calls, '/respond')).toHaveLength(1)
  })

  // Maar het is een plafond, geen belofte: op een lijn die blijft hangen gaat
  // de knop gewoon door, en het geluid dat nog staat gaat mee met het volgende
  // blok.
  it('gives up on a stuck line and asks for the turn anyway', async () => {
    const held = gate()
    const { store, ports, calls } = await live({
      routes: [
        {
          method: 'POST',
          match: '/audio',
          respond: async () => (await held.waiting, jsonResponse({ streamed: 1, dropped: 0, listening: 'listening' }, 202)),
        },
      ],
    })

    ports.mic().push()
    await tick()
    const pending = store.respond()
    await tick()
    expect(posts(calls, '/respond')).toHaveLength(0)

    ports.advance(MIC_FLUSH_MS)
    await pending
    expect(posts(calls, '/respond')).toHaveLength(1)
    held.release()
    await tick()
  })

  // De non-verbale duif leest geen venster: haar knop geeft een geluid uit haar
  // eigen set, en daar valt niets op te wachten.
  it('lets the non-verbal intervention through without emptying the sluice', async () => {
    const held = gate()
    const { store, ports, calls } = await live({
      pigeon: 'nonverbal',
      routes: [
        {
          method: 'POST',
          match: '/audio',
          respond: async () => (await held.waiting, jsonResponse({ streamed: 1, dropped: 0, listening: 'listening' }, 202)),
        },
      ],
    })

    ports.mic().push()
    await tick()
    await store.intervene()
    expect(posts(calls, '/intervene')).toHaveLength(1)
    held.release()
    await tick()
  })

  it('counts the blocks a full sluice has to drop', async () => {
    const held = gate()
    const { store, ports } = await live({
      routes: [
        {
          method: 'POST',
          match: '/audio',
          respond: async () => (await held.waiting, jsonResponse({ streamed: 1, dropped: 0, listening: 'listening' }, 202)),
        },
      ],
    })

    ports.mic().push()
    await tick()
    for (let i = 0; i < 20; i++) ports.mic().push()
    await tick()

    expect(store.dropped).toBe(8)
    held.release()
    await tick()
  })

  it('counts the blocks the server had to drop as well', async () => {
    const { store, ports } = await live({
      routes: [{ method: 'POST', match: '/audio', respond: () => jsonResponse({ streamed: 1, dropped: 3, listening: 'listening' }, 202) }],
    })

    ports.mic().push()
    await tick()
    expect(store.dropped).toBe(3)
  })

  it('counts the blocks of a mute as lost and empties the sluice', async () => {
    const held = gate()
    const { store, ports } = await live({
      routes: [
        {
          method: 'POST',
          match: '/audio',
          respond: async () => (await held.waiting, jsonResponse({ streamed: 1, dropped: 0, listening: 'listening' }, 202)),
        },
      ],
    })

    ports.mic().push()
    await tick()
    for (let i = 0; i < 3; i++) ports.mic().push()
    store.setMuted(true)

    expect(store.dropped).toBe(3)
    expect(store.localMic).toBe('muted')
    held.release()
    await tick()
  })

  it('posts nothing at all while the microphone is muted', async () => {
    const { store, ports, calls } = await live()

    ports.mic().push()
    await tick()
    expect(posts(calls, '/audio')).toHaveLength(1)

    store.setMuted(true)
    expect(ports.mic().enabled).toBe(false)

    // De echte port sluit de bron al af; de fake blijft bewust doorduwen, zodat
    // dit de poort in de store toetst en niet de fake. Gedempt is gedempt: er
    // gaat geen enkele byte stilte meer de lijn op (002).
    for (let i = 0; i < 8; i++) ports.mic().push()
    await tick()
    ports.advance(1000)
    await tick()
    expect(posts(calls, '/audio')).toHaveLength(1)
    expect(store.dropped).toBe(0)

    // En na het opheffen loopt de stroom weer, zonder herstart van de sessie.
    store.setMuted(false)
    expect(ports.mic().enabled).toBe(true)
    expect(store.localMic).toBe('open')
    ports.mic().push()
    await tick()
    expect(posts(calls, '/audio')).toHaveLength(2)
  })

  it('reports missing speech-to-text honestly and keeps the button', async () => {
    const { store, ports, calls } = await live({
      routes: [
        {
          method: 'POST',
          match: '/audio',
          respond: () =>
            jsonResponse(
              {
                error: 'stt_unavailable',
                message: { en: 'Live listening is not available.', nl: 'Live meeluisteren is niet beschikbaar.' },
              },
              503,
            ),
        },
      ],
    })

    ports.mic().push()
    await tick()
    // Geen STT hoort bij de microfoon, niet bij de algemene foutmelding: een
    // latere serverfout mag niet wegpoetsen dat de duif niet meeluistert.
    expect(store.micNoticeText).toMatch(/Live listening is not available/)
    expect(store.errorText).toBe('')
    expect(store.listen).toBe(LISTEN.off)
    expect(store.live).toBe(true)

    const before = posts(calls, '/audio').length
    ports.mic().push()
    await tick()
    expect(posts(calls, '/audio')).toHaveLength(before)
  })

  it('closes the microphone quietly when the session is gone (404)', async () => {
    const { store, ports } = await live({
      routes: [{ method: 'POST', match: '/audio', respond: () => jsonResponse({ error: 'session_not_found' }, 404) }],
    })

    ports.mic().push()
    await tick()
    expect(store.localMic).toBe('closed')
    expect(store.errorText).toBe('')
    expect(store.live).toBe(true)
  })

  it('survives a refused microphone', async () => {
    const { store } = await live({ ports: { micRefused: true } })

    expect(store.micNoticeText).toMatch(/microphone/i)
    expect(store.listen).toBe(LISTEN.off)
    expect(store.live).toBe(true)
  })

  it('survives a browser without audio at all', async () => {
    // Geen AudioContext: geen microfoon en geen stem, maar wél een vergadering.
    // De knop is de gegarandeerde basis.
    const { store, ports } = await live({ ports: { noAudioContext: true } })

    expect(ports.contexts).toHaveLength(0)
    expect(store.localMic).toBe('refused')
    expect(store.micNoticeText).not.toBe('')
    expect(store.listen).toBe(LISTEN.off)
    expect(store.live).toBe(true)
    expect(store.speaks).toBe(true)
  })

  it('gives the non-verbal pigeon its own button and its sound', async () => {
    const { store, ports, calls } = await live({ pigeon: 'nonverbal' })

    expect(store.speaks).toBe(false)

    await store.intervene()
    expect(posts(calls, '/intervene')).toHaveLength(1)

    ports.channel().emit({ type: 'sound', sound: { id: 'koeren-1', band: 'rustig', url: '/api/duif/v1/sounds/koeren-1' } })
    expect(ports.soundPlayers.at(-1)?.url).toBe('/api/duif/v1/sounds/koeren-1')
  })

  it('closes channel, microphone and screen when the meeting ends', async () => {
    const { store, ports, calls } = await live()

    store.end()
    await tick()
    expect(posts(calls, '/meeting/sessions/sessie-1', 'DELETE')).toHaveLength(1)
    expect(ports.channel().closed).toBe(true)
    expect(ports.mic().stopped).toBe(true)
    expect(ports.context().closed).toBe(true)
    expect(store.listen).toBe(LISTEN.off)
    expect(store.phase).toBe('setup')
    expect(store.closedText).toBe('')
  })

  it('reports the reason when the server closes the session', async () => {
    const { store, ports, calls } = await live()

    ports.channel().emit({ type: 'session_closed', reason: 'inactivity' })
    await tick()
    expect(store.closedReason).toBe('inactivity')
    expect(store.closedText).toMatch(/nobody answered “Are you still there\?”/)
    expect(store.live).toBe(false)
    expect(ports.mic().stopped).toBe(true)
    expect(ports.channel().closed).toBe(true)
    // De server heeft al gesloten: geen DELETE meer nodig.
    expect(posts(calls, '/meeting/sessions/sessie-1', 'DELETE')).toHaveLength(0)
  })

  it('treats a definitively closed channel as connection loss', async () => {
    const { store, ports } = await live()

    ports.channel().fail(true)
    await tick()
    expect(store.live).toBe(false)
    expect(store.closedText).toMatch(/connection was lost/)
  })

  it('opens one session on a double click', async () => {
    const held = gate()
    const scripted = scriptedFetch([
      {
        method: 'POST',
        match: OPEN_SESSION_ROUTE,
        respond: async () => (await held.waiting, jsonResponse({ sessionId: 'sessie-1', pigeonId: 'individualist', lang: 'en', listening: 'listening', voice: 'ready' }, 201)),
      },
      ...meetingRoutes(),
    ])
    const { store } = meetingHarness(scripted.fetch)
    await store.loadPigeons()

    const first = store.start()
    const second = store.start()
    expect(store.phase).toBe('opening')
    held.release()
    await Promise.all([first, second])

    expect(posts(scripted.calls, '/meeting/sessions')).toHaveLength(1)
  })

  it('lets a late answer of a superseded session alone', async () => {
    const held = gate()
    const { store, calls } = await live({
      routes: [
        {
          method: 'POST',
          match: '/respond',
          respond: async () => (await held.waiting, jsonResponse({ error: 'pigeon_busy', message: { en: 'busy' } }, 409)),
        },
      ],
    })

    const pending = store.respond()
    store.dispose()
    held.release()
    await pending

    expect(store.errorText).toBe('')
    expect(posts(calls, '/respond')).toHaveLength(1)
  })

  it('resets the per-turn state on a reconnect and marks the recovered answer', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'text', delta: 'Roek' })
    // Herverbinding: de server stuurt opnieuw een state, nu met speaking.
    ports.channel().emit({ type: 'state', sessionId: 'sessie-1', listening: 'listening', speaking: true, voice: 'ready' })
    expect(store.answers.at(-1)?.note).toMatch(/reconnect/i)
    expect(store.speaking).toBe(true)

    // Frames van de beurt waarvan we het begin misten klinken niet.
    ports.channel().emit({ type: 'tts_audio', audio: 'QUJD', seq: 1 })
    ports.channel().emit({ type: 'tts_done', frames: 1 })
    await tick()
    expect(ports.context().played).toHaveLength(0)
  })

  it('says so when the browser holds the sound back', async () => {
    const { store, ports } = await live({ ports: { suspended: true } })

    expect(store.audioBlocked).toBe(true)
    // De knop onder de melding is een gebruikersgebaar: die klik mag het wél
    // proberen, en zolang de browser weigert blijft de melding staan.
    const before = ports.context().resumes
    await store.enableAudio()
    expect(ports.context().resumes).toBe(before + 1)
    expect(store.audioBlocked).toBe(true)

    // Laat de browser het alsnog toe, dan verdwijnt de melding bij de volgende klik.
    ports.context().resumesTo = 'running'
    await store.enableAudio()
    expect(store.audioBlocked).toBe(false)
  })

  it('follows the browser when it blocks or releases the sound later on', async () => {
    const { store, ports } = await live()

    expect(store.audioBlocked).toBe(false)
    // De blokkade is geen momentopname bij het openen: een tab die naar de
    // achtergrond gaat schorst de context, en dat hoort het scherm te zeggen.
    ports.context().setState('suspended')
    expect(store.audioBlocked).toBe(true)

    ports.context().setState('running')
    expect(store.audioBlocked).toBe(false)
  })

  it('holds a blocked turn back instead of queueing it up', async () => {
    const { store, ports } = await live()

    ports.context().setState('suspended')
    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'tts_audio', audio: 'QUJD', seq: 1 })
    ports.channel().emit({ type: 'tts_done', frames: 1 })
    await tick()

    // De tekst staat er; het geluid wacht niet op een later moment.
    expect(ports.context().played).toHaveLength(0)
    expect(store.audioBlocked).toBe(true)
  })

  it('sends one DELETE and does not throw when disposed twice', async () => {
    const { store, ports, calls } = await live()

    store.dispose()
    store.dispose()
    await tick()
    expect(posts(calls, '/meeting/sessions/sessie-1', 'DELETE')).toHaveLength(1)
    expect(ports.context().closed).toBe(true)
    expect(store.disposed).toBe(true)
  })

  it('lets the session go with a keepalive DELETE when the page is hidden', async () => {
    const { calls } = await live()

    globalThis.dispatchEvent(new Event('pagehide'))
    await tick()
    const deletes = posts(calls, '/meeting/sessions/sessie-1', 'DELETE')
    expect(deletes).toHaveLength(1)
    expect(deletes[0].init?.keepalive).toBe(true)
  })

  it('ignores a broken frame instead of breaking the stream', async () => {
    const { store, ports } = await live()

    ports.channel().emitRaw('{ this is not json')
    ports.channel().emit({ type: 'arousal', arousal: 0.5 })
    expect(store.arousal).toBeCloseTo(0.5)
  })
  it('lets a late settings failure of a superseded session alone', async () => {
    const held = gate()
    const { store } = await live({
      routes: [
        {
          method: 'POST',
          match: '/settings',
          respond: async () => (await held.waiting, jsonResponse({ error: 'session_not_found' }, 404)),
        },
      ],
    })

    const pending = store.updateSettings({ autonomous: true })
    store.dispose()
    held.release()
    await pending
    expect(store.errorText).toBe('')
  })

  it('counts the blocks a failed POST could not deliver', async () => {
    const { store, ports } = await live({
      routes: [
        {
          method: 'POST',
          match: '/audio',
          respond: () => {
            throw new Error('lijn weg')
          },
        },
      ],
    })

    ports.mic().push()
    ports.mic().push()
    await tick()
    expect(store.dropped).toBeGreaterThan(0)
    store.dispose()
  })

  it('lets the sluice rest one block interval after a failed post', async () => {
    let broken = true
    const { ports, calls } = await live({
      routes: [
        {
          method: 'POST',
          match: '/audio',
          respond: () => {
            if (broken) {
              broken = false
              throw new Error('lijn weg')
            }
            return jsonResponse({ streamed: 1, dropped: 0, listening: 'listening' }, 202)
          },
        },
      ],
    })

    ports.mic().push()
    await tick()
    expect(posts(calls, '/audio')).toHaveLength(1)

    // Zonder rustpauze zou elk volgend blok de kapotte lijn meteen opnieuw
    // proberen; de sluis loopt dan in een lus van mislukkingen.
    ports.mic().push()
    await tick()
    expect(posts(calls, '/audio')).toHaveLength(1)

    ports.advance(BLOCK_MS)
    await tick()
    expect(posts(calls, '/audio')).toHaveLength(2)
  })

  it('lets a late audio answer of a superseded session alone', async () => {
    const held = gate()
    const { store, ports } = await live({
      routes: [
        {
          method: 'POST',
          match: '/audio',
          respond: async () => (await held.waiting, jsonResponse({ streamed: 1, dropped: 7, listening: 'listening' }, 202)),
        },
      ],
    })

    ports.mic().push()
    await tick()
    store.dispose()
    held.release()
    await tick()

    // De vergadering is neergelegd: het antwoord van de vorige sessie mag het
    // opgeruimde scherm niet meer aanraken.
    expect(store.dropped).toBe(0)
    expect(store.serverListen).toBe(LISTEN.off)
  })

  it('shows a session that came back from the page cache as ended', async () => {
    const { store } = await live()

    const event = new Event('pageshow') as Event & { persisted: boolean }
    Object.defineProperty(event, 'persisted', { value: true })
    globalThis.dispatchEvent(event)
    await tick()

    expect(store.live).toBe(false)
    expect(store.closedText).toMatch(/connection was lost/)
  })
})

// pcmFrame is een PCM16-frame van `n` monsters, base64 zoals de server het stuurt.
const pcmFrame = (n: number) => globalThis.btoa(String.fromCharCode(...new Uint8Array(n * 2)))
const pcm = (n: number, seq: number) => ({ type: 'tts_audio', audio: pcmFrame(n), seq, format: 'pcm16', sampleRate: 24000 })

describe('MeetingStore streaming audio (pcm16)', () => {
  it('plays pcm16 frames the moment they arrive, before tts_done', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit(pcm(4800, 1))
    ports.channel().emit(pcm(2400, 2))

    expect(ports.context().stream?.written).toEqual([4800, 2400])
    expect(ports.context().stream?.sampleRate).toBe(24000)
    expect(ports.context().decoded).toEqual([])

    ports.channel().emit({ type: 'tts_done', frames: 2 })
    ports.channel().emit({ type: 'response_done' })
    await tick()
    expect(store.audioGap).toBe(false)
    expect(ports.context().played).toHaveLength(0)
  })

  it('keeps the mp3 path for a voice that cannot stream', async () => {
    const { ports } = await live()

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'tts_audio', audio: 'QUJD', seq: 1, format: 'mp3', sampleRate: 44100 })
    expect(ports.context().streams).toHaveLength(0)
    ports.channel().emit({ type: 'tts_done', frames: 1 })
    await tick()
    expect(ports.context().decoded).toEqual([3])
    expect(ports.context().played).toHaveLength(1)
  })

  it('stops the stream locally on the stop button and ignores what still arrives', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit(pcm(100, 1))
    await store.stop()
    expect(ports.context().streams[0].stopped).toBe(true)

    ports.channel().emit(pcm(100, 2))
    expect(ports.context().streams).toHaveLength(1)
    expect(ports.context().streams[0].written).toEqual([100])
  })

  it('reports a gap in the frames honestly, after what already sounded', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit(pcm(100, 1))
    ports.channel().emit(pcm(100, 3))
    expect(ports.context().stream?.written).toEqual([100])

    ports.channel().emit({ type: 'tts_done', frames: 3 })
    expect(store.audioGap).toBe(true)
  })

  it('reports a silent turn when the session was supposed to have a voice', async () => {
    const { store, ports } = await live()

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit(pcm(100, 1))
    ports.channel().emit({ type: 'tts_done', frames: 1 })
    expect(store.audioGap).toBe(false)

    ports.channel().emit({ type: 'response_done' })
    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'tts_done', frames: 0 })
    await tick()
    expect(store.audioGap).toBe(true)
  })

  it('does not write into a stream while the browser blocks audio', async () => {
    const { ports } = await live({ ports: { suspended: true } })

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit(pcm(100, 1))
    expect(ports.context().streams).toHaveLength(0)
  })

  it('a second turn continues in the same stream, right after the first', async () => {
    const { ports } = await live()

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit(pcm(10, 1))
    ports.channel().emit({ type: 'tts_done', frames: 1 })
    ports.channel().emit({ type: 'response_done' })
    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit(pcm(20, 1))

    expect(ports.context().streams).toHaveLength(1)
    expect(ports.context().stream?.written).toEqual([10, 20])
  })
})

describe('Release meeting contracts', () => {
  it('loads configuration once with a shared pending request and cannot overwrite a live session', async () => {
    const held = gate()
    const scripted = scriptedFetch([
      { method: 'GET', match: '/duiven', respond: async () => { await held.waiting; return jsonResponse({ duiven: [], gedrag: { taalDefault: 'nl', interventieZelfstandig: false, detectieGretigheid: 'reserved', interventieThrottleSeconden: 300 } }) } },
      ...meetingRoutes(),
    ])
    const { store, locale } = meetingHarness(scripted.fetch)
    const first = store.loadPigeons()
    const second = store.loadPigeons()
    expect(first).toBe(second)
    store.pigeonId = 'individualist'
    store.updateSettings({ autonomous: true, eagerness: 'eager', cooldownSeconds: 30 })
    await store.start()
    held.release()
    await first
    expect(scripted.calls.filter((call) => call.url.endsWith('/duiven'))).toHaveLength(1)
    expect(store.settings).toMatchObject({ autonomous: true, eagerness: 'eager', cooldownSeconds: 30 })
    expect(locale.lang).toBe('en')
    await store.loadPigeons()
    expect(scripted.calls.filter((call) => call.url.endsWith('/duiven'))).toHaveLength(1)
    store.dispose()
  })

  it('keeps source ownership when a later contribution replaces the prominent answer', async () => {
    const { store, ports } = await live()
    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'text', delta: 'First claim [1]' })
    ports.channel().emit({ type: 'response_done', kbStatus: 'sources', sources: [{ number: 1, type: 'destillaat', id: 'first', text: 'First evidence' }] })
    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'text', delta: 'Second claim [1]' })
    ports.channel().emit({ type: 'response_done', kbStatus: 'sources', sources: [{ number: 1, type: 'destillaat', id: 'second', text: 'Second evidence' }] })
    expect(store.answers[0]).toMatchObject({ pigeonId: 'individualist', text: 'First claim [1]', status: 'complete', sources: [{ id: 'first' }] })
    expect(store.answers[1]).toMatchObject({ text: 'Second claim [1]', sources: [{ id: 'second' }] })
    store.end()
    expect(store.sources).toEqual([])
    expect(store.answers).toEqual([])
  })

  it('keeps unfinished speech out of the transcript and drops it the moment the microphone is muted', async () => {
    const { store, ports } = await live()
    // Een partial is nog geen regel: hij staat apart en telt niet mee in het
    // sessiegeheugen.
    ports.channel().emit({ type: 'transcript', text: 'Draft words', final: false })
    expect(store.partial).toEqual({ speaker: '', text: 'Draft words' })
    expect(store.transcript).toHaveLength(0)
    ports.channel().emit({ type: 'transcript', text: 'Corrected words', final: true })
    expect(store.partial).toBeNull()
    expect(store.transcript.at(-1)?.text).toBe('Corrected words')
    // Dempen wist wat nog niet gezegd was meteen: er blijft geen halve zin op
    // het scherm staan die de duif niet meer hoort.
    ports.channel().emit({ type: 'transcript', text: 'Next phrase', final: false })
    expect(store.partial?.text).toBe('Next phrase')
    store.setMuted(true)
    expect(store.partial).toBeNull()
    ports.channel().emit({ type: 'transcript', text: 'Later draft', final: false })
    expect(store.partial).toBeNull()
    store.end()
    ports.advance(10000)
    expect(store.transcript).toHaveLength(0)
    expect(store.partial).toBeNull()
  })

  it('plays a signal once per event, replaces rather than overlaps, and stays stopped until explicitly invited', async () => {
    const { store, ports } = await live({ pigeon: 'nonverbal' })
    const signal = (id: string) => ports.channel().emit({ type: 'sound', eventId: id, durationMs: 2000, sound: { id: 'same-recording', url: '/api/duif/v1/sounds/same' } })
    signal('one')
    const first = ports.soundPlayers.at(-1)!
    expect(store.signal?.id).toBe('one')
    signal('one')
    expect(ports.soundPlayers).toHaveLength(1)
    signal('two')
    expect(first.stopped).toBe(true)
    expect(store.signal?.id).toBe('two')
    first.end()
    expect(store.signal?.id).toBe('two')
    ports.soundPlayers.at(-1)!.end()
    expect(store.signal).toBeNull()
    await store.stop()
    signal('three')
    expect(store.signal).toBeNull()
    await store.intervene()
    signal('four')
    expect(store.signal?.id).toBe('four')
    expect(store.autonomyPaused).toBe(true)
    store.end()
  })

  // De signalerende duif doet standaard zelfstandig mee, en dan is een geblokkeerde browser
  // niet van een duif die niets doet te onderscheiden — tenzij het scherm het
  // zegt. Zij speelt dan geen geluid, houdt haar begrensde zichtbare signaal, en
  // de melding is die van het signaal en niet die van een gesproken bijdrage.
  it('says the browser blocks the sound when she takes part on her own', async () => {
    const { store, ports } = await live({ pigeon: 'nonverbal', ports: { suspended: true }, lang: 'en' })

    expect(store.settings.autonomous).toBe(true)
    expect(store.audioBlocked).toBe(true)

    ports.channel().emit({ type: 'sound', eventId: 'blocked-signal', durationMs: 1500, sound: { id: 'clip', url: '/api/duif/v1/sounds/clip' } })

    expect(ports.soundPlayers).toHaveLength(0)
    expect(store.notice?.id).toBe('sound-blocked')
    expect(store.notice?.text).toBe('The browser is blocking sound. The visual signal remains available.')
    expect(store.notice?.action).toBe('sound')
    // Het zichtbare signaal blijft staan en loopt netjes af.
    expect(store.signal?.id).toBe('blocked-signal')
    ports.advance(1500)
    expect(store.signal).toBeNull()
    store.end()
  })

  it('keeps a visible bounded signal when its audio fails', async () => {
    const { store, ports } = await live({ pigeon: 'nonverbal' })
    ports.channel().emit({ type: 'sound', eventId: 'failed-media', durationMs: 1500, sound: { id: 'clip', url: '/api/duif/v1/sounds/clip' } })
    ports.soundPlayers.at(-1)!.fail()
    expect(store.signalError).toBe(true)
    expect(store.signal?.id).toBe('failed-media')
    ports.advance(1500)
    expect(store.signal).toBeNull()
    store.end()
  })

  it('checks microphone input locally and closes every resource without sending audio', async () => {
    const scripted = scriptedFetch(meetingRoutes())
    const { store, ports } = meetingHarness(scripted.fetch)
    await store.checkDevices()
    expect(store.deviceCheck).toBe('checking')
    ports.mic().push(new Int16Array([5000, -3000]))
    expect(store.deviceCheckHeard).toBe(true)
    ports.advance(3000)
    expect(store.deviceCheck).toBe('ready')
    expect(ports.mic().stopped).toBe(true)
    expect(ports.context().closed).toBe(true)
    expect(scripted.calls).toHaveLength(0)
    store.dispose()
  })

  it('reports a refused local check and permits another explicit attempt', async () => {
    const { store, ports } = meetingHarness(scriptedFetch(meetingRoutes()).fetch, { micRefused: true })
    await store.checkDevices()
    expect(store.deviceCheck).toBe('refused')
    expect(ports.context().closed).toBe(true)
    await store.checkDevices()
    expect(ports.contexts).toHaveLength(2)
    expect(ports.context().closed).toBe(true)
    store.dispose()
  })

  it('pagehide closes the microphone, players, channel and transient content before a hanging DELETE resolves', async () => {
    const { store, ports } = await live({ routes: [{ method: 'DELETE', match: '/meeting/sessions/', respond: () => new Promise<Response>(() => {}) }] })
    ports.channel().emit({ type: 'transcript', text: 'Synthetic test phrase', final: true })
    ports.channel().emit({ type: 'sound', eventId: 'hide', sound: { id: 'clip', url: '/api/duif/v1/sounds/clip' } })
    const mic = ports.mic()
    const audio = ports.soundPlayers.at(-1)!
    globalThis.dispatchEvent(new Event('pagehide'))
    expect(mic.stopped).toBe(true)
    expect(audio.stopped).toBe(true)
    expect(ports.channel().closed).toBe(true)
    expect(store.live).toBe(false)
    expect(store.transcript).toEqual([])
    expect(store.signal).toBeNull()
  })

  it('holds automatic contributions after stop and only unpauses after a successful explicit resume', async () => {
    const { store, ports, calls } = await live()
    await store.stop()
    ports.channel().emit({ type: 'stopped', autonomyPaused: true })
    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    expect(store.speaking).toBe(false)
    expect(store.answers).toHaveLength(0)
    expect(store.autonomyPaused).toBe(true)
    await store.resumeAutonomy()
    expect(calls.some((call) => call.url.endsWith('/resume'))).toBe(true)
    expect(store.autonomyPaused).toBe(false)
    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    expect(store.speaking).toBe(true)
    store.dispose()
  })
})

describe('Tracked playback handshake', () => {
  const tracked: Route = {
    method: 'POST', match: OPEN_SESSION_ROUTE,
    respond: () => jsonResponse({ sessionId: 'sessie-1', pigeonId: 'individualist', lang: 'en', voice: 'ready', listening: 'listening', playbackTracking: true }, 201),
  }

  it('holds speaking through generation completion until scheduled PCM really ends', async () => {
    const { store, ports, calls } = await live({ routes: [tracked] })
    ports.channel().emit({ type: 'persona', turnId: '1', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'tts_audio', turnId: '1', format: 'pcm16', seq: 1, sampleRate: 24000, audio: 'AAAAAA==' })
    ports.channel().emit({ type: 'tts_done', turnId: '1', frames: 1 })
    ports.channel().emit({ type: 'response_done', turnId: '1' })
    expect(store.speaking).toBe(true)
    expect(store.playbackStatus).toBe('started')
    const acknowledgements = () => calls.filter((call) => call.url.endsWith('/playback')).map((call) => JSON.parse(String(call.init?.body)))
    expect(acknowledgements()).toContainEqual({ turnId: '1', state: 'started' })
    expect(acknowledgements()).not.toContainEqual({ turnId: '1', state: 'ended' })
    ports.context().stream!.finishPlayback()
    expect(store.speaking).toBe(false)
    expect(acknowledgements()).toContainEqual({ turnId: '1', state: 'ended' })
    store.dispose()
  })

  it('also releases the local contribution when audio ends before response_done', async () => {
    const { store, ports } = await live({ routes: [tracked] })
    ports.channel().emit({ type: 'persona', turnId: '1', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'tts_audio', turnId: '1', format: 'pcm16', seq: 1, sampleRate: 24000, audio: 'AAAAAA==' })
    ports.channel().emit({ type: 'tts_done', turnId: '1', frames: 1 })
    ports.context().stream!.finishPlayback()
    expect(store.speaking).toBe(true)
    ports.channel().emit({ type: 'response_done', turnId: '1' })
    expect(store.speaking).toBe(false)
    store.dispose()
  })

  it('rejects older turn frames and cannot replay a stopped turn', async () => {
    const { store, ports } = await live({ routes: [tracked] })
    ports.channel().emit({ type: 'persona', turnId: '2', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'text', turnId: '2', delta: 'Current' })
    ports.channel().emit({ type: 'text', turnId: '1', delta: 'Stale' })
    ports.channel().emit({ type: 'persona', turnId: '1', pigeonId: 'individualist' })
    expect(store.answers.at(-1)?.text).toBe('Current')
    await store.stop()
    ports.channel().emit({ type: 'persona', turnId: '2', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'tts_audio', turnId: '2', format: 'pcm16', seq: 1, sampleRate: 24000, audio: 'AAAAAA==' })
    expect(ports.context().streams).toHaveLength(0)
    expect(store.speaking).toBe(false)
    store.dispose()
  })
})

describe('Operational recovery', () => {
  it('retries the detector only on explicit request and keeps unavailable state after a failed request', async () => {
    const { store, ports, calls } = await live({ routes: [{ method: 'POST', match: '/detector/retry', respond: () => jsonResponse({ error: 'llm_unavailable' }, 503) }] })
    ports.channel().emit({ type: 'detector_status', detector: { enabled: false, state: 'unavailable', retryable: true } })
    ports.advance(30000)
    expect(calls.some((call) => call.url.endsWith('/detector/retry'))).toBe(false)
    await store.retryDetector()
    expect(store.detector?.enabled).toBe(false)
    expect(store.errorText).not.toBe('')
    expect(store.detectorRetryInFlight).toBe(false)
    store.dispose()
  })

  it('drops current audio when the browser suspends playback instead of replaying it later', async () => {
    const { store, ports } = await live()
    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'tts_audio', format: 'pcm16', seq: 1, sampleRate: 24000, audio: 'AAAAAA==' })
    const stream = ports.context().stream!
    ports.context().setState('suspended')
    expect(stream.stopped).toBe(true)
    expect(store.audioGap).toBe(true)
    ports.context().setState('running')
    ports.channel().emit({ type: 'tts_audio', format: 'pcm16', seq: 2, sampleRate: 24000, audio: 'AAAAAA==' })
    expect(stream.written).toHaveLength(1)
    store.dispose()
  })
})


describe('Meeting provider finish reasons', () => {
  for (const stopReason of ['truncated', 'filtered'] as const) {
    it(`retains ${stopReason} on its own answer while later turns arrive`, async () => {
      const { store, ports } = await live()
      const ch = ports.channel()
      ch.emit({ type: 'persona', turnId: '1', pigeonId: 'individualist' })
      ch.emit({ type: 'text', turnId: '1', delta: 'Synthetic unfinished answer' })
      const mediaRevision = store.mediaStopRevision
      ch.emit({ type: 'response_done', turnId: '1', stopReason, kbStatus: 'sources', sources: [{ number: 1, type: 'destillaat', text: 'Synthetic source' }] })
      expect(store.answers[0]).toMatchObject({ turnId: '1', text: 'Synthetic unfinished answer', status: 'complete', stopReason })
      expect(store.answers[0].sources).toHaveLength(1)
      expect(store.mediaStopRevision).toBe(mediaRevision)
      ch.emit({ type: 'persona', turnId: '2', pigeonId: 'individualist' })
      ch.emit({ type: 'text', turnId: '2', delta: 'Next answer' })
      ch.emit({ type: 'response_done', turnId: '1', stopReason })
      expect(store.answers[1]).toMatchObject({ turnId: '2', text: 'Next answer', status: 'streaming' })
      expect(store.answers[1].stopReason).toBeUndefined()
      ch.emit({ type: 'response_done', turnId: '2' })
      expect(store.answers[1]).toMatchObject({ status: 'complete' })
      expect(store.answers[1].stopReason).toBeUndefined()
      store.dispose()
    })
  }

  it('ignores unknown or malformed reasons and does not copy them into answers', async () => {
    const { store, ports } = await live()
    for (const stopReason of ['natural', 'future_reason', null, { type: 'filtered' }, 42]) {
      ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
      ports.channel().emit({ type: 'response_done', stopReason })
      expect(store.answers.at(-1)?.stopReason).toBeUndefined()
      expect(store.answers.at(-1)?.status).toBe('complete')
    }
    store.dispose()
  })

  it('keeps a locally stopped answer stopped when a late provider close arrives', async () => {
    const { store, ports } = await live()
    ports.channel().emit({ type: 'persona', turnId: '1', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'text', turnId: '1', delta: 'Before Stop' })
    await store.stop()
    ports.channel().emit({ type: 'response_done', turnId: '1', stopReason: 'truncated' })
    expect(store.answers[0]).toMatchObject({ text: 'Before Stop', status: 'stopped' })
    expect(store.answers[0].stopReason).toBeUndefined()
    expect(store.stopped).toBe(true)
    expect(store.speaking).toBe(false)
    store.dispose()
  })
})
