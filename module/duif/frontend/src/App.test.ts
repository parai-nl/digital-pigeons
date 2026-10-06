// De wortel van de module: de taalkeuze en wat er met een latere
// serverconfiguratie gebeurt. De regel in één zin: een keuze van de gebruiker
// wint van elke default.
import { fireEvent, render, screen, within } from '@testing-library/svelte'
import { tick } from 'svelte'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import App from './App.svelte'
import { createServices, SERVICES } from './lib/services.svelte'
import { jsonResponse, scriptedFetch, type Route } from './lib/testing/fetch'
import { GRAPH_IDS, NODE_DETAIL, SUBTHEMA_CHILDREN, graphRoutes } from './lib/testing/graph'
import { meetingHarness, meetingRoutes } from './lib/testing/meeting'
import { restoreWorkspacePlace } from './lib/shell/workspacePlace'

async function settle(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
  await tick()
}

// De vergadering, de graaf en de uitleg laden lui (LazyMeeting, LazyGraph,
// LazyHelp). In vitest betekent dat: de eerste test die zo'n scherm opent,
// wacht binnen zijn eigen 5 s tot Vite die hele boom heeft getransformeerd, en
// op een belaste machine liep dat eroverheen. Daarom hier vooraf
// laden, buiten de testklok; de lazy import in de app vindt de module daarna in
// de cache. Het lui laden zelf toetsen de Lazy*-tests met hun eigen loader.
beforeAll(async () => {
  await Promise.all([
    import('./lib/meeting/MeetingView.svelte'),
    import('./lib/graph/GraphView.svelte'),
    import('./lib/shell/HelpView.svelte'),
  ])
}, 60_000)

// Deze tests renderen de hele module en lopen er als een facilitator doorheen:
// vergadering, chat, hulp, graaf en terug. Dat is hun omvang, en die kost tijd:
// elke getByRole rekent over de volledige app-DOM de toegankelijke naam en de
// zichtbaarheid uit (getComputedStyle in jsdom). Gemeten in de volle suite deed
// de langste test er 2,8 s over bij load 12 en 4,2 tot 6,7 s bij load 25 tot 28;
// één klik met getByRole kostte daar 0,4 tot 1,1 s. De standaard van 5 s is voor
// zo'n scenario te krap; 15 s houdt een echte hang nog steeds tegen.
vi.setConfig({ testTimeout: 15_000 })

const disposers: (() => void)[] = []
afterEach(() => {
  for (const dispose of disposers.splice(0)) dispose()
  localStorage.clear()
  document.documentElement.removeAttribute('lang')
  history.replaceState(null, '', '/')
})

function app(extraRoutes: Route[] = [], hash = '') {
  const scripted = scriptedFetch([
    ...extraRoutes,
    { method: 'GET', match: '/chat/sessions', respond: () => jsonResponse({ sessions: [] }) },
    ...meetingRoutes(),
    ...graphRoutes(),
  ])
  const harness = meetingHarness(scripted.fetch)
  // Eén exemplaar van de services: de test kijkt naar dezelfde schil als de
  // app, anders zou hij een tweede, lege stand toetsen.
  const services = harness.services()
  // Zoals mountModule: eerst de plek uit het adres, dan de schil.
  if (hash) restoreWorkspacePlace(services, hash)
  const rendered = render(App, { context: new Map([[SERVICES, services]]) })
  disposers.push(() => services.dispose())
  return { locale: harness.locale, store: harness.store, ports: harness.ports, services, unmount: rendered.unmount }
}

