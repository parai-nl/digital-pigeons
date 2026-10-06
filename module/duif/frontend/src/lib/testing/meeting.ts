// Fakes voor de vergadertests: één kanaal, één audiocontext, één microfoon en
// een geluidsbord, allemaal met de hand te besturen. Er wordt niets globaal
// gepatcht — de store krijgt ze via de ports, precies zoals browser.ts de
// echte API's aanlevert.
import type { Fetcher } from '../api'
import { BLOCK_SAMPLES } from '../audio/pcm'
import { UiLocale } from '../i18n.svelte'
import { MeetingStore } from '../meeting/meetingStore.svelte'
import type {
  AudioBufferLike,
  AudioContextLike,
  EventSourceLike,
  MeetingPorts,
  MicLike,
  PcmStreamLike,
  PlayHandle,
  SoundLike,
} from '../meeting/ports'
import type { Services } from '../services.svelte'
import { testServices } from './chat'
import { jsonResponse, type Route } from './fetch'

export class FakeEventSource implements EventSourceLike {
  closed = false
  private message: ((data: string) => void) | null = null
  private error: ((terminal: boolean) => void) | null = null

  constructor(readonly url: string) {}

  onMessage(cb: (data: string) => void): void {
    this.message = cb
  }

  onError(cb: (terminal: boolean) => void): void {
    this.error = cb
  }

  close(): void {
    this.closed = true
  }

  // emit levert één servergebeurtenis af, precies zoals de echte stroom dat doet.
  emit(event: Record<string, unknown>): void {
    this.message?.(JSON.stringify(event))
  }

  emitRaw(data: string): void {
    this.message?.(data)
  }

  fail(terminal = false): void {
    this.error?.(terminal)
  }
}

export class FakeAudioBuffer implements AudioBufferLike {
  constructor(
    readonly duration = 1,
    // byteLength maakt een gedecodeerde beurt herkenbaar, zodat een test de
    // speelvolgorde kan aflezen.
    readonly byteLength = 0,
  ) {}
}

export class FakePlayHandle implements PlayHandle {
  stopped = false

  constructor(readonly buffer: AudioBufferLike) {}

  // bytes zegt wélke beurt hier klinkt: de fake decodering geeft de lengte van
  // haar invoer mee, dus de speelvolgorde is af te lezen zonder cast.
  get bytes(): number {
    return this.buffer instanceof FakeAudioBuffer ? this.buffer.byteLength : 0
  }

  stop(): void {
    this.stopped = true
  }
}

// FakePcmStream onthoudt de blokken (als aantallen monsters) en of hij
// gestopt is: genoeg om af te lezen dat de store direct speelt en op tijd stopt.
export class FakePcmStream implements PcmStreamLike {
  private ending: (() => void) | null = null
  finish(onEnded: () => void): void { this.ending = onEnded }
  finishPlayback(): void { const ended = this.ending; this.ending = null; if (!this.stopped) ended?.() }
  written: number[] = []
  stopped = false

  constructor(readonly sampleRate: number) {}

  write(pcm16: Int16Array): void {
    if (this.stopped) return
    this.written.push(pcm16.length)
  }

  stop(): void {
    this.stopped = true
  }
}

export class FakeAudioContext implements AudioContextLike {
  state: AudioContextState = 'running'
  closed = false
  resumes = 0
  decoded: number[] = []
  played: FakePlayHandle[] = []
  streams: FakePcmStream[] = []
  // resumesTo stuurt wat resume() met de state doet; 'suspended' bootst een
  // browser na die geluid tegenhoudt.
  resumesTo: AudioContextState = 'running'
  holdDecode = false
  failDecode = false

  private endings: (() => void)[] = []
  private pending: { resolve: () => void; reject: (err: Error) => void }[] = []
  private stateChange: ((state: AudioContextState) => void) | null = null

  onStateChange(cb: (state: AudioContextState) => void): void {
    this.stateChange = cb
  }

  // setState is de stand die de browser buiten ons om verandert.
  setState(state: AudioContextState): void {
    this.state = state
    this.stateChange?.(state)
  }

  async resume(): Promise<void> {
    this.resumes++
    this.state = this.resumesTo
  }

  async close(): Promise<void> {
    this.closed = true
    this.state = 'closed'
  }

  decode(bytes: ArrayBuffer): Promise<AudioBufferLike> {
    this.decoded.push(bytes.byteLength)
    const buffer = new FakeAudioBuffer(1, bytes.byteLength)
    if (this.failDecode) return Promise.reject(new Error('decode failed'))
    if (!this.holdDecode) return Promise.resolve(buffer)
    return new Promise<AudioBufferLike>((resolve, reject) => {
      this.pending.push({ resolve: () => resolve(buffer), reject })
    })
  }

  // releaseDecode laat één vastgehouden decodering los; zo is "stoppen tijdens
  // het decoderen" — en "de tweede beurt is eerder klaar dan de eerste" — een
  // gewone testregel.
  releaseDecode(index = 0): void {
    const held = this.pending[index]
    if (!held) return
    this.pending.splice(index, 1)
    held.resolve()
  }

