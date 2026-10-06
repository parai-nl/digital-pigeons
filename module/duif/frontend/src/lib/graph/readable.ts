// Wat een lezer iets zegt, en wat alleen een database iets zegt.
//
// De kennisbank draagt naast tekst ook opsommingswaarden: `scope` is
// `algemeen` of `persoonlijk`, `domain` is `wonen_en_leefomgeving`, `speaker`
// is `respondent`. Dat zijn sleutels uit de importpijplijn, geen inhoud. Ze
// onder een nette Nederlandse kop zetten maakt ze niet leesbaar — het maakt de
// kolomnaam alleen onzichtbaar terwijl de waarde nog steeds die van een kolom
// is.
//
// De regel is bewust grof en zonder lijst: een waarde is leesbaar als er een
// spatie of een hoofdletter in staat. Zo overleeft elke echte zin, elke titel
// en elke naam hem, en valt elke snake_case-sleutel, elk los kleingeschreven
// codewoord en elke `tabel:uuid` eruit. Een nieuwe kolom met een nieuwe
// opsomming hoeft daar niet apart voor te worden aangemeld.

/**
 * Of een metadatawaarde bedoeld is om gelezen te worden. Leeg is nooit
 * leesbaar; verder telt één spatie of één hoofdletter als bewijs dat er
 * geschreven is in plaats van gecodeerd.
 */
export function isReadableValue(value: string): boolean {
  const text = value.trim()
  if (text === '') return false
  if (/\s/.test(text)) return true
  return /\p{Lu}/u.test(text)
}

// Het kaartlabel is een ander geval dan een metadatawaarde.
//
// Tot nu toe ging het label van de wire ongefilterd naar de kaart, de boom en
// de detailkop, met de knoop-id als terugval op een leeg label. Allebei kunnen
// een lezer niets zeggen: `wonen_en_leefomgeving` is een sleutel uit de
// importpijplijn, en `destillaat:0be06f8c-…` is een rij in een tabel. Op een
// kaart staat zo'n sleutel er ook nog eens in het groot: er is geen kop die hem
// verbergt, hij ís de kop.
//
// De regel van isReadableValue kan hier niet dienen. Een bronsleutel als
// `lewald1987` is kleingeschreven en zonder spatie, en hoort wél op de kaart —
// dat is de naam waaronder dat paper in de kennisbank staat. Voor een label
// geldt daarom de omgekeerde, smallere regel: alleen wat de vórm van een
// sleutel heeft gaat eruit. Twee vormen, allebei zonder spatie: snake_case, en
// `tabel:rij`.
const KEY_SHAPE = /^(?:\w+(?:_\w+)+|[A-Za-z]\w*:\S+)$/

/** Of dit label een naam is en geen sleutel uit de pijplijn. */
export function isReadableLabel(label: string): boolean {
  const text = label.trim()
  if (text === '') return false
  if (/\s/.test(text)) return true
  return !KEY_SHAPE.test(text)
}

/**
 * Het label zoals het op een kaart mag staan. Is het geen naam maar een sleutel
 * of een rij-id, dan komt `fallback` in de plaats: het woord voor de soort
 * knoop ("Thema", "Bevinding"). Dat is geen naam, maar het is waar en leesbaar,
 * en de kaart blijft aanklikbaar — het detailpaneel eronder draagt de inhoud.
 *
 * Er wordt niets gelogd, ook niet in een ontwikkelbouw: modulecode in lib/
 * schrijft geen console-regels (bewaakt door meeting_frontend_test.go). Het
 * signaal voor de bouwer staat op de knoop zelf, als `unnamed` — dat is te
 * lezen in een test en in de inspector, en het overleeft een taalwissel.
 */
export function readableLabel(label: string, fallback: string): string {
  const text = label.trim()
  return isReadableLabel(text) ? text : fallback
}
