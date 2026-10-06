import { API, bilingualFrom, jsonInit, readFailure, readSSE, type ApiFailure, type Fetcher, type SSEFrame } from '../api'
import { type Lang, type MsgKey, type UiLocale } from '../i18n.svelte'
import { fetchPigeons, type Pigeon } from '../pigeons'
import { parseKBStatus, parseSourceDrops, parseSources, type KBStatus, type Source, type SourceDrops } from '../sources'

// De chatstore is de hele chatlogica zonder DOM: één exemplaar per mount, met
// fetch als enige poort. Geen $effect, geen module-scope state, geen opslag.
// Wat de gebruiker te zien krijgt is altijd óf een servermelding uit
// message{en,nl} óf een eigen i18n-tekst — nooit ruwe fout- of bodytekst.

export const MAX_MESSAGE_RUNES = 4096

// Vanaf hier waarschuwt de teller; op de limiet wordt hij destructief.
export const COUNTER_WARN_RUNES = 3900

export interface ChatPorts {
  fetch: Fetcher
}

export interface SessionRef {
  id: string
  pigeonId: string
  lang: string
  createdAt: string
  updatedAt: string
  comparison?: ComparisonOrigin
  title?: string
}

// ComparisonOrigin is de herkomst van een vergelijkgesprek: uit welk gesprek
// en van welke duif de vraag en de context komen. contextMessageIds noemt, in
// leesvolgorde, precies de rijen die meereisden — de eigen berichten, de vraag
// zelf en de antwoorden die zij van de eerste duif kreeg. De vraag wordt daarna
// nog een keer gesteld, zodat het antwoord van de tweede duif meteen streamt;
// die tweede rij staat dus ná de gekopieerde rijen, en de weergave herkent hem
// aan het aantal (presentation.repeatedQuestionKey).
export interface ComparisonOrigin {
  groupId: string
  sessionId: string
  pigeonId: string
  questionId: string
  originalQuestion: string
  contextMessageIds: string[]
}

export interface AnswerProvenance {
  contractVersion: string
  personaVersion: string
  promptVersion: string
  model: string
  kbState: string
  kbId?: string
  kbVersion?: string
}

interface SavedConversation {
  sessionId: string
  messages: ChatMessage[]
  draft: string
  error?: ApiFailure | null
}

interface PendingTurn {
  controller: AbortController
  previousMessages: ChatMessage[]
  question: string
  confirmed: boolean
}

function newComparisonRequestID(): string {
  const bytes = globalThis.crypto.getRandomValues(new Uint8Array(16))
  return [...bytes].map((byte) => byte.toString(16).padStart(2, '0')).join('')
}

export function parseProvenance(value: unknown): AnswerProvenance | undefined {
  if (!value || typeof value !== 'object') return undefined
  const raw = value as Record<string, unknown>
  if (!['contractVersion', 'personaVersion', 'promptVersion', 'model', 'kbState'].every((k) => typeof raw[k] === 'string')) return undefined
  return {
    contractVersion: raw.contractVersion as string,
    personaVersion: raw.personaVersion as string,
    promptVersion: raw.promptVersion as string,
    model: raw.model as string,
    kbState: raw.kbState as string,
    kbId: typeof raw.kbId === 'string' ? raw.kbId : undefined,
    kbVersion: typeof raw.kbVersion === 'string' ? raw.kbVersion : undefined,
  }
}

// Een beurt kan twee stemmen dragen. De facilitator praat met de mens
// (kind 'text', persona 'facilitator'); de duif antwoordt als eigen blok
// (kind 'pigeon_answer'). Een rij zonder persona is de duif — zo renderen
// oudere berichten als vanouds.
export type MessageKind = 'text' | 'pigeon_answer'

// FACILITATOR is de persona-waarde van de stem die met de mens praat. Eén
// constante, want er wordt in drie bestanden op gebrancht en een typefout in
// een los literal zou stil zijn.
export const FACILITATOR = 'facilitator'

// ARTIFACT_KIND is de enige artifact-soort die de client kan renderen; een
// onbekende soort wordt genegeerd, net als een onbekend frame.
const ARTIFACT_KIND: MessageKind = 'pigeon_answer'

// StopReason is de gesloten set van pkg/agent. De wire mag groeien, vandaar de
// open staart: een onbekende reden valt in de UI terug op geen melding.
export type StopReason = 'natural' | 'max_iter' | 'tool_error' | 'refusal' | 'truncated' | 'filtered' | 'error'

// NOTIFIED_STOP_REASONS zijn de redenen die de lezer iets vertellen over het
// antwoord dat hij voor zich heeft. De overige redenen (te veel stappen, een
// mislukte stap, een algemene fout) zijn machinetaal over onze motor: daar
// verandert niets aan wat de duif zei, en de afgebroken-markering en de
// foutbanner melden de gevolgen al.
//
// Deze lijst is één set met `notifiedStopReasons` in answer_stream.go: de
// server bewaart precies deze redenen op de rij, zodat de badge het herladen
// overleeft. TestStopReasons_GoAndClientNameTheSameSet leest beide lijsten.
export const NOTIFIED_STOP_REASONS = ['refusal', 'truncated', 'filtered'] as const
export type NotifiedStopReason = (typeof NOTIFIED_STOP_REASONS)[number]

// Elke gemelde stopreden heeft een eigen tekst. Ontbreekt er één, dan is dat
// hier een compileerfout in plaats van een stille lege badge.
type Expect<T extends true> = T
export type StopReasonsAreTranslated = Expect<
  `chat.stopped.${NotifiedStopReason}` extends MsgKey ? true : false
>

export function notifiedStopReason(value: string | undefined): NotifiedStopReason | undefined {
  return (NOTIFIED_STOP_REASONS as readonly string[]).includes(value ?? '') ? (value as NotifiedStopReason) : undefined
}

// TOOL_NAMES is de registry van de facilitator zoals de server hem pint
// (newAgentRegistry in agent_tools.go; TestAgentRegistry_ExactToolSet bewaakt
// de set aan die kant). Hier staat hij zodat een hernoemde tool een
// compileerfout geeft in plaats van stilletjes de kale naam op het scherm.
// Onbekende namen blijven op de wire toegestaan: de chip toont ze ongewijzigd.
// `search_sources` zit er niet meer bij — ask_pigeon zoekt zelf,
// dus die stap komt niet langer op de draad.
export const TOOL_NAMES = ['ask_pigeon', 'pigeon_profile', 'clarify', 'redirect_to_pigeon'] as const
export type ToolName = (typeof TOOL_NAMES)[number]

// De ene tool waar de lezer werkelijk op zit te wachten: hij opent het blok
// van de duif en de modelcall erachter duurt onvoorspelbaar lang.
export const TOOL_ASK_PIGEON: ToolName = 'ask_pigeon'

