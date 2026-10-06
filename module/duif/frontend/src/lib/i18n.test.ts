import { describe, expect, it } from 'vitest'
import { en, interpolate, nl, UiLocale } from './i18n.svelte'

// Sleutelpariteit EN/NL bewaakt het typesysteem (nl: Record<keyof typeof en>);
// wat het typesysteem niet ziet — lege teksten en scheve placeholders — toetsen
// we hier.
describe('i18n dictionaries', () => {
  it('has no empty strings in either language', () => {
    for (const [key, value] of Object.entries(en)) expect(value, `en:${key}`).not.toBe('')
    for (const [key, value] of Object.entries(nl)) expect(value, `nl:${key}`).not.toBe('')
  })

  it('uses the same placeholders in both languages', () => {
    const placeholders = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort()
    for (const key of Object.keys(en) as (keyof typeof en)[]) {
      expect(placeholders(nl[key]), key).toEqual(placeholders(en[key]))
    }
  })

  it('has identical key sets at runtime as well', () => {
    expect(Object.keys(nl).sort()).toEqual(Object.keys(en).sort())
  })
})

describe('interpolate', () => {
  it('fills known placeholders and leaves unknown ones visible', () => {
    expect(interpolate('{n} blocks for {who}', { n: 3, who: 'x' })).toBe('3 blocks for x')
    expect(interpolate('{n} blocks', {})).toBe('{n} blocks')
  })
})

describe('UiLocale', () => {
  it('translates in the active language and switches reactively', () => {
    const locale = new UiLocale()
    expect(locale.t('common.tab.meeting')).toBe('Meeting')
    locale.lang = 'nl'
    expect(locale.t('common.tab.meeting')).toBe('Vergadering')
  })

  it('prefers the server message in the UI language, then English, then the reason code, then generic', () => {
    const locale = new UiLocale('nl')
    expect(locale.fromServer({ en: 'E', nl: 'N' })).toBe('N')
    expect(locale.fromServer({ en: 'E' })).toBe('E')
    expect(locale.fromServer(null, 'llm_budget_exceeded')).toBe(nl['error.llm_budget_exceeded'])
    // De 400-route van de server stuurt zijn eigen tweetalige zin mee; valt
    // die ooit weg, dan is invalid_request de code die overblijft.
    expect(locale.fromServer(null, 'invalid_request')).toBe(nl['error.invalid_request'])
    expect(locale.fromServer(null, 'no_such_reason')).toBe(nl['error.generic'])
    expect(locale.fromServer(undefined)).toBe(nl['error.generic'])
  })

  it('picks bilingual labels with fallback', () => {
    const locale = new UiLocale('nl')
    expect(locale.pick({ en: 'Pigeon', nl: 'Duif' })).toBe('Duif')
    expect(locale.pick({ en: 'Pigeon' })).toBe('Pigeon')
    expect(locale.pick(undefined, 'id')).toBe('id')
  })
})
