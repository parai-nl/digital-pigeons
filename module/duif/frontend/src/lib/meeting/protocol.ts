// Het vergaderprotocol zoals de Go-server het spreekt (duif/meeting.go).
// Dit bestand is de enige plek met de letterlijke wire-namen; de Go-test
// TestMeetingFrontend_SpeaksTheServerProtocol scant het en faalt zodra een
// serverconstante hier ontbreekt.
import type { Bilingual } from '../i18n.svelte'

export const ROUTES = {
  sessions: '/meeting/sessions',
  events: '/events',
  audio: '/audio',
  transcript: '/transcript',
  respond: '/respond',
  intervene: '/intervene',
  settings: '/settings',
  stop: '/stop',
  resume: '/resume',
  playback: '/playback',
  detectorRetry: '/detector/retry',
  candidateSpeak: '/candidate/speak',
  presence: '/presence',
} as const

export const EVENTS = {
  state: 'state',
  transcript: 'transcript',
  persona: 'persona',
  text: 'text',
  // De bronnen komen vóór de eerste tekstdelta binnen, zoals in de chat: pas
  // dan kan een "[1]" meteen als klikbare verwijzing getekend worden.
  sources: 'sources',
  // Het geschreven antwoord is af, de stem kan nog doorspreken. Hierop
  // verdwijnt de typcursor; response_done sluit pas ná de audio de beurt.
  textDone: 'text_done',
  ttsAudio: 'tts_audio',
  ttsDone: 'tts_done',
  responseDone: 'response_done',
  playbackState: 'playback_state',
  busy: 'busy',
  stopped: 'stopped',
  error: 'error',
  presenceCheck: 'presence_check',
  sessionClosed: 'session_closed',
  listeningHeartbeat: 'listening_heartbeat',
  candidate: 'candidate',
  arousal: 'arousal',
  intervention: 'intervention',
  detectorStatus: 'detector_status',
  sound: 'sound',
  settings: 'settings',
} as const
export type EventName = (typeof EVENTS)[keyof typeof EVENTS]

export const LISTEN = { listening: 'listening', reconnecting: 'reconnecting', off: 'off' } as const
export type ListenState = (typeof LISTEN)[keyof typeof LISTEN]

export const VOICE = { ready: 'ready', off: 'off' } as const

// Wat er in een tts_audio-frame zit (duif/meeting_turn.go AudioFormat). PCM16
// speelt de store blok voor blok zodra het binnenkomt; MP3 pas als de beurt
// compleet is, want losse MP3-chunks zijn geen bestand.
export const AUDIO_FORMATS = { mp3: 'mp3', pcm16: 'pcm16' } as const
export type VoiceState = (typeof VOICE)[keyof typeof VOICE]

// De foutcode op tts_done als de stem van een beurt nooit begon: openen faalde
// of duurde te lang (duif/meeting_speaker.go ttsUnavailable). Elke andere code
// betekent dat er geluid klonk en dat het halverwege stokte.
export const TTS_UNAVAILABLE = 'tts_unavailable'

// De foutcode van een beurt die de beurtklok van de server haalde
// (duif/meeting_turn.go emitTurnTimeout). Geen afspeelfout: de client pauzeert
// er niet op.
export const TURN_TIMEOUT = 'turn_timeout'

export const CLOSE_REASONS = {
  inactivity: 'inactivity',
  endedByUser: 'ended_by_user',
  maxDuration: 'max_duration',
  serverShutdown: 'server_shutdown',
  // De gebruiker liet deze vergadering zelf wijken: de uitweg bij "er is al
  // een vergadering open" en de duifwissel.
  replacedByUser: 'replaced_by_user',
  // Er zat te lang geen enkele client meer op het sessiekanaal.
  clientGone: 'client_gone',
} as const

// De twee foutcodes van een volle tafel. Alleen bij het eerste geval heeft de
// gebruiker een uitweg: het is zijn eigen vergadering die de plaats bezet.
export const CAPACITY_SELF = 'meeting_capacity_self'
export const CAPACITY_FULL = 'meeting_capacity'

export const EAGERNESS = ['reserved', 'normal', 'eager'] as const
export type Eagerness = (typeof EAGERNESS)[number]

// De bovengrens die de server accepteert voor de interventie-cooldown; de
// schuif en de store delen dit ene getal.
export const MAX_COOLDOWN_SECONDS = 3600

// Wachters op de opsommingen: wat de server stuurt is `unknown` tot het bewezen
// is. Zonder deze twee zou een onbekende stand als geldige waarde de UI in
// glippen en de luisterindicator laten liegen.
export function isListenState(value: unknown): value is ListenState {
  return value === LISTEN.listening || value === LISTEN.reconnecting || value === LISTEN.off
}

