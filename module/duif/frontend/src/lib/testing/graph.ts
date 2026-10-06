// Fakes voor de graaftests: een tekencontext die opschrijft wat er getekend
// wordt, ports waarvan de klok, de maat en de zichtbaarheid met de hand gaan,
// en een kleine kennisbank als fixture. Er wordt niets globaal gepatcht — de
// graaf krijgt alles via GraphPorts, precies zoals browserGraphPorts() de echte
// API's aanlevert.
import type { Fetcher } from '../api'
import { GraphStore } from '../graph/graphStore.svelte'
import type { CanvasLike, GraphPorts, GraphTheme } from '../graph/ports'
import { jsonResponse, type Route } from './fetch'

// FakeCanvas schrijft elke aanroep op; de tekenaar is puur, dus deze lijst is
// het hele waarneembare gedrag.
export class FakeCanvas implements CanvasLike {
  calls: { op: string; args: unknown[] }[] = []
  fillStyle: string | CanvasGradient | CanvasPattern = '#000'
  strokeStyle: string | CanvasGradient | CanvasPattern = '#000'
  lineWidth = 1
  globalAlpha = 1
  font = '10px sans-serif'
  textAlign: CanvasTextAlign = 'start'
  textBaseline: CanvasTextBaseline = 'alphabetic'

  #record(op: string, ...args: unknown[]): void {
    this.calls.push({ op, args })
  }

  // ops levert de namen in volgorde; handig voor "eerst banden, dan knopen".
  get ops(): string[] {
    return this.calls.map((c) => c.op)
  }

  count(op: string): number {
    return this.calls.reduce((n, c) => (c.op === op ? n + 1 : n), 0)
  }

  // texts levert alles wat er als tekst op het doek kwam.
  get texts(): string[] {
    return this.calls.filter((c) => c.op === 'fillText').map((c) => String(c.args[0]))
  }

  save(): void {
    this.#record('save')
  }
  restore(): void {
    this.#record('restore')
  }
  setTransform(a?: number | DOMMatrix2DInit, b?: number, c?: number, d?: number, e?: number, f?: number): void {
    this.#record('setTransform', a, b, c, d, e, f)
  }
  translate(x: number, y: number): void {
    this.#record('translate', x, y)
  }
  scale(x: number, y: number): void {
    this.#record('scale', x, y)
  }
  clearRect(x: number, y: number, w: number, h: number): void {
    this.#record('clearRect', x, y, w, h)
  }
  fillRect(x: number, y: number, w: number, h: number): void {
    this.#record('fillRect', x, y, w, h, this.fillStyle)
  }
  beginPath(): void {
    this.#record('beginPath')
  }
  closePath(): void {
    this.#record('closePath')
  }
  moveTo(x: number, y: number): void {
    this.#record('moveTo', x, y)
  }
  lineTo(x: number, y: number): void {
    this.#record('lineTo', x, y)
  }
  arc(x: number, y: number, r: number, start: number, end: number, ccw?: boolean): void {
    this.#record('arc', x, y, r, start, end, ccw)
  }
  quadraticCurveTo(cx: number, cy: number, x: number, y: number): void {
    this.#record('quadraticCurveTo', cx, cy, x, y)
  }
  fill(a?: CanvasFillRule | Path2D, b?: CanvasFillRule): void {
    this.#record('fill', a, b, this.fillStyle)
  }
  stroke(path?: Path2D): void {
    this.#record('stroke', path, this.strokeStyle, this.lineWidth)
  }
  fillText(text: string, x: number, y: number, maxWidth?: number): void {
    this.#record('fillText', text, x, y, maxWidth, this.font, this.fillStyle)
  }
  measureText(text: string): TextMetrics {
    this.#record('measureText', text)
    return { width: text.length * 6 } as TextMetrics
  }
}

export const FAKE_THEME: GraphTheme = {
  dark: false,
  ink: 'rgb(28, 28, 28)',
  dim: 'rgb(107, 107, 107)',
  canvas: 'rgb(246, 245, 242)',
  halo: 'rgb(47, 111, 94)',
  edge: 'rgb(185, 182, 176)',
  primaryHue: 160,
  destructiveHue: 25,
}

// De knoppen van de fake zijn de takken van GraphCanvas: alleen wat de
// component zelf uit zijn ports leest, staat hier.
export interface FakeGraphPortsOptions {
  reducedMotion?: boolean
  pixelRatio?: number
  // noContext bootst jsdom na: getContext('2d') geeft daar null.
  noContext?: boolean
}

