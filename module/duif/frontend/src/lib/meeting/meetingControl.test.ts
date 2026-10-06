import { afterEach, expect, it, vi } from 'vitest'
import { jsonResponse, scriptedFetch, type Route } from '../testing/fetch'
import { meetingHarness, meetingRoutes } from '../testing/meeting'
import { SETTINGS_DEBOUNCE_MS } from './meetingStore.svelte'

const flush = () => new Promise((resolve) => setTimeout(resolve, 0))
const stores: { dispose(): void }[] = []
afterEach(() => { for (const store of stores.splice(0)) store.dispose() })

async function live(routes: Route[] = []) {
  const scripted = scriptedFetch([...routes, ...meetingRoutes()])
  const h = meetingHarness(scripted.fetch)
  stores.push(h.store)
  await h.store.loadPigeons()
  await h.store.start()
  await flush()
  h.ports.channel().emit({
    type: 'state', sessionId: 'sessie-1', turnId: '0', voice: 'ready',
    listening: 'listening', speaking: false, playbackTracking: true,
    autonomyPaused: false, settings: { autonomous: true },
  })
  return { ...h, calls: scripted.calls }
}

function pcm(h: Awaited<ReturnType<typeof live>>, turnId = '1') {
  h.ports.channel().emit({ type: 'persona', sessionId: 'sessie-1', turnId, pigeonId: 'individualist' })
  h.ports.channel().emit({
    type: 'tts_audio', sessionId: 'sessie-1', turnId,
    format: 'pcm16', seq: 1, audio: 'AQACAA==', sampleRate: 24000,
  })
}

it('treats server stopped as a new generation barrier and ignores older stops afterward', async () => {
  const h = await live()
  pcm(h)
  const stream = h.ports.context().stream!
  expect(stream.stopped).toBe(false)
  // The server increments its generation before publishing stopped.
  h.ports.channel().emit({
    type: 'stopped', sessionId: 'sessie-1', turnId: '2', reason: 'playback_timeout',
    autonomyPaused: true, listening: 'listening', speaking: false,
  })
  expect(stream.stopped).toBe(true)
  expect(h.store.autonomyPaused).toBe(true)
  await h.store.respond()
  pcm(h, '3')
  const newer = h.ports.context().stream!
  h.ports.channel().emit({ type: 'stopped', turnId: '2', autonomyPaused: true })
  expect(newer.stopped).toBe(false)
  expect(h.store.speaking).toBe(true)
})

it('keeps local Stop through a failed request, heartbeats and reconnect snapshots', async () => {
  const h = await live([{ method: 'POST', match: '/stop', respond: () => Promise.reject(new Error('network down')) }])
  pcm(h)
  await h.store.stop()
  expect(h.store.autonomyPaused).toBe(true)
  h.ports.channel().emit({
    type: 'listening_heartbeat', sessionId: 'sessie-1',
    autonomyPaused: false, listening: 'listening', speaking: true,
  })
  expect(h.store.autonomyPaused).toBe(true)
  h.ports.channel().emit({ type: 'state', turnId: '1', speaking: false, autonomyPaused: false })
  h.ports.channel().emit({ type: 'settings', autonomyPaused: false, settings: { autonomous: true } })
  h.ports.channel().emit({ type: 'persona', sessionId: 'sessie-1', turnId: '2', pigeonId: 'individualist' })
  expect(h.store.autonomyPaused).toBe(true)
  expect(h.store.speaking).toBe(false)
})

it('silences retained audio when reconnecting to a server that already stopped', async () => {
  const h = await live()
  pcm(h)
  const stream = h.ports.context().stream!
  h.ports.channel().fail(false)
  h.ports.channel().emit({
    type: 'state', sessionId: 'sessie-1', turnId: '2', voice: 'ready',
    listening: 'listening', speaking: false, playbackTracking: true, autonomyPaused: true,
  })
  expect(h.store.speaking).toBe(false)
  expect(stream.stopped).toBe(true)
})

it('preserves a manual invitation made after Stop while its server confirmation is still in flight', async () => {
  const h = await live()
  pcm(h)
  await h.store.stop()
  await h.store.respond()
  h.ports.channel().emit({ type: 'stopped', turnId: '2', autonomyPaused: true })
  pcm(h, '3')
  expect(h.store.speaking).toBe(true)
  expect(h.store.autonomyPaused).toBe(true)
})

