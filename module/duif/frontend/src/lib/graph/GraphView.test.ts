// De graafweergave opent als kaart. jsdom tekent niet, dus het canvas laat zich
// hier alleen op zijn tellingen aanspreken (wat er getekend wordt staat in
// renderer.test.ts) — de rest gaat door de omtrek, de knoppenbalk en het
// leespaneel, en dat is precies de kant die het toetsenbord en een schermlezer
// ook krijgen.
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { tick } from 'svelte'
import { describe, expect, it } from 'vitest'
import { serviceContext, testServices } from '../testing/chat'
import { jsonResponse, scriptedFetch, type Route } from '../testing/fetch'
import { fakeGraphPorts, GRAPH_IDS, graphRoutes, OVERVIEW_FIXTURE, SUBTHEMA_CHILDREN } from '../testing/graph'
import GraphView from './GraphView.svelte'
import type { GraphStore } from './graphStore.svelte'
import { COMPACT_WIDTH } from './renderer'

// Dezelfde maten als het stijlblok van GraphView: de scheiding en het
// leespaneel naast de kaart.
const SPLIT_WIDTH = 10
const READING_WIDTH = 416

async function settle(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
  await tick()
}

// De mount-context komt uit dezelfde bouwer als de rest van de module: één
// KnowledgeStore voor de schil én de graaf, precies zoals createServices hem
// deelt. Alleen de ports zijn van deze test — de klok, de maat en de
// zichtbaarheid van het canvas gaan hier met de hand.
function view(extra: Route[] = []) {
  const scripted = scriptedFetch([...extra, ...graphRoutes()])
  const ports = fakeGraphPorts(scripted.fetch)
  const services = testServices(scripted.fetch, { graphPorts: ports })
  // App zet deze vlag bij een tabwissel; hier is de graaftab de tab.
  services.graph.visible = true
  render(GraphView, { context: serviceContext(services) })
  return { services, graph: services.graph, locale: services.locale, scripted, ports }
}

function items(): HTMLElement[] {
  return screen.getAllByTestId('tree-item')
}

// tabstops is alles in de omtrek dat met Tab bereikbaar is; het ARIA-boompatroon
// staat er precies één toe.
function tabstops(): HTMLElement[] {
  const tree = screen.getByTestId('graph-tree')
  return [...tree.querySelectorAll<HTMLElement>('[tabindex="0"]')]
}

function itemFor(id: string): HTMLElement {
  const found = items().find((el) => el.dataset.id === id)
  if (!found) throw new Error('geen boomrij voor ' + id)
  return found
}

function canvas(): HTMLElement {
  return screen.getByTestId('graph-canvas')
}

