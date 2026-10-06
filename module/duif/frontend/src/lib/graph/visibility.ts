import { canExpand, type GraphNode, type NodeType } from './types'

// Wat er te zien is, is een pure functie van wat er geladen is en wat er
// uitgeklapt staat. Geen DOM, geen store, geen d3 — daardoor is het gedrag van
// de plattegrond (uitklappen, inklappen, kinderen die met hun ouder verdwijnen)
// met gewone tabellen te toetsen.
//
// De graaf is geen boom: een bronpaper hangt onder elk standpunt dat eruit
// komt. In de boomweergave verschijnt zo'n knoop dan meer dan eens — één keer
// per pad, elk met een eigen sleutel — terwijl hij op het canvas één knoop
// blijft. Dat is eerlijker dan één van de ouders stilzwijgend laten vallen.

export interface TreeRow {
  // key is het pad naar deze rij en is uniek in de boom; id is de knoop, die
  // onder meerdere ouders kan hangen.
  key: string
  id: string
  type: NodeType
  label: string
  // depth is 0-based; aria-level is depth + 1.
  depth: number
  index: number
  setsize: number
  expandable: boolean
  expanded: boolean
  loading: boolean
  family: number
}

export interface VisibilityInput {
  nodes: ReadonlyMap<string, GraphNode>
  roots: readonly string[]
  childIds: ReadonlyMap<string, readonly string[]>
  revealed: ReadonlySet<string>
  // totals is het aantal kinderen dat de server zegt te hebben; aria-setsize
  // hoort dat te noemen, ook als er pas 150 geladen zijn (APG, lazy children).
  totals?: ReadonlyMap<string, number>
  loading?: ReadonlySet<string>
  // families koppelt een thema-id aan zijn kleurfamilie; kinderen erven hem.
  families?: ReadonlyMap<string, number>
}

export interface VisibleGraph {
  rows: TreeRow[]
  ids: Set<string>
  depth: Map<string, number>
  family: Map<string, number>
}

export function computeVisible(input: VisibilityInput): VisibleGraph {
  const { nodes, roots, childIds, revealed } = input
  const rows: TreeRow[] = []
  const ids = new Set<string>()
  const depth = new Map<string, number>()
  const family = new Map<string, number>()

  const walk = (id: string, level: number, index: number, setsize: number, path: string[], inherited: number): void => {
    const node = nodes.get(id)
    if (!node) return
    // Een pad dat op zichzelf terugkomt is een datafout, geen oneindige boom.
    if (path.includes(id)) return

    const own = node.type === 'thema' ? (input.families?.get(id) ?? -1) : inherited
    const children = childIds.get(id) ?? []
    const expanded = revealed.has(id)
    const key = path.length === 0 ? id : path.join('/') + '/' + id

    ids.add(id)
    if (!depth.has(id)) depth.set(id, level)
    if (!family.has(id) || (family.get(id) ?? -1) < 0) family.set(id, own)

    rows.push({
      key,
      id,
      type: node.type,
      label: node.label,
      depth: level,
      index,
      setsize,
      expandable: canExpand(node.type),
      expanded: expanded && children.length > 0,
      loading: input.loading?.has(id) === true,
      family: own,
    })

    if (!expanded) return
    const next = [...path, id]
    // aria-setsize noemt het servertotaal; is dat er niet, dan wat er staat.
    const size = Math.max(input.totals?.get(id) ?? children.length, children.length)
    for (let i = 0; i < children.length; i++) {
      walk(children[i], level + 1, i, size, next, own)
    }
  }

  for (let i = 0; i < roots.length; i++) {
    walk(roots[i], 0, i, roots.length, [], -1)
  }
  return { rows, ids, depth, family }
}

// visibleEdges houdt alleen de banden over waarvan beide uiteinden te zien
// zijn; een band naar een ingeklapte tak tekent niets. `hidden` is het
// relatiefilter van de knoppenbalk: een uitgezette bandsoort verdwijnt hier, en
// daarmee tegelijk van de kaart en uit het leespaneel.
export function visibleEdges<E extends { source: string; target: string; type: string }>(
  edges: readonly E[],
  ids: ReadonlySet<string>,
  hidden?: ReadonlySet<string>,
): E[] {
  return edges.filter(
    (e) => e.source !== e.target && ids.has(e.source) && ids.has(e.target) && !(hidden?.has(e.type) === true),
  )
}

// matchNodes is het zoekveld: "zoek in getoonde knopen". Alleen wat er staat
// wordt doorzocht (labels van zichtbare knopen) — de server kent geen
// labelzoekopdracht, en zoeken in wat niet geladen is zou stil niets vinden.
export function matchNodes(
  nodes: ReadonlyMap<string, GraphNode>,
  ids: ReadonlySet<string>,
  query: string,
): Set<string> {
  const needle = query.trim().toLocaleLowerCase()
  const out = new Set<string>()
  if (needle === '') return out
  for (const id of ids) {
    const node = nodes.get(id)
    if (node && node.label.toLocaleLowerCase().includes(needle)) out.add(id)
  }
  return out
}

// De velden waarin het zoekveld leest naast het label. Een bevinding draagt
// haar hele zin in `text`, een citaat de passage zelf; zoeken op alleen het
// afgekapte label zou die tekst stil overslaan.
const SEARCH_FIELDS = ['text', 'beschrijving', 'citation', 'interview_id'] as const

// matchesText zoekt in álles wat geladen is, ook in de tekst achter het label.
// Wat niet geladen is, wordt niet doorzocht: de server kent geen labelzoekvraag,
// en stilletjes niets vinden in 8 800 knopen is erger dan een eerlijke teller.
export function matchesText(nodes: ReadonlyMap<string, GraphNode>, query: string): Set<string> {
  const needle = query.trim().toLocaleLowerCase()
  const out = new Set<string>()
  if (needle === '') return out
  for (const [id, node] of nodes) {
    if (node.label.toLocaleLowerCase().includes(needle)) {
      out.add(id)
      continue
    }
    for (const field of SEARCH_FIELDS) {
      const value = node.metadata[field]
      if (value !== undefined && value.toLocaleLowerCase().includes(needle)) {
        out.add(id)
        break
      }
    }
  }
  return out
}

export interface TreeBranch {
  row: TreeRow
  children: TreeBranch[]
}

// nestRows maakt van de platte, in leesvolgorde gezette rijen de geneste vorm
// die de ARIA-boom nodig heeft (treeitem met daarin een group). De rijen zelf
// blijven de bron: één berekening, twee weergaven.
export function nestRows(rows: readonly TreeRow[]): TreeBranch[] {
  const top: TreeBranch[] = []
  const stack: TreeBranch[] = []
  for (const row of rows) {
    const branch: TreeBranch = { row, children: [] }
    while (stack.length > row.depth) stack.pop()
    const parent = stack.at(-1)
    if (parent && parent.row.depth === row.depth - 1) parent.children.push(branch)
    else top.push(branch)
    stack.push(branch)
  }
  return top
}