it('reopens audio transport on microphone retry and waits for provider status before showing listening', async () => {
  let count = 0
  const h = await live([{
    method: 'POST', match: '/audio',
    respond: () => ++count === 1
      ? jsonResponse({ error: 'stt_unavailable' }, 503)
      : jsonResponse({ listening: 'listening', streamed: 1, dropped: 0 }, 202),
  }])
  const oldMic = h.ports.mic()
  oldMic.push()
  await flush()
  expect(h.store.localMic).toBe('closed')
  await h.store.retryMicrophone()
  expect(h.store.localMic).toBe('open')
  expect(h.store.listen).toBe('reconnecting')
  oldMic.push() // A late callback of the closed capture cannot enter the new transport.
  expect(count).toBe(1)
  h.ports.mic().push()
  await flush()
  expect(count).toBe(2)
  expect(h.store.listen).toBe('listening')
})

it('does not resume from an accepted response that contains no confirmed pause state', async () => {
  const h = await live([{
    method: 'POST', match: '/resume', respond: () => jsonResponse({ accepted: true }, 202),
  }])
  await h.store.stop()
  await h.store.resumeAutonomy()
  expect(h.store.autonomyPaused).toBe(true)
  expect(h.store.playbackSyncFailed).toBe(true)
})

it('a delayed resume confirmation cannot override a newer Stop', async () => {
  let release!: (response: Response) => void
  const h = await live([{
    method: 'POST', match: '/resume', respond: () => new Promise<Response>((resolve) => { release = resolve }),
  }])
  await h.store.stop()
  const pending = h.store.resumeAutonomy()
  await h.store.stop()
  release(jsonResponse({ autonomyPaused: false }, 202))
  await pending
  expect(h.store.autonomyPaused).toBe(true)
})

it('a collapsed settings toggle keeps pause unless a separate resume is confirmed', async () => {
  const h = await live([
    { method: 'POST', match: '/settings', respond: () => jsonResponse({ autonomous: true, autonomyPaused: true }, 202) },
    { method: 'POST', match: '/resume', respond: () => jsonResponse({ accepted: true }, 202) },
  ])
  await h.store.stop()
  h.ports.channel().emit({ type: 'stopped', autonomyPaused: true, speaking: false })
  h.store.updateSettings({ autonomous: false })
  h.store.updateSettings({ autonomous: true })
  h.ports.advance(SETTINGS_DEBOUNCE_MS)
  await flush()
  expect(h.store.autonomyPaused).toBe(true)
  expect(h.calls.filter((call) => call.url.endsWith('/resume'))).toHaveLength(1)
})

it('explicitly resumes a collapsed settings toggle when the server confirms success', async () => {
  const h = await live([{
    method: 'POST', match: '/settings', respond: () => jsonResponse({ autonomous: true, autonomyPaused: true }, 202),
  }])
  await h.store.stop()
  h.ports.channel().emit({ type: 'stopped', autonomyPaused: true })
  h.store.updateSettings({ autonomous: false })
  h.store.updateSettings({ autonomous: true })
  h.ports.advance(SETTINGS_DEBOUNCE_MS)
  await flush()
  expect(h.store.autonomyPaused).toBe(false)
  expect(h.calls.filter((call) => call.url.endsWith('/resume'))).toHaveLength(1)
})

it('a delayed settings response cannot release a newer Stop', async () => {
  let release!: (response: Response) => void
  const h = await live([{
    method: 'POST', match: '/settings', respond: () => new Promise<Response>((resolve) => { release = resolve }),
  }])
  await h.store.stop()
  h.store.updateSettings({ autonomous: false })
  h.store.updateSettings({ autonomous: true })
  h.ports.advance(SETTINGS_DEBOUNCE_MS)
  await h.store.stop()
  release(jsonResponse({ autonomyPaused: false, autonomous: true }, 202))
  await flush()
  expect(h.store.autonomyPaused).toBe(true)
})

