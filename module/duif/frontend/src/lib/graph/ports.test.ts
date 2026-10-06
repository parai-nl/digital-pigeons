import { afterEach, describe, expect, it } from 'vitest'
import { FakeCanvas } from '../testing/graph'
import { browserGraphPorts } from './ports'

// De echte ports zijn de enige plek in de graaf waar een browser-API staat.
// Veel valt er in jsdom niet te toetsen (er is geen tekencontext en geen
// opmaak), maar wél dat elke poort een opzegging teruggeeft en dat een
// ontbrekend token op een terugvalkleur uitkomt in plaats van op leegte.
const noFetch: typeof fetch = () => Promise.reject(new Error('niet gebruikt'))

describe('browserGraphPorts', () => {
  it('hands back a cancel for every subscription', () => {
    const ports = browserGraphPorts(noFetch)
    const el = document.createElement('div')
    expect(() => ports.frame(() => {})()).not.toThrow()
    expect(() => ports.observeSize(el, () => {})()).not.toThrow()
    expect(() => ports.onVisible(() => {})()).not.toThrow()
  })

  it('reports the visibility of the page', () => {
    const ports = browserGraphPorts(noFetch)
    const seen: boolean[] = []
    const stop = ports.onVisible((visible) => seen.push(visible))
    document.dispatchEvent(new Event('visibilitychange'))
    stop()
    document.dispatchEvent(new Event('visibilitychange'))
    expect(seen).toEqual([true])
  })

  it('keeps the pixel ratio sane', () => {
    const ports = browserGraphPorts(noFetch)
    const dpr = ports.pixelRatio()
    expect(dpr).toBeGreaterThan(0)
    expect(dpr).toBeLessThanOrEqual(3)
  })

  it('asks the page whether it wants less motion', () => {
    expect(browserGraphPorts(noFetch).reducedMotion()).toBe(false)
  })

  it('falls back to readable colours when the host has no tokens', () => {
    const ports = browserGraphPorts(noFetch)
    const el = document.createElement('div')
    // Een gastheer met een uitgesproken eigen tekstkleur: die mag de graaf
    // nooit stilzwijgend erven. Een ontbrekend token maakt de declaratie pas
    // bij het berekenen ongeldig, en dán erft de sonde — daarom staat de
    // terugvalkleur ín de var().
    el.style.color = 'rgb(255, 0, 255)'
    document.body.appendChild(el)
    const theme = ports.readTheme(el)
    expect(theme.dark).toBe(false)
    expect(theme.ink).not.toBe('')
    expect(theme.ink).not.toBe('rgb(255, 0, 255)')
    // En nooit onopgeloste var()-tekst: een canvas kent geen tokens.
    expect(theme.ink).not.toContain('var(')
    expect(theme.canvas).not.toContain('var(')
    // Een gastheer zonder --primary levert geen tint; dan houdt de graaf zijn
    // eigen tint aan in plaats van een gegokte 0°.
    expect(theme.primaryHue).toBeGreaterThan(0)
    // De sonde ruimt zichzelf op: er blijft geen span in de tekening achter.
    expect(el.childElementCount).toBe(0)

    const dark = document.createElement('div')
    dark.className = 'dark'
    dark.appendChild(el)
    document.body.appendChild(dark)
    expect(ports.readTheme(el).dark).toBe(true)
    dark.remove()
  })

  it('has no drawing context in a document that cannot draw', () => {
    const ports = browserGraphPorts(noFetch)
    expect(ports.context(document.createElement('canvas'))).toBeNull()
  })
})

// drawGlyph is de poort waarlangs de boom en het leespaneel hetzelfde teken
// krijgen als de knoop op de kaart. jsdom heeft geen tekencontext en geen
// Path2D, dus die twee worden hier neergezet: dan loopt de échte poort mee in
// plaats van alleen de fake uit de testmap.
describe('browserGraphPorts.drawGlyph', () => {
  const hadPath2D = typeof globalThis.Path2D === 'function'
  afterEach(() => {
    if (!hadPath2D) delete (globalThis as { Path2D?: unknown }).Path2D
  })

  function canvasWith(ctx: FakeCanvas | null): HTMLCanvasElement {
    const el = document.createElement('canvas')
    el.getContext = (() => ctx) as HTMLCanvasElement['getContext']
    return el
  }

  function withPath2D(): void {
    // @ts-expect-error — een minimale naamdrager; drawIcon geeft hem alleen
    // door aan ctx.fill(path).
    globalThis.Path2D = class {
      constructor(readonly d?: string) {}
    }
  }

  it('draws the icon on a canvas sized for the screen it lands on', () => {
    withPath2D()
    const ctx = new FakeCanvas()
    const canvas = canvasWith(ctx)
    const ports = browserGraphPorts(noFetch)
    ports.drawGlyph(canvas, 'book', 'rgb(10, 20, 30)', 48)

    const dpr = ports.pixelRatio()
    expect(canvas.width).toBe(Math.round(48 * dpr))
    expect(canvas.height).toBe(Math.round(48 * dpr))
    // Het doekje wordt eerst schoongeveegd en op de schermdichtheid gezet;
    // daarna tekent parai-graph het pad met de meegegeven inkt.
    expect(ctx.ops).toContain('clearRect')
    expect(ctx.ops).toContain('setTransform')
    const filled = ctx.calls.filter((c) => c.op === 'fill')
    expect(filled.length).toBeGreaterThan(0)
    expect(filled.at(-1)?.args[2]).toBe('rgb(10, 20, 30)')
    // Het teken staat gecentreerd op het doekje en is op maat geschaald.
    expect(ctx.calls.some((c) => c.op === 'translate' && c.args[0] === 24 && c.args[1] === 24)).toBe(true)
  })

  it('draws nothing at all when the page cannot draw', () => {
    withPath2D()
    const blind = canvasWith(null)
    expect(() => browserGraphPorts(noFetch).drawGlyph(blind, 'book', 'black', 48)).not.toThrow()

    delete (globalThis as { Path2D?: unknown }).Path2D
    const ctx = new FakeCanvas()
    browserGraphPorts(noFetch).drawGlyph(canvasWith(ctx), 'book', 'black', 48)
    expect(ctx.calls).toHaveLength(0)
  })
})
