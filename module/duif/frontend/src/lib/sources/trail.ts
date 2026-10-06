import { bilingualFrom } from '../api'
import type { Bilingual } from '../i18n.svelte'

// Het kruimelpad van een bron: waar in de kennisgraaf hij vandaan komt. De
// server levert het op GET /graph/nodes/{id} als `trail` (van de wortel naar de
// ouder).
//
// Drie regels, allemaal omdat het antwoord van de server een aanname is en geen
// waarheid:
//  1. Alleen de drie structuurlagen komen in beeld — domein › thema ›
//     subthema. Een standpunt of citaat in het spoor is de bron zelf, geen
//     plek waar hij hangt, en een perspectief toont de graaf niet.
//  2. Alleen het label komt in beeld. Een id of een kolomnaam zegt de lezer
//     niets en hoort niet op het scherm.
//  3. Het label is kennisbanktekst en komt als taalobject {nl, en} binnen (een
//     eentalig corpus mag een kale string sturen). De taal wordt pas bij het
//     tekenen gekozen, zodat een taalwissel het pad meeneemt — net als de rest
//     van de lade.
// Wat niet aan die vorm voldoet, bestaat niet: het spoor wordt korter, nooit
// een fout.
const CRUMB_TYPES: readonly string[] = ['domein', 'thema', 'subthema']

export interface Crumb {
  // id is de sleutel van de stap, niet iets om te tonen.
  id: string
  label: Bilingual | string
}

function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : null
}

function label(value: unknown): Bilingual | string | null {
  if (typeof value === 'string') return value.trim() === '' ? null : value.trim()
  const text = bilingualFrom(value)
  if (!text) return null
  const out: Bilingual = {}
  for (const [lang, s] of Object.entries(text) as [keyof Bilingual, string][]) {
    if (s.trim() !== '') out[lang] = s.trim()
  }
  return out.en === undefined && out.nl === undefined ? null : out
}

function crumb(value: unknown): Crumb | null {
  const raw = record(value)
  if (!raw) return null
  if (typeof raw.id !== 'string' || raw.id === '') return null
  if (typeof raw.type !== 'string' || !CRUMB_TYPES.includes(raw.type)) return null
  const text = label(raw.label)
  return text === null ? null : { id: raw.id, label: text }
}

// parseTrail leest het kruimelpad uit het node-antwoord. Een dubbele stap telt
// één keer.
export function parseTrail(body: unknown): Crumb[] {
  const raw = record(body)
  if (!raw || !Array.isArray(raw.trail)) return []
  const out: Crumb[] = []
  for (const step of raw.trail) {
    const c = crumb(step)
    if (c && !out.some((seen) => seen.id === c.id)) out.push(c)
  }
  return out
}

// crumbText is het label in de taal van de lezer; `pick` is locale.pickContent,
// die op de andere taal terugvalt als de kennisbank de UI-taal niet kent.
export function crumbText(crumb: Crumb, pick: (text: Bilingual | undefined) => string): string {
  return typeof crumb.label === 'string' ? crumb.label : pick(crumb.label)
}

export interface TrailStep {
  id: string
  text: string
}

// Een subthema kan dezelfde titel dragen als zijn thema (in de v3-kennisbank
// "Gehoor en geluid bij vogels" en "Magnetisch zintuig bij vogels"). Twee keer
// dezelfde woorden achter elkaar zeggen de lezer niets en lezen als een fout,
// dus een stap die in de getoonde taal zegt wat de stap erboven al zei, valt
// weg; de bovenste blijft. Dat kan pas na de taalkeuze: in de ene taal kan de
// titel gelijk zijn en in de andere niet. Hoofdletters, meervoudige witruimte
// en de Unicode-schrijfwijze van een accent tellen niet als verschil: de lezer
// ziet daar dezelfde titel in. Een ander accent of een ander woord wel.
function sameLabel(text: string): string {
  return text.normalize('NFC').replace(/\s+/g, ' ').trim().toLowerCase()
}

// trailSteps is het kruimelpad zoals het op het scherm komt: in de taal van de
// lezer, zonder een stap die het niveau erboven herhaalt.
export function trailSteps(trail: readonly Crumb[], pick: (text: Bilingual | undefined) => string): TrailStep[] {
  const out: TrailStep[] = []
  let above: string | null = null
  for (const crumb of trail) {
    const text = crumbText(crumb, pick)
    const key = sameLabel(text)
    if (key !== above) out.push({ id: crumb.id, text })
    above = key
  }
  return out
}
