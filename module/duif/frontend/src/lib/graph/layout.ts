import {
  forceCollide,
  forceLink,
  forceManyBody,
  forceSimulation,
  forceX,
  forceY,
  type Simulation,
  type SimulationLinkDatum,
  type SimulationNodeDatum,
} from 'd3-force'
import { quadtree, type Quadtree } from 'd3-quadtree'
import type { NodeType } from './types'
import { nodeRadius } from './visuals'

// De layout is een gewone klasse, bewust buiten de runes: d3 muteert x, y, vx
// en vy ín de knooppuntobjecten, en een $state-proxy zou daar per tik duizenden
// keren doorheen moeten. De component houdt één exemplaar per canvas vast (nooit
// de store) en leest na elke tik rechtstreeks `nodes`.
//
// De simulatie draait nooit op d3's eigen timer: forceSimulation start er zelf
// een, dus die wordt meteen gestopt en wij tikken hem in onze eigen tekenlus.
// Zo is er precies één klok in het spel, en die hangt aan GraphPorts.frame().

export interface LayoutNode extends SimulationNodeDatum {
  id: string
  type: NodeType
  family: number
  depth: number
  x: number
  y: number
  // enterAt is het moment waarop de knoop in de simulatie kwam; de tekenaar
  // laat hem daarmee opkomen in plaats van hem te laten ploppen. Nul voor de
  // knopen van de eerste tekening: die zijn er gewoon.
  enterAt: number
}

export interface LayoutEdge extends SimulationLinkDatum<LayoutNode> {
  type: string
}

export interface LayoutInput {
  nodes: readonly { id: string; type: NodeType; family: number; depth: number }[]
  edges: readonly { source: string; target: string; type: string }[]
  // now is de klok van de aanroeper; nieuwe knopen krijgen hem als enterAt.
  now?: number
}

// Structuurbanden mogen langer zijn (ze dragen de plattegrond), banden naar een
// citaat kort: die horen als een tros aan hun standpunt te hangen. De
// verhouding is dezelfde gebleven — structuur ver, citaten dichtbij — maar
// alles is ingekort: de knopen in visuals.ts zijn groter geworden, en met de
// oude afstanden lag er tussen twee thema's een leegte waar je overheen moest
// slepen. Wat overblijft is genoeg lucht voor de collide-straal en het label
// eronder, en niet meer dan dat.
const LINK_DISTANCE: Readonly<Record<string, number>> = {
  contains_thema: 168,
  contains_subthema: 108,
  belongs_to: 92,
  belongs_to_subthema: 84,
  contains: 92,
  has_quote: 50,
}
const DEFAULT_DISTANCE = 98

// Een ouder met veel kinderen heeft een grotere ring nodig, anders staan de
// labels van zijn kinderen op elkaar: op een omtrek van 2πr moeten n labels van
// ongeveer LABEL_ARC breed passen, dus r ≥ n·LABEL_ARC/2π. Zonder dit werd een
// thema met 27 subthema's een propje. De bovengrens houdt een tak van 150
// binnen het doek — daar doet de zoom de rest.
const LABEL_ARC = 74
const MAX_DISTANCE = 700
function spreadFor(children: number): number {
  return Math.min(MAX_DISTANCE, (children * LABEL_ARC) / (2 * Math.PI))
}

// Het aantal tikken van de koude start: genoeg om een nieuwe structuur al
// uitgevouwen op het scherm te zetten in plaats van hem te laten ontploffen.
export const WARM_TICKS = 260

// De gulden hoek verdeelt nieuwe knopen netjes rond hun ouder zonder toeval:
// dezelfde invoer geeft dezelfde tekening, ook in een test.
const GOLDEN = Math.PI * (3 - Math.sqrt(5))

export class GraphLayout {
  #sim: Simulation<LayoutNode, LayoutEdge>
  #nodes: LayoutNode[] = []
  #index = new Map<string, LayoutNode>()
  #links: LayoutEdge[] = []
  #tree: Quadtree<LayoutNode> | null = null
  #seq = 0
  // Hoeveel banden er aan een knoop hangen; de linkafstand groeit ermee mee.
  #degree = new Map<string, number>()

