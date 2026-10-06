// De tijdlijn van één vergaderantwoord. De vraag die deze tests
// beantwoorden is niet "komt het goed aan", maar "wanneer is de bijdrage af".
// De server splitst dat: `sources` staat vóór de eerste
// tekstdelta, `text_done` zegt dat het geschreven antwoord compleet is, en
// `response_done` sluit de beurt pas ná de stem. De twee stemvarianten spelen
// dezelfde volgorde met een ander audiopad (PCM direct, MP3 pas bij tts_done).
import { describe, expect, it, vi } from 'vitest'
vi.mock('virtual:duif-media', () => ({ default: { signalSounds: null } }))
import { scriptedFetch } from '../testing/fetch'
import { meetingHarness, meetingRoutes } from '../testing/meeting'

const tick = () => new Promise((resolve) => setTimeout(resolve, 0))

async function live() {
  const scripted = scriptedFetch(meetingRoutes())
  const harness = meetingHarness(scripted.fetch)
  await harness.store.loadPigeons()
  await harness.store.start()
  await tick()
  return harness
}

const SOURCES = [{ number: 1, type: 'destillaat', text: 'Pigeons rest on ledges' }]

// pcmFrame is een PCM16-frame van `n` monsters, base64 zoals de server het stuurt.
const pcmFrame = (n: number) => globalThis.btoa(String.fromCharCode(...new Uint8Array(n * 2)))

describe('MeetingAnswer timeline (pcm16, voice plays while it arrives)', () => {
  it('completes the answer on text_done and keeps the turn open for the voice', async () => {
    const { store, ports } = await live()
    const channel = ports.channel()

    channel.emit({ type: 'persona', turnId: '1', pigeonId: 'individualist' })
    // De bronnen komen vóór de tekst: de kaart kan de "[1]" meteen als
    // verwijzing tekenen in plaats van als ruwe tekst.
    channel.emit({ type: 'sources', turnId: '1', kbStatus: 'sources', sources: SOURCES })
    expect(store.answers.at(-1)?.sources).toHaveLength(1)
    expect(store.answers.at(-1)?.kbStatus).toBe('sources')
    expect(store.answers.at(-1)?.status).toBe('streaming')

    channel.emit({ type: 'text', turnId: '1', delta: 'Pigeons rest on ledges [1].' })
    channel.emit({ type: 'tts_audio', turnId: '1', seq: 1, format: 'pcm16', sampleRate: 24000, audio: pcmFrame(4800) })
    expect(store.answers.at(-1)?.status).toBe('streaming')

    // Tekst klaar. De cursor hoort hier weg te zijn — dat is precies wat
    // status !== 'streaming' de kaart vertelt — terwijl de stem doorspeelt.
    channel.emit({ type: 'text_done', turnId: '1', kbStatus: 'sources', sources: SOURCES })
    expect(store.answers.at(-1)?.status).toBe('complete')
    expect(store.speaking).toBe(true)
    expect(store.status).toBe('speaking')

    // De stem loopt door ná text_done; die frames mogen niet verloren gaan.
    channel.emit({ type: 'tts_audio', turnId: '1', seq: 2, format: 'pcm16', sampleRate: 24000, audio: pcmFrame(2400) })
    expect(ports.context().stream?.written).toEqual([4800, 2400])

    channel.emit({ type: 'tts_done', turnId: '1', frames: 2 })
    channel.emit({ type: 'response_done', turnId: '1', kbStatus: 'sources', sources: SOURCES })
    await tick()
    expect(store.answers.at(-1)?.status).toBe('complete')
    expect(store.answers.at(-1)?.sources).toHaveLength(1)
  })
})