// PHASE_NAMES zijn de stappen die de server per beurt meldt (`phase`-frames),
// in de volgorde waarin een volledige beurt ze doorloopt. De server
// kent dezelfde set (llm_metrics.go; TestPhases_GoAndClientNameTheSameSet
// vergelijkt de twee lijsten), en elke naam hier heeft een zin in beide talen.
//
// Een fase die deze client niet kent wordt genegeerd: de stapregel blijft dan
// staan op de laatste stap die hij wél kon benoemen. Liever een stap te weinig
// dan machinetaal op het scherm.
export const PHASE_NAMES = ['facilitator', 'pigeon_grounding', 'pigeon_related_search', 'pigeon', 'mediation'] as const
export type PhaseName = (typeof PHASE_NAMES)[number]

// Elke fase heeft een label. Ontbreekt er één, dan is dat hier een
// compileerfout in plaats van een lege chip in de app.
export type PhasesAreTranslated = Expect<`chat.step.${PhaseName}` extends MsgKey ? true : false>

export function isPhaseName(value: string): value is PhaseName {
  return (PHASE_NAMES as readonly string[]).includes(value)
}

// TurnStep is één stap van de lopende beurt: de fase en of zij al klaar is.
// Stappen zijn turn-lokaal en worden nooit bewaard — zie ChatStore.steps.
export interface TurnStep {
  phase: PhaseName
  done: boolean
}

// Announcement is één melding voor de schermlezer: welke stem een blok afrondde
// en het hoeveelste blok dat was. De store zendt hem uit op het moment dat een
// blok settelt — een tussenstand kan bij het samenvallen van frames in één
// netwerkchunk overgeslagen worden. De monotone `seq` houdt twee identieke
// meldingen achter elkaar uit elkaar.
export interface Announcement {
  seq: number
  voice: 'pigeon' | 'facilitator'
}

// ToolChip is één stap van de facilitator zoals de wire hem meldt: alleen id
// en naam, nooit argumenten of een samenvatting — die blijven server-side.
export interface ToolChip {
  id: string
  name: string
  done: boolean
  error?: string
}

export interface ChatMessage {
  key: string
  role: 'user' | 'assistant'
  kind: MessageKind
  persona?: string
  content: string
  pigeonId?: string
  provenance?: AnswerProvenance
  sources: Source[]
  kbStatus?: KBStatus
  // tools hangt aan de facilitator-rij en komt nooit uit de historie: chips
  // worden niet bewaard.
  tools: ToolChip[]
  // stopReason staat op de rij die anders dan natuurlijk eindigde, en alleen
  // dan. Dat is de facilitator-rij als zijn eigen beurt afbrak (de `done` van
  // de beurt draagt hem), en het duif-blok als de provider díé stream sloot
  // (het sluitende artifact draagt hem). Op de pipeline is dat dezelfde rij.
  // Anders dan `tools` komt hij ook uit de historie: hij wordt bewaard.
  stopReason?: StopReason | (string & {})
  // dropped meldt dat er gevonden bronnen wegvielen vóór deze lijst.
  // Hij komt van het openframe van het duif-blok (agent) of van `done`
  // (pipeline) en wordt niet bewaard: na herladen valt hij niet meer na te gaan.
  dropped?: SourceDrops
  // related zegt waar deze bronnen vandaan komen: niet uit de gewone
  // zoekopdracht op de vraag, maar uit een herformulering of uit de brede
  // zoekactie ná een kennisgat. Zonder dit onderscheid leest "3
  // bronnen" als dekking. Hij komt van het openframe én — anders dan
  // `dropped` — uit de historie: de duif benoemt de grens alleen als de
  // bronnen haar vraag echt niet dragen, dus na herladen zou een antwoord uit
  // grenzend onderzoek anders als volledig gedekt lezen.
  related?: boolean
  partial: boolean
  streaming: boolean
}

// bare zegt of een rij niets te tonen heeft. Chips zijn status en geen inhoud,
// en een einde zonder eigen melding evenmin: een tijdsafkap of een mislukte
// stap draagt de stopreden `error` of `max_iter`, en die krijgt geen regel op
// het scherm (zie NOTIFIED_STOP_REASONS). Zo'n rij bleef staan als lege kaart
// op naam van de facilitator, met "(afgebroken)" eronder, terwijl hij in die
// beurt niets zei en de foutbanner de afkap al meldt. De server
// bewaart zo'n rij ook niet, dus na herladen zou hij alsnog verdwijnen. Eén
// definitie voor de twee paden die een beurt afsluiten (#close en abort),
// zodat ze niet uit elkaar lopen.
function bare(m: ChatMessage): boolean {
  return m.content === '' && m.sources.length === 0 && notifiedStopReason(m.stopReason) === undefined
}

// frameId leest het id van een tool- of artifact-frame; zonder bruikbaar id
// is het frame niet te koppelen en wordt het genegeerd.
function frameId(frame: SSEFrame): string {
  return typeof frame.id === 'string' ? frame.id : ''
}

function frameText(frame: SSEFrame, field: string): string | undefined {
  const value = frame[field]
  return typeof value === 'string' && value !== '' ? value : undefined
}

// shortTime maakt van een ISO-tijd een korte, relatieve aanduiding in de
// UI-taal. Intl doet het rekenwerk; de module houdt zelf geen datumteksten bij.
export function shortTime(iso: string, lang: Lang, now: number = Date.now()): string {
  const at = Date.parse(iso)
  if (Number.isNaN(at)) return ''
  const seconds = Math.round((at - now) / 1000)
  const rel = new Intl.RelativeTimeFormat(lang, { numeric: 'auto', style: 'short' })
  const abs = Math.abs(seconds)
  if (abs < 60) return rel.format(Math.round(seconds), 'second')
  if (abs < 3600) return rel.format(Math.round(seconds / 60), 'minute')
  if (abs < 86400) return rel.format(Math.round(seconds / 3600), 'hour')
  if (abs < 7 * 86400) return rel.format(Math.round(seconds / 86400), 'day')
  return new Intl.DateTimeFormat(lang, { day: 'numeric', month: 'short' }).format(at)
}

// Wire-vormen; alles optioneel, want een oudere server mag velden weglaten.
interface WireMessage {
  id?: string
  role?: string
  kind?: string
  persona?: string
  content?: string
  kbStatus?: string
  sources?: unknown
  partial?: boolean
  createdAt?: string
  provenance?: unknown
  stopReason?: string
  related?: boolean
}

export class ChatStore {
  readonly ports: ChatPorts
  readonly locale: UiLocale

  // Arrays zijn $state.raw: ze worden altijd in hun geheel vervangen, wat
  // tijdens het streamen scheelt in proxies en een stabiele volgorde geeft.
  pigeons = $state.raw<Pigeon[]>([])
  sessions = $state.raw<SessionRef[]>([])
  messages = $state.raw<ChatMessage[]>([])
  // De meldingen voor de schermlezer; ChatView rendert de laatste. Alleen de
  // laatste paar blijven staan: het is een uitzending, geen logboek. Ze gelden
  // voor de beurt die ze uitzond: een nieuwe beurt, een fout en een afbreking
  // maken de lijst leeg (zie #silence), anders zegt de statusregel nog "heeft
  // geantwoord" terwijl er een nieuwe vraag loopt of juist niets kwam.
  announcements = $state.raw<Announcement[]>([])

