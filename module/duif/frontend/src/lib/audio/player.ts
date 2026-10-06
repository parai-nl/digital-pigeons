import type { AudioBufferLike, AudioContextLike, PcmStreamLike, PlayHandle, SoundLike } from '../meeting/ports'

// Alles wat de vergadering hoorbaar maakt: de verzamelaar van één spreekbeurt,
// de speler die de beurten op volgorde afspeelt, en het bord met veldopnames
// van de non-verbale duif. Browser-API's komen hier alleen via de ports binnen.

// Ruim boven een normale spreekbeurt (128 kbps ≈ 16 KB/s, dus ruim acht
// minuten), en laag genoeg dat een op hol geslagen stroom het geheugen niet
// opeet. Wat erboven komt wordt weggegooid en gemeld, nooit stil bewaard.
export const MAX_TTS_BYTES = 8 * 1024 * 1024

export type TtsResult = { ok: true; bytes: Uint8Array } | { ok: false; reason: 'empty' | 'gap' | 'overflow' }
export type TtsVerdict = { ok: true } | { ok: false; reason: 'empty' | 'gap' | 'overflow' }

function decodeBase64(value: string): Uint8Array | null {
  try {
    const binary = globalThis.atob(value)
    const out = new Uint8Array(binary.length)
    for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i)
    return out
  } catch {
    return null
  }
}

// TtsCollector houdt de frames van één beurt vast tot de beurt af is.
//
// Waarom niet frame voor frame afspelen: de server streamt MP3 uit de
// synthesizer, en zo'n chunk is geen zelfstandig MP3-bestand. Eén uiting is
// één decodeerpad. De frames zijn onderweg-data: ze staan in een rij die de
// stopknop leeggooit en gaan nergens heen.
//
// `seq` (1-gebaseerd) en `frames` op tts_done zijn het vangnet: mist er een
// frame, dan wordt er niets afgespeeld en meldt het scherm dat eerlijk — beter
// dan een halve MP3 die als geknetter uit de zaalspeakers komt.
export class TtsCollector {
  private frames: Uint8Array[] = []
  private bytes = 0
  private arrived = 0
  private lastSeq = 0
  private broken: 'gap' | 'overflow' | null = null

  get count(): number {
    return this.arrived
  }

  add(audio: string | undefined, seq?: number): void {
    if (!audio) return
    this.arrived++
    if (typeof seq === 'number' && seq > 0) {
      if (seq !== this.lastSeq + 1) this.broken ??= 'gap'
      this.lastSeq = seq
    } else {
      this.lastSeq++
    }
    if (this.broken) return
    const frame = decodeBase64(audio)
    if (frame === null) {
      this.broken = 'gap'
      this.frames = []
      return
    }
    this.bytes += frame.length
    if (this.bytes > MAX_TTS_BYTES) {
      this.broken = 'overflow'
      this.frames = []
      return
    }
    this.frames.push(frame)
  }

  // finish sluit de beurt af: alle frames worden één aaneengesloten stroom.
  finish(expected?: number): TtsResult {
    const broken = this.broken
    const arrived = this.arrived
    const frames = this.frames
    this.reset()
    if (broken) return { ok: false, reason: broken }
    if (arrived === 0) return { ok: false, reason: 'empty' }
    if (typeof expected === 'number' && expected > 0 && expected !== arrived) {
      return { ok: false, reason: 'gap' }
    }
    let length = 0
    for (const f of frames) length += f.length
    const bytes = new Uint8Array(length)
    let at = 0
    for (const f of frames) {
      bytes.set(f, at)
      at += f.length
    }
    return { ok: true, bytes }
  }

  reset(): void {
    this.frames = []
    this.bytes = 0
    this.arrived = 0
    this.lastSeq = 0
    this.broken = null
  }
}