export interface FakeGraphPorts extends GraphPorts {
  ctx: FakeCanvas
  // observed is het element waarvan de maat gevolgd wordt; een graaf die zijn
  // maat uit een ouder haalt, groeit met die ouder mee.
  observed: Element | null
  // runFrame draait één geplande tekenbeurt; zonder aanroep staat de klok stil.
  runFrame(): void
  frames: number
  resize(width: number, height: number): void
  setVisible(visible: boolean): void
}

export function fakeGraphPorts(fetcher: Fetcher, options: FakeGraphPortsOptions = {}): FakeGraphPorts {
  const ctx = new FakeCanvas()
  let pending: (() => void) | null = null
  let sizeCb: ((w: number, h: number) => void) | null = null
  let visibleCb: ((visible: boolean) => void) | null = null

  const ports: FakeGraphPorts = {
    ctx,
    observed: null,
    frames: 0,
    fetch: fetcher,
    frame(cb) {
      pending = cb
      return () => {
        if (pending === cb) pending = null
      }
    },
    runFrame() {
      const cb = pending
      pending = null
      if (cb) {
        ports.frames++
        cb()
      }
    },
    observeSize(el, cb) {
      ports.observed = el
      sizeCb = cb
      return () => {
        if (sizeCb === cb) sizeCb = null
        if (ports.observed === el) ports.observed = null
      }
    },
    resize(width, height) {
      sizeCb?.(width, height)
    },
    pixelRatio: () => options.pixelRatio ?? 1,
    reducedMotion: () => options.reducedMotion === true,
    onVisible(cb) {
      visibleCb = cb
      return () => {
        if (visibleCb === cb) visibleCb = null
      }
    },
    setVisible(visible) {
      visibleCb?.(visible)
    },
    // Een kopie: geen enkele test kan het thema van de volgende bederven.
    readTheme: () => ({ ...FAKE_THEME }),
    context: () => (options.noContext ? null : ctx),
    // jsdom tekent geen doekje: hetzelfde als een browser zonder Path2D. Welk
    // teken een rij draagt staat als `data-glyph` in de boom, dus dat blijft
    // toetsbaar zonder te tekenen.
    drawGlyph: () => {},
  }
  return ports
}

// testGraphStore bouwt een inerte graafstore: hij doet niets tot load() wordt
// aangeroepen. De kennisstore komt van buiten, want in productie deelt de graaf
// hem met de schil; een eigen exemplaar hier zou een test laten slagen die in
// de echte mount faalt. Een graaftest geeft zijn eigen ports mee (met runFrame
// en resize aan de hand), de andere tests laten de standaardfake staan.
export function testGraphStore(fetcher: Fetcher, ports?: GraphPorts): GraphStore {
  return new GraphStore(ports ?? fakeGraphPorts(fetcher))
}

// --- de kleine kennisbank ---------------------------------------------------
// Eén domein, één thema met een subthema, twee standpunten en onder het eerste
// een citaat met zijn bronpaper. Klein genoeg om in een assertie te passen,
// groot genoeg voor elk pad in de UI. Perspectieven en persona-knopen staan er
// niet in: de server stuurt ze de graaf niet meer in.

export const GRAPH_IDS = {
  domein: 'domein:11111111-1111-1111-1111-111111111111',
  thema: 'thema:22222222-2222-2222-2222-222222222222',
  subthema: 'subthema:33333333-3333-3333-3333-333333333333',
  destillaatOne: 'destillaat:66666666-6666-6666-6666-666666666666',
  destillaatTwo: 'destillaat:77777777-7777-7777-7777-777777777777',
  quote: 'quote:88888888-8888-8888-8888-888888888888',
  interview: 'interview:99999999-9999-9999-9999-999999999999',
} as const

export const OVERVIEW_FIXTURE = {
  nodes: [
    { id: GRAPH_IDS.domein, type: 'domein', label: 'Stad en dier', metadata: { beschrijving: 'Het domein' } },
    { id: GRAPH_IDS.thema, type: 'thema', label: 'Overlast', metadata: { beschrijving: 'Het thema' } },
    { id: GRAPH_IDS.subthema, type: 'subthema', label: 'Voeren', metadata: {} },
  ],
  relationships: [
    { source_id: GRAPH_IDS.domein, target_id: GRAPH_IDS.thema, relationship_type: 'contains_thema' },
    { source_id: GRAPH_IDS.thema, target_id: GRAPH_IDS.subthema, relationship_type: 'contains_subthema' },
  ],
  totals: { domein: 1, thema: 1, subthema: 1, destillaat: 2, quote: 1, interview: 1 },
  kb: { state: 'ready' },
}

