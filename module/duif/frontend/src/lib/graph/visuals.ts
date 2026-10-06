import { getThemaIcon, iconKeyFor } from '@parai/graph/lib/icons'
import { relMeta } from '@parai/graph/lib/relationTypes'
import { canonicalRelation } from '@parai/graph/lib/transform'
import type { NodeType } from './types'

// De vormtaal van de plattegrond. Kleur zegt bij welk thema iets hoort, vorm
// zegt wát het is, en het icoontje zegt waar het over gaat. De vorm- en
// maatregels staan hier; de kleuren komen uit colors.ts, de iconen en de
// relatiestijlen uit parai-graph.
//
// Waarom parai-graph voor iconen en relatiekleuren: dat zijn stelsels, geen
// tabellen. `getThemaIcon` garandeert dat geen twee thema's hetzelfde glyph
// dragen (trefwoord eerst, dan de eerste vrije), en `relMeta` geeft élke
// relatienaam een stabiele kleur — ook een die deze kennisbank morgen pas
// stuurt. Dat opnieuw schrijven zou een kopie zijn, geen ontwerp.

export type NodeShape = 'disc' | 'tile' | 'card'

// De maten zijn in graafcoördinaten; de zoom schaalt ze mee. Ze zijn opnieuw
// ruimer geworden: op een gedeeld klasscherm moet een thema een vlak zijn waar
// een icoontje in past, en moet een citaat een klikdoel zijn waar een muis of
// een vinger op landt. De kleine soorten groeiden het hardst — die waren de
// stippen waar je twee keer naar moest mikken. De afstanden in layout.ts zijn
// tegelijk korter geworden, zodat de tekening compacter wordt in plaats van
// alleen groter.
const RADIUS: Readonly<Record<NodeType, number>> = {
  domein: 34,
  thema: 27,
  subthema: 20,
  destillaat: 16,
  quote: 13,
  interview: 17,
}

const SHAPE: Readonly<Record<NodeType, NodeShape>> = {
  domein: 'tile',
  thema: 'disc',
  subthema: 'disc',
  destillaat: 'disc',
  quote: 'disc',
  interview: 'card',
}

// Vaste glyphs voor de drie soorten die geen eigen onderwerp hebben maar een
// rol: een bevinding is een blad tekst, een citaat een aanhalingsteken, een bron
// een boek. De ordenende lagen (domein, thema, subthema) krijgen
// juist wél een trefwoordicoon, want dáár zegt het onderwerp iets.
const ROLE_ICON: Readonly<Partial<Record<NodeType, string>>> = {
  destillaat: 'file',
  quote: 'quotes',
  interview: 'book',
}

export function nodeRadius(type: NodeType): number {
  return RADIUS[type]
}

export function nodeShape(type: NodeType): NodeShape {
  return SHAPE[type]
}

// iconKey kiest het glyph van een knoop. Een thema krijgt er een die binnen de
// kaart uniek is (de graafcomponent houdt dat bij per thema-id), de rest een
// trefwoordicoon of zijn rolicoon.
export function iconKey(type: NodeType, id: string, label: string): string {
  const role = ROLE_ICON[type]
  if (role) return role
  if (type === 'thema') return getThemaIcon(id, label)
  return iconKeyFor(label)
}

// relationStyle geeft de lijnkleur en het streepjespatroon van een band. De
// relatienamen van deze kennisbank staan in kleine letters op de wire
// (`contains_subthema`); de graafcomponent verwacht de canonieke vorm, dus de
// naam gaat eerst door canonicalRelation heen.
export function relationStyle(type: string): { color: string; dash: number[] } {
  const meta = relMeta(canonicalRelation(type))
  return { color: meta.color, dash: meta.dash }
}