describe('GraphView opens as a map', () => {
  // Kaart-eerst: er staat een plattegrond met de domeinen én hun thema's, niet
  // een leeslijst. Dat kost geen extra verzoek — de banden domein → thema kwamen
  // met de overview mee.
  it('shows the domains with their themes on the map, without a second request', async () => {
    const { scripted } = view()
    await settle()

    expect(canvas()).toHaveAttribute('data-node-count', '2')
    expect(Number(canvas().dataset.edgeCount)).toBeGreaterThan(0)
    expect(scripted.calls.filter((c) => c.url.includes('/children'))).toHaveLength(0)
    // De kaart is de standaard; de lijst is het alternatief.
    expect(screen.getByTestId('graph-view-map')).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByTestId('graph-view-list')).toHaveAttribute('aria-pressed', 'false')
  })

  // Het toegankelijke alternatief staat er meteen naast: dezelfde graaf, met het
  // toetsenbord te bedienen en voor te lezen.
  it('keeps a keyboard-reachable outline of the same graph beside it', async () => {
    view()
    await settle()

    const tree = screen.getByRole('tree')
    expect(tree).toHaveAttribute('aria-label')
    const rows = within(tree).getAllByRole('treeitem')
    const top = rows.filter((el) => el.getAttribute('aria-level') === '1')
    expect(top.map((el) => el.dataset.id)).toEqual([GRAPH_IDS.domein])
    expect(top[0]).toHaveAttribute('aria-setsize', '1')
    expect(top[0]).toHaveAttribute('aria-posinset', '1')
    expect(top[0]).toHaveAttribute('aria-expanded', 'true')
    expect(itemFor(GRAPH_IDS.thema)).toHaveAttribute('aria-level', '2')
    // Eén tabstop: de rest staat op -1 (roving tabindex).
    expect(tabstops()).toHaveLength(1)
  })

  it('gives the outline a hint a screen reader can read out', async () => {
    view()
    await settle()
    const tree = screen.getByRole('tree')
    const hint = tree.getAttribute('aria-describedby')
    expect(hint).toBeTruthy()
    expect(document.getElementById(hint ?? '')).toHaveTextContent('arrow keys')
  })

  // Dezelfde zin stond boven én onder de boom. Eén keer is genoeg, en de plek
  // waar hij helpt is vóór je begint te lopen.
  it('says how to walk the tree once, not above and below it', async () => {
    view()
    await settle()
    const said = screen.getAllByText(/arrow keys to walk the tree/i)
    expect(said).toHaveLength(1)
    expect(screen.getByRole('tree').getAttribute('aria-describedby')).toBe(said[0].id)
  })

  // Kleur zegt bij welk thema iets hoort, vorm wát het is en het icoontje waar
  // het over gaat. Dat alfabet stond alleen op de kaart; de boom en het
  // leespaneel spreken het nu ook.
  it('carries the map\u2019s shape, colour family and glyph into the tree and the panel', async () => {
    const { graph } = view()
    await settle()
    const mark = itemFor(GRAPH_IDS.thema).querySelector<HTMLElement>('.tree-mark')
    expect(mark?.dataset.glyph).toBe(graph.iconOf(GRAPH_IDS.thema))
    expect(mark?.dataset.shape).toBe('disc')
    expect(mark?.getAttribute('style')).toContain('oklch')

    await fireEvent.click(itemFor(GRAPH_IDS.thema))
    await settle()
    const swatch = screen.getByTestId('graph-detail').querySelector<HTMLElement>('.detail-swatch')
    expect(swatch?.dataset.glyph).toBe(graph.iconOf(GRAPH_IDS.thema))
    expect(swatch?.dataset.shape).toBe('disc')
  })

  // De kolom stond op elf tot vijftien rem en was niet te verzetten; namen van
  // domeinen en thema's braken daar middenin af.
  it('lets the keyboard widen the tree column and put it back', async () => {
    view()
    await settle()
    const split = () => screen.getByTestId('graph-split')
    const workspace = () => split().closest<HTMLElement>('.graph-workspace')
    const start = Number(split().getAttribute('aria-valuenow'))
    expect(start).toBeGreaterThan(240)
    // De maat wordt hardop genoemd met zijn eenheid; een kaal getal zegt een
    // schermlezer niets.
    expect(split().getAttribute('aria-valuetext')).toBe(start + ' pixels')

    await fireEvent.keyDown(split(), { key: 'ArrowRight' })
    await settle()
    const wider = Number(split().getAttribute('aria-valuenow'))
    expect(wider).toBeGreaterThan(start)
    expect(workspace()?.style.getPropertyValue('--graph-tree-width')).toBe(wider + 'px')
    expect(split().getAttribute('aria-valuetext')).toBe(wider + ' pixels')

    await fireEvent.keyDown(split(), { key: 'End' })
    await settle()
    expect(Number(split().getAttribute('aria-valuenow'))).toBe(Number(split().getAttribute('aria-valuemax')))

    await fireEvent.keyDown(split(), { key: 'Home' })
    await settle()
    expect(Number(split().getAttribute('aria-valuenow'))).toBe(Number(split().getAttribute('aria-valuemin')))

    // Enter zet de standaardmaat terug; zo begint de volgende lezer weer
    // gewoon.
    await fireEvent.keyDown(split(), { key: 'Enter' })
    await settle()
    expect(Number(split().getAttribute('aria-valuenow'))).toBe(start)

    // En met de muis: dubbelklikken doet hetzelfde als Enter.
    await fireEvent.keyDown(split(), { key: 'End' })
    await settle()
    expect(Number(split().getAttribute('aria-valuenow'))).not.toBe(start)
    await fireEvent.dblClick(split())
    await settle()
    expect(Number(split().getAttribute('aria-valuenow'))).toBe(start)
  })

  // Een boom die zó breed wordt dat de kaart onder de tekstgrens zakt, levert
  // een tekening zonder één naam op. De bovengrens rekent daarom met de ruimte
  // die er werkelijk is, niet met een vast getal.
  it('never lets the tree squeeze the map below its text threshold', async () => {
    view()
    await settle()
    const split = screen.getByTestId('graph-split')
    const workspace = split.closest<HTMLElement>('.graph-workspace')
    if (workspace) Object.defineProperty(workspace, 'clientWidth', { value: 1366, configurable: true })

    await fireEvent.keyDown(split, { key: 'End' })
    await settle()
    const tree = Number(split.getAttribute('aria-valuenow'))
    expect(Number(split.getAttribute('aria-valuemax'))).toBe(tree)
    // Wat de kaart overhoudt: het scherm min de boom, de scheiding en het
    // leespaneel. Dat moet ruim boven COMPACT_WIDTH liggen.
    expect(1366 - tree - SPLIT_WIDTH - READING_WIDTH).toBeGreaterThanOrEqual(COMPACT_WIDTH)

    await fireEvent.keyDown(split, { key: 'Enter' })
    await settle()
  })

  it('keeps the width the reader set when the graph is opened again', async () => {
    view()
    await settle()
    await fireEvent.keyDown(screen.getByTestId('graph-split'), { key: 'ArrowRight' })
    await settle()
    const wider = Number(screen.getByTestId('graph-split').getAttribute('aria-valuenow'))

    cleanup()
    view()
    await settle()
    expect(Number(screen.getByTestId('graph-split').getAttribute('aria-valuenow'))).toBe(wider)

    await fireEvent.keyDown(screen.getByTestId('graph-split'), { key: 'Enter' })
    await settle()
  })

  it('puts the outline on the stage when the reader asks for the list', async () => {
    const { graph, ports } = view()
    await settle()
    ports.runFrame()
    const drawn = ports.frames
    expect(drawn).toBeGreaterThan(0)

    await fireEvent.click(screen.getByTestId('graph-view-list'))
    await settle()
    expect(canvas().closest('section')).toHaveAttribute('hidden')
    // Een kaart die niemand ziet, tekent niet.
    ports.runFrame()
    expect(ports.frames).toBe(drawn)
    expect(screen.getByRole('tree')).toBeInTheDocument()

    await fireEvent.click(screen.getByTestId('graph-view-map'))
    await settle()
    ports.runFrame()
    expect(ports.frames).toBeGreaterThan(drawn)
    // De tak en de keuze overleven het wisselen.
    expect(graph.revealed.has(GRAPH_IDS.domein)).toBe(true)
  })

  // De schil draagt de terugknop en zegt al waar je bent; een tweede grote kop
  // erboven kostte alleen hoogte die de kaart beter kan gebruiken.
  it('keeps one compact heading in the toolbar and no back button of its own', async () => {
    view()
    await settle()
    expect(screen.queryByTestId('graph-back')).toBeNull()
    expect(screen.queryByRole('button', { name: /Back to the conversation/ })).toBeNull()
    const heading = screen.getByRole('heading', { name: /Explore the material behind a contribution/ })
    expect(heading.closest('.graph-toolbar')).not.toBeNull()
  })

  it('gives every column its own scroll instead of scrolling the page', async () => {
    view()
    await settle()
    expect(screen.getByTestId('graph-view').className).toContain('h-full')
    expect(screen.getByTestId('graph-view').className).toContain('overflow-hidden')
    expect(screen.getByRole('tree').className).toContain('overflow-y-auto')
  })

  // De plattegrond kreeg in de echte shell de hoogte van zijn buurman: het canvas
  // stond in de stroom, dus zijn intrinsieke maat duwde de kolom groter, de
  // waarnemer meldde die grotere maat, en zo rekte het beeld zich tot een strook
  // uit. jsdom rekent geen opmaak, dus dit toetst het mechanisme.
  it('keeps the canvas out of the flow so it cannot grow its own column', async () => {
    const { ports } = view()
    await settle()
    const host = canvas()
    expect(host.querySelector('canvas')?.className).toContain('absolute')
    expect(host.querySelector('canvas')?.className).toContain('inset-0')
    expect(host.className).toContain('overflow-hidden')
    expect(ports.observed).toBe(host)
  })
})

