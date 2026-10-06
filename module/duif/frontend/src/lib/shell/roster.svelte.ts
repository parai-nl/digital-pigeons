import type { Fetcher } from '../api'
import type { UiLocale } from '../i18n.svelte'
import { fetchPigeons, type Pigeon } from '../pigeons'

// RosterStore is de énige naambron van de schil. De weergavenaam van
// een duif staat in `config/personas.yaml` en komt via GET /v1/duiven binnen;
// de frontend heeft er geen tweede, statische kopie meer van. Hernoemen is
// daarmee één regel in de serverconfiguratie, niet een zoektocht door zeven
// schermen.
//
// De lijst is bewust ongefilterd: de chatstore houdt alleen de pratende duiven
// over en de vergaderstore alleen de vergaderende, maar een historierij of een
// transcriptregel kan over elke duif gaan. Identiteit is vast, beschikbaarheid
// niet — die twee horen dus niet in dezelfde lijst.
export type RosterStatus = 'idle' | 'loading' | 'ready' | 'failed'

export class RosterStore {
  private readonly fetcher: Fetcher
  private readonly locale: UiLocale
  pigeons = $state<Pigeon[]>([])
  status = $state<RosterStatus>('idle')
  private disposed = false

  constructor(fetcher: Fetcher, locale: UiLocale) {
    this.fetcher = fetcher
    this.locale = locale
  }

  // load haalt de lijst één keer op. Een mislukte poging blijft herhaalbaar:
  // status 'failed' is geen eindstand, want de schil biedt elders (help, graaf)
  // al een herhaalknop en een tweede mount hoort gewoon opnieuw te proberen.
  load = async (): Promise<void> => {
    if (this.disposed || this.status === 'loading' || this.status === 'ready') return
    this.status = 'loading'
    const result = await fetchPigeons(this.fetcher)
    if (this.disposed) return
    if (!result.ok) {
      this.status = 'failed'
      return
    }
    this.pigeons = result.pigeons
    this.status = 'ready'
  }

  // name geeft de naam zoals de server hem kent. Zolang die naam er niet is,
  // zegt de schil dat eerlijk: een laadregel of een foutregel, nooit een oude
  // naam als stille terugval.
  name = (id: string): string => {
    const found = this.pigeons.find((p) => p.id === id)
    if (found) {
      const name = this.locale.pick(found.name, '')
      if (name) return name
    }
    if (this.status === 'failed') return this.locale.t('shell.nameUnavailable')
    if (this.status !== 'ready') return this.locale.t('shell.nameLoading')
    // Bekende lijst, onbekende duif: dat is een rij uit een ouder gesprek.
    return this.locale.t('chat.author.unknown')
  }

  // pending zegt of de naam nog onderweg is; schermen die een naam als kop
  // dragen kunnen daarop een rustige tussenstand tonen in plaats van een
  // titel die zo meteen verspringt.
  get pending(): boolean {
    return this.status !== 'ready' && this.status !== 'failed'
  }

  dispose(): void {
    this.disposed = true
  }
}
