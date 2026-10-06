import type { Fetcher } from '../api'
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
import { BlockAssembler } from './mic'

// browserPorts is het enige bestand van de module dat echte browser-API's voor
// kanaal en geluid aanraakt (EventSource, AudioContext, getUserMedia, Audio).
// Al het gedrag eromheen — sluis, verzamelaar, speelvolgorde, stopknop — staat
// in bestanden die zonder browser te toetsen zijn.

// 8192 monsters is ~171 ms bij 48 kHz: ruim genoeg om een haperende hoofdthread
// te overleven, klein genoeg om de POST-cadans van 256 ms niet te vertragen.
const CAPTURE_BUFFER = 8192
// Een korte uitfade bij het stoppen; hard afbreken geeft een hoorbare klik.
const STOP_RAMP_SECONDS = 0.03
// Het eerste blok van een stroom (of het eerste na een hapering) wordt zo ver
// vooruit gepland: genoeg om de planning niet in het verleden te laten
// beginnen, te kort om te horen.
const STREAM_LEAD_SECONDS = 0.05

class BrowserAudioContext implements AudioContextLike {
  readonly raw: AudioContext

  constructor() {
    const Ctor =
      globalThis.AudioContext ??
      (globalThis as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
    if (!Ctor) throw new Error('audio_unavailable')
    this.raw = new Ctor()
  }

  get state(): AudioContextState {
    return this.raw.state
  }

  onStateChange(cb: (state: AudioContextState) => void): void {
    this.raw.onstatechange = () => cb(this.raw.state)
  }

  resume(): Promise<void> {
    return this.raw.resume()
  }

  close(): Promise<void> {
    this.raw.onstatechange = null
    return this.raw.close()
  }

  decode(bytes: ArrayBuffer): Promise<AudioBufferLike> {
    return this.raw.decodeAudioData(bytes)
  }

  // openStream plant elk PCM16-blok naadloos achter het vorige op de klok van
  // de context. Een blok dat na het geplande einde binnenkomt (het netwerk
  // haperde) begint een fractie ná nu: een korte stilte, geen klik, en de
  // planning loopt daarna weer aansluitend. De context herbemonstert zelf van
  // de sample rate van de stem naar die van de uitvoer.
  openStream(sampleRate: number): PcmStreamLike {
    const ctx = this.raw
    const gain = ctx.createGain()
    gain.connect(ctx.destination)
    const sources = new Set<AudioBufferSourceNode>()
    let nextAt = 0
    let stopped = false
    let ended: (() => void) | null = null
    const finishIfReady = () => {
      if (stopped || sources.size > 0 || !ended) return
      const done = ended
      ended = null
      stopped = true
      gain.disconnect()
      done()
    }
    return {
      write: (pcm16: Int16Array) => {
        if (stopped || pcm16.length === 0) return
        const buffer = ctx.createBuffer(1, pcm16.length, sampleRate)
        const channel = buffer.getChannelData(0)
        for (let i = 0; i < pcm16.length; i++) channel[i] = pcm16[i] / 0x8000
        const source = ctx.createBufferSource()
        source.buffer = buffer
        source.connect(gain)
        sources.add(source)
        source.onended = () => {
          sources.delete(source)
          source.buffer = null
          source.disconnect()
          if (stopped && sources.size === 0) gain.disconnect()
          else finishIfReady()
        }
        const at = Math.max(nextAt, ctx.currentTime + STREAM_LEAD_SECONDS)
        source.start(at)
        nextAt = at + buffer.duration
      },
      finish: (onEnded) => { ended = onEnded; finishIfReady() },
      stop: () => {
        if (stopped) return
        stopped = true
        ended = null
        const at = ctx.currentTime
        gain.gain.setValueAtTime(gain.gain.value, at)
        gain.gain.linearRampToValueAtTime(0, at + STOP_RAMP_SECONDS)
        for (const source of sources) source.stop(at + STOP_RAMP_SECONDS)
        if (sources.size === 0) gain.disconnect()
      },
    }
  }

  play(buffer: AudioBufferLike, onEnded: () => void): PlayHandle {
    const gain = this.raw.createGain()
    gain.connect(this.raw.destination)
    const source = this.raw.createBufferSource()
    source.buffer = buffer as AudioBuffer
    source.connect(gain)
    source.onended = () => {
      // De uiting is voorbij: bron en buffer los, zodat de audio niet in het
      // geheugen blijft hangen.
      source.buffer = null
      source.disconnect()
      gain.disconnect()
      onEnded()
    }
    source.start()
    return {
      stop: () => {
        const at = this.raw.currentTime
        gain.gain.setValueAtTime(gain.gain.value, at)
        gain.gain.linearRampToValueAtTime(0, at + STOP_RAMP_SECONDS)
        source.stop(at + STOP_RAMP_SECONDS)
      },
    }
  }
}

function openEvents(url: string): EventSourceLike {
  const source = new EventSource(url)
  return {
    onMessage(cb) {
      source.onmessage = (ev: MessageEvent<string>) => cb(ev.data)
    },
    onError(cb) {
      // De browser probeert een SSE-kanaal zelf opnieuw; alleen een definitief
      // gesloten kanaal is verbindingsverlies. Alles daarvóór is "herstellen".
      source.onerror = () => cb(source.readyState === EventSource.CLOSED)
    },
    close() {
      source.onmessage = null
      source.onerror = null
      source.close()
    },
  }
}

async function openMic(ctx: AudioContextLike, onBlock: (pcm16: Int16Array) => void): Promise<MicLike> {
  const devices = globalThis.navigator?.mediaDevices
  if (!devices?.getUserMedia) throw new Error('mic_unavailable')
  if (!(ctx instanceof BrowserAudioContext)) throw new Error('audio_unavailable')
  // §1.3: de browser doet de eerste echo-onderdrukking, zodat de duif haar
  // eigen stem niet terugkrijgt.
  const stream = await devices.getUserMedia({
    audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true, channelCount: 1 },
  })
  const raw = ctx.raw
  const assembler = new BlockAssembler(raw.sampleRate, onBlock)
  const source = raw.createMediaStreamSource(stream)
  const processor = raw.createScriptProcessor(CAPTURE_BUFFER, 1, 1)
  // Dempen is een poort bij de bron, niet alleen een vlag op het spoor: een
  // track.enabled=false levert stilte in plaats van níets, en die stilte zou
  // gewoon doorstromen. Hier stopt hij vóór de assembler.
  let enabled = true
  processor.onaudioprocess = (ev: AudioProcessingEvent) => {
    if (!enabled) return
    assembler.push(ev.inputBuffer.getChannelData(0))
  }
  source.connect(processor)
  processor.connect(raw.destination)
  let stopped = false
  return {
    stream,
    setEnabled(on: boolean) {
      enabled = on
      for (const track of stream.getAudioTracks()) track.enabled = on
      if (!on) assembler.reset()
    },
    stop() {
      if (stopped) return
      stopped = true
      enabled = false
      processor.onaudioprocess = null
      processor.disconnect()
      source.disconnect()
      assembler.reset()
      for (const track of stream.getTracks()) track.stop()
    },
  }
}

function playSound(url: string): SoundLike {
  const audio = new Audio(url)
  let stopped = false
  let failed = false
  let playing = false
  let started: (() => void) | null = null
  let error: (() => void) | null = null
  let ended: (() => void) | null = null
  const fail = () => {
    if (stopped || failed) return
    failed = true
    error?.()
    ended?.()
  }
  audio.onerror = fail
  audio.onended = () => { if (!stopped && !failed) ended?.() }
  void audio.play().then(() => { if (!stopped && !failed) { playing = true; started?.() } }).catch(fail)
  return {
    stop() {
      if (stopped) return
      stopped = true
      audio.pause()
      audio.removeAttribute('src')
      audio.onerror = null
      audio.onended = null
    },
    onEnded(cb) { ended = cb },
    onError(cb) { error = cb; if (failed) cb() },
    onStarted(cb) { started = cb; if (playing) cb() },
  }
}

export function browserPorts(fetcher: Fetcher): MeetingPorts {
  return {
    fetch: fetcher,
    openEvents,
    createAudioContext: () => new BrowserAudioContext(),
    openMic,
    playSound,
    schedule: (fn, ms) => {
      const id = setTimeout(fn, ms)
      return () => clearTimeout(id)
    },
    now: () => Date.now(),
  }
}
