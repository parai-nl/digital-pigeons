// Stable count shared by data grouping and the deferred canvas palette.
export const FAMILY_COUNT = 26

import { bilingualFrom } from '../api'
import { isImportState, type ImportState } from '../knowledge.svelte'
import { isLang, type Bilingual, type Lang } from '../i18n.svelte'
import { isReadableLabel, readableLabel } from './readable'
import { typeNamesEn, typeNamesNl } from './typeNames'

// De vorm van de kennisgraaf zoals de server hem stuurt, plus de lezers die er
// een getypeerde graaf van maken. Eén regel: wat de server stuurt is een
// aanname, geen waarheid — een veld dat niet klopt bestaat niet, en een node
// zonder id of bekend type wordt overgeslagen in plaats van gegokt.

// De zes soorten knopen die de graaf toont. De eerste drie vormen het
// structuurniveau (de overview); de laatste drie komen pas bij het uitklappen.
// Het perspectief en de redactionele persona staan wel in de kennisbank, maar
// de graaf toont ze niet: een knoop van die soort wordt overgeslagen
// als een onbekend type.
export const NODE_TYPES = [
  'domein',
  'thema',
  'subthema',
  'destillaat',
  'quote',
  'interview',
] as const
export type NodeType = (typeof NODE_TYPES)[number]

export function isNodeType(value: unknown): value is NodeType {
  return typeof value === 'string' && (NODE_TYPES as readonly string[]).includes(value)
}

// Een quote is het einde van de weg: hij heeft geen kinderen, dus ook geen
// uitklap-affordance (aria-expanded hoort alleen op ouders te staan).
export function canExpand(type: NodeType): boolean {
  return type !== 'quote'
}

// Metadata is bewust een vlakke tekst-tabel: de wire stuurt per tabel een
// allowlist van tekstvelden, en het detailpaneel toont ze als tekst. Wat geen
// tekst is, is geen metadata.
export type Metadata = Readonly<Record<string, string>>

// De wire draagt per veld óf een kale string (tekst die niet vertaald is: een
// citaat, een papertitel) óf een taalobject {nl, en}. Die twee vormen leven
// hier maar één laag diep: `label` en `metadata` zijn altijd al gekozen tekst,
// zodat de kaart, de boom en het leespaneel niets van talen hoeven te weten.
export interface GraphNode {
  id: string
  type: NodeType
  label: string
  metadata: Metadata
  // translations bewaart de tweetalige originelen per veld ('label' plus de
  // metadatasleutels), zodat een taalwissel de graaf niet opnieuw hoeft op te
  // halen. Afwezig bij een corpus dat maar één taal kent.
  translations?: Readonly<Record<string, Bilingual>>
  // sourceLanguage is de taal waarin de kennisbank geschreven is; afwezig
  // zolang de bundel geen taal kent.
  sourceLanguage?: Lang | null
  // translated zegt dat de tekst hierboven een vertaling is en niet het
  // origineel. Alleen dán hoort het scherm dat te melden.
  translated?: boolean
  // unnamed zegt dat deze knoop geen naam draagt: `label` is dan het woord voor
  // de soort knoop en niet iets uit de kennisbank. Een taalwissel moet
  // dat woord opnieuw kiezen — anders bleef een terugval in de oude taal staan.
  unnamed?: boolean
}

// other is de andere van de twee talen; kennisbanktekst valt daarop terug in
// plaats van op een lege regel.
function other(lang: Lang): Lang {
  return lang === 'nl' ? 'en' : 'nl'
}

/** De taal waarin deze tekst getoond wordt, of null als er niets te tonen is. */
export function textLang(text: Bilingual | undefined, lang: Lang): Lang | null {
  if (!text) return null
  if (text[lang]) return lang
  if (text[other(lang)]) return other(lang)
  return null
}

/** Kennisbanktekst in de UI-taal, met terugval op de taal die er wél is. */
export function pickText(text: Bilingual | undefined, lang: Lang): string {
  const chosen = textLang(text, lang)
  return chosen ? (text?.[chosen] ?? '') : ''
}

// wireText leest één veld van de wire: een kale string blijft zoals hij is, een
// taalobject wordt de tekst in de UI-taal plus het origineel om te bewaren.
function wireText(value: unknown, lang: Lang): { text: string; translations?: Bilingual } {
  if (typeof value === 'string') return { text: value }
  const translations = bilingualFrom(value)
  if (!translations) return { text: '' }
  return { text: pickText(translations, lang), translations }
}