describe('drill-down, one level per press', () => {
  it('reveals exactly the level below, never a grandchild', async () => {
    view()
    await settle()
    const before = Number(canvas().dataset.nodeCount)

    await fireEvent.click(itemFor(GRAPH_IDS.thema))
    await settle()
    // Het thema geeft zijn subthema prijs — en niets van het niveau daaronder.
    expect(itemFor(GRAPH_IDS.subthema)).toHaveAttribute('aria-level', '3')
    expect(items().some((el) => el.dataset.id === GRAPH_IDS.destillaatOne)).toBe(false)
    expect(Number(canvas().dataset.nodeCount)).toBe(before + 1)

    await fireEvent.click(itemFor(GRAPH_IDS.subthema))
    await settle()
    expect(itemFor(GRAPH_IDS.destillaatOne)).toHaveAttribute('aria-level', '4')
    expect(items().some((el) => el.dataset.id === GRAPH_IDS.quote)).toBe(false)
  })

  // Beide standpunten komen uit hetzelfde bronpaper: in de omtrek staat het
  // dan onder elk van de twee, op de kaart blijft het één knoop.
  async function openBothFindings(graph: GraphStore): Promise<void> {
    await graph.expand(GRAPH_IDS.thema)
    await graph.expand(GRAPH_IDS.subthema)
    await graph.expand(GRAPH_IDS.destillaatOne)
    await graph.expand(GRAPH_IDS.destillaatTwo)
    await settle()
  }

  it('draws a source once on the map even though it hangs under two findings', async () => {
    const { graph } = view()
    await settle()
    await openBothFindings(graph)
    expect(items().filter((el) => el.dataset.id === GRAPH_IDS.interview)).toHaveLength(2)
    // Domein, thema, subthema, twee standpunten, een citaat en één bronpaper.
    expect(canvas()).toHaveAttribute('data-node-count', '7')
  })

  it('closes a level again without losing what was loaded', async () => {
    const { graph, scripted } = view()
    await settle()
    await fireEvent.click(itemFor(GRAPH_IDS.thema))
    await settle()
    await fireEvent.click(itemFor(GRAPH_IDS.subthema))
    await settle()
    const requests = scripted.calls.filter((c) => c.url.includes('/children')).length

    await fireEvent.keyDown(itemFor(GRAPH_IDS.subthema), { key: 'ArrowLeft' })
    await settle()
    expect(graph.revealed.has(GRAPH_IDS.subthema)).toBe(false)
    expect(items().some((el) => el.dataset.id === GRAPH_IDS.destillaatOne)).toBe(false)

    await fireEvent.keyDown(itemFor(GRAPH_IDS.subthema), { key: 'ArrowRight' })
    await settle()
    expect(itemFor(GRAPH_IDS.destillaatOne)).toBeInTheDocument()
    // Opnieuw openen kost geen tweede verzoek: de kinderen lagen er al.
    expect(scripted.calls.filter((c) => c.url.includes('/children')).length).toBe(requests)
  })

  it('walks the outline with the arrow keys', async () => {
    view()
    await settle()
    const first = itemFor(GRAPH_IDS.domein)
    first.focus()

    await fireEvent.keyDown(first, { key: 'ArrowDown' })
    await settle()
    expect(document.activeElement).toBe(itemFor(GRAPH_IDS.thema))

    await fireEvent.keyDown(itemFor(GRAPH_IDS.thema), { key: 'ArrowLeft' })
    await settle()
    expect(document.activeElement).toBe(itemFor(GRAPH_IDS.domein))

    await fireEvent.keyDown(itemFor(GRAPH_IDS.domein), { key: 'End' })
    await settle()
    expect(document.activeElement).toBe(items().at(-1))

    await fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'Home' })
    await settle()
    expect(document.activeElement).toBe(itemFor(GRAPH_IDS.domein))
  })

  // Een bronpaper hangt onder elk standpunt dat eruit komt, dus het kan twee
  // keer in de omtrek staan. De tabstop hoort te blijven staan waar de lezer
  // hem zette, niet naar de eerste padkopie te springen.
  it('leaves the tab stop in the copy the reader is standing in', async () => {
    const { graph } = view()
    await settle()
    await openBothFindings(graph)

    const copies = items().filter((el) => el.dataset.id === GRAPH_IDS.interview)
    expect(copies).toHaveLength(2)
    const second = copies[1].dataset.key
    await fireEvent.click(copies[1])
    await settle()

    const stops = tabstops()
    expect(stops).toHaveLength(1)
    expect(stops[0].dataset.key).toBe(second)
  })

  it('offers "load more" while the server has more children', async () => {
    const { graph } = view([
      {
        method: 'GET',
        match: '/children',
        respond: () =>
          jsonResponse({
            nodes: [SUBTHEMA_CHILDREN.nodes[0]],
            relationships: SUBTHEMA_CHILDREN.relationships.slice(0, 1),
            truncated: true,
            total: 150,
            nextOffset: 1,
          }),
      },
    ])
    await settle()
    await fireEvent.click(itemFor(GRAPH_IDS.thema))
    await settle()
    await fireEvent.click(itemFor(GRAPH_IDS.subthema))
    await settle()

    // aria-setsize noemt het servertotaal, niet wat er binnen is (APG).
    expect(itemFor(GRAPH_IDS.destillaatOne)).toHaveAttribute('aria-setsize', '150')
    const more = screen.getByTestId('load-more')
    expect(more).toHaveAttribute('data-id', GRAPH_IDS.subthema)
    expect(screen.getByTestId('tree-status')).toHaveTextContent('1 of 150 loaded')
    // APG: één tabstop in de hele boom, ook met de knop erin.
    expect(more).toHaveAttribute('tabindex', '-1')
    expect(tabstops()).toHaveLength(1)

    await fireEvent.click(more)
    await settle()
    expect(graph.childIds.get(GRAPH_IDS.subthema)).toHaveLength(1)
  })

  it('walks onto the "load more" row with the arrow keys', async () => {
    view([
      {
        method: 'GET',
        match: '/children',
        respond: () =>
          jsonResponse({
            nodes: [SUBTHEMA_CHILDREN.nodes[0]],
            relationships: SUBTHEMA_CHILDREN.relationships.slice(0, 1),
            truncated: true,
            total: 150,
            nextOffset: 1,
          }),
      },
    ])
    await settle()
    await fireEvent.click(itemFor(GRAPH_IDS.thema))
    await settle()
    await fireEvent.click(itemFor(GRAPH_IDS.subthema))
    await settle()

    await fireEvent.keyDown(itemFor(GRAPH_IDS.subthema), { key: 'ArrowDown' })
    await settle()
    expect(document.activeElement).toBe(itemFor(GRAPH_IDS.destillaatOne))
    await fireEvent.keyDown(itemFor(GRAPH_IDS.destillaatOne), { key: 'ArrowDown' })
    await settle()
    expect(document.activeElement).toBe(screen.getByTestId('load-more'))
    expect(tabstops()).toHaveLength(1)

    await fireEvent.keyDown(screen.getByTestId('load-more'), { key: 'ArrowLeft' })
    await settle()
    expect(document.activeElement).toBe(itemFor(GRAPH_IDS.subthema))
  })
})

