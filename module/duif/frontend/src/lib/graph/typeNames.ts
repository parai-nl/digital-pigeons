// Het woord voor één soort knoop, in beide talen.
//
// Waarom apart van readingCopy.ts: die tabel is het hele graafwoordenboek en
// hoort bij de graafweergave, die pas laadt als je de kaart opent. `types.ts`
// staat wél in de eerste importketen — services.svelte.ts bouwt de GraphStore
// bij het opstarten — en daar is één typewoord nodig als vervangend kaartlabel.
// Zou types.ts het hele woordenboek importeren, dan verhuisde dat
// mee naar de entry-chunk.
//
// readingCopy.ts neemt deze tabel gewoon in zich op, dus er is één plek waar
// het typewoord staat.

export const typeNamesEn = {
  'graph.type.domein': 'Domain',
  'graph.type.thema': 'Theme',
  'graph.type.subthema': 'Sub-theme',
  'graph.type.destillaat': 'Finding',
  'graph.type.quote': 'Quote',
  'graph.type.interview': 'Source',
} as const

export const typeNamesNl: Record<keyof typeof typeNamesEn, string> = {
  'graph.type.domein': 'Domein',
  'graph.type.thema': 'Thema',
  'graph.type.subthema': 'Subthema',
  'graph.type.destillaat': 'Bevinding',
  'graph.type.quote': 'Citaat',
  'graph.type.interview': 'Bron',
}
