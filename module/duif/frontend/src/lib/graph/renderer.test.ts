// De tekenaar is puur, dus de neptekencontext is het hele waarneembare gedrag:
// wat er getekend werd, in welke volgorde, met welke tekst. Wat hier getoetst
// wordt is gedrag — welk label verschijnt wanneer, wanneer een "+" een belofte
// doet — niet welke tint of welke straal er precies uitkomt.
import { badgeGeometry } from '@parai/graph/lib/renderer'
import { beforeAll, describe, expect, it } from 'vitest'
import { FakeCanvas, FAKE_THEME } from '../testing/graph'
import { makePalette } from './colors'
import { drawScene, type Scene, type SceneEdge, type SceneNode } from './renderer'
import type { NodeType } from './types'
import { nodeRadius } from './visuals'

const palette = makePalette(FAKE_THEME)

// Path2D bestaat niet in jsdom. De iconen van parai-graph vragen erom, dus de
// test zet er een naamdrager neer: dan loopt het icoonpad écht mee in plaats van
// altijd de stille terugval te nemen.
beforeAll(() => {
  if (typeof globalThis.Path2D !== 'function') {
    // @ts-expect-error — een minimale naamdrager, genoeg voor ctx.fill(path)
    globalThis.Path2D = class {
      constructor(readonly d?: string) {}
    }
  }
})

function node(partial: Partial<SceneNode> & { id: string; type: NodeType }): SceneNode {
  return {
    label: partial.id,
    full: '',
    x: 0,
    y: 0,
    family: 0,
    depth: 0,
    icon: 'graph',
    selected: false,
    hovered: false,
    trail: false,
    matched: false,
    dim: false,
    badged: false,
    enterAt: 0,
    ...partial,
  }
}

function edge(partial: Partial<SceneEdge> = {}): SceneEdge {
  return { x1: 0, y1: 0, x2: 40, y2: 0, type: 'contains_thema', trail: false, incident: false, dim: false, label: '', enterAt: 0, ...partial }
}

function scene(partial: Partial<Scene> = {}): Scene {
  return {
    width: 900,
    height: 600,
    dpr: 1,
    transform: { x: 0, y: 0, k: 1 },
    nodes: [],
    edges: [],
    palette,
    now: 1_000,
    labels: true,
    ...partial,
  }
}

function draw(s: Scene): FakeCanvas {
  const ctx = new FakeCanvas()
  drawScene(ctx, s)
  return ctx
}

// --- de plaats van de labels ------------------------------------------------
// Een label mag nergens overheen: niet over een knoop, niet over de "+"-badge
// van een knoop en niet over een ander label. Om dat te kunnen nakijken worden
// hier de rechthoeken teruggerekend: die van de tekst uit wat er getekend is,
// die van de knopen uit dezelfde meetkunde als de tekening.

interface Rect {
  x: number
  y: number
  w: number
  h: number
  what: string
}

// Gelijk aan LINE_HEIGHT in de tekenaar; de neptekencontext meet zes pixels per
// teken (zie FakeCanvas.measureText) en de tekenaar legt er vijf marge omheen.
const LINE = 15

function labelRects(ctx: FakeCanvas): Rect[] {
  return ctx.calls
    .filter((c) => c.op === 'fillText' && c.args[0] !== '+')
    .map((c) => {
      const what = String(c.args[0])
      const w = what.length * 6 + 10
      // De tekst begint drie pixels onder de bovenkant van het papieren vlakje;
      // dat vlakje is het vak dat bezet gehouden wordt.
      return { what, x: Number(c.args[1]) - w / 2, y: Number(c.args[2]) - 3, w, h: LINE + 6 }
    })
}

function nodeRects(s: Scene): Rect[] {
  const v = s.transform
  return s.nodes
    .filter((n) => !n.dim)
    .map((n) => {
      const graphR = nodeRadius(n.type)
      const r = graphR * v.k
      let minX = v.x + n.x * v.k - r
      let minY = v.y + n.y * v.k - r
      let maxX = v.x + n.x * v.k + r
      let maxY = v.y + n.y * v.k + r
      if (n.badged) {
        const badge = badgeGeometry(n.x, n.y, graphR)
        minX = Math.min(minX, v.x + (badge.x - badge.r) * v.k)
        minY = Math.min(minY, v.y + (badge.y - badge.r) * v.k)
        maxX = Math.max(maxX, v.x + (badge.x + badge.r) * v.k)
        maxY = Math.max(maxY, v.y + (badge.y + badge.r) * v.k)
      }
      return { what: n.id, x: minX, y: minY, w: maxX - minX, h: maxY - minY }
    })
}

