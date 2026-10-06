import { API, type Fetcher } from '../api'
import type { Source } from '../sources'
import { parseTrail, type Crumb } from './trail'

// De bronlade is één plek in de module: overal waar een antwoord staat kan een
// knop `sources.show(...)` roepen, en App.svelte tekent de lade. Zo hangt er
// geen bronnenlijst meer onder elk antwoord, en hoeft geen enkele weergave te
// weten hoe een bron eruitziet.
//
// De store is de hele laadlogica zonder DOM: welke bronnen, welke van die
// bronnen, of er een verduistering achter hoort, en het kruimelpad dat lui
// wordt opgehaald. De focus (erheen bij openen, terug bij sluiten) hoort bij
// het scherm en staat in SourceDrawer.svelte.

export interface SourcePanelPorts {
  fetch: Fetcher
}

export interface ShowOptions {
  // owner: welk antwoord de lade opende. Twee antwoorden kunnen allebei een
  // bron 1 hebben; zonder dit zou de verwijzing in het ene antwoord oplichten
  // terwijl de lezer die van het andere las.
  owner?: string
  // backdrop: of de lade de rest van het scherm verduistert. Een lopende
  // vergadering zet hem uit — zie SourceDrawer.svelte.
  backdrop?: boolean
}

export class SourcePanelStore {
  open = $state(false)
  owner = $state('')
  backdrop = $state(true)
  // De bronnen van één antwoord; ze worden altijd in hun geheel vervangen.
  sources = $state.raw<readonly Source[]>([])
  index = $state(0)
  // Het kruimelpad van de getoonde bron: domein › thema › subthema, met het
  // label nog tweetalig — de lade kiest de taal bij het tekenen. Leeg is de
  // normale stand — ook als het ophalen mislukte.
  trail = $state.raw<readonly Crumb[]>([])
  disposed = false

  // #gen ongeldigt een lopende ophaalactie: een traag antwoord voor bron 1 mag
  // het kruimelpad van bron 2 niet overschrijven, en na sluiten helemaal niets.
  #gen = 0
  #controller = new AbortController()

  constructor(private readonly ports: SourcePanelPorts) {}

  get current(): Source | null {
    return this.sources[this.index] ?? null
  }

  get total(): number {
    return this.sources.length
  }

  // showsSource beantwoordt de vraag van een citatieknop: sta ík open? Alleen
  // dat antwoord mag de verwijzing in de zin laten oplichten.
  showsSource(owner: string, number: number): boolean {
    return this.open && this.owner === owner && this.current?.number === number
  }

  // show opent de lade op de bron met dít nummer. Een nummer dat er niet is
  // (een verwijzing naar een bron die de server niet meestuurde) opent de
  // eerste bron in plaats van een lege lade.
  show(sources: readonly Source[], number: number, options: ShowOptions = {}): void {
    if (sources.length === 0) return
    const at = sources.findIndex((source) => source.number === number)
    this.sources = sources
    this.owner = options.owner ?? ''
    this.backdrop = options.backdrop !== false
    this.open = true
    this.#select(at < 0 ? 0 : at)
  }

  step(delta: number): void {
    if (!this.open) return
    const next = this.index + delta
    if (next < 0 || next >= this.sources.length) return
    this.#select(next)
  }

  hide(): void {
    if (!this.open) return
    this.open = false
    this.owner = ''
    this.sources = []
    this.index = 0
    this.trail = []
    this.#gen++
  }

  dispose(): void {
    if (this.disposed) return
    this.disposed = true
    this.#gen++
    this.#controller.abort()
  }

  #select(index: number): void {
    this.index = index
    this.trail = []
    void this.#loadTrail()
  }

  // Het kruimelpad is een extraatje, geen voorwaarde: mislukt het, dan staat
  // het er niet. Een foutbanner over een mislukte structuurvraag zou de lezer
  // afleiden van de bron die hij juist wél voor zich heeft.
  async #loadTrail(): Promise<void> {
    const gen = ++this.#gen
    const id = this.current?.id
    if (!id || this.disposed) return
    try {
      const res = await this.ports.fetch(`${API}/graph/nodes/${encodeURIComponent(id)}`, { signal: this.#controller.signal })
      if (gen !== this.#gen || this.disposed || !res.ok) return
      const body = (await res.json()) as unknown
      if (gen !== this.#gen || this.disposed) return
      this.trail = parseTrail(body)
    } catch {
      // afgebroken, geen netwerk, geen JSON: gewoon geen kruimelpad
    }
  }
}