// PcmTurn is de streamende tegenhanger van de verzamelaar: een PCM16-frame
// wordt meteen omgezet in monsters en afgespeeld, dus hier wordt niets
// vastgehouden — alleen de nummering, het bytebudget en een halve monster die
// op de framegrens over kan blijven. Een gat in de nummering stopt de beurt
// (wat al klonk is niet terug te halen) en wordt op tts_done gemeld.
export class PcmTurn {
  private arrived = 0
  private lastSeq = 0
  private bytes = 0
  private carry: number | null = null
  private broken: 'gap' | 'overflow' | null = null

  // add levert de monsters van één frame, of null als er niets te spelen is.
  add(audio: string | undefined, seq?: number): Int16Array | null {
    if (!audio) return null
    this.arrived++
    if (typeof seq === 'number' && seq > 0) {
      if (seq !== this.lastSeq + 1) this.broken ??= 'gap'
      this.lastSeq = seq
    } else {
      this.lastSeq++
    }
    if (this.broken) return null
    const frame = decodeBase64(audio)
    if (frame === null) {
      this.broken = 'gap'
      return null
    }
    this.bytes += frame.length
    if (this.bytes > MAX_TTS_BYTES) {
      this.broken = 'overflow'
      return null
    }
    return this.samples(frame)
  }

  // samples leest little-endian PCM16; een losse byte aan het eind wacht op
  // het volgende frame.
  private samples(frame: Uint8Array): Int16Array | null {
    let bytes = frame
    if (this.carry !== null) {
      bytes = new Uint8Array(frame.length + 1)
      bytes[0] = this.carry
      bytes.set(frame, 1)
      this.carry = null
    }
    const count = bytes.length >> 1
    if (bytes.length & 1) this.carry = bytes[bytes.length - 1]
    if (count === 0) return null
    const out = new Int16Array(count)
    for (let i = 0; i < count; i++) {
      const v = bytes[2 * i] | (bytes[2 * i + 1] << 8)
      out[i] = v & 0x8000 ? v - 0x10000 : v
    }
    return out
  }

  finish(expected?: number): TtsVerdict {
    const broken = this.broken
    const arrived = this.arrived
    this.reset()
    if (broken) return { ok: false, reason: broken }
    if (arrived === 0) return { ok: false, reason: 'empty' }
    if (typeof expected === 'number' && expected > 0 && expected !== arrived) {
      return { ok: false, reason: 'gap' }
    }
    return { ok: true }
  }

  reset(): void {
    this.arrived = 0
    this.lastSeq = 0
    this.bytes = 0
    this.carry = null
    this.broken = null
  }
}

// TtsPlayer speelt de beurten op volgorde af. De generatieteller is de
// stopknop: hij loopt op bij elke silence(), zodat een decodering die nog
// onderweg was niets meer start en de onended van een gestopte bron de rij niet
// opnieuw aanzwengelt.

// Eén plek in de speelrij. De plek wordt gereserveerd vóórdat er gedecodeerd
// wordt; de decodering vult hem later. Zonder die reservering bepaalt de
// decodeersnelheid de volgorde, en dan spreekt de duif haar tweede zin eerst.
export type PlaybackState = 'started' | 'ended' | 'aborted' | 'failed'

interface Slot {
  buffer: AudioBufferLike | null
  failed: boolean
}

export class TtsPlayer {
  private generation = 0
  private active = false
  private inputFinished = false
  private started = false
  private queue: Slot[] = []
  private current: PlayHandle | null = null
  // De streamende kant: één open PCM-stroom, aansluitend over beurten heen,
  // zodat een volgende beurt vanzelf achter de vorige klinkt.
  private stream: { handle: PcmStreamLike; sampleRate: number } | null = null

  constructor(
    private readonly context: () => AudioContextLike | null,
    private readonly onDecodeFailed: () => void,
    private readonly onPlayback: (state: PlaybackState) => void = () => {},
  ) {}

  async enqueue(bytes: Uint8Array): Promise<void> {
    const generation = this.generation
    const ctx = this.context()
    this.begin()
    if (!ctx || ctx.state !== 'running') { this.fail(); return }
    const slot: Slot = { buffer: null, failed: false }
    this.queue.push(slot)
    try {
      // decodeAudioData detacheert zijn invoer; een verse kopie houdt de
      // verzamelaar bruikbaar en de detach onschadelijk.
      const copy = new Uint8Array(bytes)
      const buffer = await ctx.decode(copy.buffer)
      if (generation !== this.generation) return
      slot.buffer = buffer
    } catch {
      if (generation !== this.generation) return
      slot.failed = true
      this.fail()
    }
    this.playNext()
  }