// "Passend maken" verzet alleen de camera; wie drie niveaus diep zat bleef drie
// niveaus diep zitten, alleen dan uitgezoomd. Beginstand zet de graaf zelf
// terug op wat je bij het openen zag.
describe('back to the start view', () => {
  it('collapses to the domains with their themes, lets go of the selection and closes the panel', async () => {
    const { graph } = view()
    await settle()
    const opening = Number(canvas().dataset.nodeCount)

    await graph.focus(GRAPH_IDS.destillaatOne)
    await settle()
    graph.query = 'voeren'
    await settle()
    expect(graph.selected).toBe(GRAPH_IDS.destillaatOne)
    expect(Number(canvas().dataset.nodeCount)).toBeGreaterThan(opening)

    await fireEvent.click(screen.getByTestId('graph-home'))
    await settle()
    expect(graph.selected).toBeNull()
    expect(screen.queryByTestId('graph-detail')).toBeNull()
    expect(screen.queryByTestId('graph-path')).toBeNull()
    expect(Number(canvas().dataset.nodeCount)).toBe(opening)
    expect(graph.query).toBe('')
    // Geen tweede rondje langs de server: de beginstand ligt al in het geheugen.
    expect(screen.getByRole('tree')).toBeInTheDocument()
  })

  // In de kaartweergave staat Beginstand in de knoppenbalk, vlak boven de boom;
  // hem daar herhalen is twee keer dezelfde knop op één scherm. In de
  // lijstweergave neemt de boom de plek van de kaart in en staat de
  // knoppenbalk ver weg — daar hoort hij wel, en daar moet hij ook werken.
  it('offers the tree its own button only in the list view', async () => {
    const { graph } = view()
    await settle()
    expect(screen.queryByTestId('graph-home-tree')).toBeNull()

    await fireEvent.click(screen.getByTestId('graph-view-list'))
    await settle()
    expect(screen.getByTestId('graph-home-tree')).toBeInTheDocument()

    await graph.expand(GRAPH_IDS.thema)
    graph.select(GRAPH_IDS.subthema)
    await settle()
    expect(graph.selected).toBe(GRAPH_IDS.subthema)

    await fireEvent.click(screen.getByTestId('graph-home-tree'))
    await settle()
    expect(graph.selected).toBeNull()
    expect(screen.queryByTestId('graph-detail')).toBeNull()
    expect(graph.revealed.has(GRAPH_IDS.thema)).toBe(false)
    expect(graph.revealed.has(GRAPH_IDS.domein)).toBe(true)
    // De lijst blijft de lijst: terug naar de beginstand is geen weergavewissel.
    expect(screen.getByTestId('graph-view-list')).toHaveAttribute('aria-pressed', 'true')
  })

  // Het relatiefilter is een bewuste instelling met een eigen "alles tonen";
  // die achter iemands rug om terugzetten maakt van één knop twee daden.
  it('leaves the relationship filter alone', async () => {
    const { graph } = view()
    await settle()
    graph.hiddenRelations = new Set(['contains_subthema'])
    await settle()

    await fireEvent.click(screen.getByTestId('graph-home'))
    await settle()
    expect(graph.hiddenRelations.has('contains_subthema')).toBe(true)
  })
})

describe('the selection trail', () => {
  it('lights the whole path up to the domain, on the map and in the breadcrumb', async () => {
    const { graph } = view()
    await settle()
    await fireEvent.click(itemFor(GRAPH_IDS.thema))
    await settle()
    await fireEvent.click(itemFor(GRAPH_IDS.subthema))
    await settle()

    expect(graph.selected).toBe(GRAPH_IDS.subthema)
    expect([...graph.trailIds]).toEqual([GRAPH_IDS.domein, GRAPH_IDS.thema, GRAPH_IDS.subthema])
    const crumbs = within(screen.getByTestId('graph-path')).getAllByRole('button')
    expect(crumbs.map((el) => el.textContent?.trim())).toEqual(['Stad en dier', 'Overlast', 'Voeren'])
  })

  it('walks back up the trail from the breadcrumb', async () => {
    const { graph } = view()
    await settle()
    await fireEvent.click(itemFor(GRAPH_IDS.thema))
    await settle()
    const crumbs = within(screen.getByTestId('graph-path')).getAllByRole('button')
    await fireEvent.click(crumbs[0])
    await settle()
    expect(graph.selected).toBe(GRAPH_IDS.domein)
  })
})

describe('opening a node from an answer', () => {
  // De belangrijkste demo-route: "Bekijk de onderbouwing" opent de kaart op de
  // knoop, met het pad ernaartoe uitgeklapt en het spoor verlicht.
  it('opens the trail to a finding when the chat asks for it', async () => {
    const { services, graph } = view()
    await settle()

    services.shell.openInGraph(GRAPH_IDS.destillaatOne)
    await settle()
    await settle()

    expect(services.shell.tab).toBe('graph')
    expect(graph.selected).toBe(GRAPH_IDS.destillaatOne)
    expect(itemFor(GRAPH_IDS.destillaatOne)).toHaveAttribute('aria-selected', 'true')
    expect([...graph.trailIds]).toEqual([
      GRAPH_IDS.domein,
      GRAPH_IDS.thema,
      GRAPH_IDS.subthema,
      GRAPH_IDS.destillaatOne,
    ])
    // De knoop staat ook echt op de kaart, niet alleen in de lijst.
    expect(Number(canvas().dataset.nodeCount)).toBeGreaterThanOrEqual(5)
    expect(screen.getByTestId('graph-detail')).toHaveAttribute('data-id', GRAPH_IDS.destillaatOne)
  })

  // De boom mag de aandacht alleen naar zich toe halen als er van buiten om
  // gevraagd is: een klik op de kaart hoort de focus niet uit de handen van de
  // gebruiker te trekken.
  it('only takes the focus when the selection came from outside', async () => {
    const { services, graph } = view()
    await settle()

    graph.select(GRAPH_IDS.thema)
    await settle()
    expect(document.activeElement).toBe(document.body)
    expect(itemFor(GRAPH_IDS.thema)).toHaveAttribute('tabindex', '0')

    services.shell.openInGraph(GRAPH_IDS.destillaatOne)
    await settle()
    await settle()
    expect(document.activeElement).toBe(itemFor(GRAPH_IDS.destillaatOne))
  })

  it('does not present the previous selection when an explicit source cannot be found', async () => {
    const { graph } = view()
    await settle()
    graph.select(GRAPH_IDS.domein)
    await settle()
    await graph.focus('destillaat:missing')
    await settle()
    expect(screen.queryByTestId('graph-detail')).toBeNull()
    expect(screen.getByTestId('graph-notice')).toHaveTextContent('not in this knowledge base')
    expect(screen.getByRole('tree')).toBeInTheDocument()
  })
})

