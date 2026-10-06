import type { MsgKey } from '../i18n.svelte'
import { type Eagerness, LISTEN, type ListenState } from './protocol'
import type { MeetingStatus, MicState } from './meetingStore.svelte'

// De vertaalsleutels van de opsommingen uit het protocol, op één plek: zo blijft
// t() getypeerd en valt een ontbrekende sleutel bij het compileren op.
export const EAGERNESS_KEYS: Record<Eagerness, MsgKey> = {
  reserved: 'meeting.eagerness.reserved',
  normal: 'meeting.eagerness.normal',
  eager: 'meeting.eagerness.eager',
}

export const MAX_COOLDOWN_SECONDS = 3600

// listenKey kiest de tekst bij de luisterstand. Een gedempte microfoon krijgt
// haar eigen zin: "uit" en "gedempt" zijn niet hetzelfde verhaal.
export function listenKey(state: ListenState, mic: MicState): MsgKey {
  if (state === LISTEN.listening) return 'meeting.listen.listening'
  if (state === LISTEN.reconnecting) return 'meeting.listen.reconnecting'
  return mic === 'muted' ? 'meeting.listen.muted' : 'meeting.listen.off'
}

// De vier vergaderstanden (plus aandacht) in tekst. De rail en het podium
// putten uit dezelfde tabel, zodat er nergens een vijfde nuance bij sluipt.
export const STATUS_KEYS: Record<MeetingStatus, MsgKey> = {
  attention: 'meeting.status.attention',
  speaking: 'meeting.status.speaking',
  thinking: 'meeting.status.thinking',
  listening: 'meeting.status.listening',
  paused: 'meeting.status.paused',
}

// De uitslag van de apparatencheck in één zin: wat werkt, of wat er aan
// scheelt. De losse technische regels ("invoer gedetecteerd · browseraudio
// toegestaan") zeggen de gebruiker niets wat deze zin niet zegt.
export function deviceCheckKey(
  state: 'idle' | 'checking' | 'ready' | 'refused' | 'unavailable',
  heard: boolean,
  audio: boolean,
): MsgKey | null {
  if (state === 'idle') return null
  if (state === 'checking') return 'meeting.check.checking'
  if (state === 'refused') return 'meeting.check.refused'
  if (state === 'unavailable') return 'meeting.check.unavailable'
  if (!heard) return 'meeting.check.noInput'
  return audio ? 'meeting.check.ok' : 'meeting.check.noSound'
}

// formatCountdown zet de resterende tijd van "Ben je er nog?" in m:ss. Naar
// boven afgerond: de vraag opent op 2:00 en staat pas op 0:00 als de tijd echt
// om is.
export function formatCountdown(ms: number): string {
  const seconds = Math.max(0, Math.ceil(ms / 1000))
  return Math.floor(seconds / 60) + ':' + String(seconds % 60).padStart(2, '0')
}
