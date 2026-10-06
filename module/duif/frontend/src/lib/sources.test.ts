import { describe, expect, it } from 'vitest'
import type { Bilingual, Lang } from './i18n.svelte'
import { parseKBStatus, parseSourceDrops, parseSources, sourcePerspective, sourceText, sourceTranslated } from './sources'

// De bronnenlezer is de enige plek waar de wire-vorm wordt vertrouwd; deze
// tests leggen vast wat er van een oude rij, een nieuwe rij en een kapotte rij
// overblijft.
describe('parseSources', () => {
  it('reads the full new shape including id and ref', () => {
    expect(
      parseSources([
        {
          number: 1,
          type: 'destillaat',
          id: 'destillaat:9f1c',
          text: 'Pigeons nest on ledges.',
          perspective: 'The critical scientist',
          theme: 'Urban ecology',
          ref: { quote: 'They prefer a ledge.', paper: 'lewald1987', doi: '10.1234/x' },
        },
      ]),
    ).toEqual([
      {
        number: 1,
        type: 'destillaat',
        id: 'destillaat:9f1c',
        text: 'Pigeons nest on ledges.',
        perspective: 'The critical scientist',
        // Het thema van de wire komt er niet in: de bronkaart toont het niet,
        // dus is er niets om te bewaren.
        ref: { quote: 'They prefer a ledge.', paper: 'lewald1987', doi: '10.1234/x' },
      },
    ])
  })

  it('reads an old row without id and with the old ref fields', () => {
    expect(parseSources([{ number: 2, type: 'standpunt', text: 'Old row.', ref: { file: 'x.md', doi: '10.5/y' } }])).toEqual([
      { number: 2, type: 'standpunt', text: 'Old row.', ref: { file: 'x.md', doi: '10.5/y' } },
    ])
  })

  it('drops fields that are not strings and leaves them undefined', () => {
    const [source] = parseSources([
      { number: 3, type: 7, text: null, id: 42, perspective: '', theme: {}, ref: { quote: 5, paper: 'p' } },
    ])
    expect(source).toEqual({ number: 3, type: '', text: '', ref: { paper: 'p' } })
    expect(source.id).toBeUndefined()
    expect(source.ref?.quote).toBeUndefined()
  })

  it('drops a ref that carries nothing usable', () => {
    expect(parseSources([{ number: 4, type: 'destillaat', text: 't', ref: { quote: '', file: null } }])).toEqual([
      { number: 4, type: 'destillaat', text: 't' },
    ])
    expect(parseSources([{ number: 5, type: 'destillaat', text: 't', ref: 'nope' }])).toEqual([
      { number: 5, type: 'destillaat', text: 't' },
    ])
  })

  it('skips entries without a usable number and anything that is not a list', () => {
    expect(parseSources([{ type: 'destillaat', text: 't' }, null, 'x', { number: Number.NaN, text: 't' }])).toEqual([])
    expect(parseSources(undefined)).toEqual([])
    expect(parseSources({ sources: [] })).toEqual([])
  })
})

describe('parseKBStatus', () => {
  it('accepts the three known states and nothing else', () => {
    expect(parseKBStatus('sources')).toBe('sources')
    expect(parseKBStatus('gap')).toBe('gap')
    expect(parseKBStatus('persona_only')).toBe('persona_only')
    expect(parseKBStatus('sourcesish')).toBeUndefined()
    expect(parseKBStatus(1)).toBeUndefined()
  })
})


it('reads only public bibliography fields and preserves legacy values', () => {
  const [source] = parseSources([{ number: 1, ref: { id: 'interview:synthetic', paper: 'Synthetic title', citation: 'Synthetic original citation', citationConfidence: 'low', citationMissing: 'Pages', doi: '10.1/synthetic', storage_key: 'PRIVATE_CANARY', content_hash: 'PRIVATE_CANARY', bron_bestand: 'PRIVATE_CANARY', citatie_bron: 'PRIVATE_CANARY' } }])
  expect(source.ref).toEqual({ id: 'interview:synthetic', paper: 'Synthetic title', citation: 'Synthetic original citation', citationConfidence: 'low', citationMissing: 'Pages', doi: '10.1/synthetic' })
  expect(JSON.stringify(source)).not.toContain('PRIVATE_CANARY')
  expect(parseSources([{ number: 1, ref: { citation: {}, citationMissing: true, citationConfidence: '97%' } }])[0].ref).toBeUndefined()
})