export const SUBTHEMA_CHILDREN = {
  nodes: [
    {
      id: GRAPH_IDS.destillaatOne,
      type: 'destillaat',
      label: 'Voeren houdt de kolonie groot',
      metadata: { text: 'Voeren houdt de kolonie groot en gezond.', domain: 'Stad en dier' },
    },
    {
      id: GRAPH_IDS.destillaatTwo,
      type: 'destillaat',
      label: 'Een voerverbod verplaatst het probleem',
      metadata: { text: 'Een voerverbod verplaatst het probleem naar de rand.' },
    },
  ],
  relationships: [
    { source_id: GRAPH_IDS.destillaatOne, target_id: GRAPH_IDS.subthema, relationship_type: 'belongs_to_subthema' },
    { source_id: GRAPH_IDS.destillaatTwo, target_id: GRAPH_IDS.subthema, relationship_type: 'belongs_to_subthema' },
  ],
  total: 2,
}

export const DESTILLAAT_CHILDREN = {
  nodes: [
    { id: GRAPH_IDS.quote, type: 'quote', label: 'Ze komen elke ochtend', metadata: { speaker: 'Rie' } },
    { id: GRAPH_IDS.interview, type: 'interview', label: 'lewald1987', metadata: { interview_id: 'lewald1987' } },
  ],
  relationships: [
    { source_id: GRAPH_IDS.destillaatOne, target_id: GRAPH_IDS.quote, relationship_type: 'has_quote' },
    { source_id: GRAPH_IDS.interview, target_id: GRAPH_IDS.destillaatOne, relationship_type: 'contains' },
  ],
  total: 2,
}

// Het tweede standpunt komt uit hetzelfde bronpaper als het eerste. Dat is de
// vorm waarin één knoop onder twee ouders hangt: in de omtrek twee keer, op de
// kaart één keer.
export const DESTILLAAT_TWO_CHILDREN = {
  nodes: [DESTILLAAT_CHILDREN.nodes[1]],
  relationships: [{ source_id: GRAPH_IDS.interview, target_id: GRAPH_IDS.destillaatTwo, relationship_type: 'contains' }],
  total: 1,
}

export const NODE_DETAIL = {
  node: SUBTHEMA_CHILDREN.nodes[0],
  trail: [
    { id: GRAPH_IDS.domein, type: 'domein', label: 'Stad en dier' },
    { id: GRAPH_IDS.thema, type: 'thema', label: 'Overlast' },
    { id: GRAPH_IDS.subthema, type: 'subthema', label: 'Voeren' },
  ],
}

const NOT_FOUND = {
  error: 'node_not_found',
  message: { en: 'That node is not in this knowledge base.', nl: 'Die knoop staat niet in deze kennisbank.' },
}

// graphRoutes is de normale server achter de graaf; een test zet er zijn eigen
// route vóór om één antwoord te veranderen.
export function graphRoutes(): Route[] {
  return [
    { method: 'GET', match: '/graph/overview', respond: () => jsonResponse(OVERVIEW_FIXTURE) },
    {
      method: 'GET',
      match: /\/graph\/nodes\/[^/]+\/children/,
      respond: ({ url }) => {
        if (url.includes(encodeURIComponent(GRAPH_IDS.subthema))) return jsonResponse(SUBTHEMA_CHILDREN)
        if (url.includes(encodeURIComponent(GRAPH_IDS.destillaatOne))) return jsonResponse(DESTILLAAT_CHILDREN)
        if (url.includes(encodeURIComponent(GRAPH_IDS.destillaatTwo))) return jsonResponse(DESTILLAAT_TWO_CHILDREN)
        return jsonResponse({ nodes: [], relationships: [], total: 0 })
      },
    },
    {
      method: 'GET',
      match: /\/graph\/nodes\/[^/]+$/,
      respond: ({ url }) =>
        url.includes(encodeURIComponent(GRAPH_IDS.destillaatOne))
          ? jsonResponse(NODE_DETAIL)
          : jsonResponse(NOT_FOUND, 404),
    },
    { method: 'GET', match: '/knowledge/status', respond: () => jsonResponse({ state: 'ready', kbId: 'proef' }) },
  ]
}
