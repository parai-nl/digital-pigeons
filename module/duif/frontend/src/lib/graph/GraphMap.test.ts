// De kaart tekent in jsdom niet, maar zij is er wél volledig te toetsen: de
// tekencontext, de klok, de maat en de zichtbaarheid komen allemaal via
// GraphPorts binnen, en de fake laat elke tik met de hand zetten. Wat er
// getekend wordt staat in renderer.test.ts; hier gaat het om de lus eromheen —
// wanneer er een frame komt, wanneer juist niet, en wat een klik op de kaart
// met de graaf doet.
import { fireEvent, render } from '@testing-library/svelte'
import { tick } from 'svelte'
import { describe, expect, it } from 'vitest'
import { UiLocale } from '../i18n.svelte'
import { jsonResponse, scriptedFetch, type Route } from '../testing/fetch'
import { fakeGraphPorts, GRAPH_IDS, graphRoutes, type FakeGraphPortsOptions } from '../testing/graph'
import GraphMap from './GraphMap.svelte'
import { GraphStore } from './graphStore.svelte'

// De store is al geladen vóór de kaart gemonteerd wordt: dat is de normale gang
// van zaken zodra de graaftab een tweede keer opengaat.
async function mounted(options: FakeGraphPortsOptions = { reducedMotion: true }, extra: Route[] = []) {
  const scripted = scriptedFetch([...extra, ...graphRoutes()])
  const ports = fakeGraphPorts(scripted.fetch, options)
  const store = new GraphStore(ports)
  await store.load()
  store.visible = true

  const rendered = render(GraphMap, { props: { store, locale: new UiLocale() } })
  await tick()
  const el = rendered.getByTestId('graph-canvas').querySelector('canvas')
  if (!el) throw new Error('geen canvas gemonteerd')
  return { store, ports, canvas: el, scripted, ...rendered }
}

// Een klik op de kaart: neerzetten en op dezelfde plek weer loslaten. Verder dan
// de klikdrempel uit elkaar is het een sleep, en dan gebeurt er niets.
async function clickAt(canvas: Element, x: number, y: number): Promise<void> {
  await fireEvent.pointerDown(canvas, { clientX: x, clientY: y, pointerId: 1, buttons: 1 })
  await fireEvent.pointerUp(canvas, { clientX: x, clientY: y, pointerId: 1, buttons: 0 })
  await tick()
}