export interface GraphEdge {
  source: string
  target: string
  type: string
}

// De relaties die in de overview een ouder-kindband vormen. `belongs_to_*`
// staat er niet in: dat zijn de omgekeerde banden van een standpunt naar zijn
// structuur, en die komen pas mee als kind van een uitklap.
export const OVERVIEW_PARENT_RELATIONS: readonly string[] = ['contains_thema', 'contains_subthema']

export interface GraphPayload {
  nodes: GraphNode[]
  edges: GraphEdge[]
}

export interface OverviewPayload extends GraphPayload {
  totals: Readonly<Record<string, number>>
  // childCounts zegt per knoop hoeveel kinderen de kinderroute zou geven. De
  // kaart tekent daarop de "+"-badge: zonder dat getal zou de badge een belofte
  // zijn die de data niet hoeft waar te maken. Een knoop zonder kinderen staat
  // er niet in (de server laat nullen weg), dus "afwezig" is "onbekend of nul" —
  // en dat verschil doet er niet toe: allebei geen badge.
  childCounts: ReadonlyMap<string, number>
  // kb is de importstand die de server bij de overview meestuurt, zodat een
  // agent niet apart hoeft te pollen om te weten waarom de graaf leeg is. Voor
  // de UI is het een hint: de banner leest de gedeelde KnowledgeStore, die ook
  // tijdens een lopende import blijft bijwerken. Een stand die we niet kennen
  // is geen stand (null), geen gok.
  kb: ImportState | null
}

export interface ChildrenPayload extends GraphPayload {
  truncated: boolean
  total: number
  nextOffset: number | null
}

export interface NodeDetail {
  node: GraphNode
  // trail loopt van de wortel naar de ouder; de node zelf staat er niet in.
  trail: GraphNode[]
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : null
}

const METADATA_FIELDS = new Set(['text', 'beschrijving', 'speaker', 'thema_titel', 'domain', 'scope', 'interview_id', 'citation', 'doi', 'citationConfidence', 'citationMissing', 'grounding'])

function parseMetadata(value: unknown, lang: Lang, translations: Record<string, Bilingual>): Metadata {
  const raw = asRecord(value)
  if (!raw) return {}
  const out: Record<string, string> = {}
  for (const [key, v] of Object.entries(raw)) {
    if (!METADATA_FIELDS.has(key)) continue
    const field = wireText(v, lang)
    if (field.text.trim() === '') continue
    // De opsommingswaarden blijven kale strings en worden hier niet vertaald;
    // een taalobject op zo'n veld is geen geldige waarde.
    if (key === 'citationConfidence' && !['high', 'medium', 'low'].includes(field.text)) continue
    if (key === 'grounding' && !['rejected', 'corpus_grounded'].includes(field.text)) continue
    out[key] = field.text
    if (field.translations) translations[key] = field.translations
  }
  return out
}

// typeName is het woord voor de soort knoop, in de taal van de lezer. Het is de
// terugval voor een kaartlabel dat geen naam is.
function typeName(type: NodeType, lang: Lang): string {
  return (lang === 'nl' ? typeNamesNl : typeNamesEn)[`graph.type.${type}`]
}

export function parseNode(value: unknown, lang: Lang = 'en'): GraphNode | null {
  const raw = asRecord(value)
  if (!raw) return null
  if (typeof raw.id !== 'string' || raw.id === '') return null
  if (!isNodeType(raw.type)) return null
  const translations: Record<string, Bilingual> = {}
  const label = wireText(raw.label, lang)
  if (label.translations) translations.label = label.translations
  const metadata = parseMetadata(raw.metadata, lang, translations)
  const sourceLanguage = isLang(raw.sourceLanguage) ? raw.sourceLanguage : null
  return {
    id: raw.id,
    type: raw.type,
    label: readableLabel(label.text, typeName(raw.type, lang)),
    metadata,
    translations,
    sourceLanguage,
    translated: isTranslated(translations, sourceLanguage, lang),
    unnamed: !isReadableLabel(label.text),
  }
}

// isTranslated kijkt naar de tekst die de knoop draagt — de volledige tekst als
// die er is, anders het label. Staat die in een andere taal dan de brontaal,
// dan leest de gebruiker een vertaling.
function isTranslated(translations: Record<string, Bilingual>, sourceLanguage: Lang | null, lang: Lang): boolean {
  if (!sourceLanguage) return false
  const primary = translations.text ?? translations.label
  const shown = textLang(primary, lang)
  return shown !== null && shown !== sourceLanguage
}

