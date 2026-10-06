import { API, type Fetcher } from './api'

// De stand van de kennisbank (import-niveau, niet turn-niveau: dat is
// kbStatus op de chat). Eén exemplaar per mount, gedeeld door chat en graaf,
// zodat "de duif leest nog" op beide tabbladen dezelfde waarheid is.
export const IMPORT_STATES = ['empty', 'importing', 'ready', 'incomplete', 'failed'] as const
export type ImportState = (typeof IMPORT_STATES)[number]

export interface KnowledgeStatus {
  state: ImportState
  kbId?: string
  counts?: Record<string, number>
  importedAt?: string
  skipped?: boolean
  reason?: string
}

export function isImportState(value: unknown): value is ImportState {
  return typeof value === 'string' && (IMPORT_STATES as readonly string[]).includes(value)
}

// parseKnowledgeStatus maakt van een ongetypeerde body een status; een
// onbekende stand is geen stand (null), geen gok.
export function parseKnowledgeStatus(body: unknown): KnowledgeStatus | null {
  if (!body || typeof body !== 'object') return null
  const raw = body as Record<string, unknown>
  if (!isImportState(raw.state)) return null
  const out: KnowledgeStatus = { state: raw.state }
  if (typeof raw.kbId === 'string') out.kbId = raw.kbId
  if (raw.counts && typeof raw.counts === 'object') {
    const counts: Record<string, number> = {}
    for (const [k, v] of Object.entries(raw.counts as Record<string, unknown>)) {
      if (typeof v === 'number' && Number.isFinite(v)) counts[k] = v
    }
    out.counts = counts
  }
  if (typeof raw.importedAt === 'string') out.importedAt = raw.importedAt
  if (typeof raw.skipped === 'boolean') out.skipped = raw.skipped
  if (typeof raw.reason === 'string') out.reason = raw.reason
  return out
}

// Zolang de import loopt kijkt de store elke vijf seconden opnieuw; daarna
// niet meer. Een setTimeout-keten (geen setInterval) zodat een trage server
// nooit overlappende peilingen krijgt.
export const KNOWLEDGE_POLL_MS = 5000

export interface KnowledgePorts {
  fetch: Fetcher
  schedule(fn: () => void, ms: number): () => void
}

export class KnowledgeStore {
  status = $state<KnowledgeStatus | null>(null)
  // failed: de statusroute zelf was onbereikbaar (niet: de import faalde).
  unreachable = $state(false)

  #gen = 0
  #cancel: (() => void) | null = null
  #disposed = false

  constructor(readonly ports: KnowledgePorts) {}

  get state(): ImportState | null {
    return this.status?.state ?? null
  }

  get importing(): boolean {
    return this.status?.state === 'importing'
  }

  // load haalt de stand op en plant, zolang er geïmporteerd wordt, de
  // volgende peiling. Een tweede load() maakt een lopende keten ongeldig.
  async load(): Promise<void> {
    if (this.#disposed) return
    const gen = ++this.#gen
    this.#cancel?.()
    this.#cancel = null
    let next: KnowledgeStatus | null = null
    let ok = false
    try {
      const res = await this.ports.fetch(API + '/knowledge/status')
      ok = res.ok
      if (res.ok) next = parseKnowledgeStatus(await res.json().catch(() => null))
    } catch {
      ok = false
    }
    if (gen !== this.#gen || this.#disposed) return
    this.unreachable = !ok
    if (next) this.status = next
    // De keten hangt aan de laatst bekende stand, niet aan dit ene antwoord:
    // een netwerkhikje of een onbegrijpelijke body laat `status` staan, en
    // zolang die 'importing' zegt blijven we kijken. Anders bevroor de banner
    // op "de duif leest nog" terwijl er niemand meer peilde.
    if (this.importing) {
      this.#cancel = this.ports.schedule(() => {
        this.#cancel = null
        void this.load()
      }, KNOWLEDGE_POLL_MS)
    }
  }

  dispose(): void {
    this.#disposed = true
    this.#gen++
    this.#cancel?.()
    this.#cancel = null
  }
}