for (const earlyEnd of [true, false]) {
  it(`releases speaking when actual PCM ends ${earlyEnd ? 'before' : 'after'} response_done`, async () => {
    const h = await live()
    pcm(h)
    h.ports.channel().emit({ type: 'text', turnId: '1', delta: 'A complete reply.' })
    h.ports.channel().emit({ type: 'tts_done', turnId: '1', frames: 1 })
    if (earlyEnd) h.ports.context().stream!.finishPlayback()
    expect(h.store.speaking).toBe(true)
    h.ports.channel().emit({ type: 'response_done', turnId: '1', kbStatus: 'gap' })
    if (!earlyEnd) {
      expect(h.store.speaking).toBe(true)
      h.ports.context().stream!.finishPlayback()
    }
    expect(h.store.speaking).toBe(false)
    expect(h.store.answers.at(-1)?.status).toBe('complete')
  })
}

it('keeps unfixable detector detail out of the user text without logging it', async () => {
  const logged = vi.spyOn(console, 'warn').mockImplementation(() => {})
  try {
    const h = await live()
    h.ports.channel().emit({ type: 'detector_status', detector: { enabled: false, state: 'budget_stopped', reason: 'llm_budget_exceeded' } })
    // Wat de gebruiker wél merkt: de duif springt niet meer uit zichzelf in.
    // Wat zij niet kan verhelpen — de kostenlimiet — staat nergens: niet op
    // het scherm en niet in de console (modulecode logt niets).
    expect(h.store.notice?.text).toMatch(/Automatic participation is unavailable/)
    expect(h.store.notice?.text).not.toMatch(/cost limit/)
    h.locale.choose('nl')
    expect(h.store.notice?.text).not.toMatch(/kostenlimiet/)
    h.ports.channel().emit({ type: 'detector_status', detector: { enabled: false, state: 'invalid', reason: 'detector_invalid_response' } })
    expect(h.store.notice?.text).not.toMatch(/niet bruikbaar/)
    expect(logged).not.toHaveBeenCalled()
  } finally {
    logged.mockRestore()
  }
})

// Een mislukte beurt zonder geluid mag daarna geen geluidsmelding opleveren.
// De store bevestigde het afspelen ("failed") ook bij een tekstbeurt; de server
// had zijn generatie toen al opgehoogd, weigerde die bevestiging en zette
// playbackSyncFailed. Onder de foutmelding bleef dat onzichtbaar, maar de
// herkansing wist de fout en liet "het geluid speelde niet volledig af" staan
// over een beurt die nooit een klank had.
it('does not blame the sound after an error turn that never had any', async () => {
  const h = await live([{ method: 'POST', match: '/playback', respond: () => jsonResponse({ accepted: false }) }])
  h.ports.channel().emit({ type: 'persona', sessionId: 'sessie-1', turnId: '1', pigeonId: 'individualist' })
  h.ports.channel().emit({ type: 'text', sessionId: 'sessie-1', turnId: '1', delta: 'Half a senten' })
  h.ports.channel().emit({ type: 'error', sessionId: 'sessie-1', turnId: '1', error: 'internal_error', message: 'upstream' })
  h.ports.channel().emit({ type: 'response_done', sessionId: 'sessie-1', turnId: '1', kbStatus: 'gap' })
  await flush()
  expect(h.store.notice?.id).toBe('error')
  // Er is geen enkele afspeelbevestiging verstuurd: er viel niets af te spelen.
  expect(h.calls.filter((call) => call.url.includes('/playback'))).toHaveLength(0)

  await h.store.respond()
  await flush()
  expect(h.store.notice?.id).not.toBe('sound')
  expect(h.store.notice?.text ?? '').not.toContain('The sound did not play in full')
  h.locale.choose('nl')
  expect(h.store.notice?.id).not.toBe('sound')
  expect(h.store.notice?.text ?? '').not.toContain('Het geluid speelde niet volledig af')
})

it('shows at most one notice and lets a failure outrank a quieter one', async () => {
  const h = await live()
  h.ports.channel().emit({ type: 'detector_status', detector: { enabled: false, state: 'unavailable' } })
  expect(h.store.notice?.id).toBe('detector')
  expect(h.store.status).toBe('listening')
  h.store.audioBlocked = true
  expect(h.store.notice?.id).toBe('sound-blocked')
  expect(h.store.notice?.action).toBe('sound')
  h.store.error = { status: 503, reason: 'llm_unavailable' }
  expect(h.store.notice?.id).toBe('error')
  expect(h.store.notice?.tone).toBe('danger')
  expect(h.store.status).toBe('attention')
})

