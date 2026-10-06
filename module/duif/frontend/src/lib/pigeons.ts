import { API, type ApiFailure, type Fetcher, readFailure } from './api'
import type { Bilingual } from './i18n.svelte'

// GET /v1/duiven levert één lijst die zowel de chat als de vergadering voedt.
// Beide stores lazen hem eerder zelf, met twee net iets andere DTO's; hier
// staat de wire-vorm één keer, en elke store maakt er zijn eigen selectie van
// (chat: alleen `chat`, vergadering: alleen `meeting`).

export interface PigeonTopic {
  id: string
  label?: Bilingual
}

// PigeonEngine zegt welke motor de chatbeurt van deze duif draait: `pipeline`
// is haar directe spreekbeurt, `agent` zet de facilitator ervóór — dan is de
// stem in de chat niet die van de duif. De vergadermodus negeert het veld.
export type PigeonEngine = 'pipeline' | 'agent'

// Pigeon is de duif-DTO zoals de server hem stuurt (Engelse identifiers,
// wire-namen letterlijk overgenomen).
export interface Pigeon {
  id: string
  name: Bilingual
  voiceReady: boolean
  knowledgeBase: boolean
  chat: boolean
  meeting: boolean
  engine: PigeonEngine
  // autonomousDefault is de startstand van zelfstandig meedoen voor deze duif:
  // de signalerende duif doet vanzelf mee, de pratende twee wachten
  // op een uitnodiging. De vergadering mag hem altijd verzetten.
  autonomousDefault: boolean
  topics: PigeonTopic[]
}

// BehaviourDTO is de gedragsconfig naast de lijst; de veldnamen zijn de
// Nederlandse wire-namen van de server, de rest van de module is Engels.
export interface BehaviourDTO {
  interventieZelfstandig?: boolean
  interventieThrottleSeconden?: number
  detectieGretigheid?: string
  taalDefault?: string
}

export interface PigeonsBody {
  pigeons: Pigeon[]
  behaviour: BehaviourDTO | undefined
}

export type PigeonsResult = ({ ok: true } & PigeonsBody) | { ok: false; failure: ApiFailure }

function topics(list: unknown): PigeonTopic[] {
  if (!Array.isArray(list)) return []
  const out: PigeonTopic[] = []
  for (const raw of list) {
    const t = raw as Partial<PigeonTopic>
    if (typeof t?.id !== 'string') continue
    out.push(t.label ? { id: t.id, label: t.label } : { id: t.id })
  }
  return out
}

// parsePigeons maakt van een ongetypeerde body de lijst; alles wat geen id
// heeft valt af, en een body zonder `duiven`-veld is geen lijst maar een fout.
export function parsePigeons(body: unknown): PigeonsBody | null {
  if (!body || typeof body !== 'object') return null
  const raw = body as { duiven?: unknown; gedrag?: unknown }
  if (!Array.isArray(raw.duiven)) return null
  const pigeons: Pigeon[] = []
  for (const entry of raw.duiven) {
    const p = entry as Partial<Pigeon>
    if (typeof p?.id !== 'string' || p.id === '') continue
    pigeons.push({
      id: p.id,
      name: p.name ?? {},
      voiceReady: Boolean(p.voiceReady),
      knowledgeBase: Boolean(p.knowledgeBase),
      // Ontbreekt `chat`, dan praat de duif: alleen een expliciete false maakt
      // haar non-verbaal.
      chat: p.chat !== false,
      meeting: Boolean(p.meeting),
      // Alleen een expliciete `agent` zet de facilitator ervóór; een oudere
      // server die het veld niet kent laat de duif zelf spreken.
      engine: p.engine === 'agent' ? 'agent' : 'pipeline',
      // Een oudere server kent het veld niet; dan blijft de gedeelde stand uit
      // `gedrag` het enige wat er is, en die is uit.
      autonomousDefault: p.autonomousDefault === true,
      topics: topics(p.topics),
    })
  }
  const behaviour = raw.gedrag && typeof raw.gedrag === 'object' ? (raw.gedrag as BehaviourDTO) : undefined
  return { pigeons, behaviour }
}

// fetchPigeons haalt de lijst op en gooit nooit: een onbereikbare of onleesbare
// server is een melding, geen kapot scherm.
export async function fetchPigeons(fetch: Fetcher): Promise<PigeonsResult> {
  let res: Response
  try {
    res = await fetch(API + '/duiven')
  } catch {
    return { ok: false, failure: { status: 0 } }
  }
  if (!res.ok) return { ok: false, failure: await readFailure(res) }
  const parsed = parsePigeons(await res.json().catch(() => null))
  if (!parsed) return { ok: false, failure: { status: res.status } }
  return { ok: true, ...parsed }
}