describe('MeetingAnswer timeline (mp3, voice plays only when the turn is whole)', () => {
  it('completes the answer on text_done even though the audio is still one file away', async () => {
    const { store, ports } = await live()
    const channel = ports.channel()

    channel.emit({ type: 'persona', turnId: '1', pigeonId: 'individualist' })
    channel.emit({ type: 'sources', turnId: '1', kbStatus: 'sources', sources: SOURCES })
    channel.emit({ type: 'text', turnId: '1', delta: 'Grounded contribution [1].' })
    channel.emit({ type: 'tts_audio', turnId: '1', seq: 1, format: 'mp3', sampleRate: 44100, audio: 'QUJD' })
    // MP3-brokken zijn geen bestand: er speelt nog niets.
    expect(ports.context().decoded).toEqual([])

    channel.emit({ type: 'text_done', turnId: '1', kbStatus: 'sources', sources: SOURCES })
    expect(store.answers.at(-1)?.status).toBe('complete')
    expect(store.speaking).toBe(true)

    channel.emit({ type: 'tts_done', turnId: '1', frames: 1 })
    await tick()
    expect(ports.context().played).toHaveLength(1)
    channel.emit({ type: 'response_done', turnId: '1', kbStatus: 'sources', sources: SOURCES })
    await tick()
    expect(store.answers.at(-1)?.status).toBe('complete')
  })
})

describe('MeetingAnswer timeline, late arrivals', () => {
  it('lets a provider finish notice on text_done stand without a second turn or a lost Stop', async () => {
    const { store, ports } = await live()
    const channel = ports.channel()

    channel.emit({ type: 'persona', turnId: '1', pigeonId: 'individualist' })
    channel.emit({ type: 'sources', turnId: '1', kbStatus: 'sources', sources: SOURCES })
    channel.emit({ type: 'text', turnId: '1', delta: 'Half a sentence' })
    channel.emit({ type: 'text_done', turnId: '1', stopReason: 'truncated', kbStatus: 'sources', sources: SOURCES })
    expect(store.answers.at(-1)?.stopReason).toBe('truncated')
    expect(store.answers.at(-1)?.status).toBe('complete')

    channel.emit({ type: 'response_done', turnId: '1', stopReason: 'truncated', kbStatus: 'sources', sources: SOURCES })
    expect(store.answers.at(-1)?.stopReason).toBe('truncated')

    // De volgende beurt begint schoon: geen stopreden, geen bronnen van hiervoor.
    channel.emit({ type: 'persona', turnId: '2', pigeonId: 'individualist' })
    channel.emit({ type: 'text', turnId: '2', delta: 'A whole thought.' })
    const next = store.answers.at(-1)
    expect(next?.stopReason).toBeUndefined()
    expect(next?.sources).toEqual([])
    expect(next?.status).toBe('streaming')
  })

  it('keeps a local Stop after the text was complete: the answer reads as stopped, not as finished', async () => {
    const { store, ports } = await live()
    const channel = ports.channel()

    channel.emit({ type: 'persona', turnId: '1', pigeonId: 'individualist' })
    channel.emit({ type: 'sources', turnId: '1', kbStatus: 'sources', sources: SOURCES })
    channel.emit({ type: 'text', turnId: '1', delta: 'Pigeons rest on ledges [1].' })
    channel.emit({ type: 'text_done', turnId: '1', kbStatus: 'sources', sources: SOURCES })
    expect(store.answers.at(-1)?.status).toBe('complete')

    await store.stop()
    channel.emit({ type: 'stopped', turnId: '1', reason: 'user' })
    expect(store.answers.at(-1)?.status).toBe('stopped')
    expect(store.speaking).toBe(false)

    // Een response_done dat ná de Stop nog binnenkomt, mag het antwoord niet
    // terugzetten op "af": de gebruiker heeft de duif afgekapt.
    channel.emit({ type: 'response_done', turnId: '1', kbStatus: 'sources', sources: SOURCES })
    expect(store.answers.at(-1)?.status).toBe('stopped')
  })

  it('ignores a sources or text_done frame that belongs to no open answer', async () => {
    const { store, ports } = await live()
    const channel = ports.channel()

    channel.emit({ type: 'sources', kbStatus: 'sources', sources: SOURCES })
    channel.emit({ type: 'text_done' })
    expect(store.answers).toHaveLength(0)
    expect(store.sources).toEqual([])
  })
})