  // failDecodePending laat één vastgehouden decodering alsnog mislukken — een
  // kapotte beurt tussen twee goede in.
  failDecodePending(index = 0): void {
    const held = this.pending[index]
    if (!held) return
    this.pending.splice(index, 1)
    held.reject(new Error('decode failed'))
  }

  get decodesPending(): number {
    return this.pending.length
  }

  play(buffer: AudioBufferLike, onEnded: () => void): PlayHandle {
    const handle = new FakePlayHandle(buffer)
    this.played.push(handle)
    this.endings.push(onEnded)
    return handle
  }

  openStream(sampleRate: number): PcmStreamLike {
    const stream = new FakePcmStream(sampleRate)
    this.streams.push(stream)
    return stream
  }

  // stream is de lopende (laatst geopende) PCM-stroom.
  get stream(): FakePcmStream | undefined {
    return this.streams[this.streams.length - 1]
  }

  // finish laat een bron uitklinken (de onended van de echte bron).
  finish(index = this.played.length - 1): void {
    this.endings[index]?.()
  }
}

export class FakeMic implements MicLike {
  enabled = true
  stopped = false
  stream: MediaStream | null = null

  constructor(private readonly onBlock: (pcm16: Int16Array) => void) {}

  setEnabled(on: boolean): void {
    this.enabled = on
  }

  stop(): void {
    this.stopped = true
  }

  // push levert één blok van 4096 monsters af, zoals de capture dat doet — óók
  // als de microfoon gedempt staat. De echte port houdt zo'n blok bij de bron
  // tegen; door dat hier bewust níet te doen, toetst de test de poort in de
  // store in plaats van de fake.
  push(block?: Int16Array): void {
    this.onBlock(block ?? new Int16Array(BLOCK_SAMPLES))
  }
}

export class FakeSound implements SoundLike {
  onStarted(cb: () => void): void { cb() }
  private error: (() => void) | null = null
  onError(cb: () => void): void { this.error = cb }
  fail(): void { this.error?.() }
  stopped = false
  private ended: (() => void) | null = null

  constructor(readonly url: string) {}

  stop(): void {
    this.stopped = true
  }

  onEnded(cb: () => void): void {
    this.ended = cb
  }

  // end laat de opname uitklinken, zoals de onended van het echte Audio-element.
  end(): void {
    this.ended?.()
  }
}

export interface FakePortsOptions {
  micRefused?: boolean
  noAudioContext?: boolean
  suspended?: boolean
}

export interface FakeMeetingPorts extends MeetingPorts {
  channels: FakeEventSource[]
  contexts: FakeAudioContext[]
  mics: FakeMic[]
  soundPlayers: FakeSound[]
  channel(): FakeEventSource
  context(): FakeAudioContext
  mic(): FakeMic
  // advance draait de klok van de geplande taken (debounce, rustpauze) vooruit.
  advance(ms: number): void
  // jump verzet alleen de klok, zonder de taken te draaien: een
  // achtergrondtabblad waarin de browser de timers heeft afgeremd.
  jump(ms: number): void
}

export function fakePorts(fetcher: Fetcher, options: FakePortsOptions = {}): FakeMeetingPorts {
  const channels: FakeEventSource[] = []
  const contexts: FakeAudioContext[] = []
  const mics: FakeMic[] = []
  const soundPlayers: FakeSound[] = []
  let clock = 0
  let timers: { at: number; fn: () => void }[] = []
  const last = <T>(list: T[], what: string): T => {
    const item = list[list.length - 1]
    if (!item) throw new Error('geen ' + what + ' geopend')
    return item
  }
  return {
    fetch: fetcher,
    channels,
    contexts,
    mics,
    soundPlayers,
    channel: () => last(channels, 'kanaal'),
    context: () => last(contexts, 'audiocontext'),
    mic: () => last(mics, 'microfoon'),
    jump(ms: number) {
      clock += ms
    },
    advance(ms: number) {
      clock += ms
      const due = timers.filter((t) => t.at <= clock)
      timers = timers.filter((t) => t.at > clock)
      for (const t of due) t.fn()
    },
    // De klok van de geplande taken is ook de klok van de store: één sprong
    // van advance() is voor de aftelling precies zoveel verstreken tijd.
    now: () => clock,
    schedule(fn: () => void, ms: number) {
      const timer = { at: clock + ms, fn }
      timers.push(timer)
      return () => {
        timers = timers.filter((t) => t !== timer)
      }
    },
    openEvents(url) {
      const channel = new FakeEventSource(url)
      channels.push(channel)
      return channel
    },
    createAudioContext() {
      if (options.noAudioContext) throw new Error('audio_unavailable')
      const ctx = new FakeAudioContext()
      if (options.suspended) {
        ctx.state = 'suspended'
        ctx.resumesTo = 'suspended'
      }
      contexts.push(ctx)
      return ctx
    },
    async openMic(_ctx, onBlock) {
      if (options.micRefused) throw new Error('NotAllowedError')
      const mic = new FakeMic(onBlock)
      mics.push(mic)
      return mic
    },
    playSound(url) {
      const sound = new FakeSound(url)
      soundPlayers.push(sound)
      return sound
    },
  }
}

