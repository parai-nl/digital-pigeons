import { describe, expect, it } from 'vitest'
import { inlineRuns } from './inline'

describe('inlineRuns', () => {
  it('zet **vet** om in een strong-run en laat de rest tekst', () => {
    expect(inlineRuns('Wij zijn **alleseters** geworden.')).toEqual([
      { kind: 'text', value: 'Wij zijn ' },
      { kind: 'strong', value: 'alleseters' },
      { kind: 'text', value: ' geworden.' },
    ])
  })

  it('maakt van een kopregel een vette regel zonder hekjes', () => {
    expect(inlineRuns('## Wat we eten\nZaden.')).toEqual([
      { kind: 'strong', value: 'Wat we eten' },
      { kind: 'text', value: '\nZaden.' },
    ])
  })

  it('kent *cursief* maar niet een los sterretje of een sterretje in een woord', () => {
    expect(inlineRuns('een *echte* duif')).toEqual([
      { kind: 'text', value: 'een ' },
      { kind: 'em', value: 'echte' },
      { kind: 'text', value: ' duif' },
    ])
    expect(inlineRuns('5 * 3 = 15')).toEqual([{ kind: 'text', value: '5 * 3 = 15' }])
    expect(inlineRuns('a*b*c')).toEqual([{ kind: 'text', value: 'a*b*c' }])
  })

  it('laat ongepaarde markering staan en rendert nooit HTML', () => {
    expect(inlineRuns('**open en <b>niet</b>')).toEqual([{ kind: 'text', value: '**open en <b>niet</b>' }])
  })

  it('geeft lege tekst geen runs', () => {
    expect(inlineRuns('')).toEqual([])
  })
})