// De tweetalige bronkaart (duiven/3). De brontaalvelden blijven staan zoals ze
// altijd stonden; een oud opgeslagen antwoord houdt daardoor zijn eigen
// Nederlandse momentopname.
describe('bilingual sources', () => {
  const wire = {
    number: 1,
    type: 'destillaat',
    text: 'Duiven nestelen op richels.',
    perspective: 'De kritische wetenschapper',
    theme: 'Stadsecologie',
    sourceLanguage: 'nl',
    localized: {
      text: { nl: 'Duiven nestelen op richels.', en: 'Pigeons nest on ledges.' },
      perspective: { nl: 'De kritische wetenschapper', en: 'The critical scientist' },
      theme: { nl: 'Stadsecologie', en: 'Urban ecology' },
    },
  }

  const pick = (lang: Lang) => (text: Bilingual | undefined) => (text ? text[lang] || text[lang === 'nl' ? 'en' : 'nl'] || '' : '')
  const shown = (lang: Lang) => (text: Bilingual | undefined): Lang | null =>
    text?.[lang] ? lang : text?.[lang === 'nl' ? 'en' : 'nl'] ? (lang === 'nl' ? 'en' : 'nl') : null

  it('keeps both languages and the source language', () => {
    const [source] = parseSources([wire])
    expect(source.sourceLanguage).toBe('nl')
    expect(source.localized?.text).toEqual({ nl: 'Duiven nestelen op richels.', en: 'Pigeons nest on ledges.' })
    // Het brontaalveld blijft ongewijzigd naast de vertaling staan.
    expect(source.text).toBe('Duiven nestelen op richels.')
  })

  it('shows the reader language and reports a translation only to the other reader', () => {
    const [source] = parseSources([wire])
    expect(sourceText(source, pick('en'))).toBe('Pigeons nest on ledges.')
    expect(sourcePerspective(source, pick('en'))).toBe('The critical scientist')
    expect(sourceTranslated(source, shown('en'))).toBe(true)
    // De Nederlandse lezer krijgt het origineel, dus geen label.
    expect(sourceText(source, pick('nl'))).toBe('Duiven nestelen op richels.')
    expect(sourcePerspective(source, pick('nl'))).toBe('De kritische wetenschapper')
    expect(sourceTranslated(source, shown('nl'))).toBe(false)
  })

  // Het tweetalige thema werd wel gelezen en nergens getoond. De
  // bronkaart heeft het ook vóór duiven/3 nooit laten zien — het thema staat in
  // het kruimelpad onder "Over deze bron" — dus draagt de bron het niet meer.
  it('keeps no theme of its own, in either language', () => {
    const [source] = parseSources([wire])
    expect(source.localized).toEqual({
      text: { nl: 'Duiven nestelen op richels.', en: 'Pigeons nest on ledges.' },
      perspective: { nl: 'De kritische wetenschapper', en: 'The critical scientist' },
    })
    expect(JSON.stringify(source)).not.toContain('Stadsecologie')
    expect(JSON.stringify(source)).not.toContain('Urban ecology')
  })

  it('leaves a source from before the translation exactly as it was', () => {
    const [source] = parseSources([{ number: 1, type: 'destillaat', text: 'Duiven nestelen op richels.', perspective: 'De kritische wetenschapper' }])
    expect(source.localized).toBeUndefined()
    expect(source.sourceLanguage).toBeUndefined()
    expect(sourceText(source, pick('en'))).toBe('Duiven nestelen op richels.')
    expect(sourcePerspective(source, pick('en'))).toBe('De kritische wetenschapper')
    // Zonder taal op de bron valt er niets te melden over een vertaling.
    expect(sourceTranslated(source, shown('en'))).toBe(false)
  })

  it('ignores a language field that is not a language and a localized value that is not text', () => {
    const [source] = parseSources([{ number: 1, type: 'destillaat', text: 'x', sourceLanguage: 'de', localized: { text: { nl: 42 } } }])
    expect(source.sourceLanguage).toBeUndefined()
    expect(source.localized).toBeUndefined()
  })
})

// De uitvalmelding. Zonder bruikbaar aantal is er niets te melden —
// een melding zonder getal laat de lezer raden.
describe('parseSourceDrops', () => {
  it('reads a count with its reasons', () => {
    expect(parseSourceDrops({ count: 3, reasons: { no_text: 1, other_knowledge_base: 2 } })).toEqual({
      count: 3,
      reasons: { no_text: 1, other_knowledge_base: 2 },
    })
  })

  it('reads a count without reasons', () => {
    expect(parseSourceDrops({ count: 1 })).toEqual({ count: 1, reasons: {} })
  })

  it('returns nothing for zero, a missing count or a non-object', () => {
    expect(parseSourceDrops({ count: 0, reasons: { no_text: 1 } })).toBeUndefined()
    expect(parseSourceDrops({ reasons: { no_text: 1 } })).toBeUndefined()
    expect(parseSourceDrops(null)).toBeUndefined()
    expect(parseSourceDrops('drie')).toBeUndefined()
  })

  it('skips reason counts that are not usable numbers', () => {
    expect(parseSourceDrops({ count: 2, reasons: { no_text: 2, kapot: 'veel', leeg: 0 } })).toEqual({
      count: 2,
      reasons: { no_text: 2 },
    })
  })
})