function hits(a: Rect, b: Rect): boolean {
  return a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y
}

// Een ring van acht thema's rond een domein, allemaal met een badge: de knopen
// staan zo dicht op elkaar dat er onder lang niet elk label plek is. Precies de
// stand waarin de beginstand van een echte kennisbank verkeert.
function crowd(): Scene {
  return scene({
    transform: { x: 450, y: 300, k: 1 },
    nodes: [
      node({ id: 'domein:0', type: 'domein', label: 'Stad en dier', badged: true, x: 0, y: 0 }),
      ...Array.from({ length: 8 }, (_, i) =>
        node({
          id: `thema:${i}`,
          type: 'thema',
          label: `Thema ${i}`,
          badged: true,
          x: Math.cos((i / 8) * Math.PI * 2) * 90,
          y: Math.sin((i / 8) * Math.PI * 2) * 90,
        }),
      ),
    ],
  })
}

describe('drawScene', () => {
  it('paints its own paper instead of showing the host through', () => {
    const ctx = draw(scene())
    expect(ctx.ops.slice(0, 4)).toContain('fillRect')
    expect(ctx.calls.find((c) => c.op === 'fillRect')?.args[4]).toBe(palette.canvas)
  })

  it('draws the lines before the nodes, so no line crosses a node', () => {
    const ctx = draw(
      scene({ nodes: [node({ id: 'thema:1', type: 'thema' })], edges: [edge()] }),
    )
    expect(ctx.ops.indexOf('lineTo')).toBeLessThan(ctx.ops.lastIndexOf('arc'))
  })

  it('keeps a deep label off screen until you zoom in, and always shows the chosen one', () => {
    const quote = node({ id: 'quote:1', type: 'quote', label: 'Ze komen elke ochtend' })
    const far = draw(scene({ nodes: [quote], transform: { x: 0, y: 0, k: 0.5 } }))
    expect(far.texts.join(' ')).not.toContain('ochtend')

    const near = draw(scene({ nodes: [quote], transform: { x: 0, y: 0, k: 2 } }))
    expect(near.texts.join(' ')).toContain('ochtend')

    const chosen = draw(scene({ nodes: [node({ ...quote, selected: true })], transform: { x: 0, y: 0, k: 0.5 } }))
    expect(chosen.texts.join(' ')).toContain('ochtend')
  })

  it('reads out the full text of the chosen node, not its cut label', () => {
    const ctx = draw(
      scene({
        nodes: [
          node({
            id: 'destillaat:1',
            type: 'destillaat',
            label: 'De auteurs erkennen dat hun onderzoeksopzet het effect…',
            full: 'De auteurs erkennen dat hun onderzoeksopzet het effect van de volière niet kan scheiden van het leefgebied.',
            selected: true,
          }),
        ],
      }),
    )
    expect(ctx.texts.join(' ')).toContain('leefgebied')
    expect(ctx.texts.join(' ')).not.toContain('effect…')
  })

  // Tekst over een schijf maakt allebei onleesbaar, en tekst over een
  // "+"-badge verstopt de affordance die zegt dat er nog een niveau onder zit.
  it('never prints a label on a node or on its + badge', () => {
    const s = crowd()
    const labels = labelRects(draw(s))
    expect(labels.length).toBeGreaterThan(3)
    for (const label of labels) {
      for (const node of nodeRects(s)) {
        expect(hits(label, node), `${label.what} ligt op ${node.what}`).toBe(false)
      }
    }
  })

  // De hint linksonder en de zoomknoppen rechtsboven zijn opmaak die de
  // component over het doek legt; de tekenaar ziet ze niet, dus krijgt hij hun
  // vakken mee. Een naam eronder is net zo weg als een naam over een knoop.
  it('keeps its hands off the areas the component paints over', () => {
    const reserved = [{ x: 0, y: 520, w: 400, h: 40 }]
    const s = { ...crowd(), reserved }
    for (const label of labelRects(draw(s))) {
      expect(hits(label, { ...reserved[0], what: 'hint' }), `${label.what} ligt onder de hint`).toBe(false)
    }
  })

  it('never prints two labels over each other', () => {
    const s = crowd()
    const labels = labelRects(draw(s))
    expect(labels.length).toBeGreaterThan(3)
    for (let i = 0; i < labels.length; i++) {
      for (let j = i + 1; j < labels.length; j++) {
        expect(hits(labels[i], labels[j]), `${labels[i].what} ligt op ${labels[j].what}`).toBe(false)
      }
    }
  })

  // Liever een label aan de andere kant van zijn knoop dan een label eroverheen.
  it('puts a label above its node when the space below is taken', () => {
    // Midden op het doek, zodat een plek erboven of eronder ook echt in beeld
    // valt: een label dat half buiten het doek zou vallen telt niet als plek.
    const s = scene({
      transform: { x: 450, y: 300, k: 1 },
      nodes: [
        node({ id: 'thema:a', type: 'thema', label: 'Alfa', x: 0, y: 0 }),
        node({ id: 'thema:b', type: 'thema', label: 'Beta', x: 0, y: 60 }),
      ],
    })
    const labels = labelRects(draw(s))
    const alfa = labels.find((l) => l.what === 'Alfa')
    const beta = labels.find((l) => l.what === 'Beta')
    // Onder Alfa staat Beta; Alfa's naam wijkt naar boven, niet naar de schijf.
    expect(alfa?.y).toBeLessThan(300 - nodeRadius('thema'))
    // Onder Beta is het vrij, dus die naam blijft gewoon staan waar je hem zoekt.
    expect(beta?.y).toBeGreaterThan(360 + nodeRadius('thema'))
  })

  it('never lets two forced labels print over each other', () => {
    const ctx = draw(
      scene({
        nodes: [
          node({ id: 'a', type: 'destillaat', label: 'Eerste bevinding hier', selected: true, x: 0, y: 0 }),
          node({ id: 'b', type: 'destillaat', label: 'Tweede bevinding hier', trail: true, x: 0, y: 0 }),
        ],
      }),
    )
    const rows = ctx.calls.filter((c) => c.op === 'fillText').map((c) => Number(c.args[2]))
    expect(new Set(rows).size).toBe(rows.length)
  })

  it('marks a node that still has a level underneath, and only that one', () => {
    const open = draw(scene({ nodes: [node({ id: 'thema:1', type: 'thema', badged: false })] }))
    expect(open.texts).not.toContain('+')

    const closed = draw(scene({ nodes: [node({ id: 'thema:1', type: 'thema', badged: true })] }))
    expect(closed.texts).toContain('+')
  })

  it('draws the glyph of a node inside it', () => {
    const ctx = draw(scene({ nodes: [node({ id: 'thema:1', type: 'thema', icon: 'book' })] }))
    expect(ctx.calls.some((c) => c.op === 'fill' && c.args[0] instanceof Path2D)).toBe(true)
  })

  it('lights the trail towards the selection with the host accent', () => {
    const ctx = draw(scene({ edges: [edge({ trail: true })] }))
    expect(ctx.calls.find((c) => c.op === 'stroke')?.args[1]).toBe(palette.halo)
  })

  it('names the relationship on the edges that touch the selection', () => {
    const ctx = draw(scene({ edges: [edge({ incident: true, label: 'Citaten' })] }))
    expect(ctx.texts).toContain('Citaten')
  })

  it('lets a fresh node grow in instead of popping at full size', () => {
    const arriving = draw(scene({ now: 1_000, nodes: [node({ id: 'quote:1', type: 'quote', enterAt: 1_000 })] }))
    const settled = draw(scene({ now: 5_000, nodes: [node({ id: 'quote:1', type: 'quote', enterAt: 1_000 })] }))
    const radius = (ctx: FakeCanvas) => Number(ctx.calls.find((c) => c.op === 'arc')?.args[2])
    expect(radius(arriving)).toBeLessThan(radius(settled))
  })

  it('says nothing at all when there is no room for text', () => {
    const ctx = draw(scene({ width: 300, nodes: [node({ id: 'thema:1', type: 'thema', label: 'Overlast', selected: true })] }))
    expect(ctx.texts.join(' ')).not.toContain('Overlast')
  })
})