// relocalize zet een al geladen knoop om naar een andere UI-taal. Zo hoeft een
// taalwissel de graaf niet opnieuw op te halen en blijft de uitklapstand staan.
export function relocalize(node: GraphNode, lang: Lang): GraphNode {
  const translations = node.translations
  // Het typewoord is de terugval, en dat woord heeft zelf een taal: een knoop
  // zonder naam moet hem dus ook opnieuw kiezen als er verder niets te vertalen
  // valt.
  const fallback = typeName(node.type, lang)
  if (!translations || Object.keys(translations).length === 0) {
    return node.unnamed ? { ...node, label: fallback } : node
  }
  const metadata: Record<string, string> = { ...node.metadata }
  let label = node.unnamed ? fallback : node.label
  let unnamed = node.unnamed
  for (const [key, text] of Object.entries(translations)) {
    const picked = pickText(text, lang)
    if (picked === '') continue
    if (key === 'label') {
      // Ook na een taalwissel: de andere taal kan net zo goed een sleutel dragen.
      unnamed = !isReadableLabel(picked)
      label = readableLabel(picked, fallback)
    } else metadata[key] = picked
  }
  return {
    ...node,
    label,
    metadata,
    translated: isTranslated(translations, node.sourceLanguage ?? null, lang),
    unnamed,
  }
}

function parseEdge(value: unknown): GraphEdge | null {
  const raw = asRecord(value)
  if (!raw) return null
  const { source_id: source, target_id: target, relationship_type: type } = raw
  if (typeof source !== 'string' || typeof target !== 'string' || typeof type !== 'string') return null
  if (source === '' || target === '' || type === '') return null
  return { source, target, type }
}

export function parsePayload(body: unknown, lang: Lang = 'en'): GraphPayload {
  const raw = asRecord(body)
  const nodes: GraphNode[] = []
  const edges: GraphEdge[] = []
  if (!raw) return { nodes, edges }
  if (Array.isArray(raw.nodes)) {
    for (const item of raw.nodes) {
      const node = parseNode(item, lang)
      if (node) nodes.push(node)
    }
  }
  if (Array.isArray(raw.relationships)) {
    for (const item of raw.relationships) {
      const edge = parseEdge(item)
      if (edge) edges.push(edge)
    }
  }
  return { nodes, edges }
}

export function parseOverview(body: unknown, lang: Lang = 'en'): OverviewPayload {
  const raw = asRecord(body)
  const totals: Record<string, number> = {}
  const counts = asRecord(raw?.totals)
  if (counts) {
    for (const [key, v] of Object.entries(counts)) {
      if (typeof v === 'number' && Number.isFinite(v)) totals[key] = v
    }
  }
  const childCounts = new Map<string, number>()
  const counted = asRecord(raw?.childCounts)
  if (counted) {
    for (const [key, v] of Object.entries(counted)) {
      if (typeof v === 'number' && Number.isFinite(v) && v > 0) childCounts.set(key, Math.trunc(v))
    }
  }
  const kb = asRecord(raw?.kb)
  return { ...parsePayload(body, lang), totals, childCounts, kb: isImportState(kb?.state) ? kb.state : null }
}

export function parseChildren(body: unknown, lang: Lang = 'en'): ChildrenPayload {
  const raw = asRecord(body)
  const payload = parsePayload(body, lang)
  const total = typeof raw?.total === 'number' && Number.isFinite(raw.total) ? raw.total : payload.nodes.length
  const next = raw?.nextOffset
  return {
    ...payload,
    truncated: raw?.truncated === true,
    total,
    // Een nextOffset die niet vooruit wijst is geen volgende pagina: hem toch
    // volgen zou de "meer laden"-knop in een kringetje laten lopen.
    nextOffset: typeof next === 'number' && Number.isFinite(next) && next > 0 ? next : null,
  }
}

export function parseNodeDetail(body: unknown, lang: Lang = 'en'): NodeDetail | null {
  const raw = asRecord(body)
  if (!raw) return null
  const node = parseNode(raw.node, lang)
  if (!node) return null
  const trail: GraphNode[] = []
  if (Array.isArray(raw.trail)) {
    for (const item of raw.trail) {
      const step = parseNode(item, lang)
      // De node zelf hoort niet in zijn eigen kruimelpad.
      if (step && step.id !== node.id) trail.push(step)
    }
  }
  return { node, trail }
}