  private playNext(): void {
    if (this.current) return
    const ctx = this.context()
    if (!ctx) return
    // Een mislukte beurt houdt de rij niet vast; een beurt die nog decodeert
    // wél — die was eerder aan de beurt.
    while (this.queue.length > 0 && this.queue[0].failed) this.queue.shift()
    const head = this.queue[0]
    if (!head?.buffer) { this.maybeEnded(); return }
    this.queue.shift()
    const generation = this.generation
    try {
      this.current = ctx.play(head.buffer, () => {
        if (generation !== this.generation) return
        this.current = null
        this.playNext()
      })
      this.markStarted()
    } catch {
      this.fail()
      this.playNext()
    }
  }

  // write zet een PCM16-blok direct in de stroom; het klinkt zodra het aan de
  // beurt is. Een andere sample rate (een andere stem) opent een nieuwe stroom.
  write(pcm16: Int16Array, sampleRate: number): void {
    if (pcm16.length === 0) return
    const ctx = this.context()
    this.begin()
    if (!ctx || ctx.state !== 'running') { this.fail(); return }
    if (this.stream && this.stream.sampleRate !== sampleRate) {
      this.stream.handle.stop()
      this.stream = null
    }
    try {
      this.stream ??= { handle: ctx.openStream(sampleRate), sampleRate }
      this.stream.handle.write(pcm16)
      this.markStarted()
    } catch { this.fail() }
  }

  private begin(): void {
    if (this.active) return
    this.active = true
    this.inputFinished = false
    this.started = false
  }

  private markStarted(): void {
    if (!this.active || this.started) return
    this.started = true
    this.onPlayback('started')
  }

  private terminal(state: Exclude<PlaybackState, 'started'>): void {
    if (!this.active) return
    this.active = false
    this.onPlayback(state)
  }

  private fail(): void {
    this.onDecodeFailed()
    this.terminal('failed')
  }

  // finish marks the end of incoming audio, not the end of playback. MP3
  // waits for its onended; PCM waits for the port's real scheduled drain.
  finish(): void {
    this.inputFinished = true
    if (this.stream) {
      const generation = this.generation
      const stream = this.stream
      const handle = stream.handle as PcmStreamLike & { finish?: (onEnded: () => void) => void }
      if (!handle.finish) { this.fail(); stream.handle.stop(); this.stream = null; return }
      handle.finish(() => {
        if (generation !== this.generation || this.stream !== stream) return
        this.stream = null
        this.maybeEnded()
      })
    }
    this.maybeEnded()
  }

  private maybeEnded(): void {
    if (this.inputFinished && !this.current && !this.stream && this.queue.length === 0) this.terminal('ended')
  }

  // silence is de lokale helft van de stopknop: meteen stil, zonder op de
  // server te wachten — de speelrij, de lopende uiting én de stroom.
  silence(): void {
    this.generation++
    this.terminal('aborted')
    this.queue = []
    const handle = this.current
    this.current = null
    handle?.stop()
    this.stream?.handle.stop()
    this.stream = null
  }
}

// SoundBoard houdt de klinkende veldopnames vast zolang ze klinken: zonder die
// verwijzing had de stopknop geen greep op ze en zou de stopknop voor de
// non-verbale duif een loze belofte zijn. Zodra een opname uit is laat het bord haar los —
// het is een greep op wat klinkt, geen verzameling van wat geklonken heeft.
export class SoundBoard {
  private players: SoundLike[] = []

  get count(): number {
    return this.players.length
  }

  play(handle: SoundLike): void {
    this.players.push(handle)
    handle.onEnded(() => {
      this.players = this.players.filter((p) => p !== handle)
    })
  }

  silence(): void {
    const players = this.players
    this.players = []
    for (const p of players) p.stop()
  }
}