// meetingServices is dezelfde mount-context als elders, met de vergaderstore en
// de taal van het harnas erin: één bouwer voor alle drie de slices, zodat de
// context in een vergadertest niets anders belooft dan in een chattest.
export function meetingServices(store: MeetingStore, locale: UiLocale, fetcher: Fetcher): Services {
  return testServices(fetcher, { ctx: { mode: 'standalone' }, locale, meeting: store })
}

// meetingHarness zet een store met fakes klaar; de tests hoeven alleen nog de
// routes te scripten.
export function meetingHarness(fetcher: Fetcher, options: FakePortsOptions = {}) {
  const locale = new UiLocale()
  const ports = fakePorts(fetcher, options)
  const store = new MeetingStore(ports, locale)
  return { locale, ports, store, services: () => meetingServices(store, locale, fetcher) }
}

// De duivenlijst zoals de server hem levert: één duif buiten de vergadering,
// één pratende vergaderduif en de non-verbale. De namen zijn die uit
// config/personas.yaml: dat is de enige naambron, dus een
// fixture met eigen namen zou de schermen op een naam toetsen die nergens
// bestaat.
export const DUIVEN_FIXTURE = {
  duiven: [
    { id: 'biological', name: { en: 'Knowledge Partner', nl: 'Kennispartner' }, voiceReady: true, chat: true, meeting: false, topics: [], autonomousDefault: false },
    { id: 'individualist', name: { en: 'Stakeholder', nl: 'Stakeholder' }, voiceReady: true, chat: true, meeting: true, topics: [], autonomousDefault: false },
    // De signalerende duif doet standaard zelfstandig mee; de fixture
    // volgt daarin de echte configuratie.
    { id: 'nonverbal', name: { en: 'Signaling Pigeon', nl: 'Signalerende duif' }, voiceReady: false, chat: false, meeting: true, topics: [], autonomousDefault: true },
  ],
  gedrag: {
    // De gedeelde terugval voor een duif die zelf geen stand noemt.
    interventieZelfstandig: false,
    interventieThrottleSeconden: 120,
    detectieGretigheid: 'normal',
    taalDefault: 'en',
  },
}

export const OPEN_SESSION_ROUTE = /\/meeting\/sessions$/

// meetingRoutes is de normale server achter een vergadering; tests zetten er
// hun eigen route vóór om één antwoord te veranderen.
export function meetingRoutes(): Route[] {
  return [
    { method: 'GET', match: '/v1/duiven', respond: () => jsonResponse(DUIVEN_FIXTURE) },
    {
      method: 'POST',
      match: OPEN_SESSION_ROUTE,
      respond: ({ body }) => {
        const req = body as Record<string, unknown>
        return jsonResponse(
          {
            sessionId: 'sessie-1',
            pigeonId: req.pigeonId,
            lang: req.lang,
            listening: 'listening',
            voice: 'ready',
            settings: {
              autonomous: req.autonomous,
              eagerness: req.eagerness,
              cooldownSeconds: req.cooldownSeconds,
            },
          },
          201,
        )
      },
    },
    { method: 'POST', match: '/resume', respond: ({ body }) => jsonResponse({ autonomyPaused: false, settings: { turnId: (body as { turnId: string }).turnId } }, 202) },
    { method: 'POST', match: '/audio', respond: () => jsonResponse({ streamed: 1, dropped: 0, listening: 'listening' }, 202) },
    { method: 'POST', match: '/meeting/sessions/', respond: () => jsonResponse({ accepted: true }, 202) },
    { method: 'DELETE', match: '/meeting/sessions/', respond: () => new Response(null, { status: 204 }) },
  ]
}

// De signaleerregel in één regex: geen bandnaam, geen emotie en geen duiding van wat een
// signaal "betekent", nergens waar de zaal het kan lezen of voorgelezen krijgt.
// Twee tests lezen hem — de scan over de deelnemersweergave en die over de
// bronvermelding onder het vergaderbeeld — en die twee horen niet uit elkaar te
// lopen, dus staat hij hier en niet twee keer.
export const SIGNAL_EXPLANATION =
  /\b(calm|insistent|alarm|arous\w*|excit\w*|agitat\w*|kalm|rustig|nadrukkelijk|opgewonden|opwinding|emotie|emotion|angst|fear|protest|boos|blij)\b/i

// readEverything leest alles wat de zaal van het scherm kan halen: de tekst
// zelf plus de attributen die een schermlezer voorleest. Een dichtgeklapt
// <details> telt mee — de bronvermelding staat er open of dicht evengoed in.
export function readEverything(into: string[]): void {
  into.push(document.body.textContent ?? '')
  for (const node of document.body.querySelectorAll('*')) {
    for (const attribute of node.attributes) {
      if (/^(aria-|title$|alt$|placeholder$)/.test(attribute.name)) into.push(attribute.value)
    }
  }
}
