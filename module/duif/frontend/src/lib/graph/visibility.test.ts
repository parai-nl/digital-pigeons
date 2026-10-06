import { describe, expect, it } from 'vitest'
import type { GraphNode, NodeType } from './types'
import { computeVisible, matchNodes, matchesText, nestRows, visibleEdges } from './visibility'

function node(id: string, type: NodeType, label = id): GraphNode {
  return { id, type, label, metadata: {} }
}

// Een kleine plattegrond: twee domeinen, onder het eerste een thema met twee
// subthema's, en onder het eerste subthema twee standpunten uit hetzelfde
// bronpaper — dat bronpaper hangt dus onder allebei.
function fixture() {
  const nodes = new Map<string, GraphNode>([
    ['d', node('d', 'domein')],
    ['e', node('e', 'domein')],
    ['t', node('t', 'thema')],
    ['s', node('s', 'subthema')],
    ['u', node('u', 'subthema')],
    ['x', node('x', 'destillaat', 'Voeren houdt de kolonie groot')],
    ['y', node('y', 'destillaat')],
    ['q', node('q', 'quote')],
    ['b', node('b', 'interview')],
  ])
  const childIds = new Map<string, string[]>([
    ['d', ['t']],
    ['t', ['s', 'u']],
    ['s', ['x', 'y']],
    ['x', ['q', 'b']],
    ['y', ['b']],
  ])
  return { nodes, childIds, roots: ['d', 'e'], families: new Map([['t', 3]]) }
}

describe('computeVisible', () => {
  it('shows only the roots until something is expanded', () => {
    const { nodes, childIds, roots } = fixture()
    const view = computeVisible({ nodes, roots, childIds, revealed: new Set() })
    expect(view.rows.map((r) => r.id)).toEqual(['d', 'e'])
    expect([...view.ids]).toEqual(['d', 'e'])
    expect(view.rows[0]).toMatchObject({ depth: 0, index: 0, setsize: 2, expandable: true, expanded: false })
  })

  it('reveals one level per expanded parent and drops it again on collapse', () => {
    const { nodes, childIds, roots } = fixture()
    const open = computeVisible({ nodes, roots, childIds, revealed: new Set(['d', 't']) })
    expect(open.rows.map((r) => r.id)).toEqual(['d', 't', 's', 'u', 'e'])
    expect(open.rows[1]).toMatchObject({ depth: 1, expanded: true })

    // Inklappen van het thema neemt zijn kinderen mee; wat eronder hing komt
    // niet als wees terug.
    const closed = computeVisible({ nodes, roots, childIds, revealed: new Set(['d']) })
    expect(closed.rows.map((r) => r.id)).toEqual(['d', 't', 'e'])
  })

  it('lets a node hang under both its parents, each with its own key', () => {
    const { nodes, childIds, roots } = fixture()
    const view = computeVisible({ nodes, roots, childIds, revealed: new Set(['d', 't', 's', 'x', 'y']) })
    const papers = view.rows.filter((r) => r.id === 'b')
    expect(papers.map((r) => r.key)).toEqual(['d/t/s/x/b', 'd/t/s/y/b'])
    // Op het canvas blijft het één knoop.
    expect([...view.ids].filter((id) => id === 'b')).toHaveLength(1)
  })

  it('inherits the theme family downwards and leaves structure without one', () => {
    const { nodes, childIds, roots, families } = fixture()
    const view = computeVisible({ nodes, roots, childIds, revealed: new Set(['d', 't', 's', 'x']), families })
    expect(view.family.get('d')).toBe(-1)
    expect(view.family.get('t')).toBe(3)
    expect(view.family.get('x')).toBe(3)
    expect(view.family.get('q')).toBe(3)
    expect(view.depth.get('q')).toBe(4)
  })

  it('reports the server total as setsize and marks a loading parent', () => {
    const { nodes, childIds, roots } = fixture()
    const view = computeVisible({
      nodes,
      roots,
      childIds,
      revealed: new Set(['d', 't', 's']),
      totals: new Map([['s', 150]]),
      loading: new Set(['s']),
    })
    const distillate = view.rows.find((r) => r.id === 'x')
    expect(distillate?.setsize).toBe(150)
    expect(view.rows.find((r) => r.id === 's')?.loading).toBe(true)
  })

  it('does not walk into a cycle and skips children it never loaded', () => {
    const nodes = new Map([
      ['a', node('a', 'thema')],
      ['b', node('b', 'subthema')],
    ])
    const childIds = new Map<string, string[]>([
      ['a', ['b']],
      ['b', ['a', 'ghost']],
    ])
    const view = computeVisible({ nodes, roots: ['a'], childIds, revealed: new Set(['a', 'b']) })
    expect(view.rows.map((r) => r.key)).toEqual(['a', 'a/b'])
  })

  it('marks a quote as a leaf: no expand affordance', () => {
    const { nodes, childIds, roots } = fixture()
    const view = computeVisible({ nodes, roots, childIds, revealed: new Set(['d', 't', 's', 'x']) })
    expect(view.rows.find((r) => r.id === 'q')?.expandable).toBe(false)
  })
})

