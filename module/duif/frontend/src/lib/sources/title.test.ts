import { describe, expect, it } from 'vitest'
import { paperTitle, sourceKey } from './title'

// Eén veld, twee betekenissen: het paperveld is een titel of een
// citatiesleutel. De lade moet ze uit elkaar houden, want ze verdienen een
// ander woord op het scherm.

describe('paperTitle', () => {
  it('keeps a real title', () => {
    expect(paperTitle({ paper: 'Proximity to food sources' })).toBe('Proximity to food sources')
  })
  it('is empty for a citation key or for nothing at all', () => {
    expect(paperTitle({ paper: 'lim2023' })).toBe('')
    expect(paperTitle({})).toBe('')
    expect(paperTitle(undefined)).toBe('')
  })
})

describe('sourceKey', () => {
  it('reads the citation key the collection files the paper under', () => {
    expect(sourceKey({ paper: 'lim2023' })).toBe('lim2023')
    expect(sourceKey({ paper: 'capocciaetal2018-lovedorloathed-1' })).toBe('capocciaetal2018-lovedorloathed-1')
  })
  it('is empty for a real title or for nothing at all', () => {
    expect(sourceKey({ paper: 'Proximity to food sources' })).toBe('')
    expect(sourceKey({})).toBe('')
    expect(sourceKey(undefined)).toBe('')
  })
})
