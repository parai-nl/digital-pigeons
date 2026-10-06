import { describe, expect, it } from 'vitest'
import { endpoints, GraphLayout, type LayoutInput } from './layout'

const STRUCTURE: LayoutInput = {
  nodes: [
    { id: 'd', type: 'domein', family: -1, depth: 0 },
    { id: 't', type: 'thema', family: 0, depth: 1 },
    { id: 's', type: 'subthema', family: 0, depth: 2 },
  ],
  edges: [
    { source: 'd', target: 't', type: 'contains_thema' },
    { source: 't', target: 's', type: 'contains_subthema' },
  ],
}

const WITH_CHILD: LayoutInput = {
  nodes: [...STRUCTURE.nodes, { id: 'x', type: 'destillaat', family: 0, depth: 3 }],
  edges: [...STRUCTURE.edges, { source: 'x', target: 's', type: 'belongs_to_subthema' }],
}

describe('GraphLayout', () => {
  it('lays out a new structure in one cold start instead of over frames', () => {
    const layout = new GraphLayout()
    layout.sync(STRUCTURE)
    const places = layout.nodes.map((n) => `${Math.round(n.x)},${Math.round(n.y)}`)
    // Alles op nul zou betekenen dat er nog niets gebeurd is.
    expect(new Set(places).size).toBe(3)
    expect(layout.links).toHaveLength(2)
  })

  it('keeps the places of nodes that stay and seeds a new one next to its neighbour', () => {
    const layout = new GraphLayout()
    layout.sync(STRUCTURE)
    const before = layout.position('s')
    layout.sync(WITH_CHILD)
    // De uitklap verwarmt alleen; de plattegrond springt niet opnieuw.
    expect(layout.position('s')).toEqual(before)
    const fresh = layout.position('x')
    const anchor = layout.position('s')
    expect(fresh).not.toBeNull()
    const distance = Math.hypot((fresh?.x ?? 0) - (anchor?.x ?? 0), (fresh?.y ?? 0) - (anchor?.y ?? 0))
    expect(distance).toBeLessThan(60)
  })

  it('drops the links of nodes that went away on a collapse', () => {
    const layout = new GraphLayout()
    layout.sync(WITH_CHILD)
    layout.sync(STRUCTURE)
    expect(layout.nodes.map((n) => n.id)).toEqual(['d', 't', 's'])
    expect(layout.links).toHaveLength(2)
  })

  it('cools down on its own and warms up again on request', () => {
    const layout = new GraphLayout()
    layout.sync(STRUCTURE)
    layout.warm(600)
    expect(layout.step()).toBe(false)
    layout.reheat()
    expect(layout.step()).toBe(true)
    layout.stop()
    expect(layout.step()).toBe(false)
  })

  it('does not animate when the reader asked for less motion', () => {
    const layout = new GraphLayout(true)
    layout.sync(STRUCTURE)
    // De koude start is meteen het eindbeeld: geen enkel frame beweging.
    expect(layout.step()).toBe(false)
    layout.reheat()
    expect(layout.step()).toBe(false)
    expect(layout.nodes.every((n) => Number.isFinite(n.x))).toBe(true)
  })

  it('finds the node under the pointer and nothing beyond the reach', () => {
    const layout = new GraphLayout()
    layout.sync(STRUCTURE)
    layout.pin('t', 100, 100)
    expect(layout.find(102, 103)?.id).toBe('t')
    expect(layout.find(4000, 4000)).toBeNull()
    layout.release('t')
    expect(layout.nodes.find((n) => n.id === 't')?.fx).toBeNull()
  })

  it('has no bounds without nodes and a box around them once there are', () => {
    const layout = new GraphLayout()
    expect(layout.bounds()).toBeNull()
    layout.sync(STRUCTURE)
    layout.pin('d', -50, -50)
    layout.pin('t', 50, 50)
    layout.pin('s', 0, 0)
    const box = layout.bounds()
    expect(box?.minX).toBeLessThanOrEqual(-50)
    expect(box?.maxY).toBeGreaterThanOrEqual(50)
  })

  it('reads both ends of a link once d3 resolved them', () => {
    const layout = new GraphLayout()
    layout.sync(STRUCTURE)
    const drawn = layout.links.map(endpoints)
    expect(drawn.every((e) => e !== null)).toBe(true)
    expect(endpoints({ source: 'd', target: 't', type: 'contains_thema' })).toBeNull()
  })

  it('ignores a link to a node that is not on screen', () => {
    const layout = new GraphLayout()
    layout.sync({
      nodes: STRUCTURE.nodes,
      edges: [...STRUCTURE.edges, { source: 't', target: 'ghost', type: 'contains_subthema' }],
    })
    expect(layout.links).toHaveLength(2)
  })

  // Een ouder met veel kinderen heeft een grotere ring nodig: op een omtrek van
  // 2πr moeten n labels naast elkaar passen. Zonder dat werd een thema met 27
  // subthema's een propje waarin elk label het volgende overschreef.
  it('makes room for a parent with many children', async () => {
    const branch = (children: number): number => {
      const layout = new GraphLayout(true)
      const nodes: LayoutInput['nodes'][number][] = [{ id: 'p', type: 'thema', family: 0, depth: 0 }]
      const edges: LayoutInput['edges'][number][] = []
      for (let i = 0; i < children; i++) {
        nodes.push({ id: 'c' + i, type: 'subthema', family: 0, depth: 1 })
        edges.push({ source: 'p', target: 'c' + i, type: 'contains_subthema' })
      }
      layout.sync({ nodes, edges })
      const parent = layout.position('p')
      let sum = 0
      for (let i = 0; i < children; i++) {
        const child = layout.position('c' + i)
        sum += Math.hypot((child?.x ?? 0) - (parent?.x ?? 0), (child?.y ?? 0) - (parent?.y ?? 0))
      }
      return sum / children
    }

    expect(branch(27)).toBeGreaterThan(branch(3) * 1.5)
  })
})
