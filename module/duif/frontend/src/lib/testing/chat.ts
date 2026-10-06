// Gedeelde fixtures en hulpjes voor de chattests. De store-test en de
// weergavetest gebruiken dezelfde duivenlijst en dezelfde stroom-fake; twee
// kopieën gingen anders langzaam uit elkaar lopen.
import type { ModuleContext } from '../../index'
import type { Fetcher } from '../api'
import { ChatStore, type SessionRef } from '../chat/chatStore.svelte'
import type { GraphPorts } from '../graph/ports'
import { UiLocale } from '../i18n.svelte'
import { KnowledgeStore } from '../knowledge.svelte'
import { MeetingStore } from '../meeting/meetingStore.svelte'
import { SERVICES, type Services, ShellState } from '../services.svelte'
import { RosterStore } from '../shell/roster.svelte'
import { SourcePanelStore } from '../sources/sourcePanel.svelte'
import type { Source } from '../sources'
import { jsonResponse, type Route, scriptedFetch } from './fetch'
import { testGraphStore } from './graph'
import { fakePorts } from './meeting'

// Wat een test van de mount-context wil kunnen sturen. Alles is optioneel: de
// gewone componenttest geeft alleen een fetch mee.
export interface TestServicesOptions {
  ctx?: ModuleContext
  // locale en meeting: de vergadertests bouwen hun store al met een eigen
  // harnas en willen dezelfde exemplaren in de context zien.
  locale?: UiLocale
  meeting?: MeetingStore
  // graphPorts: een graaftest houdt de klok, de maat en de zichtbaarheid zelf
  // in de hand (fakeGraphPorts met runFrame en resize).
  graphPorts?: GraphPorts
}

// testServices bouwt de mount-context van de module met echte stores op fakes.
// Elke componenttest heeft hem nodig; zonder dit hulpje staat dezelfde opbouw
// (of, erger, een halve store achter een cast) in elk testbestand opnieuw.
// Eén kennisstore voor de schil én de graaf, precies zoals createServices hem
// deelt, en dezelfde koppeling van de bronkaart aan de plattegrond.
export function testServices(fetch: Fetcher, options: TestServicesOptions = {}): Services {
  const locale = options.locale ?? new UiLocale()
  const knowledge = new KnowledgeStore({ fetch, schedule: () => () => {} })
  const chat = new ChatStore({ fetch }, locale)
  const meeting = options.meeting ?? new MeetingStore(fakePorts(fetch), locale)
  const graph = testGraphStore(fetch, options.graphPorts)
  const sources = new SourcePanelStore({ fetch })
  const roster = new RosterStore(fetch, locale)
  const shell = new ShellState()
  shell.onOpenInGraph = (id) => void graph.focus(id)
  let disposed = false
  return {
    ctx: options.ctx ?? {},
    locale,
    shell,
    knowledge,
    graph,
    chat,
    meeting,
    roster,
    sources,
    dispose() {
      if (disposed) return
      disposed = true
      for (const store of [meeting, knowledge, graph, chat, roster, sources]) store.dispose()
    },
  }
}

// De contextvorm die render(..., { context }) verwacht.
export function serviceContext(services: Services): Map<unknown, unknown> {
  return new Map<unknown, unknown>([[SERVICES, services]])
}

export const PIGEONS_FIXTURE = {
  duiven: [
    {
      id: 'biological',
      name: { en: 'Knowledge Partner', nl: 'Kennispartner' },
      voiceReady: false,
      knowledgeBase: true,
      chat: true,
      meeting: true,
      // De kennispartner draait de pipeline: zij spreekt zelf.
      engine: 'pipeline',
      topics: [{ id: 'urban-ecology', label: { en: 'Urban ecology', nl: 'Stadsecologie' } }],
    },
    {
      id: 'individualist',
      name: { en: 'Stakeholder', nl: 'Stakeholder' },
      voiceReady: true,
      knowledgeBase: false,
      chat: true,
      meeting: true,
      // De stakeholder draait op de agent-motor: de facilitator
      // voert het woord en de duif antwoordt als eigen blok.
      engine: 'agent',
      topics: [],
    },
    {
      id: 'nonverbal',
      name: { en: 'Signaling Pigeon', nl: 'Signalerende duif' },
      voiceReady: false,
      knowledgeBase: false,
      chat: false,
      meeting: true,
      topics: [],
    },
  ],
  gedrag: { taalDefault: 'en' },
}