describe('GraphMap', () => {
  // De layout wordt op scriptniveau gemaakt en niet in onMount: het sync-$effect
  // draait eerder, en met een layout die dan nog null was bleef een kaart op een
  // al gevulde store leeg tot de volgende storewijziging.
  it('draws the graph that was already in the store when it mounted', async () => {
    const { ports } = await mounted()
    ports.resize(900, 600)
    ports.runFrame()

    expect(ports.ctx.count('arc')).toBeGreaterThan(0)
    expect(ports.ctx.texts.join(' ')).toContain('Stad en dier')
  })

  it('plans a frame when the box changes size', async () => {
    const { ports, canvas } = await mounted()
    ports.resize(900, 600)
    ports.runFrame()
    // Met beperkte beweging is de koude start meteen het eindbeeld: de lus staat
    // nu stil, en zonder de waarnemer zou er niets meer komen.
    ports.runFrame()
    const before = ports.frames

    ports.resize(400, 300)
    ports.runFrame()

    expect(ports.frames).toBe(before + 1)
    expect(canvas.width).toBe(400)
    expect(canvas.height).toBe(300)
  })

  // Een tabblad dat dicht is en een pagina die verborgen is hebben allebei maar
  // één klok: die staat stil.
  it('draws no frame while the tab is closed or the page is hidden', async () => {
    const { ports, store } = await mounted()
    ports.resize(900, 600)
    ports.runFrame()

    store.visible = false
    await tick()
    const closed = ports.frames
    ports.resize(500, 500)
    ports.runFrame()
    expect(ports.frames).toBe(closed)

    store.visible = true
    await tick()
    ports.setVisible(false)
    const hidden = ports.frames
    ports.resize(600, 400)
    ports.runFrame()
    expect(ports.frames).toBe(hidden)

    ports.setVisible(true)
    ports.runFrame()
    expect(ports.frames).toBe(hidden + 1)
  })

  it('redraws the bitmap when the pixel density changes', async () => {
    const options: FakeGraphPortsOptions = { reducedMotion: true, pixelRatio: 1 }
    const { ports, canvas, store } = await mounted(options)
    ports.resize(900, 600)
    ports.runFrame()
    expect(canvas.width).toBe(900)

    options.pixelRatio = 2
    store.select(GRAPH_IDS.thema)
    await tick()
    ports.runFrame()

    expect(canvas.width).toBe(1800)
    expect(canvas.height).toBe(1200)
  })

  it('lays down the clock and the size watcher when it goes away', async () => {
    const { ports, unmount } = await mounted()
    ports.resize(900, 600)
    ports.runFrame()
    expect(ports.observed).not.toBeNull()

    unmount()
    expect(ports.observed).toBeNull()
    const after = ports.ctx.calls.length
    ports.runFrame()
    expect(ports.ctx.calls.length).toBe(after)
  })

  it('says nothing and draws nothing in a document that cannot draw', async () => {
    const { ports } = await mounted({ reducedMotion: true, noContext: true })
    ports.resize(900, 600)
    expect(() => ports.runFrame()).not.toThrow()
    expect(ports.ctx.calls).toHaveLength(0)
  })

  // Wat er gesimuleerd wordt is precies wat er open staat: één niveau per klik.
  // De klik zelf wordt in GraphView.test.ts gereden (daar is de graaf zichtbaar
  // aan te wijzen); hier telt dat de kaart niet méér tekent dan er open staat.
  it('only simulates the level that is open', async () => {
    const { ports, store, scripted } = await mounted()
    ports.resize(900, 600)
    ports.runFrame()

    await store.expand(GRAPH_IDS.thema)
    await tick()
    expect(store.visibleIds.has(GRAPH_IDS.subthema)).toBe(true)
    expect(store.visibleIds.has(GRAPH_IDS.destillaatOne)).toBe(false)
    // De banden van het structuurniveau kwamen met de overview mee.
    expect(scripted.calls.filter((c) => c.url.includes('/children'))).toHaveLength(0)
  })

  // Een klik náást elke knoop laat de keuze los; dat is de enige manier om op de
  // kaart weer "niets" te kiezen.
  it('lets go of the selection on a click in open space', async () => {
    const { ports, store, canvas } = await mounted()
    ports.resize(900, 600)
    ports.runFrame()
    store.select(GRAPH_IDS.thema)
    await tick()

    await clickAt(canvas, 5, 5)
    expect(store.selected).toBeNull()
  })

  // De camera vliegt naar de knoop waar een antwoord om vroeg, en blijft hem
  // volgen zolang de simulatie hem nog verplaatst.
  it('moves the camera to a node the answer asked for', async () => {
    const { ports, store } = await mounted({ reducedMotion: false })
    ports.resize(900, 600)
    ports.runFrame()
    const before = ports.frames

    await store.focus(GRAPH_IDS.destillaatOne)
    await tick()
    ports.runFrame()

    expect(store.selected).toBe(GRAPH_IDS.destillaatOne)
    expect(store.focusRequest).toBeGreaterThan(0)
    expect(ports.frames).toBeGreaterThan(before)
  })

  // Namen op elke lijn maken een uitgeklapte tak onleesbaar: 27 keer
  // "Subthema" naast elkaar is een muur. Boven de drempel blijft alleen het
  // spoor benoemd.
  it('stops naming the lines when the selection hangs on too many of them', async () => {
    const children = Array.from({ length: 12 }, (_, i) => ({
      id: `destillaat:${String(i).padStart(8, '0')}-0000-0000-0000-000000000000`,
      type: 'destillaat',
      label: `Bevinding ${i}`,
      metadata: { text: `Bevinding nummer ${i} met een hele zin erachter.` },
    }))
    const { ports, store } = await mounted({ reducedMotion: true }, [
      {
        method: 'GET',
        match: /\/graph\/nodes\/[^/]+\/children/,
        respond: ({ url }) =>
          url.includes(encodeURIComponent(GRAPH_IDS.subthema))
            ? jsonResponse({
                nodes: children,
                relationships: children.map((c) => ({
                  source_id: c.id,
                  target_id: GRAPH_IDS.subthema,
                  relationship_type: 'belongs_to_subthema',
                })),
                total: children.length,
              })
            : jsonResponse({ nodes: [], relationships: [], total: 0 }),
      },
    ])
    ports.resize(900, 600)
    ports.runFrame()

    await store.expand(GRAPH_IDS.thema)
    await store.expand(GRAPH_IDS.subthema)
    store.select(GRAPH_IDS.subthema)
    await tick()
    ports.runFrame()
    const many = ports.ctx.texts.filter((text) => text === 'Finding').length
    expect(many).toBe(0)

    // Met een handvol banden zijn de namen juist de uitleg.
    store.select(GRAPH_IDS.thema)
    await tick()
    ports.runFrame()
    expect(ports.ctx.texts).toContain('Sub-theme')
  })
})
