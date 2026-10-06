// Teksten van de kennisgraaf. Sleutels zijn Engels en dot-genamespaced; de
// Nederlandse tabel is getypeerd op de Engelse, zodat svelte-check ontbrekende
// of overbodige sleutels afkeurt. De drie error.*-codes horen bij de
// reason-codes van de graafroutes: locale.fromServer valt erop terug als de
// server geen eigen melding meestuurt. Ze staan daarom woord voor woord gelijk
// aan errorMessages in module/duif/errors.go — twee verschillende zinnen voor
// dezelfde situatie, afhankelijk van of de server zijn melding kwijt kon, is
// voor de lezer niet uit te leggen.
export const graphEn = {
  'graph.tab': 'Knowledge graph',
  'graph.module.loading': 'Opening the knowledge graph… Your conversation stays available.',
  'graph.module.failed': 'The graph view could not be opened. Try again or use the back button to return to your conversation.',

  'graph.retry': 'Try again',




  // De stand van de import zelf staat in de gedeelde KnowledgeBanner
  // (knowledge.state.*); hier blijft alleen wat die banner niet weet: dat de
  // statusroute zélf onbereikbaar was.

  // Eén woord voor één ding: net als source.type.destillaat in de bronkaart.


  'error.node_not_found': 'That node is not in this knowledge graph.',
  'error.invalid_offset': 'The offset must be a whole number of zero or more.',
  'error.graph_unavailable': 'The knowledge graph cannot be read right now. Please try again in a moment.',
} as const

export const graphNl: Record<keyof typeof graphEn, string> = {
  'graph.tab': 'Kennisgraaf',
  'graph.module.loading': 'De kennisgraaf wordt geopend… Je gesprek blijft beschikbaar.',
  'graph.module.failed': 'De graafweergave kon niet worden geopend. Probeer opnieuw of gebruik de terugknop om naar je gesprek te gaan.',

  'graph.retry': 'Opnieuw proberen',







  'error.node_not_found': 'Die knoop staat niet in deze kennisgraaf.',
  'error.invalid_offset': 'De offset moet een geheel getal van nul of hoger zijn.',
  'error.graph_unavailable': 'De kennisgraaf is nu niet te lezen. Probeer het zo weer.',
}
