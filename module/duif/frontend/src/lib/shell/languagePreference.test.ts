import { afterEach, describe, expect, it, vi } from 'vitest'
import { createBrowserLocale, followHostLanguage } from './languagePreference'
import type { Lang } from '../i18n.svelte'

afterEach(() => { vi.unstubAllGlobals() })

describe('language preference privacy boundary', () => {
  it('ignores stored content that is not an allowed language and refuses to persist invalid values', () => {
    const setItem = vi.fn()
    const getItem = vi.fn((_key: string) => 'a private transcript must never become a preference')
    vi.stubGlobal('localStorage', { getItem, setItem })
    const locale = createBrowserLocale()
    expect(locale.lang).toBe('en')
    // Eén sleutel, de eigen voorkeur; de taal van de gastheer komt uit
    // `<html lang>`. Verder wordt er niets uit de opslag gelezen.
    expect(getItem.mock.calls.flat()).toEqual(['duif.ui-language'])
    locale.choose('private content' as Lang)
    expect(setItem).not.toHaveBeenCalled()
    locale.choose('nl')
    expect(setItem).toHaveBeenCalledExactlyOnceWith('duif.ui-language', 'nl')
  })

  it('keeps language selection usable when the browser denies preference storage', () => {
    vi.stubGlobal('localStorage', {
      getItem: () => { throw new Error('denied') },
      setItem: () => { throw new Error('denied') },
    })
    const locale = createBrowserLocale()
    expect(locale.lang).toBe('en')
    expect(() => locale.choose('nl')).not.toThrow()
    expect(locale.lang).toBe('nl')
  })

  it('follows the host language without leaving a second, diverging preference behind', () => {
    const setItem = vi.fn()
    vi.stubGlobal('localStorage', { getItem: () => null, setItem })
    const locale = createBrowserLocale()
    followHostLanguage(locale, 'nl')
    expect(locale.lang).toBe('nl')
    // chosen houdt de serverconfig van de taal af; de gastheer onthoudt zijn
    // eigen keuze, dus de module bewaart er niets van.
    expect(locale.chosen).toBe(true)
    expect(setItem).not.toHaveBeenCalled()
  })
})
