import type { Fetcher } from '../api'

// Ports van de vergadermodus: de enige plek waar browser-API's de store
// binnenkomen. lib/audio/browser.ts implementeert ze met echte browser-API's;
// tests injecteren de fakes uit lib/testing/meeting.ts. De store zelf noemt
// nergens een browser-API, zodat de vergaderlogica in jsdom te toetsen is
// zonder één audio-API te stubben.

// EventSourceLike is het sessiekanaal, teruggebracht tot wat de store nodig
// heeft. onError krijgt `terminal`: de browser probeert een SSE-kanaal zelf
// opnieuw, dus alleen een definitief gesloten kanaal is verbindingsverlies.
export interface EventSourceLike {
  onMessage(cb: (data: string) => void): void
  onError(cb: (terminal: boolean) => void): void
  close(): void
}

// AudioBufferLike is een gedecodeerde uiting. De store kijkt er nooit in; hij
// geeft hem alleen door aan play() en laat hem daarna los. `duration` staat er
// als vorm, niet als gebruik: een lege interface zou elke waarde slikken.
export interface AudioBufferLike {
  readonly duration: number
}

// PlayHandle is één klinkende bron. stop() dempt met een korte ramp (30 ms),
// zodat afbreken geen klik geeft.
export interface PlayHandle {
  stop(): void
}

// AudioContextLike is de gedeelde audiocontext van de mount: één exemplaar
// voor microfoon én speler (Chrome staat er zes per tab toe).
export interface AudioContextLike {
  readonly state: AudioContextState
  resume(): Promise<void>
  close(): Promise<void>
  // onStateChange meldt élke standwissel, ook die de gebruiker buiten ons om
  // veroorzaakt (een tab die naar de achtergrond gaat, een browser die geluid
  // alsnog toestaat). De blokkademelding is daarvan een afgeleide in plaats van
  // een momentopname bij het openen.
  onStateChange(cb: (state: AudioContextState) => void): void
  // decode ontvangt een ArrayBuffer die daarna gedetacheerd is: hergebruik hem
  // niet. Eén beurt is één decodeerpad (losse MP3-chunks decoderen niet).
  decode(bytes: ArrayBuffer): Promise<AudioBufferLike>
  play(buffer: AudioBufferLike, onEnded: () => void): PlayHandle
  // openStream is de streamende speler: PCM16-blokken gaan er in de volgorde
  // van aankomst in en klinken aansluitend, ook als ze in stukjes binnenkomen.
  // Eén stroom per sample rate; stop() dempt en gooit weg wat nog gepland stond.
  openStream(sampleRate: number): PcmStreamLike
}

// PcmStreamLike is één aansluitende PCM-stroom. write plant het blok direct
// achter het vorige; wat te laat komt (een hapering in het netwerk) begint
// gewoon zo snel mogelijk, zonder klik.
export interface PcmStreamLike {
  write(pcm16: Int16Array): void
  finish?(onEnded: () => void): void
  stop(): void
}

// MicLike is de geopende microfoon. setEnabled(false) is dempen (het spoor
// blijft open, er gaat niets meer de lijn op); stop() sluit hem definitief.
export interface MicLike {
  setEnabled(on: boolean): void
  stop(): void
  readonly stream: MediaStream | null
}

// SoundLike is één veldopname van de non-verbale duif. onEnded laat het
// geluidsbord de uitgeklonken opname loslaten in plaats van hem te verzamelen.
export interface SoundLike {
  stop(): void
  onEnded(cb: () => void): void
  onError?(cb: () => void): void
  onStarted?(cb: () => void): void
}

export interface MeetingPorts {
  fetch: Fetcher
  openEvents(url: string): EventSourceLike
  createAudioContext(): AudioContextLike
  // openMic levert blokken van BLOCK_SAMPLES monsters PCM16 op 16 kHz; de
  // herbemonstering zit in de port, niet in de store.
  openMic(ctx: AudioContextLike, onBlock: (pcm16: Int16Array) => void): Promise<MicLike>
  playSound(url: string): SoundLike
  // schedule plant werk in de toekomst en levert de annulering; zo houdt de
  // store geen browsertimer vast en is uitstel in tests een gewone regel.
  schedule(fn: () => void, ms: number): () => void
  // now is de klok van de browser in milliseconden. De aftelling van "Ben je
  // er nog?" rekent ermee in plaats van met een tikkende teller: een
  // achtergrondtabblad remt timers af, maar de klok loopt door.
  now(): number
}