  constructor(readonly reducedMotion = false) {
    this.#sim = forceSimulation<LayoutNode, LayoutEdge>([])
      .force(
        'link',
        forceLink<LayoutNode, LayoutEdge>([])
          .id((d) => d.id)
          .distance((e) => {
            const base = LINK_DISTANCE[e.type] ?? DEFAULT_DISTANCE
            const id = (end: LayoutNode | string | number) => (typeof end === 'object' ? end.id : String(end))
            const crowd = Math.max(this.#degree.get(id(e.source)) ?? 1, this.#degree.get(id(e.target)) ?? 1)
            return Math.max(base, spreadFor(crowd))
          })
          .strength(0.5),
      )
      // De afstoot is zwakker en reikt minder ver, en de twee middenkrachten
      // trekken harder: samen houden ze de tekening bij elkaar in plaats van
      // haar over het doek uit te waaieren. De collide-straal is relatief
      // kleiner omdat de knopen zelf groter zijn — de absolute vrije ruimte
      // rond een knoop blijft daarmee ongeveer gelijk.
      .force('charge', forceManyBody<LayoutNode>().strength(-300).distanceMax(800))
      .force('collide', forceCollide<LayoutNode>().radius((d) => nodeRadius(d.type) * 1.3))
      .force('x', forceX<LayoutNode>(0).strength(0.055))
      .force('y', forceY<LayoutNode>(0).strength(0.07))
      .stop()
  }

  get nodes(): readonly LayoutNode[] {
    return this.#nodes
  }

  get links(): readonly LayoutEdge[] {
    return this.#links
  }

  // sync legt de nieuwe zichtbare graaf op de simulatie. Knopen die blijven
  // houden hun plek (anders springt de tekening bij elke uitklap); nieuwe
  // knopen beginnen naast een buur die er al stond.
  sync(input: LayoutInput): void {
    const wasEmpty = this.#nodes.length === 0
    const next: LayoutNode[] = []
    const index = new Map<string, LayoutNode>()
    const fresh: LayoutNode[] = []

    for (const item of input.nodes) {
      const existing = this.#index.get(item.id)
      if (existing) {
        existing.family = item.family
        existing.depth = item.depth
        next.push(existing)
        index.set(item.id, existing)
        continue
      }
      const node: LayoutNode = {
        id: item.id,
        type: item.type,
        family: item.family,
        depth: item.depth,
        x: 0,
        y: 0,
        // Op de eerste tekening komt niets "op": dan staat de kaart er gewoon.
        enterAt: wasEmpty ? 0 : (input.now ?? 0),
      }
      next.push(node)
      index.set(item.id, node)
      fresh.push(node)
    }

    // Nieuwe knopen naast een buur die al een plek heeft; kent hij er geen,
    // dan op een spiraal rond de oorsprong.
    if (fresh.length > 0) {
      const anchor = new Map<string, LayoutNode>()
      for (const edge of input.edges) {
        const from = this.#index.get(edge.source)
        const to = this.#index.get(edge.target)
        if (from && !anchor.has(edge.target)) anchor.set(edge.target, from)
        if (to && !anchor.has(edge.source)) anchor.set(edge.source, to)
      }
      for (const node of fresh) {
        const seed = anchor.get(node.id)
        const angle = this.#seq++ * GOLDEN
        const radius = seed ? 46 : 110 + Math.sqrt(this.#seq) * 22
        node.x = (seed?.x ?? 0) + Math.cos(angle) * radius
        node.y = (seed?.y ?? 0) + Math.sin(angle) * radius
      }
    }

    // removed telt de knopen die verdwenen (een inklap): wat er stond, min wat
    // ervan bleef staan.
    const removed = this.#nodes.length - (next.length - fresh.length)
    this.#nodes = next
    this.#index = index
    this.#links = input.edges
      .filter((e) => index.has(e.source) && index.has(e.target))
      .map((e) => ({ source: e.source, target: e.target, type: e.type }))
    this.#degree = new Map()
    for (const edge of this.#links) {
      const from = typeof edge.source === 'object' ? edge.source.id : String(edge.source)
      const to = typeof edge.target === 'object' ? edge.target.id : String(edge.target)
      this.#degree.set(from, (this.#degree.get(from) ?? 0) + 1)
      this.#degree.set(to, (this.#degree.get(to) ?? 0) + 1)
    }
    this.#tree = null

    this.#sim.nodes(this.#nodes)
    const link = this.#sim.force<ReturnType<typeof forceLink<LayoutNode, LayoutEdge>>>('link')
    link?.links(this.#links)

    // Alleen een lege plattegrond wordt koud uitgevouwen. Elke andere wissel —
    // een uitklap, een inklap — verwarmt wat er ligt: 260 synchrone tikken per
    // klik is een halve seconde stilstand voor een tekening die al klopt.
    if (wasEmpty) this.warm()
    else if (fresh.length > 0 || removed > 0) this.reheat()
  }

  // warm vouwt de structuur synchroon uit: 260 tikken zonder te tekenen is
  // sneller dan 260 frames waarin je de knopen uit elkaar ziet vliegen.
  warm(ticks = WARM_TICKS): void {
    this.#sim.alpha(1)
    this.#sim.tick(ticks)
    this.#tree = null
    // Met beperkte beweging is de koude start meteen het eindbeeld.
    if (this.reducedMotion) this.#sim.alpha(0)
  }

  reheat(alpha = 0.25): void {
    if (this.reducedMotion) {
      this.warm(60)
      return
    }
    this.#sim.alpha(Math.max(this.#sim.alpha(), alpha))
  }

  // step is één frame. Hij zegt of er nog beweging in zit, zodat de tekenlus
  // vanzelf stilvalt in plaats van eeuwig door te draaien.
  step(): boolean {
    if (this.#sim.alpha() <= this.#sim.alphaMin()) return false
    this.#sim.tick()
    this.#tree = null
    return true
  }

  stop(): void {
    this.#sim.alpha(0)
    this.#sim.stop()
  }

  // find is de raaktest onder de muis; de quadtree wordt pas gebouwd als er
  // echt geklikt wordt, en na elke tik weer weggegooid.
  find(x: number, y: number, radius = 24): LayoutNode | null {
    if (this.#nodes.length === 0) return null
    if (!this.#tree) {
      this.#tree = quadtree<LayoutNode>()
        .x((d) => d.x)
        .y((d) => d.y)
        .addAll(this.#nodes)
    }
    return this.#tree.find(x, y, radius) ?? null
  }

  pin(id: string, x: number, y: number): void {
    const node = this.#index.get(id)
    if (!node) return
    node.fx = x
    node.fy = y
    node.x = x
    node.y = y
    this.#tree = null
  }

  release(id: string): void {
    const node = this.#index.get(id)
    if (!node) return
    node.fx = null
    node.fy = null
  }

  position(id: string): { x: number; y: number } | null {
    const node = this.#index.get(id)
    return node ? { x: node.x, y: node.y } : null
  }

  // boundsOf is hetzelfde rechthoekje om een deel van de tekening: de camera
  // volgt daarmee een net uitgeklapte knoop mét zijn nieuwe kinderen, in plaats
  // van naar één punt te vliegen dat de simulatie meteen weer verplaatst.
  boundsOf(ids: ReadonlySet<string>): { minX: number; minY: number; maxX: number; maxY: number } | null {
    const nodes: LayoutNode[] = []
    for (const id of ids) {
      const node = this.#index.get(id)
      if (node) nodes.push(node)
    }
    return boxOf(nodes)
  }

  // bounds is het omhullende rechthoekje van de tekening; de knop "passend
  // maken" rekent er de zoom uit.
  bounds(): { minX: number; minY: number; maxX: number; maxY: number } | null {
    return boxOf(this.#nodes)
  }
}

function boxOf(nodes: readonly LayoutNode[]): { minX: number; minY: number; maxX: number; maxY: number } | null {
  if (nodes.length === 0) return null
  let minX = Infinity
  let minY = Infinity
  let maxX = -Infinity
  let maxY = -Infinity
  for (const node of nodes) {
    const r = nodeRadius(node.type)
    if (node.x - r < minX) minX = node.x - r
    if (node.y - r < minY) minY = node.y - r
    if (node.x + r > maxX) maxX = node.x + r
    if (node.y + r > maxY) maxY = node.y + r
  }
  return { minX, minY, maxX, maxY }
}

// GraphControls is wat de knoppenbalk van de plattegrond mag: zoomen, passend
// maken en terug naar de beginstand. De stand zelf blijft bij het canvas, waar
// de maat bekend is.
//
// `home` is meer dan `reset`: de store heeft dan net een hele tak weggehaald,
// dus de knopen die blijven staan nog op hun oude, uitgewaaierde plek. Eerst de
// simulatie opnieuw laten uitvouwen, dan pas kaderen — anders kadert de camera
// een tekening die een tel later heel anders ligt.
export interface GraphControls {
  zoomIn(): void
  zoomOut(): void
  reset(): void
  home(): void
}

// endpoints leest de plek van beide uiteinden van een band; d3 vervangt de
// id-tekst tijdens het eerste tikken door het knooppunt zelf.
export function endpoints(edge: LayoutEdge): { x1: number; y1: number; x2: number; y2: number } | null {
  const from = edge.source
  const to = edge.target
  if (typeof from === 'string' || typeof from === 'number') return null
  if (typeof to === 'string' || typeof to === 'number') return null
  return { x1: from.x, y1: from.y, x2: to.x, y2: to.y }
}