  selectedPigeonId = $state('')
  sessionId = $state('')
  draft = $state('')
  streaming = $state(false)
  loadingPigeons = $state(false)
  loadingHistory = $state(false)
  pigeonsLoaded = $state(false)
  sessionsLoaded = $state(false)
  loadingSessions = $state(false)
  sessionsError = $state<ApiFailure | null>(null)
  error = $state<ApiFailure | null>(null)
  disposed = $state(false)
  // Het zichtbare wachten op de duif. `pigeonWaitSince` is het moment
  // waarop `ask_pigeon` begon (0 = er wordt niet gewacht); `pigeonWaitOpen`
  // zegt dat haar blok nog niet openstaat, zodat het scherm intussen tóch een
  // wachtstand kan tonen. Beide lopen door tot de eerste letter van de duif:
  // dát is het moment waarop het wachten voorbij is, niet het openen van het
  // blok. De klok begint bij de tool-aanroep, want ook het opzoeken in de
  // kennisbank gaat via dezelfde onvoorspelbare proxy.
  pigeonWaitSince = $state(0)
  pigeonWaitOpen = $state(false)
  // De stappen van de lopende beurt, in de volgorde waarin de server
  // ze meldde. De laatste is de stap die nú loopt; de rest is klaar.
  //
  // Ze leven precies zo lang als de beurt. Afgeronde stappen blijven dus niet
  // onder het antwoord staan: chips worden niet bewaard, dus na herladen zouden
  // ze er niet meer zijn, en een gesprek dat bij elk antwoord vier grijze
  // vinkjes draagt leest als een logboek. Wat er van de beurt overblijft is het
  // antwoord zelf, met zijn bron- en dekkingschip.
  steps = $state.raw<TurnStep[]>([])
  // De stap die nu loopt, of undefined buiten een beurt.
  get currentStep(): PhaseName | undefined {
    const last = this.steps.at(-1)
    return last && !last.done ? last.phase : undefined
  }
  // Vergelijken is één klik en dus één fase: opzoeken, kopiëren, overschakelen
  // en versturen zitten in dezelfde vlag. `confirmingComparison` bestaat nog
  // als naam voor de schermen die de invoer erop uitzetten; er valt niets meer
  // te bevestigen.
  loadingComparison = $state(false)
  get confirmingComparison(): boolean {
    return this.loadingComparison
  }
  get activeComparison(): ComparisonOrigin | undefined {
    return this.sessions.find((s) => s.id === this.sessionId)?.comparison
  }
  get conversationLang(): string {
    return this.sessions.find((s) => s.id === this.sessionId)?.lang ?? this.locale.lang
  }

  // De capfout heeft één renderer: pre-stream (JSON-body) en in-stream
  // (error-frame) landen allebei in `error`, dus dit ene afgeleide veld dekt
  // beide gevallen.
  capExceeded = $derived(this.error?.reason === 'llm_budget_exceeded')
  selectedPigeon = $derived(this.pigeons.find((p) => p.id === this.selectedPigeonId) ?? null)

  // Een getter i.p.v. $derived: een veldinitialisator mag `locale` nog niet
  // lezen (die wordt in de constructor gezet). De getter is even reactief —
  // hij leest dezelfde $state-velden — en kost hier niets.
  get errorText(): string {
    return this.error ? this.locale.fromServer(this.error.message, this.error.reason) : ''
  }

  #controller: AbortController | null = null
  #seq = 0
  #announceSeq = 0
  #historyGen = 0
  #comparisonGen = 0
  #active = new Map<string, SavedConversation>()
  #drafts = new Map<string, string>()
  #comparisonRequests = new Map<string, string>()
  #pendingTurn: PendingTurn | null = null
  #sessionsFlight: Promise<void> | null = null
  #deletedSessions = new Set<string>()

