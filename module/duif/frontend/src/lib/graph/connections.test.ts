import { describe, expect, it } from 'vitest'
import { groupedConnections } from './connections'
import type { GraphEdge, GraphNode } from './types'

const finding: GraphNode = { id: 'destillaat:a', type: 'destillaat', label: 'Bevinding', metadata: {} }
const source: GraphNode = { id: 'interview:b', type: 'interview', label: 'corbel2016', metadata: {} }
const quoteOne: GraphNode = { id: 'quote:c', type: 'quote', label: 'Eerste passage', metadata: {} }
const quoteTwo: GraphNode = { id: 'quote:d', type: 'quote', label: 'Tweede passage', metadata: {} }
const subtheme: GraphNode = { id: 'subthema:e', type: 'subthema', label: 'Stress', metadata: {} }

const nodes = new Map([finding, source, quoteOne, quoteTwo, subtheme].map((n) => [n.id, n]))
const edges: GraphEdge[] = [
  { source: source.id, target: finding.id, type: 'contains' },
  { source: finding.id, target: quoteOne.id, type: 'has_quote' },
  { source: finding.id, target: quoteTwo.id, type: 'has_quote' },
  { source: finding.id, target: subtheme.id, type: 'belongs_to_subthema' },
]

describe('groupedConnections', () => {
  it('groups a finding by what sits at the other end, in reading order', () => {
    const groups = groupedConnections(finding, nodes, edges, new Set())
    expect(groups.map((g) => g.otherType)).toEqual(['subthema', 'quote', 'interview'])
    expect(groups.find((g) => g.otherType === 'quote')?.nodes.map((n) => n.id)).toEqual([quoteOne.id, quoteTwo.id])
    expect(groups.find((g) => g.otherType === 'interview')?.relation).toBe('contains')
  })

  it('reads the same edge from the other side without inventing a direction', () => {
    const groups = groupedConnections(quoteOne, nodes, edges, new Set())
    expect(groups).toHaveLength(1)
    expect(groups[0].otherType).toBe('destillaat')
    expect(groups[0].nodes[0].id).toBe(finding.id)
  })

  it('drops a hidden relationship, so the panel says what the map shows', () => {
    const groups = groupedConnections(finding, nodes, edges, new Set(['has_quote']))
    expect(groups.map((g) => g.otherType)).toEqual(['subthema', 'interview'])
  })

  it('never lists the same neighbour twice, and never one it has not loaded', () => {
    const doubled: GraphEdge[] = [
      ...edges,
      { source: source.id, target: finding.id, type: 'contains' },
      { source: 'destillaat:missing', target: finding.id, type: 'has_quote' },
    ]
    const groups = groupedConnections(finding, nodes, doubled, new Set())
    expect(groups.find((g) => g.otherType === 'interview')?.nodes).toHaveLength(1)
    expect(groups.flatMap((g) => g.nodes).every((n) => nodes.has(n.id))).toBe(true)
  })
})
