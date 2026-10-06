// De ontwikkelaarschakelaar. Versiegegevens (model, gedragscontract, de drie
// hashes) zijn bouwbewijs, geen antwoord: een scholier of demonstratiepubliek
// leest ze niet en raakt er alleen door afgeleid. Ze staan er dus alleen voor
// wie er expliciet om vraagt.
//
// Zelfde vorm als entryIntent: de querystring is de enige invoer. Er wordt
// niets bewaard — de module schrijft geen browseropslag buiten de taalkeuze
// (zie de frontend-wachter in meeting_frontend_test.go). `?dev=1` zet de
// schakelaar aan voor deze paginalading; zonder parameter staat hij uit.
export function readDevMode(search: string): boolean {
  return new URLSearchParams(search).get('dev') === '1'
}

// browserDevMode leest de stand van de echte pagina. Ontbreekt er een browser
// (server, test zonder DOM), dan staat de schakelaar uit.
export function browserDevMode(): boolean {
  if (typeof globalThis.location === 'undefined') return false
  return readDevMode(globalThis.location.search)
}