// SOURCE is de nieuwe wire-vorm zoals de UI hem geparsed ziet: met `id` (de
// knop naar de kennisgraaf) en een `ref` met citaat en bronpaper.
export const SOURCE: Source = {
  number: 1,
  type: 'destillaat',
  id: 'destillaat:9f1c',
  text: 'Pigeons nest on ledges.',
  ref: { quote: 'They prefer a ledge on the north side.', paper: 'lewald1987' },
}

// OLD_SOURCE is een bron uit een bericht van vóór de kennisgraaf: ander type,
// geen id, oud ref. Hij moet renderen zonder knop naar de graaf.
export const OLD_SOURCE: Source = {
  number: 1,
  type: 'standpunt',
  text: 'Pigeons nest on ledges.',
  ref: { file: 'notulen.md', doi: '10.5/y' },
}

// COMPARISON_QUESTION is de vraag die in een vergelijkgesprek twee keer
// voorkomt: gekopieerd uit het brongesprek en daarna opnieuw gesteld aan de
// tweede duif.
export const COMPARISON_QUESTION = 'Where do pigeons nest?'

// comparisonSession is een vergelijkgesprek zoals GET /chat/sessions het
// teruggeeft: de stakeholder antwoordt op een vraag uit het gesprek met de
// kennispartner. contextMessageIds noemt de rijen die meereisden, in
// leesvolgorde — hier de vraag en het antwoord dat zij al kreeg. De client telt
// ze om te weten waar de kopie ophoudt.
export function comparisonSession(contextMessageIds = ['src-question', 'src-answer'], patch: Partial<SessionRef> = {}): SessionRef {
  return {
    id: 'target', pigeonId: 'individualist', lang: 'en',
    createdAt: '2026-09-09T10:00:00Z', updatedAt: '2026-09-09T10:00:00Z',
    comparison: {
      groupId: 'src-question', sessionId: 'origin', pigeonId: 'biological',
      questionId: 'src-question', originalQuestion: COMPARISON_QUESTION, contextMessageIds,
    },
    ...patch,
  }
}

export function pigeonRoute(): Route {
  return { method: 'GET', match: '/duiven', respond: () => jsonResponse(PIGEONS_FIXTURE) }
}

// loadedRoster geeft de naamlijst met de duivenfixture er al in. Een
// componenttest die alleen een duifnaam nodig heeft hoeft daar geen route en
// geen wachtlus voor op te tuigen; de laad- en foutstand toetst de roster-test
// zelf (lib/shell/roster.test.ts).
export async function loadedRoster(locale: UiLocale): Promise<RosterStore> {
  const roster = new RosterStore(scriptedFetch([pigeonRoute()]).fetch, locale)
  await roster.load()
  return roster
}

// liveStream geeft een SSE-antwoord dat de test frame voor frame vult, zodat
// tussenstanden (bronnen vóór de eerste tekst, afbreken halverwege) toetsbaar
// zijn.
export function liveStream() {
  const enc = new TextEncoder()
  let ctrl!: ReadableStreamDefaultController<Uint8Array>
  const stream = new ReadableStream<Uint8Array>({
    start(c) {
      ctrl = c
    },
  })
  return {
    response: new Response(stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }),
    push(frame: unknown) {
      // Na een abort is de stroom al geannuleerd; een late push is dan een
      // no-op, precies zoals bij een echte server.
      try {
        ctrl.enqueue(enc.encode(`data: ${JSON.stringify(frame)}\n\n`))
      } catch {
        // stroom al gesloten
      }
    },
    close() {
      try {
        ctrl.close()
      } catch {
        // stroom al gesloten
      }
    },
  }
}
