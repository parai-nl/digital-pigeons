// Het bron-DTO van de wire, één keer, voor chat én vergadering. De server
// stuurt hem op het `sources`-frame, op `done`, op `response_done` en in
// opgeslagen berichten; die vier vormen zijn hetzelfde ding en horen dus ook in
// de frontend één type te zijn.
//
// Alles is feature-detectie in plaats van een cast: oude rijen in de database
// dragen het oude schema (`type: "standpunt"`, geen `id`, `ref{file, doi}`) en
// moeten zonder fout renderen. Een veld dat er niet is, is `undefined` — nooit
// een lege string of een gok.

import { bilingualFrom } from './api'
import { isLang, type Bilingual, type Lang } from './i18n.svelte'

// SourceRef is de terugverwijzing naar het bewijs: het citaat waar de
// samenvatting op steunt en het bronpaper. `file` en `doi` zijn de velden van
// het oude schema; ze blijven leesbaar zolang er oude berichten bestaan.
export interface SourceRef {
  id?: string
  citation?: string
  citationConfidence?: 'high' | 'medium' | 'low'
  citationMissing?: string
  quote?: string
  passages?: { id?: string; text: string }[]
  paper?: string
  file?: string
  doi?: string
}

// Source is vormgelijk aan CitationSource van @parai/ui (number, type, text,
// perspective?) plus de twee velden die van ons zijn: `id` (de opaque node-id
// waarmee de kennisgraaf het standpunt kan openen) en `ref`.
//
// Het thema van de wire staat er bewust niet in. De bronkaart heeft het nooit
// getoond — het thema staat al in het kruimelpad onder "Over deze bron" — en
// een veld dat alleen gelezen wordt om nergens terecht te komen is geen DTO
// maar ballast. De server mag het blijven sturen; een onbekend veld
// van de wire heeft hier altijd al niets gedaan.
export interface Source {
  number: number
  type: string
  text: string
  id?: string
  perspective?: string
  ref?: SourceRef
  // grounding zegt wat er van de oorspronkelijke passages bekend is:
  // gecontroleerd, niet gecontroleerd, of — ook zichtbaar —
  // gecontroleerd én afgekeurd. Dat laatste is een bron die haar passages
  // kwijt is en alleen nog haar samenvatting draagt.
  grounding?: 'corpus_grounded' | 'unverified' | 'rejected'
  // localized draagt dezelfde twee teksten in de talen waarin ze bestaan. Hij
  // ontbreekt op een opgeslagen antwoord van vóór de vertaling, en dan blijven
  // `text` en `perspective` de momentopname die dat antwoord had.
  localized?: SourceLocalized
  // sourceLanguage is de taal van die momentopname. Zonder taal is er nooit een
  // vertaling om te melden.
  sourceLanguage?: Lang
}

// SourceLocalized zijn de bronkaartvelden die de kennisbank vertaalt. Het
// citaat, de papertitel, de bronvermelding en de doi staan er niet in: die zijn
// het origineel en worden nooit vertaald.
export interface SourceLocalized {
  text?: Bilingual
  perspective?: Bilingual
}

// SourceDrops is wat er wegviel vóór de bronnenlijst van één beurt: gevonden
// kandidaten die alsnog geen bron werden, of een half gelukte zoekopdracht.
// `count` is voor de lezer, `reasons` voor de begeleider in devMode.
//
// Hij hoort bij één beurt en wordt niet bewaard: na herladen beschrijft hij een
// uitkomst die niet meer te controleren is, dus dan staat hij er gewoon niet.
export interface SourceDrops {
  count: number
  reasons: Record<string, number>
}

// DROP_REASONS zijn de redencodes die de server kent. Een onbekende code krijgt
// geen eigen zin — dan blijft alleen de neutrale mededeling staan.
//
// Het zijn er vier: alleen harde fouten laten nog een gevonden
// kandidaat vallen. Een bron zonder oorspronkelijke passages of met een
// afgekeurde citaatgronding valt niet meer af; die staat gewoon in de lade, met
// het oordeel op haar eigen kaart.
export const DROP_REASONS = ['no_text', 'other_knowledge_base', 'evidence_unreadable', 'partial_search'] as const
export type DropReason = (typeof DROP_REASONS)[number]

export function isDropReason(value: string): value is DropReason {
  return (DROP_REASONS as readonly string[]).includes(value)
}

// parseSourceDrops leest de uitvalmelding. Zonder bruikbaar aantal is er niets
// te melden: een melding zonder getal zou de lezer laten raden.
export function parseSourceDrops(raw: unknown): SourceDrops | undefined {
  if (!raw || typeof raw !== 'object') return undefined
  const drops = raw as Record<string, unknown>
  const count = typeof drops.count === 'number' && Number.isFinite(drops.count) ? Math.trunc(drops.count) : 0
  if (count <= 0) return undefined
  const reasons: Record<string, number> = {}
  if (drops.reasons && typeof drops.reasons === 'object') {
    for (const [reason, n] of Object.entries(drops.reasons as Record<string, unknown>)) {
      if (typeof n === 'number' && Number.isFinite(n) && n > 0) reasons[reason] = Math.trunc(n)
    }
  }
  return { count, reasons }
}