  async deleteSession(id: string): Promise<boolean> {
    if (this.disposed) return false
    if (id === this.sessionId) this.abort()
    const controller = new AbortController()
    const deadline = setTimeout(() => controller.abort(), 15_000)
    try {
      const response = await this.ports.fetch(`${API}/chat/sessions/${encodeURIComponent(id)}`, { method: 'DELETE', signal: controller.signal })
      if (this.disposed) return false
      if (!response.ok && response.status !== 404) return false
      this.#deletedSessions.add(id)
      this.#drafts.delete(id)
      this.#comparisonRequests.clear()
      this.#stopComparison()
      for (const [pigeon, saved] of this.#active) if (saved.sessionId === id) this.#active.delete(pigeon)
      this.sessions = this.sessions.filter((s) => s.id !== id).map((s) => s.comparison?.sessionId === id ? { ...s, comparison: undefined } : s)
      if (id === this.sessionId) this.newConversation()
      return true
    } catch { return false } finally { clearTimeout(deadline) }
  }

  constructor(ports: ChatPorts, locale: UiLocale) {
    this.ports = ports
    this.locale = locale
  }

  // ---- laden ---------------------------------------------------------------

  async loadPigeons(): Promise<void> {
    if (this.disposed || this.loadingPigeons || this.pigeonsLoaded) return
    this.loadingPigeons = true
    const result = await fetchPigeons(this.ports.fetch)
    if (this.disposed) return
    this.loadingPigeons = false
    if (!result.ok) {
      this.error = result.failure
      return
    }
    // Alleen duiven die kunnen chatten horen op de zitstok (productmatrix).
    const list = result.pigeons.filter((p) => p.chat)
    this.pigeons = list
    this.pigeonsLoaded = true
    if (!this.selectedPigeonId && list.length > 0) this.selectedPigeonId = list[0].id
  }

  async loadSessions(refresh = false): Promise<void> {
    if (this.disposed) return
    if (this.#sessionsFlight) return this.#sessionsFlight
    if (this.sessionsLoaded && !refresh) return
    this.loadingSessions = true
    this.sessionsError = null
    const flight = this.#fetchSessions()
    this.#sessionsFlight = flight
    try { await flight } finally {
      this.#sessionsFlight = null
      if (!this.disposed) this.loadingSessions = false
    }
  }

  async #fetchSessions(): Promise<void> {
    // Preserve sessions created/updated locally while this snapshot is in flight.
    // Unchanged entries absent from the server may have been removed elsewhere.
    const before = new Map(this.sessions.map((session) => [session.id, session]))
    try {
      const res = await this.ports.fetch(`${API}/chat/sessions`)
      if (this.disposed) return
      if (!res.ok) {
        const failure = await readFailure(res)
        if (!this.disposed) this.sessionsError = failure
        return
      }
      const body = (await res.json()) as { sessions?: SessionRef[] }
      if (this.disposed) return
      const merged = new Map((body.sessions ?? []).map((session) => [session.id, session]))
      for (const session of this.sessions) {
        if (before.get(session.id) !== session) merged.set(session.id, session)
      }
      const at = (s: SessionRef): number => {
        const t = Date.parse(s.updatedAt)
        return Number.isNaN(t) ? 0 : t
      }
      this.sessions = [...merged.values()].filter((s) => !this.#deletedSessions.has(s.id)).map((s) => s.comparison && this.#deletedSessions.has(s.comparison.sessionId) ? { ...s, comparison: undefined } : s).sort((a, b) => at(b) - at(a))
      this.sessionsLoaded = true
    } catch {
      if (!this.disposed) this.sessionsError = { status: 0 }
    }
  }

  // History retains its original language; opening it does not change UI language.
  // Each pigeon has its own active conversation and unsent draft.
  async openSession(id: string, keepComparison = false): Promise<void> {
    if (this.disposed) return
    this.abort()
    this.#saveActive()
    if (!keepComparison) this.#stopComparison()
    const gen = ++this.#historyGen
    const record = this.sessions.find((s) => s.id === id)
    this.sessionId = id
    if (record) {
      this.selectedPigeonId = record.pigeonId
    }
    this.messages = []
    this.draft = this.#drafts.get(id) ?? ''
    this.error = null
    this.loadingHistory = true
    try {
      const res = await this.ports.fetch(`${API}/chat/sessions/${encodeURIComponent(id)}/messages`)
      if (this.disposed || gen !== this.#historyGen) return
      if (!res.ok) {
        const failure = await readFailure(res)
        if (!this.disposed && gen === this.#historyGen) this.error = failure
        return
      }
      const body = (await res.json()) as { messages?: WireMessage[] }
      if (this.disposed || gen !== this.#historyGen) return
      this.messages = (body.messages ?? []).map((m, i) => this.#fromWire(m, i))
      // A committed comparison can survive an interrupted creation response.
      // Opening it presents its saved question as a draft, never silently
      // starts a model call and never leaves an empty, unresumable branch.
      if (!this.#drafts.has(id) && record?.comparison && this.messages.length === record.comparison.contextMessageIds.length) {
        this.draft = record.comparison.originalQuestion
      }
      this.#saveActiveAfterLoad()
    } catch {
      if (!this.disposed && gen === this.#historyGen) this.error = { status: 0 }
    } finally {
      if (!this.disposed && gen === this.#historyGen) this.loadingHistory = false
    }
  }

  #saveActive(): void {
    if (!this.selectedPigeonId || this.loadingHistory) return
    if (!this.sessionId && !this.messages.length && !this.draft) return
    this.#saveActiveAfterLoad()
  }

  #saveActiveAfterLoad(): void {
    if (this.sessionId) this.#drafts.set(this.sessionId, this.draft)
    this.#active.set(this.selectedPigeonId, { sessionId: this.sessionId, messages: this.messages, draft: this.draft, error: this.error })
  }

  newConversation(): void {
    if (this.disposed) return
    this.abort()
    this.#stopComparison()
    this.#historyGen++
    this.loadingHistory = false
    this.sessionId = ''
    this.messages = []
    this.draft = ''
    this.error = null
    this.#active.set(this.selectedPigeonId, { sessionId: '', messages: [], draft: '' })
  }

  selectPigeon(id: string): void {
    if (this.disposed || id === this.selectedPigeonId || !this.pigeons.some((p) => p.id === id)) return
    this.abort()
    this.#saveActive()
    this.#stopComparison()
    this.#historyGen++
    this.loadingHistory = false
    this.selectedPigeonId = id
    this.sessionId = ''
    this.messages = []
    this.draft = ''
    this.error = null
    const saved = this.#active.get(id)
    if (saved) {
      this.sessionId = saved.sessionId
      this.messages = saved.messages
      this.draft = saved.draft
      this.error = saved.error ?? null
    } else {
      const latest = this.sessions.find((session) => session.pigeonId === id)
      if (latest) void this.openSession(latest.id)
    }
  }

  // ---- vergelijken ---------------------------------------------------------
  //
  // Eén klik. Er is altijd precies één andere sprekende duif, dus valt er niets
  // te kiezen: de vraag verhuist naar haar gesprek en haar antwoord begint
  // meteen te streamen. Wat meegaat ligt vast en is elke keer hetzelfde — het
  // hele eigen gesprek tot aan de vraag, de eigen berichten én de antwoorden
  // van de eerste duif. De server kopieert dat; de client kiest niets.

  // #stopComparison laat een lopende overschakeling los. Een antwoord dat
  // daarna nog binnenkomt navigeert niet meer en vraagt geen model.
  #stopComparison(): void {
    this.#comparisonGen++
    this.loadingComparison = false
  }

  async beginComparison(questionKey: string): Promise<void> {
    if (this.disposed || this.streaming || this.loadingComparison || !this.sessionId) return
    const users = this.messages.filter((m) => m.role === 'user')
    const index = users.findIndex((m) => m.key === questionKey)
    const question = users[index]
    // De duivenlijst bevat alleen duiven die kunnen chatten, en er is er maar
    // één andere; zonder die duif is er niets te vergelijken.
    const target = this.pigeons.find((p) => p.id !== this.selectedPigeonId)
    if (!question || !target) return
    this.#stopComparison()
    const gen = this.#comparisonGen
    const sourceSessionId = this.sessionId
    this.loadingComparison = true
    this.error = null
    try {
      // Live keys are local; resolve the persisted message ID before copying.
      // Verify the text too, so a concurrent edit cannot silently redirect the
      // user's chosen question.
      const res = await this.ports.fetch(`${API}/chat/sessions/${encodeURIComponent(sourceSessionId)}/messages`)
      if (this.disposed || gen !== this.#comparisonGen) return
      if (!res.ok) {
        const failure = await readFailure(res)
        if (!this.disposed && gen === this.#comparisonGen) this.error = failure
        return
      }
      const body = await res.json() as { messages?: WireMessage[] }
      if (this.disposed || gen !== this.#comparisonGen) return
      const rows = (body.messages ?? []).filter((m) => m.role === 'user')
      const selected = rows[index]
      if (!selected?.id || selected.content !== question.content) {
        this.error = { status: 409, message: { en: 'Reload this conversation before comparing the question.', nl: 'Herlaad dit gesprek voordat je de vraag vergelijkt.' } }
        return
      }
      await this.#askOtherPigeon(gen, sourceSessionId, selected.id, question.content, target.id)
    } catch {
      if (!this.disposed && gen === this.#comparisonGen) this.error = { status: 0 }
    } finally {
      if (!this.disposed && gen === this.#comparisonGen) this.loadingComparison = false
    }
  }

  // #askOtherPigeon maakt het doelgesprek aan, opent het en stelt de vraag.
  // Een onzekere retry gebruikt dezelfde idempotentiesleutel, en een gesprek
  // waarin de vraag al staat wordt hervat in plaats van opnieuw gevraagd.
  async #askOtherPigeon(gen: number, sourceSessionId: string, questionId: string, question: string, pigeonId: string): Promise<void> {
    const intent = JSON.stringify([sourceSessionId, questionId, pigeonId])
    const requestId = this.#comparisonRequests.get(intent) ?? newComparisonRequestID()
    this.#comparisonRequests.set(intent, requestId)
    const res = await this.ports.fetch(`${API}/chat/comparisons`, jsonInit({ requestId, sourceSessionId, questionId, pigeonId }))
    if (this.disposed) return
    if (!res.ok) {
      const failure = await readFailure(res)
      if (gen === this.#comparisonGen) this.error = failure
      return
    }
    const body = await res.json() as { session?: SessionRef }
    if (this.disposed) return
    const session = body.session
    if (!session?.id || session.pigeonId !== pigeonId) {
      if (gen === this.#comparisonGen) this.error = { status: 502 }
      return
    }
    // Even an abandoned switch may have committed its target. Keep the branch
    // discoverable; stale completion never navigates or calls a model.
    this.sessions = [session, ...this.sessions.filter((s) => s.id !== session.id)]
    if (gen !== this.#comparisonGen) return
    await this.openSession(session.id, true)
    if (this.disposed || gen !== this.#comparisonGen || this.sessionId !== session.id || this.error) return
    // Alles vóór de vraag is de kopie; wat daarna staat is de vraag zelf.
    const copied = session.comparison?.contextMessageIds.length ?? 0
    const existing = this.messages[copied]
    if (existing) {
      // An uncertain previous response may already have sent the question.
      // Recover that branch and its answer, without another model invocation.
      if (existing.role !== 'user' || existing.content !== question) {
        this.error = { status: 409, message: { en: 'This comparison has changed. Review its conversation before continuing.', nl: 'Deze vergelijking is veranderd. Bekijk het gesprek voordat je verdergaat.' } }
        return
      }
      if (this.draft === question) {
        this.draft = ''
        this.#drafts.set(session.id, '')
      }
      this.#comparisonRequests.delete(intent)
      return
    }
    this.draft = ''
    // Vanaf hier is het een gewone beurt: de streamingstatus vertelt de rest.
    this.loadingComparison = false
    await this.send(question)
    if (this.disposed || gen !== this.#comparisonGen) return
    if (!this.error) this.#comparisonRequests.delete(intent)
  }

  // ---- beurt ---------------------------------------------------------------

  async send(text: string): Promise<void> {
    if (this.disposed || this.streaming || this.loadingHistory) return
    const message = text.trim()
    if (!message) return
    // De server telt runes, dus de client ook: emoji mogen niet stiekem twee
    // tellen en een 4096-tekenbericht mag niet op een 400 stuklopen.
    if ([...message].length > MAX_MESSAGE_RUNES) return
    const pigeonId = this.selectedPigeonId
    if (!pigeonId) return

    const lang = this.conversationLang
    this.error = null
    // Een nieuwe beurt begint zonder stappen en zonder de afrondingsmelding van
    // de vorige; die is voorbij.
    this.#resetSteps()
    this.#silence()
    // De stem van de beurt staat vóór het eerste frame vast: draait deze duif
    // op de agent-motor, dan praat de facilitator en niet de duif. Afleiden uit
    // de binnenkomende frames zou de eerste zin onder de naam en de stem van de
    // duif zetten.
    const persona = this.selectedPigeon?.engine === 'agent' ? FACILITATOR : undefined
    const reply: ChatMessage = {
      key: this.#key(),
      role: 'assistant',
      pigeonId,
      kind: 'text',
      persona,
      content: '',
      sources: [],
      tools: [],
      partial: false,
      streaming: true,
    }
    const previousMessages = this.messages
    this.messages = [
      ...this.messages,
      {
        key: this.#key(),
        role: 'user',
        kind: 'text',
        pigeonId,
        content: message,
        sources: [],
        tools: [],
        partial: false,
        streaming: false,
      },
      reply,
    ]
    this.streaming = true

    const controller = new AbortController()
    this.#controller = controller
    this.#pendingTurn = { controller, previousMessages, question: message, confirmed: false }
    const { signal } = controller
    let sawDone = false
    let sawError = false
    // settled: `done` heeft de beurt al afgerond. Sluit de server de stroom
    // meteen daarna, dan is die sluiting het normale einde en geen fout — en
    // een staart die er tóch nog is mag niets opnieuw afsluiten of aankondigen.
    let settled = false

    // De koppeling van artifact-id aan bericht is turn-lokaal: ids zijn alleen
    // binnen één beurt uniek, en een oude beurt mag nooit meeschrijven.
    const artifacts = new Map<string, string>()

    // De facilitator praat op twee momenten: een kader vóór het duif-blok en
    // een bemiddeling erna. Beide komen als gewone `text` binnen, dus tekst
    // stroomt gewoon in `reply` — de rij die de beurt afsluit, met de chips, de
    // stopreden en de bemiddeling. Gaat er dan een duif-blok open terwijl daar
    // al woorden staan, dan waren die woorden het kader: ze verhuizen naar een
    // eigen rij bóven haar antwoord en `reply` komt vrij voor de bemiddeling.
    //
    // Splitsen kan pas op dat moment. Eerder is niet te zien of er nog een
    // duif-blok komt: een wedervraag of een terugverwijzing is óók tekst zonder
    // artifact, en die hoort één rij te blijven.
    const guidance = [reply.key]
    let opened = false

    try {
      const res = await this.ports.fetch(`${API}/chat/turn`, {
        ...jsonInit({ sessionId: this.sessionId, pigeonId, message, lang }),
        signal,
      })
      if (this.#stale(signal)) return
      const confirmedSessionId = res.headers.get('X-Duif-Session-ID')
      if (confirmedSessionId) {
        this.sessionId = confirmedSessionId
        if (this.#pendingTurn?.controller === controller) this.#pendingTurn.confirmed = true
        this.#rememberSession(confirmedSessionId, pigeonId, lang)
      }
      if (!res.ok) {
        const failure = await readFailure(res)
        if (this.#stale(signal)) return
        this.error = failure
        sawError = true
        return
      }
      await readSSE(
        res.body,
        (frame) => {
          if (this.#stale(signal)) return
          const type = frame.type
          if (type === 'sources') {
            // Met een id horen de bronnen bij dat duif-blok (de agent-motor
            // zoekt per vraag); zonder id bij de beurt zelf (de pipeline).
            const id = frameId(frame)
            const target = id ? artifacts.get(id) : reply.key
            if (target) this.#patch(target, { sources: parseSources(frame.sources) })
          } else if (type === 'text') {
            if (typeof frame.delta === 'string' && frame.delta !== '') {
              this.#append(reply.key, frame.delta)
            }
          } else if (type === 'phase') {
            // De stand van de beurt: welke stap nu begint. Geen inhoud, dus
            // ook geen rij en geen patch — alleen de stapregel verandert.
            this.#step(frameText(frame, 'phase') ?? '')
          } else if (type === 'tool') {
            const id = frameId(frame)
            if (id === '') return
            const name = frameText(frame, 'name') ?? ''
            // Vanaf hier wacht de lezer op de duif: eerst op het opzoeken, dan
            // op haar eerste woord. Beide gaan langs dezelfde trage proxy, dus
            // de klok loopt vanaf nu.
            if (name === TOOL_ASK_PIGEON) this.#beginPigeonWait()
            this.#chip(reply.key, id, { name, done: false })
          } else if (type === 'tool_result') {
            const id = frameId(frame)
            if (id === '') return
            const name = frameText(frame, 'name') ?? ''
            // De tool is klaar — geslaagd of niet. Er valt niets meer te
            // wachten; een fout draagt zijn eigen melding.
            if (name === TOOL_ASK_PIGEON) this.#endPigeonWait()
            this.#chip(reply.key, id, { name, done: true }, frameText(frame, 'error'))
          } else if (type === 'artifact') {
            const id = frameId(frame)
            if (id === '') return
            const known = artifacts.get(id)
            if (frame.done === true) {
              // Sluiten zonder open: er is niets om af te ronden, en er komt ook
              // niets meer bij — de map kent dit id niet.
              if (known) {
                // De stopreden hoort bij dít blok: brak het antwoord van de
                // duif af op de lengtegrens, dan staat de melding onder haar
                // tekst en niet onder de bemiddeling die erna nog komt.
                const closed: Partial<ChatMessage> = {
                  streaming: false,
                  partial: frameText(frame, 'error') !== undefined,
                }
                const stopReason = frameText(frame, 'stopReason')
                if (stopReason !== undefined && stopReason !== 'natural') closed.stopReason = stopReason
                this.#patch(known, closed)
                this.#announce(known)
              }
              return
            }
            if (known) return
            // Alleen het duif-antwoord heeft een renderer; een andere soort zou
            // stil als duif-antwoord verschijnen.
            if (frameText(frame, 'kind') !== ARTIFACT_KIND) return
            // Haar blok staat er nu zelf; de losse wachtbubbel is niet meer
            // nodig. De klok loopt door: er is nog geen woord geschreven.
            this.pigeonWaitOpen = false
            const key = this.#key()
            artifacts.set(id, key)
            // Tussen het kader en de afsluitende rij: dat is de volgorde waarin
            // de server de beurt bewaart, dus live en na herladen staat alles
            // gelijk. De kennisbankstand van dít blok komt mee: een kennisgat is
            // een badge op het antwoord van de duif, niet op de tekst van de
            // facilitator.
            if (!opened) {
              opened = true
              const said = this.#find(reply.key)
              if (said && said.content !== '') {
                const framingKey = this.#key()
                guidance.unshift(framingKey)
                this.#insertBefore(reply.key, { ...said, key: framingKey, tools: [], streaming: false })
                this.#patch(reply.key, { content: '' })
              }
            }
            this.#insertBefore(reply.key, {
              key,
              role: 'assistant',
              kind: ARTIFACT_KIND,
              pigeonId,
              persona: frameText(frame, 'persona'),
              content: '',
              sources: [],
              kbStatus: parseKBStatus(frame.kbStatus),
              dropped: parseSourceDrops(frame.dropped),
              related: frame.related === true,
              tools: [],
              partial: false,
              streaming: true,
            })
          } else if (type === 'artifact_delta') {
            const key = artifacts.get(frameId(frame))
            // Bewust niet gegate op de sluiting: een laatste stukje tekst mag
            // ná het close-frame binnenkomen zonder te verdwijnen.
            if (key && typeof frame.delta === 'string' && frame.delta !== '') {
              // Het eerste woord van de duif is het einde van het wachten.
              this.#endPigeonWait()
              this.#append(key, frame.delta)
            }
          } else if (type === 'error') {
            sawError = true
            this.error = this.#failureFromFrame(frame)
          } else if (type === 'done') {
            sawDone = true
            if (typeof frame.sessionId === 'string' && frame.sessionId !== '') {
              this.sessionId = frame.sessionId
              if (this.#pendingTurn?.controller === controller) this.#pendingTurn.confirmed = true
              this.#rememberSession(frame.sessionId, pigeonId, lang)
            }
            const provenance = parseProvenance(frame.provenance)
            const patch: Partial<ChatMessage> = { provenance }
            // De kennisbankstand van de beurt hoort bij de duif. Op de pipeline
            // ís deze rij haar antwoord; op de agent-motor is het de rij van de
            // facilitator, en die bezit geen bronnen — een gat- of
            // onbereikbaar-badge onder zijn bubbel zou een bronclaim zijn van
            // een spreker die er geen mag doen. De duif draagt haar
            // eigen stand op haar eigen blok (`artifact`), en een onbereikbare
            // kennisbank meldt zich als foutbanner.
            if (reply.persona !== FACILITATOR) patch.kbStatus = parseKBStatus(frame.kbStatus)
            if (Array.isArray(frame.sources)) patch.sources = parseSources(frame.sources)
            // Uitval hoort bij dezelfde rij als de stand: op de agent-motor
            // draagt het duif-blok hem al, hier is het de pipeline-rij.
            if (reply.persona !== FACILITATOR) {
              const dropped = parseSourceDrops(frame.dropped)
              if (dropped) patch.dropped = dropped
            }
            const stopReason = frameText(frame, 'stopReason')
            // Een natuurlijk einde is geen melding; alleen het afwijkende einde
            // wordt vastgelegd. Een ontbrekende reden is het pipeline-pad.
            const broken = stopReason !== undefined && stopReason !== 'natural'
            if (broken) patch.stopReason = stopReason
            this.#patch(reply.key, patch)
            // `done` settelt élk open blok van deze beurt, ook als het
            // close-frame van een artifact onderweg is blijven steken. Eindigde
            // de beurt niet natuurlijk, dan is zo'n blok afgebroken en niet af.
            for (const key of artifacts.values()) {
              if (provenance) this.#patch(key, { provenance })
              const artifact = this.#find(key)
              if (artifact?.streaming !== true) continue
              this.#patch(key, { streaming: false, partial: artifact.partial || broken })
              this.#announce(key)
            }
            // `done` is het einde van de beurt: de invoer is weer vrij en elk
            // blok is af. De server sluit de stroom er meteen achter.
            this.#close(guidance, true, sawError, [...artifacts.values()])
            settled = true
            this.streaming = false
          }
        },
        signal,
      )
    } catch {
      if (this.#stale(signal)) return
      // Een afgeronde beurt die daarna alsnog struikelt is geen mislukt
      // antwoord: het antwoord staat er al en `done` is geweest.
      if (settled) return
      sawError = true
      if (!this.error) this.error = { status: 0 }
    } finally {
      // owned: is dit nog de lopende beurt? Sinds `done` de invoer vrijgeeft
      // kan de gebruiker al een volgende vraag hebben gesteld terwijl deze
      // stroom nog aan het afsluiten is. De staart van een oude beurt mag de
      // nieuwe beurt niet stilzetten; per-bericht patchen blijft veilig, want
      // berichtsleutels zijn stabiel.
      const owned = this.#controller === controller
      if (owned) this.#controller = null
      if (!this.#stale(signal)) {
        if (owned) this.streaming = false
        if (this.#pendingTurn?.controller === controller && !this.#pendingTurn.confirmed) {
          this.#recoverUnconfirmed()
        } else if (!settled) {
          this.#close(guidance, sawDone, sawError, [...artifacts.values()])
        }
        if (this.#pendingTurn?.controller === controller) this.#pendingTurn = null
      }
    }
  }

  #recoverUnconfirmed(): void {
    const pending = this.#pendingTurn
    if (!pending || pending.confirmed) return
    this.messages = pending.previousMessages
    this.draft = pending.question
    this.#silence()
    // Deze afronding gaat niet door #close — het gesprek wordt teruggedraaid in
    // plaats van afgesloten — dus ruimt ze de stapregel zelf op. Het scherm
    // verbergt de rij al zodra de beurt niet meer streamt, maar `currentStep`
    // zou blijven zeggen dat er een stap loopt terwijl er niets meer loopt. Een
    // stand die niet meer waar is, is een stand die iemand later gelooft.
    this.#resetSteps()
    this.error ??= {
      status: 0,
      reason: 'chat_turn_unconfirmed',
      message: {
        en: 'The question was not confirmed. It is back in the input field. Check the conversation history before sending it again.',
        nl: 'De vraag is niet bevestigd. Ze staat weer in het invoerveld. Bekijk de gesprekshistorie voordat je haar opnieuw verstuurt.',
      },
    }
    this.#pendingTurn = null
  }

  // abort is de stopknop: hij breekt de lopende stroom af en sluit het
  // ontvangen antwoord netjes af. De afgebroken fetch mag daarna niets meer
  // schrijven (elke schrijfactie gaat door #stale).
  abort(): void {
    const controller = this.#controller
    this.#controller = null
    controller?.abort()
    // Wie stopt, wacht niet meer: geen tikkende teller en geen lopende stap
    // onder een afgebroken beurt.
    this.#endPigeonWait()
    this.#resetSteps()
    // En geen "heeft geantwoord" voor een beurt die gestopt is, of voor een
    // gesprek dat de lezer net verliet (openen, wisselen en opnieuw beginnen
    // lopen allemaal hierlangs).
    this.#silence()
    if (this.disposed || !this.streaming) return
    this.streaming = false
    if (this.#pendingTurn && !this.#pendingTurn.confirmed) {
      this.#recoverUnconfirmed()
      return
    }
    this.#pendingTurn = null
    // Een beurt kan meer dan één open blok hebben (duif-artifact plus
    // facilitator-rij); ze worden allemaal afgerond. Een blok dat niets
    // opleverde verdwijnt; een abort is geen fout, dus er komt geen melding.
    // De leegheidsregel is dezelfde als in #close (zie #bare): twee paden die
    // hetzelfde werk doen horen niet elk hun eigen definitie van "leeg" te
    // hebben.
    this.messages = this.messages.flatMap((m) => {
      if (!m.streaming) return [m]
      if (bare(m)) return []
      return [{ ...m, streaming: false, partial: true }]
    })
  }

  // dispose is idempotent en schrijft na de vlag niets meer: het is het laatste
  // wat er bij een unmount gebeurt, dus een late frame mag niets meer aanraken.
  dispose(): void {
    if (this.disposed) return
    this.#stopComparison()
    this.#active.clear()
    this.#drafts.clear()
    this.#comparisonRequests.clear()
    this.#pendingTurn = null
    this.disposed = true
    const controller = this.#controller
    this.#controller = null
    controller?.abort()
  }

  // ---- intern --------------------------------------------------------------

  #stale(signal: AbortSignal): boolean {
    return this.disposed || signal.aborted
  }

  #key(): string {
    this.#seq += 1
    return `m${this.#seq}`
  }

  #fromWire(m: WireMessage, index: number): ChatMessage {
    return {
      key: m.id && m.id !== '' ? m.id : `h${index}`,
      role: m.role === 'user' ? 'user' : 'assistant',
      pigeonId: this.selectedPigeonId,
      provenance: parseProvenance(m.provenance),
      // Oude rijen dragen geen kind en geen persona: die gelden als gewone
      // tekst van de duif, precies zoals ze altijd renderden.
      kind: m.kind === 'pigeon_answer' ? 'pigeon_answer' : 'text',
      persona: typeof m.persona === 'string' && m.persona !== '' ? m.persona : undefined,
      content: typeof m.content === 'string' ? m.content : '',
      sources: parseSources(m.sources),
      kbStatus: parseKBStatus(m.kbStatus),
      // Chips zijn turn-lokaal; de historie kent ze niet.
      tools: [],
      // De stopreden is bewaard, dus na herladen staat de melding er nog:
      // een antwoord dat halverwege stopte, blijft een antwoord dat halverwege
      // stopte. Een lege of natuurlijke reden is het normale geval.
      stopReason:
        typeof m.stopReason === 'string' && m.stopReason !== '' && m.stopReason !== 'natural' ? m.stopReason : undefined,
      // En de herkomst van de bronnen: wie een gesprek terugleest hoort
      // dezelfde markering te zien als wie het live volgde. Een
      // oudere server laat het veld weg; dan gelden de bronnen als gewone
      // treffers, zoals ze destijds ook getoond werden.
      related: m.related === true ? true : undefined,
      partial: m.partial === true,
      streaming: false,
    }
  }

  #failureFromFrame(frame: SSEFrame): ApiFailure {
    const failure: ApiFailure = { status: 0 }
    if (typeof frame.error === 'string') failure.reason = frame.error
    const message = bilingualFrom(frame.message)
    if (message) failure.message = message
    return failure
  }

  // #announce meldt één afgerond blok aan de schermlezer. Een blok zonder
  // inhoud verdwijnt en heeft dus niets te melden.
  #announce(key: string): void {
    const message = this.#find(key)
    if (!message || message.content === '') return
    this.#announceSeq += 1
    const voice = message.persona === FACILITATOR ? 'facilitator' : 'pigeon'
    this.announcements = [...this.announcements.slice(-3), { seq: this.#announceSeq, voice }]
  }

  // #silence trekt de afrondingsmelding in. Een lege lijst laat de statusregel
  // leeg, en een schermlezer leest een lege live-regel niet voor.
  #silence(): void {
    if (this.announcements.length) this.announcements = []
  }

  #find(key: string): ChatMessage | undefined {
    return this.messages.find((m) => m.key === key)
  }

  // #insertBefore zet een bericht vóór een bestaande rij; het duif-artifact en
  // het kader van de facilitator horen vóór de afsluitende rij te staan
  // (persist-volgorde).
  #insertBefore(key: string, message: ChatMessage): void {
    const at = this.messages.findIndex((m) => m.key === key)
    if (at < 0) {
      this.messages = [...this.messages, message]
      return
    }
    this.messages = [...this.messages.slice(0, at), message, ...this.messages.slice(at)]
  }

  // #beginPigeonWait en #endPigeonWait zetten de zichtbare wachtstand van het
  // duif-blok. Ze staan hier apart omdat er drie kanten aan zitten: de klok
  // (sinds wanneer), de losse wachtbubbel (zolang haar blok nog niet openstaat)
  // en het opruimen — een beurt die stopt of omvalt laat geen tikkende teller
  // achter.
  #beginPigeonWait(): void {
    this.pigeonWaitSince = Date.now()
    this.pigeonWaitOpen = true
  }

  #endPigeonWait(): void {
    this.pigeonWaitSince = 0
    this.pigeonWaitOpen = false
  }

  // #step zet de stapregel op de gemelde fase. Wat ervóór stond is daarmee
  // klaar: de server meldt een stap wanneer hij begint, dus de vorige is
  // voorbij.
  //
  // Een fase die al eerder langskwam krijgt geen tweede chip maar schuift naar
  // achteren. Dat gebeurt bij de brede zoekactie na een kennisgat: opzoeken,
  // verwant zoeken, opzoeken. Twee keer "Bronnen lezen" op één regel zou
  // suggereren dat er twee keer gezocht is in plaats van dat één stap doorliep.
  #step(phase: string): void {
    if (!isPhaseName(phase)) return
    const done = this.steps.filter((s) => s.phase !== phase).map((s) => (s.done ? s : { ...s, done: true }))
    this.steps = [...done, { phase, done: false }]
  }

  // #resetSteps maakt de stapregel leeg: aan het begin van een beurt, en bij
  // elke afronding ervan.
  #resetSteps(): void {
    if (this.steps.length) this.steps = []
  }

  // #chip werkt één tool-chip bij. Een `tool_result` zonder voorafgaand
  // `tool`-frame maakt zelf een chip aan; de wire mag ons niets schuldig
  // blijven.
  #chip(key: string, id: string, patch: { name: string; done: boolean }, error?: string): void {
    const tools = this.#find(key)?.tools ?? []
    const at = tools.findIndex((c) => c.id === id)
    if (at < 0) {
      this.#patch(key, { tools: [...tools, { id, name: patch.name, done: patch.done, error }] })
      return
    }
    const current = tools[at]
    const next: ToolChip = { ...current, done: current.done || patch.done }
    if (patch.name !== '') next.name = patch.name
    if (error !== undefined) next.error = error
    this.#patch(key, { tools: [...tools.slice(0, at), next, ...tools.slice(at + 1)] })
  }

  #patch(key: string, patch: Partial<ChatMessage>): void {
    this.messages = this.messages.map((m) => (m.key === key ? { ...m, ...patch } : m))
  }

  #append(key: string, delta: string): void {
    this.messages = this.messages.map((m) => (m.key === key ? { ...m, content: m.content + delta } : m))
  }

  // #close rondt de beurt af: élk open blok wordt gesetteld, zonder `done` is
  // wat er staat afgebroken, en een blok dat niets bevat verdwijnt (de melding
  // staat in de Banner). De facilitator kan meer dan één rij hebben (kader en
  // bemiddeling), dus ze gaan als lijst mee.
  #close(guidanceKeys: string[], sawDone: boolean, sawError: boolean, artifactKeys: string[]): void {
    // Elke afronding van een beurt — netjes, met fout of afgebroken — komt
    // hierlangs. Eén plek dus om de wachtstand en de stapregel op te ruimen.
    this.#endPigeonWait()
    this.#resetSteps()
    // Een beurt met een fout of zonder `done` is niet beantwoord, ook al stond
    // er al een blok: wat er kwam is afgebroken, en de foutbanner meldt het.
    // Dan geen "heeft geantwoord", ook niet voor een blok dat eerder in deze
    // beurt al gemeld was.
    const failed = sawError || !sawDone
    if (failed) this.#silence()
    const announce = (key: string): void => { if (!failed) this.#announce(key) }
    for (const artifactKey of artifactKeys) {
      const artifact = this.#find(artifactKey)
      if (!artifact) continue
      if (artifact.content === '') {
        this.messages = this.messages.filter((m) => m.key !== artifactKey)
        continue
      }
      if (artifact.streaming) {
        this.#patch(artifactKey, { streaming: false, partial: artifact.partial || !sawDone })
        announce(artifactKey)
      }
    }
    let carried: ToolChip[] = []
    let spoken: string | null = null
    for (const key of guidanceKeys) {
      const message = this.#find(key)
      if (!message) continue
      if (bare(message)) {
        // Een rij zonder woorden verdwijnt, maar de chips zijn de stappen van
        // deze beurt. Ze schuiven naar de dichtstbijzijnde rij waar de
        // facilitator wél iets zei: vooruit als die nog komt, anders terug naar
        // de vorige. Zo blijven ze niet aan een lege bubbel hangen.
        if (message.tools.length) {
          if (spoken) this.#patch(spoken, { tools: [...(this.#find(spoken)?.tools ?? []), ...message.tools] })
          else carried = [...carried, ...message.tools]
        }
        this.messages = this.messages.filter((m) => m.key !== key)
        continue
      }
      const tools = carried.length ? [...carried, ...message.tools] : message.tools
      carried = []
      spoken = key
      if (tools !== message.tools) this.#patch(key, { tools })
      // Een fout gevolgd door `done` blijft een afgebroken antwoord: beide
      // servermotoren sturen `error` en dán `done`.
      this.#patch(key, { streaming: false, partial: sawError || (!sawDone && message.content !== '') })
      announce(key)
    }
    // Geen `done` betekent dat de beurt niet is afgemaakt, of er nu al woorden
    // stonden of niet. Zonder melding staat de lezer met een half antwoord dat
    // niets over zichzelf zegt, en zonder knop om het opnieuw te proberen. De
    // server stuurt zelf een `error` met de reden vóór hij
    // afkapt; deze regel is de vangnetmelding voor elke stilte die hij niet
    // meer kon uitleggen (een weggevallen verbinding, een proxy die sluit).
    if (!sawDone && !this.error) this.error = { status: 0 }
  }

  // De sessielijst wordt lokaal bijgewerkt: de server heeft de sessie net
  // aangemaakt en een extra GET zou alleen maar een tweede waarheid opleveren.
  #rememberSession(id: string, pigeonId: string, lang: string): void {
    const now = new Date().toISOString()
    const previous = this.sessions.find((s) => s.id === id)
    this.sessions = [
      { id, pigeonId, lang, comparison: previous?.comparison, title: previous?.title ?? [...(this.messages.find((m) => m.role === 'user')?.content ?? '').replace(/\s+/g, ' ').trim()].slice(0, 96).join(''), createdAt: previous?.createdAt ?? now, updatedAt: now },
      ...this.sessions.filter((s) => s.id !== id),
    ]
  }
}
