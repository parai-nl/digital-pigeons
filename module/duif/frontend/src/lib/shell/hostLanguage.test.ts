import { afterEach, describe, expect, it } from 'vitest'
import { hostLanguage, observeHostLanguage } from './hostLanguage'

// De module heeft geen eigen taalkiezer meer: zij leest de taal van de
// gastheer. Deze test legt vast waar zij kijkt en wat zij negeert.
afterEach(() => {
  document.documentElement.removeAttribute('lang')
})

describe('hostLanguage', () => {
  it('reads the language the host writes on the document, region tag and all', () => {
    document.documentElement.lang = 'nl-NL'
    expect(hostLanguage()).toBe('nl')
  })

  it('reports no host language when the document does not name one', () => {
    expect(hostLanguage()).toBeNull()
  })

  it('reports no host language for a language this module does not speak', () => {
    document.documentElement.lang = 'de'
    expect(hostLanguage()).toBeNull()
  })

  it('reports a later switch and stops reporting once released', async () => {
    const seen: string[] = []
    const stop = observeHostLanguage((lang) => seen.push(lang))
    document.documentElement.lang = 'nl'
    await Promise.resolve()
    expect(seen).toEqual(['nl'])
    stop()
    document.documentElement.lang = 'en'
    await Promise.resolve()
    expect(seen).toEqual(['nl'])
  })
})
