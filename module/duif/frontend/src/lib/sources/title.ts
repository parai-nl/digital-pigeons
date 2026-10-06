import type { SourceRef } from '../sources'

// Het paperveld van de kennisbank draagt twee dingen door elkaar. Soms is het
// een echte titel ("Proximity to anthropogenic food sources"), soms de
// citatiesleutel waaronder het paper in de collectie staat ("lim2023"). Het
// verschil is te zien: een sleutel heeft geen spaties.
//
// De lade laat ze allebei zien, maar elk met zijn eigen woord — een titel als
// de paper waar de passage uit komt, een sleutel als bronsleutel. Eerder werd
// een sleutel stilzwijgend weggelaten, en dan noemde de lade de bron nergens
// meer bij naam.

export function paperTitle(ref: SourceRef | undefined): string {
  const paper = ref?.paper?.trim() ?? ''
  return paper !== '' && /\s/.test(paper) ? paper : ''
}

export function sourceKey(ref: SourceRef | undefined): string {
  const paper = ref?.paper?.trim() ?? ''
  return paper !== '' && !/\s/.test(paper) ? paper : ''
}