// Een duif zonder stem met automatisch meedoen aan: dat de stem uit staat weet
// de begeleider al bij het opzetten, dat de detectie uitviel niet. Die laatste
// gaat dus voor, anders ziet zij nooit dat de duif uit zichzelf niets meer
// inbrengt.
it('lets a paused detector outrank the voiceless notice while autonomy is on', async () => {
  const h = await live()
  h.ports.channel().emit({
    type: 'state', sessionId: 'sessie-1', turnId: '0', voice: 'off',
    listening: 'listening', speaking: false, playbackTracking: true, settings: { autonomous: true },
  })
  expect(h.store.notice?.id).toBe('voice')
  h.ports.channel().emit({ type: 'detector_status', detector: { enabled: false, state: 'unavailable' } })
  expect(h.store.notice?.id).toBe('detector')
})

// De bronaudit is uit de beurt verdwenen, dus de server stuurt dit
// bericht niet meer. Een client die tijdens een deploy nog even naast een oude
// server draait kan het wél binnenkrijgen, en dan geldt dezelfde regel als voor
// elk onbekend bericht: negeren, en niets aan het antwoord veranderen.
it('ignores a source-check event from an older server', async () => {
  const h = await live()
  h.ports.channel().emit({ type: 'persona', sessionId: 'sessie-1', turnId: '1', pigeonId: 'individualist' })
  h.ports.channel().emit({ type: 'text', sessionId: 'sessie-1', turnId: '1', delta: 'A grounded sentence.' })
  h.ports.channel().emit({ type: 'response_done', sessionId: 'sessie-1', turnId: '1', kbStatus: 'sources' })

  const before = h.store.answers.at(-1)
  h.ports.channel().emit({
    type: 'audit', sessionId: 'sessie-1', turnId: '1',
    audit: { status: 'flagged', findings: [{ sentence: 1, text: 'A grounded sentence.', reason: 'unsupported_fact' }] },
  })
  expect(h.store.answers.at(-1)).toEqual(before)
  expect(h.store.answers.at(-1)?.text).toBe('A grounded sentence.')
  expect(h.store.notice).toBeNull()
})

it('a confirmed resume supersedes stopped events from the generation it resolved', async () => {
  const h = await live([{ method: 'POST', match: '/stop', respond: () => jsonResponse({ stopped: true, settings: { turnId: '2', autonomyPaused: true } }, 202) }, {
    method: 'POST', match: '/resume', respond: () => jsonResponse({ autonomyPaused: false, settings: { turnId: '2' } }, 202),
  }])
  pcm(h)
  await h.store.stop()
  await h.store.resumeAutonomy()
  h.ports.channel().emit({ type: 'stopped', turnId: '2', autonomyPaused: true })
  expect(h.store.autonomyPaused).toBe(false)
  pcm(h, '3')
  expect(h.store.speaking).toBe(true)
})

it('resume can confirm an older stop whose SSE confirmation arrived during the request', async () => {
  let release!: (response: Response) => void
  const h = await live([{ method: 'POST', match: '/stop', respond: () => jsonResponse({ stopped: true, settings: { turnId: '2', autonomyPaused: true } }, 202) }, {
    method: 'POST', match: '/resume', respond: () => new Promise<Response>((resolve) => { release = resolve }),
  }])
  pcm(h)
  await h.store.stop()
  const pending = h.store.resumeAutonomy()
  h.ports.channel().emit({ type: 'stopped', turnId: '2', autonomyPaused: true })
  release(jsonResponse({ autonomyPaused: false, settings: { turnId: '2' } }, 202))
  await pending
  expect(h.store.autonomyPaused).toBe(false)
})

it('resume cannot supersede a genuinely newer server stop', async () => {
  let release!: (response: Response) => void
  const h = await live([{
    method: 'POST', match: '/resume', respond: () => new Promise<Response>((resolve) => { release = resolve }),
  }])
  await h.store.stop()
  const pending = h.store.resumeAutonomy()
  h.ports.channel().emit({ type: 'stopped', turnId: '4', autonomyPaused: true })
  release(jsonResponse({ autonomyPaused: false, settings: { turnId: '2' } }, 202))
  await pending
  expect(h.store.autonomyPaused).toBe(true)
})
