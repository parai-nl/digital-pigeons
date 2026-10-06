import { readFileSync } from 'node:fs'
import path from 'node:path'
import { describe, expect, it } from 'vitest'

// De chips staan in een gedeeld stylesheet, niet in een <style>-blok, dus geen
// enkele componenttest ziet ze: vitest draait zonder CSS. Wat hier misging is
// wel merkbaar — de uitklapchip van de broncontrole is in de vergadering een
// <summary>, en die viel onder de uitzondering "geen knop, dus geen wijzende
// cursor". Aanklikbaar zonder handje. Deze test bewaakt precies die regel.

const css = readFileSync(path.resolve(import.meta.dirname, 'answerChips.css'), 'utf8')

// De verklaringen van één regel, op selector.
function rule(selector: string): string {
  const match = new RegExp(`(^|\\n)\\s*${selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\s*\\{([^}]*)\\}`).exec(css)
  expect(match, `regel ontbreekt: ${selector}`).not.toBeNull()
  return match?.[2] ?? ''
}

describe('answerChips.css', () => {
  it('gives every chip a pointing cursor by default', () => {
    expect(rule('.duif-answer-chip')).toContain('cursor: pointer')
  })

  it('exempts the one chip that clicks nowhere: de standpuntmarkering', () => {
    // De bronchip opent de lade en wijst dus ergens heen. De markering van de
    // Stakeholder doet dat niet — zij is een label, geen knop, en hoort de
    // lezer geen klik te beloven.
    expect(rule('.duif-answer-chip.is-stance')).toContain('cursor: default')
    expect(rule('.duif-answer-chip.is-sources')).not.toContain('cursor: default')
  })
})
