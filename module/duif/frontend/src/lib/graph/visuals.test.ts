import { describe, expect, it } from 'vitest'
import { iconKey, nodeRadius, nodeShape, relationStyle } from './visuals'
import { NODE_TYPES } from './types'

describe('the shape language of the map', () => {
  it('gives every table of this knowledge base a size and a shape', () => {
    for (const type of NODE_TYPES) {
      expect(nodeRadius(type), type).toBeGreaterThan(0)
      expect(nodeShape(type), type).toBeTypeOf('string')
    }
  })

  it('makes the ordering layers bigger than what hangs under them', () => {
    expect(nodeRadius('domein')).toBeGreaterThan(nodeRadius('thema'))
    expect(nodeRadius('thema')).toBeGreaterThan(nodeRadius('subthema'))
    expect(nodeRadius('destillaat')).toBeGreaterThan(nodeRadius('quote'))
  })

  it('tells a source apart from a theme by its shape, not only by its colour', () => {
    expect(nodeShape('interview')).not.toBe(nodeShape('thema'))
    expect(nodeShape('domein')).not.toBe(nodeShape('thema'))
  })
})

describe('iconKey', () => {
  it('gives the three roles a fixed glyph, whatever they are called', () => {
    expect(iconKey('destillaat', 'destillaat:1', 'Een bevinding')).toBe('file')
    expect(iconKey('quote', 'quote:1', 'Een citaat')).toBe('quotes')
    expect(iconKey('interview', 'interview:1', 'corbel2016')).toBe('book')
  })

  it('never gives two themes the same glyph, and keeps a theme its own', () => {
    const ids = ['thema:a', 'thema:b', 'thema:c', 'thema:d']
    const first = ids.map((id) => iconKey('thema', id, 'Onderwerp ' + id))
    expect(new Set(first).size).toBe(ids.length)
    // Dezelfde kennisbank levert elke sessie dezelfde iconen.
    expect(ids.map((id) => iconKey('thema', id, 'Onderwerp ' + id))).toEqual(first)
  })

  it('derives a glyph from what an ordering layer is about', () => {
    expect(iconKey('subthema', 'subthema:1', 'Groen en natuur in de stad')).toBe('tree')
  })
})

describe('relationStyle', () => {
  it('reads this knowledge base its lower-case relation names', () => {
    // De wire stuurt kleine letters; zonder normalisatie zou elke band dezelfde
    // naamloze kleur krijgen in plaats van zijn familie.
    expect(relationStyle('contains_subthema').color).toBe(relationStyle('CONTAINS_SUBTHEMA').color)
    expect(relationStyle('contains_thema').color).not.toBe(relationStyle('belongs_to_subthema').color)
  })

  it('gives an unknown relation a colour of its own instead of dead grey', () => {
    const style = relationStyle('spreekt_tegen')
    expect(style.color).toMatch(/^#/)
    expect(relationStyle('spreekt_tegen').color).toBe(style.color)
  })
})