describe('App', () => {
  it('follows the language of the shell instead of offering a second picker', async () => {
    const { locale, store } = app()
    await settle()
    // De schil kiest de taal één keer, bovenin het harnas; de module tekent er
    // geen tweede kiezer naast.
    expect(screen.queryByRole('button', { name: 'App language' })).not.toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Chat' })).toBeInTheDocument()

    // Zo maakt de parai-core-schil haar taal bekend.
    document.documentElement.lang = 'nl'
    await settle()
    expect(locale.lang).toBe('nl')
    expect(locale.chosen).toBe(true)
    expect(screen.getByRole('tab', { name: 'Vergadering' })).toBeInTheDocument()

    // De gedragsconfig zegt taalDefault 'en'. Dat is een startwaarde, geen
    // keuze, en mag de taal van de schil dus niet terugdraaien.
    await store.loadPigeons()
    await settle()
    expect(locale.lang).toBe('nl')
  })

  it('lets go of the host-language watch when the module unmounts', async () => {
    const disconnect = vi.spyOn(MutationObserver.prototype, 'disconnect')
    const { locale, unmount } = app()
    await settle()
    document.documentElement.lang = 'nl'
    await settle()
    expect(locale.lang).toBe('nl')

    // De waarnemer hangt aan de mount: de schil van de core mag de module
    // afbreken zonder dat er iets aan het document blijft luisteren.
    const before = disconnect.mock.calls.length
    unmount()
    expect(disconnect.mock.calls.length).toBeGreaterThan(before)
    document.documentElement.lang = 'en'
    await settle()
    expect(locale.lang).toBe('nl')
    disconnect.mockRestore()
  })

  it('keeps the meeting language locked while a meeting runs and catches up afterwards', async () => {
    const { locale, store } = app()
    await settle()
    await fireEvent.click(screen.getByRole('tab', { name: 'Meeting' }))
    await fireEvent.click(await screen.findByTestId('seat-button'))
    await settle()
    expect(store.live).toBe(true)

    document.documentElement.lang = 'nl'
    await settle()
    expect(locale.lang).toBe('en')
    expect(screen.getByTestId('language-locked')).toHaveTextContent('The meeting language stays English')

    await fireEvent.click(screen.getByTestId('call-control-end'))
    await settle()
    expect(locale.lang).toBe('nl')
    expect(screen.queryByTestId('language-locked')).not.toBeInTheDocument()
  })

  it('links back to the public project site with a full page load, except during a meeting', async () => {
    const { store } = app()
    await settle()
    // "/" is de etalage, maar alleen voor een echte paginawissel: zonder
    // data-sveltekit-reload routeert de parai-core-schil de klik zelf naar haar
    // dashboard, en dat stuurt met één module meteen terug naar /duif.
    const site = screen.getByRole('link', { name: 'Project site' })
    expect(site).toHaveAttribute('href', '/')
    expect(site).toHaveAttribute('data-sveltekit-reload')

    await fireEvent.click(screen.getByRole('tab', { name: 'Meeting' }))
    await fireEvent.click(await screen.findByTestId('seat-button'))
    await settle()
    expect(store.live).toBe(true)
    expect(screen.queryByRole('link', { name: 'Project site' })).not.toBeInTheDocument()

    await fireEvent.click(screen.getByTestId('call-control-end'))
    await settle()
    expect(screen.getByRole('link', { name: 'Project site' })).toBeInTheDocument()
  })

  it('loads configuration once and does not reapply it on repeated view mounts', async () => {
    const { locale, store } = app()
    await settle()
    expect(locale.chosen).toBe(false)

    locale.lang = 'nl'
    await store.loadPigeons()
    expect(locale.lang).toBe('nl')
  })
})

describe('App tabs', () => {
  it('opens the knowledge graph as a secondary route and only draws while it is open', async () => {
    const { services } = app()
    await settle()
    expect(services.graph.visible).toBe(false)

    await fireEvent.click(screen.getByRole('button', { name: 'Knowledge graph' }))
    await settle()

    expect(services.shell.tab).toBe('graph')
    expect(services.graph.visible).toBe(true)
    // De vergaderorb tekent niet meer; de vergadering zelf loopt gewoon door.
    expect(services.meeting.orbVisible).toBe(false)
    expect(await screen.findByTestId('graph-view')).toBeInTheDocument()
    await screen.findAllByRole('treeitem')
    expect(screen.getAllByRole('treeitem').length).toBeGreaterThan(0)
  })

  it('lets the shell open a node in the graph and switch to it', async () => {
    const { services } = app()
    await settle()

    // Alle tabbladen blijven gemonteerd; wat telt is welke er openstaat en of
    // de plattegrond mag tekenen.
    expect(services.graph.visible).toBe(false)
    expect(services.graph.phase).toBe('idle')

    services.shell.openInGraph('destillaat:1')
    await settle()
    expect(services.shell.tab).toBe('graph')
    expect(services.graph.visible).toBe(true)
  })
})

describe('createServices', () => {
  it('wires a source card to the graph: first load the node, then switch tabs', () => {
    const services = createServices({})
    const asked: string[] = []
    const seenTab: string[] = []
    // De volgorde is de belofte: de graaf krijgt de vraag vóór de tabwissel,
    // zodat de laadstand zichtbaar is in plaats van een leeg canvas.
    services.graph.focus = async (id: string) => {
      asked.push(id)
      seenTab.push(services.shell.tab)
    }

    services.shell.openInGraph('destillaat:1')

    expect(asked).toEqual(['destillaat:1'])
    expect(seenTab).toEqual(['chat'])
    expect(services.shell.tab).toBe('graph')
    services.dispose()
  })

  it('lays down every store, also when one of them stumbles', () => {
    // Waarom: bij het afbreken van de mount mag een struikelende store de
    // andere niet met een open microfoon of kanaal laten staan — en de schil
    // van de core moet gewoon kunnen unmounten.
    const services = createServices({})
    const closed: string[] = []
    services.meeting.dispose = () => closed.push('meeting')
    services.knowledge.dispose = () => {
      throw new Error('kapot')
    }
    services.graph.dispose = () => closed.push('graph')
    services.chat.dispose = () => closed.push('chat')

    expect(() => services.dispose()).not.toThrow()
    expect(closed).toEqual(['meeting', 'graph', 'chat'])
  })
})

