import { describe, expect, it } from 'vitest'
import { elapsedSuffix, WAIT_ELAPSED_AFTER_MS } from './waiting'

// Het staartje achter de wachttekst. Korte wachttijden krijgen niets:
// een teller die meteen begint te tikken maakt een normale seconde onrustig.
describe('elapsedSuffix', () => {
  it('zwijgt zolang er niet gewacht wordt', () => {
    expect(elapsedSuffix(0, 10_000)).toBe('')
  })

  it('zwijgt onder de drempel', () => {
    const since = 1_000
    expect(elapsedSuffix(since, since + WAIT_ELAPSED_AFTER_MS - 1)).toBe('')
  })

  it('noemt vanaf de drempel de hele seconden', () => {
    const since = 1_000
    expect(elapsedSuffix(since, since + WAIT_ELAPSED_AFTER_MS)).toBe(' 8 s')
    expect(elapsedSuffix(since, since + 23_400)).toBe(' 23 s')
    expect(elapsedSuffix(since, since + 63_900)).toBe(' 63 s')
  })

  it('laat een teruggezette klok geen onzin tonen', () => {
    expect(elapsedSuffix(10_000, 1_000)).toBe('')
  })
})