export function isVoiceState(value: unknown): value is VoiceState {
  return value === VOICE.ready || value === VOICE.off
}

export function isEagerness(value: unknown): value is Eagerness {
  return typeof value === 'string' && (EAGERNESS as readonly string[]).includes(value)
}

export interface TopicRef {
  id: string
  label?: Bilingual
}

// De klaarliggende bijdrage van de duif. Er staat geen letter transcript
// en geen letter bijdrage in: de vergadering ziet dát de duif iets heeft en
// waarover, niet wat. `reason` is een vaste code van de server.
export const CANDIDATE_REASONS = { addressed: 'addressed', topic: 'topic', signal: 'signal' } as const
export type CandidateReason = (typeof CANDIDATE_REASONS)[keyof typeof CANDIDATE_REASONS]

export interface CandidateDTO {
  id: string
  topic?: TopicRef
  score?: number
  reason?: string
  addressed?: boolean
  sources?: number
}

export interface MeetingCandidate {
  id: string
  topic: TopicRef | null
  score: number
  reason: CandidateReason
  addressed: boolean
  sources: number
}

function isCandidateReason(value: unknown): value is CandidateReason {
  return value === CANDIDATE_REASONS.addressed || value === CANDIDATE_REASONS.topic || value === CANDIDATE_REASONS.signal
}

// parseCandidate vertrouwt niets: zonder id of met een onbekende reden is er
// geen kandidaat om te tonen — liever geen blok dan een blok dat liegt.
export function parseCandidate(value: unknown): MeetingCandidate | null {
  if (typeof value !== 'object' || value === null) return null
  const raw = value as CandidateDTO
  if (typeof raw.id !== 'string' || raw.id === '') return null
  if (!isCandidateReason(raw.reason)) return null
  const topic = typeof raw.topic?.id === 'string' && raw.topic.id !== '' ? raw.topic : null
  return {
    id: raw.id,
    topic,
    score: typeof raw.score === 'number' && Number.isFinite(raw.score) ? Math.max(0, Math.min(1, raw.score)) : 0,
    reason: raw.reason,
    addressed: raw.addressed === true,
    sources: typeof raw.sources === 'number' && Number.isFinite(raw.sources) ? Math.max(0, Math.trunc(raw.sources)) : 0,
  }
}

// Wat de server over de gedragsknoppen zegt: op de open-respons én op elk
// `state`- en `settings`-bericht. Alles optioneel — een oudere server laat het weg.
export interface SettingsDTO {
  turnId?: string
  autonomyPaused?: boolean
  autonomous?: boolean
  eagerness?: string
  cooldownSeconds?: number
}

export type AnswerStopReason = 'truncated' | 'filtered'

// MeetingEvent is één bericht van server naar client; `type` stuurt de UI. Het
// type is een kale string: het protocol mag groeien, en een onbekend woord
// hoort door de default van de switch te vallen, niet door het typesysteem.
export interface MeetingEvent {
  type: string
  sessionId?: string
  pigeonId?: string
  lang?: string
  listening?: unknown
  voice?: unknown
  speaking?: boolean
  autonomyPaused?: boolean
  playbackTracking?: boolean
  turnId?: string
  eventId?: string
  durationMs?: number
  playback?: 'pending' | 'playing' | 'ended' | 'failed' | 'aborted' | 'timeout'
  text?: string
  speaker?: string
  final?: boolean
  pigeonWindow?: boolean
  delta?: string
  audio?: string
  seq?: number
  format?: string
  sampleRate?: number
  frames?: number
  kbStatus?: string
  // Alleen response_done; onbekende waarden krijgen geen publiekslabel.
  stopReason?: unknown
  // De bronnen op `sources`, `text_done` en `response_done` dragen hetzelfde
  // DTO als de chat; ze komen ruw binnen en gaan door parseSources uit
  // lib/sources.ts.
  sources?: unknown
  // Wat er wegviel vóór die bronnenlijst: ruw binnen, door
  // parseSourceDrops uit lib/sources.ts.
  dropped?: unknown
  error?: string
  reason?: string
  message?: Bilingual
  candidate?: unknown
  topic?: TopicRef
  score?: number
  addressed?: boolean
  arousal?: number
  detector?: { enabled: boolean; reason?: string; state?: 'ready' | 'retrying' | 'unavailable' | 'invalid' | 'budget_stopped'; retryable?: boolean; retryAfterMs?: number }
  sound?: { id: string; url: string; band?: string }
  settings?: SettingsDTO
  // De resterende tijd van "Ben je er nog?" als duur: op
  // presence_check, op het state-event en op de hartslag zolang de vraag
  // openstaat. Ontbreekt hij op de hartslag, dan is er (inmiddels) geen vraag.
  graceMs?: number
}