describe('Pigeon-first navigation', () => {
  it('offers all three forms and opens signaling setup without microphone capture', async () => {
    const { services, ports } = app()
    await settle()
    const choices = within(screen.getByRole('group', { name: 'Choose a pigeon' })).getAllByRole('button')
    expect(choices).toHaveLength(3)
    await fireEvent.click(choices[2])
    await settle()
    expect(choices[2]).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    expect(ports.mics).toHaveLength(0)
    await fireEvent.click(screen.getByRole('button', { name: 'Bring this pigeon to a meeting' }))
    await settle()
    expect(services.shell.tab).toBe('meeting')
    expect(services.meeting.pigeonId).toBe('nonverbal')
    expect(ports.mics).toHaveLength(0)
    expect(await screen.findByTestId('seat-button')).toBeEnabled()
  })

  it('preserves the exact composer and draft through help and graph, restoring help focus', async () => {
    const { services } = app()
    await settle()
    const input = screen.getByRole('textbox')
    await fireEvent.input(input, { target: { value: 'A question we are still writing' } })
    const help = screen.getByRole('button', { name: 'About & help' })
    help.focus()
    await fireEvent.click(help)
    await settle()
    // De uitleg is een venster over het gesprek heen: de focus gaat
    // naar het sluitkruis en het halve bericht blijft eronder staan.
    expect(screen.getByTestId('help-close')).toHaveFocus()
    expect(screen.getByRole('textbox')).toBe(input)
    await fireEvent.keyDown(window, { key: 'Escape' })
    await settle()
    expect(help).toHaveFocus()
    expect(screen.getByRole('textbox')).toBe(input)
    expect(input).toHaveValue('A question we are still writing')

    await fireEvent.click(screen.getByRole('button', { name: 'Knowledge graph' }))
    await settle()
    await fireEvent.click(screen.getByRole('button', { name: 'Back to Chat' }))
    await settle()
    expect(services.shell.tab).toBe('chat')
    expect(screen.getByRole('textbox')).toBe(input)
    expect(input).toHaveValue('A question we are still writing')
  })

  it('supports keyboard mode selection without opening the microphone', async () => {
    const { services, ports } = app()
    await settle()
    const chat = screen.getByRole('tab', { name: 'Chat' })
    chat.focus()
    await fireEvent.keyDown(chat, { key: 'ArrowRight' })
    await settle()
    expect(services.shell.tab).toBe('meeting')
    expect(screen.getByRole('tab', { name: 'Meeting' })).toHaveFocus()
    expect(ports.mics).toHaveLength(0)
  })

  it('keeps the active pigeon fixed and puts a global Stop on chat, help and graph', async () => {
    const { services, ports, store } = app([
      { method: 'POST', match: '/stop', respond: () => new Promise<Response>(() => {}) },
    ])
    await settle()
    await fireEvent.click(screen.getByRole('button', { name: 'Stakeholder' }))
    await fireEvent.click(screen.getByRole('tab', { name: 'Meeting' }))
    await fireEvent.click(await screen.findByTestId('seat-button'))
    await settle()
    expect(store.live).toBe(true)
    expect(ports.mic().enabled).toBe(true)
    expect(screen.getByTestId('language-locked')).toHaveTextContent('English')
    // Eén bedieningsset: op het vergaderscherm bedient MeetingActions, dus de
    // rail hoeft er niet naast te staan.
    expect(screen.queryByTestId('global-session')).not.toBeInTheDocument()
    await fireEvent.click(screen.getByTestId('call-control-mute'))
    await settle()
    expect(ports.mic().enabled).toBe(false)

    await fireEvent.click(screen.getByRole('tab', { name: 'Chat' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Knowledge Partner' }))
    await settle()
    expect(store.pigeonId).toBe('individualist')
    const rail = within(screen.getByTestId('global-session'))
    expect(rail.getByText('Stakeholder')).toBeInTheDocument()
    expect(rail.getByText('Microphone off')).toBeInTheDocument()

    await fireEvent.click(screen.getByRole('button', { name: 'About & help' }))
    await settle()
    // De rail blijft onder de dimlaag staan: de vergadering loopt zichtbaar
    // door. Bedienen kan pas weer als het venster dicht is.
    expect(screen.getByTestId('global-session')).toBeInTheDocument()
    await fireEvent.keyDown(window, { key: 'Escape' })
    await settle()
    await fireEvent.click(rail.getByRole('button', { name: 'Stop contribution' }))
    await settle()
    expect(store.stopPending).toBe(true)

    await fireEvent.click(screen.getByRole('button', { name: 'Knowledge graph' }))
    await settle()
    expect(services.shell.tab).toBe('graph')
    expect(rail.getByRole('button', { name: 'Stop contribution' })).toBeEnabled()

    // Beëindigen staat één keer: op het vergaderscherm.
    await fireEvent.click(screen.getByRole('button', { name: 'Back to Chat' }))
    await settle()
    await fireEvent.click(screen.getByRole('tab', { name: 'Meeting' }))
    await settle()
    await fireEvent.click(screen.getByTestId('call-control-end'))
    await settle()
    expect(ports.mic().stopped).toBe(true)
    expect(ports.context().closed).toBe(true)
    expect(store.live).toBe(false)
    expect(screen.queryByTestId('global-session')).not.toBeInTheDocument()
  })

  it('renders Dutch help and preserves original FAQ content when returning from the graph', async () => {
    const { locale, services } = app()
    await settle()
    locale.choose('nl')
    await settle()
    await fireEvent.click(screen.getByRole('button', { name: 'Uitleg & hulp' }))
    await settle()
    const question = await screen.findByText('Wat is kennis en wat is interpretatie?')
    await fireEvent.click(question)
    await fireEvent.click(screen.getByRole('button', { name: 'Verken de kennisgraaf' }))
    await settle()
    expect(services.shell.tab).toBe('graph')
    await fireEvent.click(screen.getByRole('button', { name: 'Terug naar Chat' }))
    await settle()
    expect(services.shell.help).toBe(true)
    expect(screen.getByText('Wat is kennis en wat is interpretatie?')).toBe(question)
  })
})

// Historie en "nieuw gesprek" gaan over het gesprek, dus staan ze bij
// de tabs die over het gesprek gaan; rechts blijft staan wat ernaast ligt.
// Onder "nieuw gesprek" staat geen keuze: de tabs kiezen de modus, dus de
// knop begint meteen een chat.
describe('Gespreksknoppen links naast de tabs', () => {
  it('zet Historie en Nieuw gesprek direct achter de tabs, vóór de weergaven', async () => {
    app()
    await settle()
    const order = [
      screen.getByRole('tab', { name: 'Chat' }),
      screen.getByRole('tab', { name: 'Meeting' }),
      screen.getByRole('button', { name: /^History/ }),
      screen.getByRole('button', { name: 'New conversation' }),
      screen.getByRole('button', { name: 'Knowledge graph' }),
      screen.getByRole('button', { name: 'About & help' }),
    ]
    for (let i = 1; i < order.length; i += 1) {
      expect(order[i - 1].compareDocumentPosition(order[i]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    }
  })

  // Eén klik: een nieuwe sessie voor de gekozen duif, met de cursor in het
  // invoerveld. (Het concept dat daarbij verloren zou gaan wordt nog steeds
  // eerst bevraagd; dat staat in ConversationActions.test.ts, waar de
  // bevestiging te bedienen is.)
  it('begint met één klik een nieuw gesprek met de cursor in het invoerveld', async () => {
    const { services } = app()
    await settle()
    services.chat.sessionId = 'een-lopend-gesprek'
    await settle()

    await fireEvent.click(screen.getByRole('button', { name: 'New conversation' }))
    await settle()

    expect(services.shell.tab).toBe('chat')
    expect(services.chat.sessionId).toBe('')
    expect(screen.getByRole('textbox')).toHaveFocus()
  })
})

// Een uitleg die het gesprek vervangt, laat niet zien dat je in een
// venster zit en ook niet hoe je eruit komt. Dit blok houdt vast wat een
// venster ervan maakt: de ervaring blijft eronder staan en er zijn vier
// uitgangen.
describe('De uitleg als venster over de ervaring', () => {
  it('opent als dialoog met de focus in het venster en sluit op vier manieren', async () => {
    const { services } = app()
    await settle()
    const help = screen.getByRole('button', { name: 'About & help' })
    help.focus()

    await fireEvent.click(help)
    await settle()
    expect(screen.getByRole('dialog', { name: 'About & help' })).toBeInTheDocument()
    expect(screen.getByTestId('help-close')).toHaveFocus()
    await screen.findByTestId('help-view')
    // Het gesprek verdwijnt niet: het schemert door de dimlaag.
    expect(screen.getByRole('textbox')).toBeInTheDocument()

    // 1. het sluitkruis rechtsboven
    await fireEvent.click(screen.getByTestId('help-close'))
    await settle()
    expect(services.shell.help).toBe(false)
    expect(help).toHaveFocus()

    // 2. Escape
    await fireEvent.click(help)
    await settle()
    await fireEvent.keyDown(window, { key: 'Escape' })
    await settle()
    expect(services.shell.help).toBe(false)
    expect(help).toHaveFocus()

    // 3. een klik naast het venster. De volgorde is die van een echte muis:
    // tussen indrukken en klikken verzet de browser zelf de focus, en jsdom
    // doet dat niet, dus zet de test hem hier weg. Wie het venster al bij het
    // indrukken sluit, ziet de herstelde focus daarna alsnog verdwijnen.
    await fireEvent.click(help)
    await settle()
    const overlay = screen.getByTestId('help-overlay')
    await fireEvent.pointerDown(overlay)
    await fireEvent.mouseDown(overlay)
    if (document.activeElement instanceof HTMLElement) document.activeElement.blur()
    await fireEvent.click(overlay)
    await settle()
    expect(services.shell.help).toBe(false)
    expect(help).toHaveFocus()

    // 4. de tekstlink onderaan de uitleg
    await fireEvent.click(help)
    await settle()
    const view = await screen.findByTestId('help-view')
    await fireEvent.click(within(view).getByRole('button', { name: /Back to the experience/ }))
    await settle()
    expect(services.shell.help).toBe(false)
    expect(help).toHaveFocus()
  })

  it('laat een klik binnen het venster met rust', async () => {
    const { services } = app()
    await settle()
    await fireEvent.click(screen.getByRole('button', { name: 'About & help' }))
    await settle()
    const view = await screen.findByTestId('help-view')
    const question = within(view).getByText('Is this a real pigeon speaking?')
    await fireEvent.pointerDown(question)
    await fireEvent.click(question)
    await settle()
    expect(services.shell.help).toBe(true)
    // Ook wie binnen het venster begint te slepen en buiten de rand loslaat,
    // verliest de uitleg niet: het indrukken bepaalt of dit een klik ernaast is.
    await fireEvent.pointerDown(question)
    await fireEvent.click(screen.getByTestId('help-overlay'))
    await settle()
    expect(services.shell.help).toBe(true)
  })

  // De uitleg blijft gemonteerd, dus een uitgeklapte vraag staat er bij
  // terugkomst nog steeds uit. De leespositie hoort juist wél van dit bezoek te
  // zijn: wie de uitleg opent, zoekt meestal de drie duiven, en die staan
  // bovenaan.
  it('houdt een uitgeklapte vraag vast maar begint weer bovenaan', async () => {
    app()
    await settle()
    const help = screen.getByRole('button', { name: 'About & help' })
    await fireEvent.click(help)
    await settle()
    const view = await screen.findByTestId('help-view')
    const answer = screen.getByText('What is knowledge, and what is interpretation?').closest('details') as HTMLDetailsElement
    answer.open = true
    view.scrollTop = 900

    await fireEvent.keyDown(window, { key: 'Escape' })
    await settle()
    await fireEvent.click(help)
    await settle()

    expect(screen.getByTestId('help-view')).toBe(view)
    expect(answer.open).toBe(true)
    expect(view.scrollTop).toBe(0)
  })

  // Het venster blijft gemonteerd en verbergt zich alleen. Een verborgen
  // video speelt in een browser gewoon door, met geluid; dichtdoen hoort dus
  // ook stil te zetten, langs elke uitgang.
  it('zet de uitlegvideo stil zodra het venster dichtgaat', async () => {
    const pause = vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
    app()
    await settle()
    const help = screen.getByRole('button', { name: 'About & help' })
    await fireEvent.click(help)
    await settle()
    const video = (await screen.findByTestId('help-video')).querySelector('video') as HTMLVideoElement
    // jsdom speelt niets af; de test zet de video zelf op "speelt".
    Object.defineProperty(video, 'paused', { configurable: true, value: false })
    pause.mockClear()

    await fireEvent.keyDown(window, { key: 'Escape' })
    await settle()
    expect(pause.mock.contexts).toContain(video)

    pause.mockClear()
    await fireEvent.click(help)
    await settle()
    expect(pause).not.toHaveBeenCalled()
    await fireEvent.click(screen.getByTestId('help-close'))
    await settle()
    expect(pause.mock.contexts).toContain(video)
    pause.mockRestore()
  })

  // De schil geeft door dat er een vergadering loopt; de help biedt de
  // video dan niet aan, want de microfoon staat open onder het venster.
  it('biedt de uitlegvideo niet aan zolang er een vergadering loopt', async () => {
    const { store } = app()
    await settle()
    await fireEvent.click(screen.getByRole('button', { name: 'Stakeholder' }))
    await fireEvent.click(screen.getByRole('tab', { name: 'Meeting' }))
    await fireEvent.click(await screen.findByTestId('seat-button'))
    await settle()
    expect(store.live).toBe(true)
    await fireEvent.click(screen.getByRole('button', { name: 'About & help' }))
    await settle()
    const section = await screen.findByTestId('help-video')
    expect(section.querySelector('video')).toBeNull()
    expect(within(section).getByRole('status')).toHaveTextContent('the microphone is on')
  })

  it('laat de vergadering doorlopen en zichtbaar blijven onder de dimlaag', async () => {
    const { services, store, ports } = app()
    await settle()
    await fireEvent.click(screen.getByRole('button', { name: 'Stakeholder' }))
    await fireEvent.click(screen.getByRole('tab', { name: 'Meeting' }))
    await fireEvent.click(await screen.findByTestId('seat-button'))
    await settle()
    expect(store.live).toBe(true)

    await fireEvent.click(screen.getByRole('button', { name: 'About & help' }))
    await settle()
    // De vergadering stopt niet, verliest haar orb niet en blijft in beeld:
    // het vergaderscherm zelf is de strook die onder de dimlaag doorschemert,
    // dus de rail komt er niet als tweede bedieningsset naast.
    expect(store.live).toBe(true)
    expect(store.orbVisible).toBe(true)
    expect(screen.getByTestId('meeting-view').closest('[hidden]')).toBeNull()
    expect(screen.queryByTestId('global-session')).not.toBeInTheDocument()
    expect(ports.mic().stopped).toBe(false)

    // Escape sluit het venster en zet de vergadering niet stil: de sneltoets
    // van STOP hoort bij het vergaderscherm, niet bij het sluiten van de uitleg.
    await fireEvent.keyDown(window, { key: 'Escape' })
    await settle()
    expect(services.shell.help).toBe(false)
    expect(store.stopPending).toBe(false)
    expect(store.live).toBe(true)
  })
})

describe('Language preference', () => {
  it('restores only a valid explicit language preference and prevents server defaults overriding it', () => {
    const services = createServices({})
    services.locale.choose('nl')
    services.dispose()
    const remounted = createServices({})
    expect(remounted.locale.lang).toBe('nl')
    expect(remounted.locale.chosen).toBe(true)
    expect(localStorage.getItem('duif.ui-language')).toBe('nl')
    expect(localStorage.length).toBe(1)
    remounted.dispose()
  })

  it('ignores an invalid stored language', () => {
    localStorage.setItem('duif.ui-language', 'de')
    const services = createServices({})
    expect(services.locale.lang).toBe('en')
    expect(services.locale.chosen).toBe(false)
    services.dispose()
  })
})

describe('de duifkiezer tijdens een lopende vergadering', () => {
  it('stelt de wisselvraag in plaats van niets te doen, en laat het gesprek met rust', async () => {
    const { store, services } = app()
    await settle()
    await fireEvent.click(screen.getByRole('button', { name: 'Stakeholder' }))
    await fireEvent.click(screen.getByRole('tab', { name: 'Meeting' }))
    await fireEvent.click(await screen.findByTestId('seat-button'))
    await settle()
    expect(store.live).toBe(true)

    await fireEvent.click(screen.getByRole('button', { name: 'Signaling Pigeon' }))
    await settle()
    expect(store.pendingSwitch).toBe('nonverbal')
    expect(screen.getByTestId('switch-dialog')).toBeInTheDocument()
    // De vergadering houdt haar duif tot de gebruiker antwoordt, en de kiezer
    // wijst de duif aan tafel aan — niet vast de gesprekskeuze.
    expect(store.pigeonId).toBe('individualist')
    expect(services.shell.signaling).toBe(false)
    expect(services.shell.tab).toBe('meeting')
    expect(screen.getByRole('button', { name: 'Stakeholder' })).toHaveAttribute('aria-pressed', 'true')

    await fireEvent.click(screen.getByTestId('switch-dialog-cancel'))
    await settle()
    expect(store.pigeonId).toBe('individualist')
    expect(services.chat.selectedPigeonId).toBe('individualist')
  })

  it('doet niets bij een duif die geen plaats aan de vergadertafel heeft', async () => {
    const { store, services } = app()
    await settle()
    await fireEvent.click(screen.getByRole('button', { name: 'Stakeholder' }))
    await fireEvent.click(screen.getByRole('tab', { name: 'Meeting' }))
    await fireEvent.click(await screen.findByTestId('seat-button'))
    await settle()
    expect(store.live).toBe(true)

    // De kennispartner vergadert niet mee: er valt niets te wisselen.
    await fireEvent.click(screen.getByRole('button', { name: 'Knowledge Partner' }))
    await settle()

    expect(store.pendingSwitch).toBe('')
    expect(screen.queryByTestId('switch-dialog')).toBeNull()
    expect(store.pigeonId).toBe('individualist')
    // En de kiezer verzet niet stilletjes het gesprek eronder: op het
    // vergaderscherm gaat hij over de duif aan tafel, of over niets.
    expect(services.chat.selectedPigeonId).toBe('individualist')
    expect(services.shell.signaling).toBe(false)
  })

  it('kiest op het gespreksscherm gewoon het gesprek, ook tijdens een vergadering', async () => {
    const { store, services } = app()
    await settle()
    await fireEvent.click(screen.getByRole('button', { name: 'Stakeholder' }))
    await fireEvent.click(screen.getByRole('tab', { name: 'Meeting' }))
    await fireEvent.click(await screen.findByTestId('seat-button'))
    await settle()
    await fireEvent.click(screen.getByRole('tab', { name: 'Chat' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Knowledge Partner' }))
    await settle()

    expect(store.pendingSwitch).toBe('')
    expect(store.pigeonId).toBe('individualist')
    expect(services.chat.selectedPigeonId).toBe('biological')
  })
})

describe('Shell meeting shortcut and session status', () => {
  it('labels the signaling shortcut truthfully when another pigeon already has a meeting', async () => {
    const { store } = app()
    await settle()
    await fireEvent.click(screen.getByRole('button', { name: 'Stakeholder' }))
    await fireEvent.click(screen.getByRole('tab', { name: 'Meeting' }))
    await fireEvent.click(await screen.findByTestId('seat-button'))
    await settle()
    await fireEvent.click(screen.getByRole('tab', { name: 'Chat' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Signaling Pigeon' }))
    await settle()
    expect(screen.queryByRole('button', { name: 'Bring this pigeon to a meeting' })).not.toBeInTheDocument()
    await fireEvent.click(screen.getByRole('button', { name: 'Go to the current meeting with Stakeholder' }))
    expect(store.pigeonId).toBe('individualist')
  })

  it('gives the global rail one plain status while microphone state stays separate', async () => {
    const { store } = app()
    await settle()
    await fireEvent.click(screen.getByRole('button', { name: 'Stakeholder' }))
    await fireEvent.click(screen.getByRole('tab', { name: 'Meeting' }))
    await fireEvent.click(await screen.findByTestId('seat-button'))
    await settle()
    await fireEvent.click(screen.getByRole('tab', { name: 'Chat' }))
    await fireEvent.click(screen.getByRole('button', { name: 'About & help' }))
    store.error = { status: 503, reason: 'llm_unavailable' }
    await settle()
    const rail = within(screen.getByTestId('global-session'))
    expect(rail.getByTestId('session-status')).toHaveTextContent('Something needs attention')
    expect(rail.getByText('Microphone on')).toBeInTheDocument()
    // Ook een stille degradatie blijft één stand, niet een elfde nuance.
    store.error = null
    store.audioGap = true
    await settle()
    expect(rail.getByTestId('session-status')).toHaveTextContent('Something needs attention')
    store.audioGap = false
    await settle()
    expect(rail.getByTestId('session-status')).toHaveTextContent('The pigeon is listening')
  })
})

// Een taalwissel mag de lezer niet van zijn plek jagen: in de graaf
// is hij juist een leeshandeling (de Nederlandse naast de Engelse
// samenvatting, met het vertaalvlaggetje ertussen). Binnen één mount verandert
// er dus niets dan de tekst — dit blok houdt dat vast voor alle vier de
// weergaven.
//
// De échte schil doet meer: een taalwissel daar herlaadt de pagina, waarna de module van voren af aan begint. Daarvoor staat
// de plek ook in het adresfragment (lib/shell/workspacePlace.ts); de laatste
// test hieronder speelt die herlaad na met een tweede mount.
describe('Een taalwissel laat de lezer staan waar hij staat', () => {
  // Dezelfde knoop als NODE_DETAIL, maar tweetalig en met een brontaal, zodat
  // het vlaggetje "vertaalde samenvatting" iets te zeggen heeft. Hij komt langs
  // twee wegen binnen — het detail én de kinderen van het subthema die focus()
  // uitklapt — en moet er op allebei hetzelfde uitzien.
  const BILINGUAL_NODE = {
    id: GRAPH_IDS.destillaatOne,
    type: 'destillaat',
    label: { nl: 'Voeren houdt de kolonie groot', en: 'Feeding keeps the colony large' },
    metadata: {
      text: {
        nl: 'Voeren houdt de kolonie groot en gezond.',
        en: 'Feeding keeps the colony large and healthy.',
      },
    },
    sourceLanguage: 'nl',
  }
  const bilingualRoutes: Route[] = [
    { method: 'GET', match: /\/graph\/nodes\/[^/]+$/, respond: () => jsonResponse({ ...NODE_DETAIL, node: BILINGUAL_NODE }) },
    {
      method: 'GET',
      match: encodeURIComponent(GRAPH_IDS.subthema) + '/children',
      respond: () => jsonResponse({ ...SUBTHEMA_CHILDREN, nodes: [BILINGUAL_NODE, SUBTHEMA_CHILDREN.nodes[1]] }),
    },
  ]

  it('houdt het destillaat open en wisselt alleen de tekst en het vertaalvlaggetje', async () => {
    const { services } = app(bilingualRoutes)
    await settle()
    services.shell.openInGraph(GRAPH_IDS.destillaatOne)
    await settle()
    await settle()
    expect(services.shell.tab).toBe('graph')
    expect(await screen.findByTestId('graph-view')).toBeInTheDocument()
    await settle()
    expect(services.graph.selected).toBe(GRAPH_IDS.destillaatOne)
    expect(screen.getByTestId('graph-detail-title')).toHaveTextContent('Feeding keeps the colony large and healthy.')
    expect(screen.getByTestId('graph-translated')).toBeInTheDocument()

    // Zo maakt de schil haar taalwissel bekend.
    document.documentElement.lang = 'nl'
    await settle()

    expect(services.shell.tab).toBe('graph')
    expect(services.shell.help).toBe(false)
    expect(services.graph.selected).toBe(GRAPH_IDS.destillaatOne)
    expect(services.graph.trail.map((node) => node.id)).toEqual([
      GRAPH_IDS.domein,
      GRAPH_IDS.thema,
      GRAPH_IDS.subthema,
      GRAPH_IDS.destillaatOne,
    ])
    expect(screen.getByTestId('graph-detail-title')).toHaveTextContent('Voeren houdt de kolonie groot en gezond.')
    // De knoop stáát in het Nederlands: dan is er niets vertaald.
    expect(screen.queryByTestId('graph-translated')).not.toBeInTheDocument()
  })

  it('laat een half getypte vraag in het gesprek staan', async () => {
    const { services } = app()
    await settle()
    const input = screen.getByRole('textbox')
    await fireEvent.input(input, { target: { value: 'Een vraag die nog niet af is' } })
    await settle()

    document.documentElement.lang = 'nl'
    await settle()

    expect(services.shell.tab).toBe('chat')
    expect(services.chat.draft).toBe('Een vraag die nog niet af is')
    expect(screen.getByRole('textbox')).toBe(input)
    expect(input).toHaveValue('Een vraag die nog niet af is')
  })

  it('zet de lezer na de herlaad van de schil terug bij hetzelfde destillaat', async () => {
    const first = app(bilingualRoutes)
    await settle()
    first.services.shell.openInGraph(GRAPH_IDS.destillaatOne)
    await settle()
    await settle()
    expect(first.services.graph.selected).toBe(GRAPH_IDS.destillaatOne)
    const hash = location.hash
    expect(new URLSearchParams(hash.slice(1)).get('node')).toBe(GRAPH_IDS.destillaatOne)

    // De herlaad: de oude mount gaat weg, de taal is gewisseld, een verse
    // mount leest alleen nog het adres.
    first.unmount()
    first.services.dispose()
    document.documentElement.lang = 'nl'
    const second = app(bilingualRoutes, hash)
    await settle()
    await settle()
    expect(second.services.shell.tab).toBe('graph')
    expect(await screen.findByTestId('graph-view')).toBeInTheDocument()
    await settle()
    expect(second.services.graph.selected).toBe(GRAPH_IDS.destillaatOne)
    expect(screen.getByTestId('graph-detail-title')).toHaveTextContent('Voeren houdt de kolonie groot en gezond.')
    expect(screen.getByRole('button', { name: 'Terug naar Chat' })).toBeInTheDocument()
  })

  it('blijft op het vergaderscherm en op de hulp staan', async () => {
    const { services, locale } = app()
    await settle()
    await fireEvent.click(screen.getByRole('tab', { name: 'Meeting' }))
    await settle()

    document.documentElement.lang = 'nl'
    await settle()
    expect(services.shell.tab).toBe('meeting')
    expect(locale.lang).toBe('nl')
    expect(screen.getByRole('tab', { name: 'Vergadering' })).toHaveAttribute('aria-selected', 'true')

    await fireEvent.click(screen.getByRole('button', { name: 'Uitleg & hulp' }))
    await settle()
    document.documentElement.lang = 'en'
    await settle()
    expect(services.shell.help).toBe(true)
    expect(services.shell.tab).toBe('meeting')
  })
})

