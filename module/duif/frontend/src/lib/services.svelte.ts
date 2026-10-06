import { getContext } from 'svelte'
import { shareInFlight } from './api'
import type { ModuleContext } from '../index'
import { browserDevMode } from './access/devMode'
import { browserPorts } from './audio/browser'
import { ChatStore } from './chat/chatStore.svelte'
import { GraphStore } from './graph/graphStore.svelte'
import { browserGraphPorts } from './graph/ports'
import type { UiLocale } from './i18n.svelte'
import { RosterStore } from './shell/roster.svelte'
import { createBrowserLocale } from './shell/languagePreference'
import { KnowledgeStore } from './knowledge.svelte'
import { MeetingStore } from './meeting/meetingStore.svelte'
import { SourcePanelStore } from './sources/sourcePanel.svelte'

// Services zijn de per-mount-exemplaren: één locale, één chatstore, één
// vergaderstore. Ze leven in de mount-context (nooit op module-niveau), zodat
// een navigatie weg van /duif en terug een schone lei geeft en een tabwissel
// binnen de module de vergadersessie niet raakt.
export type Tab = 'chat' | 'meeting' | 'graph'

export interface HeldPlace {
  node: string | null
  pigeon: string | null
  session: string | null
}
export const NOTHING_HELD: HeldPlace = { node: null, pigeon: null, session: null }

// isTab hoort bij de function binding op Tabs.Root: bits-ui geeft een string
// terug, en alleen een tabblad dat wij kennen mag de schil verzetten.
export function isTab(value: unknown): value is Tab {
  return value === 'chat' || value === 'meeting' || value === 'graph'
}

// ShellState is de stand van de module-schil: welk tabblad open staat. Hij
// leeft in de services (niet in App.svelte) zodat een view een ander tabblad
// kan openen zonder prop-drilling — de bronkaart in de chat opent zo de graaf.
export class ShellState {
  tab = $state<Tab>('chat')
  previousTab = $state<'chat' | 'meeting'>('chat')
  previousHelp = $state(false)
  help = $state(false)
  signaling = $state(false)
  // dev: de ontwikkelaarschakelaar (?dev=1 of de bewaarde vlag). Alleen dan
  // toont de chat bouwbewijs zoals model, gedragscontract en versiehashes.
  dev = $state(browserDevMode())
  // heldPlace is wat er na een herlaad uit het adres terug moet komen maar
  // nog onderweg is: de knoop, de duif en het gesprek wachten op hun lijsten.
  // Zolang de lezer zelf niets anders koos, houdt het adres ze vast, zodat
  // een trage of mislukte lijst de plek niet uitwist
  // (lib/shell/workspacePlace.ts).
  heldPlace = $state.raw<HeldPlace>(NOTHING_HELD)

  navigate = (tab: Tab): void => {
    if (tab === 'graph' && this.tab !== 'graph') {
      this.previousTab = this.tab
      this.previousHelp = this.help
    }
    this.tab = tab
    this.help = false
  }

  back = (): void => { this.tab = this.previousTab; this.help = this.previousHelp }
  // onOpenInGraph wordt door createServices gevuld zodra er een graafstore is;
  // de chat kent die store niet en hoeft hem ook niet te kennen.
  onOpenInGraph: ((id: string) => void) | null = null

  // openInGraph: eerst de graaf laten laden, dán het tabblad wisselen, zodat
  // de laadstaat zichtbaar is in plaats van een leeg canvas.
  openInGraph = (id: string): void => {
    this.onOpenInGraph?.(id)
    this.navigate('graph')
  }
}

export interface Services {
  ctx: ModuleContext
  locale: UiLocale
  shell: ShellState
  knowledge: KnowledgeStore
  chat: ChatStore
  meeting: MeetingStore
  graph: GraphStore
  // roster is de naamlijst van de drie duiven uit personas.yaml: de enige
  // bron van duifnamen in de schil.
  roster: RosterStore
  // sources is de gedeelde bronlade: elk antwoord (chat én vergadering) opent
  // hem, App.svelte tekent hem.
  sources: SourcePanelStore
  dispose(): void
}

export const SERVICES = Symbol('duif.services')

export function createServices(ctx: ModuleContext): Services {
  const locale = createBrowserLocale()
  const shell = new ShellState()
  // De schil mount drie stores die elk GET /v1/duiven lezen; shareInFlight
  // maakt daar één aanvraag van (zie lib/api.ts).
  const fetcher: typeof fetch = shareInFlight((input, init) => globalThis.fetch(input, init))
  const chat = new ChatStore({ fetch: fetcher }, locale)
  const meeting = new MeetingStore(browserPorts(fetcher), locale)
  const roster = new RosterStore(fetcher, locale)
  const knowledge = new KnowledgeStore({
    fetch: fetcher,
    schedule: (fn, ms) => {
      const id = setTimeout(fn, ms)
      return () => clearTimeout(id)
    },
  })
  const graph = new GraphStore(browserGraphPorts(fetcher))
  const sources = new SourcePanelStore({ fetch: fetcher })
  // De schil kent de graafstore niet; de chat ook niet. Deze ene regel legt de
  // bronkaart en de plattegrond aan elkaar: eerst laten laden, dan pas van
  // tabblad wisselen (dat doet openInGraph).
  shell.onOpenInGraph = (id) => void graph.focus(id)
  let disposed = false
  return {
    ctx,
    locale,
    shell,
    knowledge,
    chat,
    meeting,
    graph,
    roster,
    sources,
    dispose() {
      if (disposed) return
      disposed = true
      // Elke store krijgt zijn eigen beurt: struikelt er één (een microfoon die
      // niet meer luistert, een kanaal dat al dicht is), dan blijven de andere
      // niet met een open verbinding achter. De schil moet kunnen afbreken —
      // daarom sterft de fout hier en niet in de unmount van de core-shell.
      for (const store of [meeting, knowledge, graph, chat, roster, sources]) {
        try {
          store.dispose()
        } catch {
          // een store die al niets meer kan, hoeft niets meer te doen
        }
      }
    },
  }
}

export function useServices(): Services {
  const s = getContext<Services | undefined>(SERVICES)
  if (!s) throw new Error('duif: services ontbreken in de mount-context')
  return s
}