describe('searching', () => {
  it('counts the matches over everything that is loaded', async () => {
    view()
    await settle()

    const search = screen.getByTestId('graph-search')
    await fireEvent.input(search, { target: { value: 'overlast' } })
    await settle()
    expect(screen.getByTestId('graph-search-count')).toHaveTextContent('1 of 3')

    // Nul treffers is geen "0 van de 5": dan hoort er te staan wat de lezer
    // eraan kan doen.
    await fireEvent.input(search, { target: { value: 'zeearend' } })
    await settle()
    expect(screen.getByTestId('graph-search-count')).toHaveTextContent('Open a branch to load more')
  })

  it('also finds the text behind a shortened label', async () => {
    const { graph } = view()
    await settle()
    await graph.expand(GRAPH_IDS.subthema)
    await settle()

    await fireEvent.input(screen.getByTestId('graph-search'), { target: { value: 'kolonie groot en gezond' } })
    await settle()
    expect(graph.matchesLoaded.has(GRAPH_IDS.destillaatOne)).toBe(true)
  })

  it('says what search covers, without a banner in the way', async () => {
    view()
    await settle()
    expect(screen.getByTestId('graph-search')).toHaveAccessibleDescription(/not the full collection/)
    expect(screen.getByTestId('graph-legend')).not.toHaveAttribute('open')
    expect(screen.getByTestId('graph-relations')).not.toHaveAttribute('open')
  })

  // Een menu dat over de kaart blijft hangen is de onrust die deze knoppenbalk
  // moet wegnemen; klikken ín het menu laat het uiteraard staan.
  it('closes an open menu when the reader points somewhere else', async () => {
    view()
    await settle()
    const legend = screen.getByTestId('graph-legend') as HTMLDetailsElement
    legend.open = true

    await fireEvent.pointerDown(within(legend).getByText(/Domain/))
    expect(legend.open).toBe(true)

    await fireEvent.pointerDown(screen.getByTestId('graph-view'))
    expect(legend.open).toBe(false)
  })

  it('marks matches without dimming the surrounding text into unreadability', async () => {
    view()
    await settle()
    await fireEvent.input(screen.getByTestId('graph-search'), { target: { value: 'Stad' } })
    await settle()
    expect(itemFor(GRAPH_IDS.domein)).toHaveTextContent('Match')
    expect(itemFor(GRAPH_IDS.thema).textContent).toContain('Overlast')
  })
})

describe('the relationship filter', () => {
  it('takes a relationship off the map and out of the reading panel together', async () => {
    const { graph } = view()
    await settle()
    await fireEvent.click(itemFor(GRAPH_IDS.thema))
    await settle()
    const edges = Number(canvas().dataset.edgeCount)

    const filter = screen.getByTestId('graph-relations')
    // Het filter noemt een band naar zijn twee uiteinden, zodat een lezer weet
    // wat hij uitzet zonder de relatienaam uit de database te kennen.
    expect(filter).toHaveTextContent('Theme → Sub-theme')
    const panel = screen.getByTestId('graph-detail')
    expect(within(panel).getByText('Voeren')).toBeInTheDocument()
    const box = filter.querySelector<HTMLInputElement>('input[data-relation="contains_subthema"]')
    await fireEvent.click(box as HTMLInputElement)
    await settle()

    expect(graph.hiddenRelations.has('contains_subthema')).toBe(true)
    expect(Number(canvas().dataset.edgeCount)).toBeLessThan(edges)
    expect(within(panel).queryByText('Voeren')).toBeNull()
  })
})

