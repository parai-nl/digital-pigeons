import { groupConnections } from '@parai/graph/lib/connections'
import type { GLink, GNode } from '@parai/graph/lib/transform'
import type { GraphEdge, GraphNode, NodeType } from './types'

// De verbindingen van de gekozen knoop, gegroepeerd voor het leespaneel.
//
// Het groeperen zelf komt uit parai-graph: op (relatiesoort, soort van de
// andere kant) in plaats van op richting, zodat een symmetrische band in één
// groep valt terwijl een structuurband waarvan de twee kanten naar
// verschillende soorten wijzen (een subthema met zijn bevindingen én zijn
// thema) netjes splitst. Verborgen relaties en dubbele buren vallen daar al af.
//
// Wat hier wél van ons is, is de volgorde en de naamgeving: de naam van een
// groep is een sleutel die het leespaneel vertaalt, in het Engels of het
// Nederlands, ook voor de tabellen die alleen deze kennisbank kent.

export interface ConnectionGroup {
  // key is uniek per groep en stabiel: relatiesoort plus de soort aan de
  // andere kant.
  key: string
  relation: string
  otherType: NodeType
  nodes: GraphNode[]
}

// De leesvolgorde van het paneel: eerst waar je vandaan komt (de ordening),
// dan wat eronder hangt, dan de onderbouwing. Een soort die hier niet in staat
// zakt naar onderen in plaats van te verdwijnen.
const TYPE_ORDER: Readonly<Record<string, number>> = {
  domein: 0,
  thema: 1,
  subthema: 2,
  destillaat: 3,
  quote: 4,
  interview: 5,
}

export function groupedConnections(
  node: GraphNode,
  nodes: ReadonlyMap<string, GraphNode>,
  edges: readonly GraphEdge[],
  hidden: ReadonlySet<string>,
): ConnectionGroup[] {
  // De twee woordenlijsten verschillen alleen in de namen van de tabellen;
  // structureel is een GraphNode een GNode. Deze cast is de hele overbrugging,
  // en hij is veilig omdat groupConnections het type alleen als sleutel
  // gebruikt — nooit als tabel waaruit het iets opzoekt.
  const byId = nodes as unknown as Map<string, GNode>
  const groups = groupConnections(
    node as unknown as GNode,
    edges as unknown as GLink[],
    byId,
    hidden as Set<string>,
  )
  const out: ConnectionGroup[] = []
  for (const group of groups) {
    const members: GraphNode[] = []
    for (const other of group.nodes) {
      const known = nodes.get(other.id)
      if (known) members.push(known)
    }
    if (members.length === 0) continue
    out.push({
      key: group.type + ':' + members[0].type,
      relation: group.type,
      otherType: members[0].type,
      nodes: members,
    })
  }
  return out.sort(
    (a, b) => (TYPE_ORDER[a.otherType] ?? 9) - (TYPE_ORDER[b.otherType] ?? 9) || b.nodes.length - a.nodes.length,
  )
}