describe('visibleEdges', () => {
  it('keeps only edges with both ends on screen', () => {
    const edges = [
      { source: 'a', target: 'b', type: 'contains' },
      { source: 'b', target: 'z', type: 'contains' },
      { source: 'a', target: 'a', type: 'relates_to' },
    ]
    expect(visibleEdges(edges, new Set(['a', 'b']))).toEqual([edges[0]])
  })

  // Het relatiefilter van de knoppenbalk werkt hier, één plek vóór de kaart én
  // het leespaneel — anders zou een uitgezette band uit de tekening verdwijnen
  // en toch in de lijst met verbindingen blijven staan.
  it('drops a relationship the reader switched off', () => {
    const edges = [
      { source: 'a', target: 'b', type: 'contains' },
      { source: 'a', target: 'b', type: 'has_quote' },
    ]
    expect(visibleEdges(edges, new Set(['a', 'b']), new Set(['has_quote']))).toEqual([edges[0]])
  })
})

describe('matchesText', () => {
  it('also finds the text behind a shortened label', () => {
    const nodes = new Map<string, GraphNode>([
      ['x', { id: 'x', type: 'destillaat', label: 'Voeren houdt de kolonie…', metadata: { text: 'Voeren houdt de kolonie groot en gezond.' } }],
      ['y', { id: 'y', type: 'thema', label: 'Overlast', metadata: { beschrijving: 'Het thema' } }],
    ])
    expect([...matchesText(nodes, 'GROOT EN GEZOND')]).toEqual(['x'])
    expect([...matchesText(nodes, 'overlast')]).toEqual(['y'])
    expect([...matchesText(nodes, 'het thema')]).toEqual(['y'])
    expect([...matchesText(nodes, '  ')]).toEqual([])
  })

  // Wat niet geladen is, wordt niet doorzocht. De teller mag daar nooit een
  // uitspraak over de hele kennisbank van maken.
  it('says nothing about what has not been loaded', () => {
    expect([...matchesText(new Map(), 'kolonie')]).toEqual([])
  })
})

describe('matchNodes', () => {
  it('searches the labels of what is on screen, case-insensitively', () => {
    const { nodes } = fixture()
    const ids = new Set(['d', 't', 'x'])
    expect([...matchNodes(nodes, ids, 'KOLONIE')]).toEqual(['x'])
    // Wat niet getoond wordt telt niet mee, ook al is het geladen.
    expect([...matchNodes(nodes, new Set(['d']), 'kolonie')]).toEqual([])
    expect([...matchNodes(nodes, ids, '  ')]).toEqual([])
  })
})

describe('nestRows', () => {
  it('turns the reading order back into a nested tree', () => {
    const { nodes, childIds, roots } = fixture()
    const view = computeVisible({ nodes, roots, childIds, revealed: new Set(['d', 't']) })
    const tree = nestRows(view.rows)
    expect(tree.map((b) => b.row.id)).toEqual(['d', 'e'])
    expect(tree[0].children.map((b) => b.row.id)).toEqual(['t'])
    expect(tree[0].children[0].children.map((b) => b.row.id)).toEqual(['s', 'u'])
    expect(nestRows([])).toEqual([])
  })
})