describe('the reading panel', () => {
  it('reads a chosen node and closes it with Escape without losing the selection', async () => {
    const { graph } = view()
    await settle()
    await fireEvent.click(itemFor(GRAPH_IDS.thema))
    await settle()

    const panel = screen.getByTestId('graph-detail')
    expect(panel).toHaveAttribute('data-id', GRAPH_IDS.thema)
    expect(within(panel).getByText('Overlast')).toBeInTheDocument()
    expect(within(panel).getByText('Het thema')).toBeInTheDocument()

    await fireEvent.keyDown(itemFor(GRAPH_IDS.thema), { key: 'Escape' })
    await settle()
    expect(screen.queryByTestId('graph-detail')).toBeNull()
    expect(graph.selected).toBe(GRAPH_IDS.thema)
    expect(itemFor(GRAPH_IDS.thema)).toHaveAttribute('aria-selected', 'true')
  })

  it('hands the focus back to the outline when the panel closes', async () => {
    view()
    await settle()
    await fireEvent.click(itemFor(GRAPH_IDS.thema))
    await settle()

    await fireEvent.click(screen.getByTestId('detail-close'))
    await settle()
    expect(screen.queryByTestId('graph-detail')).toBeNull()
    expect(document.activeElement).toBe(itemFor(GRAPH_IDS.thema))
  })

  it('lets the keyboard read, close and reopen the same item without losing focus', async () => {
    view()
    await settle()
    await fireEvent.keyDown(itemFor(GRAPH_IDS.domein), { key: 'Enter' })
    await settle()
    await fireEvent.click(screen.getByTestId('detail-close'))
    await settle()
    expect(itemFor(GRAPH_IDS.domein)).toHaveFocus()

    await fireEvent.click(screen.getByRole('button', { name: /Reopen selected item/ }))
    await settle()
    expect(screen.getByRole('heading', { name: 'Stad en dier' })).toHaveFocus()
  })

  // Nooit een ruwe kolomnaam en nooit een interne id op het scherm: elk veld
  // draagt een mensennaam uit het woordenboek.
  it('never shows a raw column name or an internal id', async () => {
    const { graph, locale } = view()
    await settle()
    await graph.focus(GRAPH_IDS.destillaatOne)
    await settle()

    for (const language of ['en', 'nl'] as const) {
      locale.choose(language)
      await settle()
      const text = screen.getByTestId('graph-detail').textContent ?? ''
      for (const raw of ['kernstandpunt', 'interview_id', 'thema_titel', 'citationConfidence', 'destillaat:', 'quote:']) {
        expect(text).not.toContain(raw)
      }
    }
  })

  // Ruwe kolomnamen én ruwe waarden horen niet op het scherm. `wonen_en_leefomgeving` en
  // `algemeen` zijn sleutels uit de importpijplijn; een nette kop erboven maakt
  // daar geen inhoud van.
  it('never shows a raw enum value under a friendly label', async () => {
    const { graph, locale } = view([
      {
        method: 'GET',
        match: '/children',
        respond: () =>
          jsonResponse({
            nodes: [
              {
                id: GRAPH_IDS.destillaatOne,
                type: 'destillaat',
                label: 'Voeren houdt de kolonie groot',
                metadata: {
                  text: 'Voeren houdt de kolonie groot en gezond.',
                  domain: 'wonen_en_leefomgeving',
                  scope: 'algemeen',
                  speaker: 'respondent',
                  thema_titel: 'Vogelkleuren en overleving',
                },
              },
            ],
            relationships: [],
            total: 1,
          }),
      },
    ])
    await settle()
    await graph.expand(GRAPH_IDS.subthema)
    graph.select(GRAPH_IDS.destillaatOne)
    await settle()

    for (const language of ['en', 'nl'] as const) {
      locale.choose(language)
      await settle()
      const text = screen.getByTestId('graph-detail').textContent ?? ''
      expect(text).toContain('Voeren houdt de kolonie groot en gezond.')
      // Een echte titel blijft wel staan: die is geschreven, niet gecodeerd.
      expect(text).toContain('Vogelkleuren en overleving')
      for (const raw of ['wonen_en_leefomgeving', 'algemeen', 'respondent']) {
        expect(text, language + ' / ' + raw).not.toContain(raw)
      }
    }
  })

  // Op een kaart is het label de kop — er staat geen nette
  // Nederlandse kop boven die de kolomnaam verbergt. Een sleutel uit de
  // importpijplijn hoort er dus net zomin als in het detailpaneel.
  it('never puts a pipeline key or a row id on a card', async () => {
    const { graph, locale } = view([
      {
        method: 'GET',
        match: '/children',
        respond: () =>
          jsonResponse({
            nodes: [
              { id: GRAPH_IDS.destillaatOne, type: 'destillaat', label: 'wonen_en_leefomgeving', metadata: { text: 'Voeren houdt de kolonie groot en gezond.' } },
              { id: GRAPH_IDS.destillaatTwo, type: 'destillaat', metadata: { text: 'De kade is smal.' } },
            ],
            relationships: [
              { source_id: GRAPH_IDS.destillaatOne, target_id: GRAPH_IDS.subthema, relationship_type: 'belongs_to_subthema' },
              { source_id: GRAPH_IDS.destillaatTwo, target_id: GRAPH_IDS.subthema, relationship_type: 'belongs_to_subthema' },
            ],
            total: 2,
          }),
      },
    ])
    await settle()
    await fireEvent.click(itemFor(GRAPH_IDS.thema))
    await settle()
    await fireEvent.click(itemFor(GRAPH_IDS.subthema))
    await settle()

    for (const [language, kind] of [['en', 'Finding'], ['nl', 'Bevinding']] as const) {
      locale.choose(language)
      await settle()
      // De kaartnaam in de omtrek is het typewoord, niet de sleutel en niet de
      // rij-id die er vroeger als terugval stond.
      for (const id of [GRAPH_IDS.destillaatOne, GRAPH_IDS.destillaatTwo]) {
        expect(itemFor(id).querySelector('.tree-label')).toHaveTextContent(kind)
      }
      const outline = screen.getByTestId('graph-tree').textContent ?? ''
      expect(outline, language).not.toContain('wonen_en_leefomgeving')
      expect(outline, language).not.toContain(GRAPH_IDS.destillaatTwo)
    }

    // Een echte naam blijft staan: het subthema waaronder ze hangen is niet
    // aangeraakt.
    expect(screen.getByTestId('graph-tree').textContent).toContain('Voeren')
    // Het signaal voor de bouwer staat op de knoop zelf; modulecode logt niets.
    expect(graph.nodes.get(GRAPH_IDS.destillaatOne)?.unnamed).toBe(true)
    expect(graph.nodes.get(GRAPH_IDS.subthema)?.unnamed).toBe(false)
  })

  it('groups the connections by what sits at the other end', async () => {
    const { graph } = view()
    await settle()
    await graph.focus(GRAPH_IDS.destillaatOne)
    await settle()

    await fireEvent.click(screen.getByTestId('detail-expand'))
    await settle()
    const panel = screen.getByTestId('graph-detail')
    expect(within(panel).getByText('Quote')).toBeInTheDocument()
    expect(within(panel).getByText('Source')).toBeInTheDocument()
    expect(within(panel).getByRole('button', { name: 'lewald1987' })).toBeInTheDocument()
  })

  it('distinguishes a finding, its stored quote and its source', async () => {
    const { graph } = view()
    await settle()
    await graph.focus(GRAPH_IDS.destillaatOne)
    await settle()
    expect(within(screen.getByTestId('graph-detail')).getByTestId('graph-item-kind')).toHaveTextContent(
      'Summarising involves interpretation',
    )
    await fireEvent.click(screen.getByTestId('detail-expand'))
    await settle()

    graph.select(GRAPH_IDS.quote)
    await settle()
    expect(screen.getByTestId('graph-detail')).toHaveTextContent('The full quote is not available here')

    graph.select(GRAPH_IDS.interview)
    await settle()
    expect(screen.getByTestId('graph-detail')).toHaveTextContent('A full reference is not available here')
    expect(screen.queryByRole('link')).toBeNull()
  })

  // De tweetalige kennisbank (duiven/3): de bevinding volgt de taal van de
  // lezer, het citaat eronder blijft het origineel, en alleen wie een vertaling
  // ziet krijgt daar een regel bij.
  const BILINGUAL_DETAIL = {
    node: {
      id: GRAPH_IDS.destillaatOne,
      type: 'destillaat',
      label: { nl: 'Voeren houdt de kolonie groot', en: 'Feeding keeps the colony large' },
      metadata: {
        text: { nl: 'Voeren houdt de kolonie groot.', en: 'Feeding keeps the colony large.' },
        scope: 'stad',
      },
      sourceLanguage: 'nl',
    },
    trail: [],
  }

  const bilingualRoute: Route = {
    method: 'GET',
    match: /\/graph\/nodes\/[^/]+$/,
    respond: () => jsonResponse(BILINGUAL_DETAIL),
  }

  it('says that the English reader is looking at a translation', async () => {
    const { graph, locale } = view([bilingualRoute])
    locale.choose('en')
    await settle()
    await graph.focus(GRAPH_IDS.destillaatOne)
    await settle()
    const panel = screen.getByTestId('graph-detail')
    expect(panel).toHaveTextContent('Feeding keeps the colony large.')
    expect(within(panel).getByTestId('graph-translated')).toHaveTextContent(
      'Translated summary; the quotation is the original.',
    )
  })

  it('gives the Dutch reader the original without a translation line', async () => {
    const { graph, locale } = view([bilingualRoute])
    locale.choose('nl')
    await settle()
    await graph.focus(GRAPH_IDS.destillaatOne)
    await settle()
    const panel = screen.getByTestId('graph-detail')
    expect(panel).toHaveTextContent('Voeren houdt de kolonie groot.')
    expect(within(panel).queryByTestId('graph-translated')).toBeNull()
  })

  // De DOI staat er om over te nemen, niet om te openen: modulecode kent geen
  // externe adressen, en een veld uit de kennisbank is invoer.
  it('shows a DOI as text to copy and never as a link', async () => {
    const { graph } = view([
      {
        method: 'GET',
        match: '/children',
        respond: () =>
          jsonResponse({
            nodes: [
              {
                id: GRAPH_IDS.interview,
                type: 'interview',
                label: 'corbel2016',
                metadata: { doi: '10.1007/s10336-016-1331-9' },
              },
            ],
            relationships: [],
            total: 1,
          }),
      },
    ])
    await settle()
    await graph.expand(GRAPH_IDS.subthema)
    graph.select(GRAPH_IDS.interview)
    await settle()
    expect(screen.getByTestId('graph-doi')).toHaveTextContent('10.1007/s10336-016-1331-9')
    expect(screen.getByTestId('graph-detail')).toHaveTextContent('does not open it for you')
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('shows original bibliography and the scoped completeness note in EN/NL', async () => {
    const citation = 'Synthetic Author. <b>A synthetic title</b>.'
    const { graph, locale } = view([
      {
        method: 'GET',
        match: '/children',
        respond: () =>
          jsonResponse({
            nodes: [
              {
                id: GRAPH_IDS.interview,
                type: 'interview',
                label: 'Synthetic title',
                metadata: { citation, doi: 'javascript:synthetic-doi', citationConfidence: 'low', citationMissing: 'Volume and pages' },
              },
            ],
            relationships: [],
            total: 1,
          }),
      },
    ])
    await settle()
    await graph.expand(GRAPH_IDS.subthema)
    graph.select(GRAPH_IDS.interview)
    await settle()
    const panel = screen.getByTestId('graph-detail')
    expect(panel).toHaveTextContent(citation)
    expect(panel).toHaveTextContent('javascript:synthetic-doi')
    expect(panel).toHaveTextContent('Original reference')
    // Een kennisbankveld is invoer: geen opmaak, en geen adres dat we openen.
    expect(panel.querySelector('a, b')).toBeNull()
    expect(screen.getByTestId('graph-bibliography-note')).toHaveTextContent('reference is incomplete')
    locale.choose('nl')
    await settle()
    expect(panel).toHaveTextContent('Oorspronkelijke bronvermelding')
    expect(panel).toHaveTextContent(citation)
    expect(screen.getByTestId('graph-bibliography-note')).toHaveTextContent('bronvermelding is onvolledig')
  })

  it('shows a real doi as text, never as a link, and keeps internal identifiers off the panel', async () => {
    const { graph } = view([{ method: 'GET', match: '/children', respond: () => jsonResponse({ nodes: [{ id: GRAPH_IDS.interview, type: 'interview', label: 'Synthetic title', metadata: { doi: '10.1234/synthetic-9', interview_id: 'iv-7f21', speaker: 'Synthetic speaker' } }], relationships: [], total: 1 }) }])
    await settle()
    await graph.expand(GRAPH_IDS.subthema)
    graph.select(GRAPH_IDS.interview)
    await settle()
    const panel = screen.getByTestId('graph-detail')
    expect(screen.getByTestId('graph-doi')).toHaveTextContent('10.1234/synthetic-9')
    expect(panel.querySelector('a[href]')).toBeNull()
    // Een opaque sleutel uit de kennisbank is geen inhoud voor de lezer.
    expect(panel).not.toHaveTextContent('iv-7f21')
    expect(panel).toHaveTextContent('Synthetic speaker')
  })

  for (const grounding of ['rejected', 'corpus_grounded', undefined]) {
    it(`keeps a ${grounding ?? 'legacy'} quote readable with the correct evidence-use notice`, async () => {
      const { graph, locale } = view([
        {
          method: 'GET',
          match: '/children',
          respond: () =>
            jsonResponse({
              nodes: [
                {
                  id: GRAPH_IDS.quote,
                  type: 'quote',
                  label: 'Synthetic passage',
                  metadata: { text: 'Synthetic original passage.', ...(grounding ? { grounding } : {}) },
                },
              ],
              relationships: [],
              total: 1,
            }),
        },
      ])
      await settle()
      await graph.expand(GRAPH_IDS.subthema)
      graph.select(GRAPH_IDS.quote)
      await settle()
      expect(screen.getByTestId('graph-detail')).toHaveTextContent('Synthetic original passage.')
      if (grounding === 'rejected') {
        expect(screen.getByTestId('graph-quote-rejected')).toHaveTextContent('not used as evidence for answers')
      } else expect(screen.queryByTestId('graph-quote-rejected')).toBeNull()
      locale.choose('nl')
      await settle()
      if (grounding === 'rejected') {
        expect(screen.getByTestId('graph-quote-rejected')).toHaveTextContent('niet als bewijs voor antwoorden gebruikt')
      } else expect(screen.queryByTestId('graph-quote-rejected')).toBeNull()
    })
  }

  it('shows the full stored quotation verbatim without translating or HTML interpretation', async () => {
    const full = 'Long original quotation <strong>is still plain text</strong>. '.repeat(8)
    const { graph } = view([
      {
        method: 'GET',
        match: '/children',
        respond: () =>
          jsonResponse({
            nodes: [{ id: GRAPH_IDS.quote, type: 'quote', label: 'Long original quotation…', metadata: { text: full } }],
            relationships: [],
            total: 1,
          }),
      },
    ])
    await settle()
    await graph.expand(GRAPH_IDS.subthema)
    graph.select(GRAPH_IDS.quote)
    await settle()
    const panel = screen.getByTestId('graph-detail')
    // De passage staat er voluit als de kop van het paneel; het afgekapte label
    // van de wire komt er niet meer naast te staan.
    expect(within(panel).getByTestId('graph-detail-title').textContent).toBe(full)
    expect(panel.querySelector('strong')).toBeNull()
    expect(panel).not.toHaveTextContent('The full quote is not available here')
    expect(panel.textContent ?? '').not.toContain('Long original quotation…')
  })

  // Levert de kennisbank de tekst van een bevinding niet, dan staat er alsnog
  // het afgekapte label als kop. Dat mag niet stilzwijgend: dezelfde soort
  // melding als bij een citaat zonder tekst hoort erbij.
  it('says so when a finding arrives without its full text', async () => {
    const { graph, locale } = view([
      {
        method: 'GET',
        match: '/children',
        respond: () =>
          jsonResponse({
            nodes: [
              {
                id: GRAPH_IDS.destillaatOne,
                type: 'destillaat',
                label: 'De onderzoekers stellen vast dat het verplaatsen van een voederplek de kolonie niet…',
                metadata: { thema_titel: 'Voeren' },
              },
            ],
            relationships: [],
            total: 1,
          }),
      },
    ])
    await settle()
    await graph.expand(GRAPH_IDS.subthema)
    graph.select(GRAPH_IDS.destillaatOne)
    await settle()
    expect(screen.getByTestId('graph-text-incomplete')).toHaveTextContent('full text of this finding is not available')
    locale.choose('nl')
    await settle()
    expect(screen.getByTestId('graph-text-incomplete')).toHaveTextContent('volledige tekst van deze bevinding')
  })

  // De graaf-API kapt de tekst van een bevinding af tot een label van
  // honderdveertig tekens. Dat label is geen titel maar een halve zin: het
  // paneel leest de passage voluit en herhaalt de afkapping nergens.
  it('reads a finding in full instead of the shortened title the API sends', async () => {
    const full =
      'De onderzoekers stellen vast dat het verplaatsen van een voederplek de kolonie niet kleiner maakt, maar haar wel verspreidt over meer daken in de wijk.'
    const { graph } = view([
      {
        method: 'GET',
        match: '/children',
        respond: () =>
          jsonResponse({
            nodes: [
              {
                id: GRAPH_IDS.destillaatOne,
                type: 'destillaat',
                label:
                  'De onderzoekers stellen vast dat het verplaatsen van een voederplek de kolonie niet kleiner maakt, maar haar wel verspreidt over meer…',
                metadata: { text: full },
              },
            ],
            relationships: [],
            total: 1,
          }),
      },
    ])
    await settle()
    await graph.expand(GRAPH_IDS.subthema)
    graph.select(GRAPH_IDS.destillaatOne)
    await settle()

    const panel = screen.getByTestId('graph-detail')
    expect(within(panel).getByTestId('graph-detail-title')).toHaveTextContent(full)
    expect(panel.textContent ?? '').not.toContain('over meer…')
  })

  // De graaf toont geen perspectieven en geen redactionele persona's:
  // niet in de omtrek, niet op de kaart en niet in de legenda — ook niet als een
  // oudere server ze nog in de overview meestuurt.
  it('shows no perspective or editorial persona anywhere, in either language', async () => {
    const withPerspective = {
      ...OVERVIEW_FIXTURE,
      nodes: [
        ...OVERVIEW_FIXTURE.nodes,
        { id: 'perspective:44444444-4444-4444-4444-444444444444', type: 'perspective', label: 'De kritische wetenschapper', metadata: {} },
        { id: 'persona:55555555-5555-5555-5555-555555555555', type: 'persona', label: 'Rie', metadata: {} },
      ],
      relationships: [
        ...OVERVIEW_FIXTURE.relationships,
        { source_id: GRAPH_IDS.thema, target_id: 'perspective:44444444-4444-4444-4444-444444444444', relationship_type: 'contains_perspective' },
      ],
    }
    const { locale } = view([{ method: 'GET', match: '/graph/overview', respond: () => jsonResponse(withPerspective) }])
    await settle()
    await fireEvent.click(itemFor(GRAPH_IDS.thema))
    await settle()
    // Domein, thema en subthema; niets meer.
    expect(canvas()).toHaveAttribute('data-node-count', '3')
    for (const lang of ['en', 'nl'] as const) {
      locale.choose(lang)
      await settle()
      const text = screen.getByTestId('graph-view').textContent ?? ''
      expect(text, lang).not.toMatch(/perspecti|persona|kritische wetenschapper/i)
    }
  })
})

describe('the state of the knowledge base', () => {
  it('shows an empty state instead of an empty canvas', async () => {
    view([{ method: 'GET', match: '/graph/overview', respond: () => jsonResponse({ nodes: [], relationships: [] }) }])
    await settle()
    expect(screen.getByTestId('graph-empty')).toBeInTheDocument()
    expect(screen.queryByRole('tree')).toBeNull()
  })

  it('shows the server message and a way back when the graph is unavailable', async () => {
    view([
      {
        method: 'GET',
        match: '/graph/overview',
        respond: () =>
          jsonResponse({ error: 'graph_unavailable', message: { en: 'No graph today', nl: 'Geen graaf' } }, 503),
      },
    ])
    await settle()
    expect(screen.getByTestId('graph-notice')).toHaveTextContent('No graph today')
    expect(screen.getByTestId('graph-retry')).toBeInTheDocument()
  })

  // De banner van de kennisbank hoort alleen bij een kennisbank die nog niet
  // klaar is; boven een werkende kaart is hij ruis.
  it('keeps the knowledge banner away from a ready knowledge base', async () => {
    view()
    await settle()
    expect(screen.queryByTestId('knowledge-banner')).toBeNull()
  })

  it('says the pigeon is still reading while the import runs', async () => {
    view([
      { method: 'GET', match: '/knowledge/status', respond: () => jsonResponse({ state: 'importing' }) },
      { method: 'GET', match: '/graph/overview', respond: () => jsonResponse(OVERVIEW_FIXTURE) },
    ])
    await settle()
    const banner = screen.getByTestId('knowledge-banner')
    expect(banner).toHaveTextContent('still reading')
    expect(banner).toHaveAttribute('data-state', 'importing')
    expect(screen.queryByTestId('graph-kb-banner')).toBeNull()
  })

  it('says so when the state of the knowledge base cannot be read at all', async () => {
    view([
      {
        method: 'GET',
        match: '/knowledge/status',
        respond: () => {
          throw new Error('offline')
        },
      },
    ])
    await settle()
    expect(screen.getByTestId('graph-kb-banner')).toHaveTextContent('Could not read the state')
    expect(screen.queryByTestId('knowledge-banner')).toBeNull()
  })

  it("reloads when the import finishes under the reader's eyes", async () => {
    let state = 'importing'
    const { services, scripted } = view([
      { method: 'GET', match: '/knowledge/status', respond: () => jsonResponse({ state }) },
    ])
    await settle()
    const before = scripted.calls.filter((c) => c.url.includes('/graph/overview')).length

    state = 'ready'
    await services.knowledge.load()
    await settle()
    expect(scripted.calls.filter((c) => c.url.includes('/graph/overview')).length).toBe(before + 1)
  })

  it('does not pull the branch out from under a reader on another tab', async () => {
    let state = 'importing'
    const { services, graph, scripted } = view([
      { method: 'GET', match: '/knowledge/status', respond: () => jsonResponse({ state }) },
    ])
    await settle()
    await fireEvent.click(itemFor(GRAPH_IDS.thema))
    await settle()
    const before = scripted.calls.filter((c) => c.url.includes('/graph/overview')).length

    graph.visible = false
    state = 'ready'
    await services.knowledge.load()
    await settle()

    expect(scripted.calls.filter((c) => c.url.includes('/graph/overview')).length).toBe(before)
    expect(graph.revealed.has(GRAPH_IDS.thema)).toBe(true)
    expect(graph.phase).toBe('idle')
  })

  it('identifies an empty graph caused by an import in progress', async () => {
    view([
      { method: 'GET', match: '/graph/overview', respond: () => jsonResponse({ nodes: [], relationships: [], kb: { state: 'importing' } }) },
      { method: 'GET', match: '/knowledge/status', respond: () => jsonResponse({ state: 'importing' }) },
    ])
    await settle()
    expect(screen.getByTestId('graph-empty')).toHaveTextContent('being imported')
    expect(screen.queryByRole('tree')).toBeNull()
  })
})