// KBStatus is de stand van één beurt (niet van de import: dat is ImportState in
// knowledge.svelte.ts). Hij hoort bij de bronnen, dus staat hij hier.
export const KB_STATUSES = ['sources', 'gap', 'persona_only', 'unavailable'] as const
export type KBStatus = (typeof KB_STATUSES)[number]

export function parseKBStatus(value: unknown): KBStatus | undefined {
  return typeof value === 'string' && (KB_STATUSES as readonly string[]).includes(value)
    ? (value as KBStatus)
    : undefined
}

// Een leeg tekstveld is geen waarde: het zou een lege regel of een lege knop
// opleveren. Alleen een niet-lege string telt.
function text(value: unknown): string | undefined {
  return typeof value === 'string' && value !== '' ? value : undefined
}

function parseRef(value: unknown): SourceRef | undefined {
  if (!value || typeof value !== 'object') return undefined
  const raw = value as Record<string, unknown>
  const ref: SourceRef = {}
  const id = text(raw.id)
  if (id) ref.id = id
  const citation = text(raw.citation)
  if (citation) ref.citation = citation
  const missing = text(raw.citationMissing)
  if (missing) ref.citationMissing = missing
  if (raw.citationConfidence === 'high' || raw.citationConfidence === 'medium' || raw.citationConfidence === 'low') ref.citationConfidence = raw.citationConfidence
  const quote = text(raw.quote)
  if (quote) ref.quote = quote
  if (Array.isArray(raw.passages)) {
    const passages = raw.passages.slice(0, 8).flatMap((item) => {
      if (!item || typeof item !== 'object') return []
      const passage = item as Record<string, unknown>
      const body = text(passage.text)
      if (!body?.trim()) return []
      const id = text(passage.id)
      return [{ ...(id ? { id } : {}), text: body }]
    })
    if (passages.length) ref.passages = passages
  }
  const paper = text(raw.paper)
  if (paper) ref.paper = paper
  const file = text(raw.file)
  if (file) ref.file = file
  const doi = text(raw.doi)
  if (doi) ref.doi = doi
  return Object.keys(ref).length > 0 ? ref : undefined
}

function parseLocalized(value: unknown): SourceLocalized | undefined {
  if (!value || typeof value !== 'object') return undefined
  const raw = value as Record<string, unknown>
  const out: SourceLocalized = {}
  for (const key of ['text', 'perspective'] as const) {
    const bilingual = bilingualFrom(raw[key])
    if (bilingual) out[key] = bilingual
  }
  return Object.keys(out).length > 0 ? out : undefined
}

// sourceText en sourcePerspective zijn de twee teksten van de bronkaart in de
// taal van de lezer. De tweetalige vorm wint als hij er is; een oud opgeslagen
// antwoord houdt de kale tekst die het toen had.
export function sourceText(source: Source | null | undefined, pick: (text: Bilingual | undefined) => string): string {
  return source ? pick(source.localized?.text) || source.text : ''
}

export function sourcePerspective(source: Source | null | undefined, pick: (text: Bilingual | undefined) => string): string {
  return source ? pick(source.localized?.perspective) || source.perspective || '' : ''
}


// sourceTranslated zegt of de samenvatting op het scherm een vertaling is. Dat
// kan alleen als de bron zijn eigen taal meldt én de getoonde tekst een andere
// is; zonder die twee is er niets te melden.
export function sourceTranslated(source: Source | null | undefined, shown: (text: Bilingual | undefined) => Lang | null): boolean {
  if (!source?.sourceLanguage || !source.localized?.text) return false
  const lang = shown(source.localized.text)
  return lang !== null && lang !== source.sourceLanguage
}

// parseSources leest de bronnenlijst van de wire. Een bron zonder bruikbaar
// nummer valt af (zonder nummer is er niets om een citatiechip aan te hangen);
// de rest komt binnen met precies de velden die er echt in stonden.
export function parseSources(raw: unknown): Source[] {
  if (!Array.isArray(raw)) return []
  const out: Source[] = []
  for (const entry of raw) {
    if (!entry || typeof entry !== 'object') continue
    const s = entry as Record<string, unknown>
    if (typeof s.number !== 'number' || !Number.isFinite(s.number)) continue
    const source: Source = {
      number: s.number,
      type: typeof s.type === 'string' ? s.type : '',
      text: typeof s.text === 'string' ? s.text : '',
    }
    const id = text(s.id)
    if (id) source.id = id
    const perspective = text(s.perspective)
    if (perspective) source.perspective = perspective
    const ref = parseRef(s.ref)
    if (ref) source.ref = ref
    if (s.grounding === 'corpus_grounded' || s.grounding === 'unverified' || s.grounding === 'rejected') source.grounding = s.grounding
    const localized = parseLocalized(s.localized)
    if (localized) source.localized = localized
    if (isLang(s.sourceLanguage)) source.sourceLanguage = s.sourceLanguage
    out.push(source)
  }
  return out
}

// holdPartialCitation houdt een half ontvangen citatie ("… zie [1" of "… [")
// achter zolang er gestreamd wordt: anders flitst er losse markup door het
// antwoord voordat parseCitations hem kan omzetten in een verwijzing. De chat
// en de vergadering streamen allebei, dus staat hij hier bij het bron-DTO en
// niet in één van de twee stores.
export function holdPartialCitation(text: string): string {
  return text.replace(/\s*\[[\d,\s]*$/, '')
}
