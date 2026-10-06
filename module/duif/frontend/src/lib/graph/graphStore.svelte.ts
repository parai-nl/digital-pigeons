import { API, readFailure, type ApiFailure } from '../api'
import type { Lang } from '../i18n.svelte'
import type { ImportState } from '../knowledge.svelte'
import type { GraphPorts } from './ports'
import {
  FAMILY_COUNT,
  OVERVIEW_PARENT_RELATIONS,
  parseChildren,
  parseNodeDetail,
  parseOverview,
  relocalize,
  canExpand,
  type GraphEdge,
  type GraphNode,
  type NodeType,
} from './types'
import { computeVisible, matchNodes, matchesText, visibleEdges } from './visibility'
import { iconKey } from './visuals'

// De graafstore is de hele plattegrondlogica zonder DOM: één exemplaar per
// mount, met GraphPorts als enige poort naar buiten. Geen $effect, geen
// module-scope state, geen opslag. Wat de gebruiker aan meldingen ziet is
// altijd óf een servermelding uit message{en,nl} óf een eigen i18n-tekst.
//
// Elke await is een plek waar de wereld verschoven kan zijn: een tweede klik,
// een tabwissel, een unmount. Daarom staan er vier wachters in:
//  * #navGen ongeldigt een lopende load() zodra er een nieuwe navigatie komt;
//  * #focusGen doet hetzelfde voor focus(), los van #navGen — een focus mag
//    een lopende uitklap niet meesleuren, en een uitklap geen focus;
//  * #inflight laat een dubbele uitklap dezelfde belofte delen;
//  * #controller breekt bij dispose() alles af wat nog onderweg is.
// Een kinderfetch hangt aan geen van beide tellers: hij hoort bij één tak, en
// blijft geldig zolang die tak er staat.

export type GraphPhase = 'idle' | 'loading' | 'ready' | 'failed'

interface Mutation {
  nodes: readonly GraphNode[]
  edges: readonly GraphEdge[]
}

export class GraphStore {
  // De grote tabellen zijn $state.raw: ze worden altijd in hun geheel
  // vervangen, dus een diepe proxy zou alleen maar kosten.
  nodes = $state.raw<ReadonlyMap<string, GraphNode>>(new Map())
  edges = $state.raw<readonly GraphEdge[]>([])
  roots = $state.raw<readonly string[]>([])
  childIds = $state.raw<ReadonlyMap<string, readonly string[]>>(new Map())
  // childTotals is wat de server zegt te hebben (aria-setsize); nextOffsets is
  // waar "meer laden" verdergaat.
  childTotals = $state.raw<ReadonlyMap<string, number>>(new Map())
  // childCounts komt uit de overview en zegt hoeveel kinderen een knoop heeft
  // zónder ze op te halen. Bewust naast childTotals en niet erin: childTotals
  // betekent "de kinderen zijn binnen", en dat is precies wat expand() gebruikt
  // om te weten of er nog een verzoek nodig is.
  childCounts = $state.raw<ReadonlyMap<string, number>>(new Map())
  nextOffsets = $state.raw<ReadonlyMap<string, number>>(new Map())
  families = $state.raw<ReadonlyMap<string, number>>(new Map())
  revealed = $state.raw<ReadonlySet<string>>(new Set())
  loadingIds = $state.raw<ReadonlySet<string>>(new Set())
  totals = $state.raw<Readonly<Record<string, number>>>({})

  selected = $state<string | null>(null)
  hovered = $state<string | null>(null)
  // hiddenRelations is het relatiefilter van de knoppenbalk: een verborgen
  // relatie verdwijnt van de kaart én uit het leespaneel, zodat de twee
  // hetzelfde vertellen.
  hiddenRelations = $state.raw<ReadonlySet<string>>(new Set())
  selectionRequest = $state(0)
  query = $state('')
  phase = $state<GraphPhase>('idle')
  notice = $state<ApiFailure | null>(null)
  // kb is de importstand zoals de overview hem bij dít antwoord meldde: een
  // hint waarom de graaf leeg of dun is. De lopende waarheid staat in de
  // gedeelde KnowledgeStore — die blijft peilen, en daar leest de banner hem.
  kb = $state<ImportState | null>(null)
  // focusRequest telt de keren dat de selectie van buiten kwam ("open in de
  // graaf" vanuit een bronkaart). Alleen dán mag de boom de aandacht naar zich
  // toe halen; bij een gewone klik op het canvas hoort de focus te blijven waar
  // de gebruiker hem zette.
  focusRequest = $state(0)
  // visible zegt of de graaftab openstaat; een gesloten tab hoort geen
  // simulatie te laten draaien (hetzelfde idee als orbVisible bij de
  // vergadering).
  visible = $state(false)
  disposed = $state(false)
  // lang is de taal waarin de kennisinhoud getoond wordt. De graaf haalt hem
  // niet zelf op — GraphView duwt de UI-taal erin — omdat de store geen
  // services kent. Een wissel her-kiest de tekst van wat al geladen is; de
  // uitklapstand en de selectie blijven dus staan.
  lang = $state<Lang>('en')

  #navGen = 0
  #focusGen = 0
  #inflight = new Map<string, Promise<void>>()
  #pendingFocus: string | null = null
  #controller = new AbortController()

  constructor(
    readonly ports: GraphPorts,
  ) {}

  // setLang wisselt de taal van de kennisinhoud. Alles wat al binnen is wordt
  // opnieuw gekozen uit de tweetalige originelen die de knopen bij zich dragen:
  // geen tweede verzoek, en de tak waar de gebruiker in zat blijft open. Een
  // corpus met maar één taal verandert er niet van.
  setLang = (lang: Lang): void => {
    if (this.lang === lang) return
    this.lang = lang
    const next = new Map(this.nodes)
    for (const [id, node] of next) {
      next.set(id, relocalize(node, lang))
    }
    this.nodes = next
  }

  // --- afgeleide beelden ----------------------------------------------------

  readonly view = $derived(
    computeVisible({
      nodes: this.nodes,
      roots: this.roots,
      childIds: this.childIds,
      revealed: this.revealed,
      totals: this.childTotals,
      loading: this.loadingIds,
      families: this.families,
    }),
  )

  readonly rows = $derived(this.view.rows)
  readonly visibleIds = $derived(this.view.ids)
  readonly visibleEdges = $derived(visibleEdges(this.edges, this.view.ids, this.hiddenRelations))
  readonly matches = $derived(matchNodes(this.nodes, this.view.ids, this.query))
  // matchesLoaded zoekt in álles wat geladen is, ook in de tekst achter het
  // label (de samenvatting van een bevinding, het citaat zelf). Het verschil met
  // `matches` is wat de teller eerlijk maakt: "3 op de kaart, 11 geladen".
  readonly matchesLoaded = $derived(matchesText(this.nodes, this.query))
  readonly searching = $derived(this.query.trim() !== '')

  // relationLinks zijn de bandsoorten die er werkelijk liggen, elk met de
  // soorten die hij verbindt. Het filter en de legenda tonen alleen wat deze
  // kennisbank stuurt, en noemen een band naar zijn twee uiteinden: twee
  // relaties die bij dezelfde soort uitkomen zijn anders twee vinkjes met
  // dezelfde naam.
  readonly relationLinks = $derived.by((): { type: string; from: NodeType; to: NodeType }[] => {
    const seen = new Map<string, { type: string; from: NodeType; to: NodeType }>()
    for (const edge of this.edges) {
      if (seen.has(edge.type)) continue
      const from = this.nodes.get(edge.source)
      const to = this.nodes.get(edge.target)
      if (!from || !to) continue
      seen.set(edge.type, { type: edge.type, from: from.type, to: to.type })
    }
    return [...seen.values()].sort((a, b) => a.type.localeCompare(b.type))
  })

  // Het kruimelpad komt uit de boom zelf, niet uit een tweede stukje state:
  // wat er staat is dan per definitie wat er te zien is, ook na een inklap.
  readonly trail = $derived.by((): GraphNode[] => {
    const id = this.selected
    if (!id) return []
    const row = this.rows.find((r) => r.id === id)
    if (!row) return []
    const out: GraphNode[] = []
    for (const step of row.key.split('/')) {
      const node = this.nodes.get(step)
      if (node) out.push(node)
    }
    return out
  })

  readonly trailIds = $derived(new Set(this.trail.map((n) => n.id)))
  readonly selectedNode = $derived(this.selected ? (this.nodes.get(this.selected) ?? null) : null)
  readonly empty = $derived(this.phase === 'ready' && this.roots.length === 0)

  // hasChildren zegt of er nog een niveau onder een knoop zit. Drie bronnen, in
  // volgorde van zekerheid: wat we hebben opgehaald, wat de overview telde, en
  // anders de vorm van de graaf (een citaat is het blad, de rest kán kinderen
  // hebben). De kaart tekent hierop zijn "+"; een badge die niets oplevert is
  // een gebroken belofte, dus een geteld nul telt zwaarder dan de vorm.
  hasChildren(id: string): boolean {
    const loaded = this.childIds.get(id)
    if (loaded && loaded.length > 0) return true
    const counted = this.childCounts.get(id)
    if (counted !== undefined) return counted > 0
    const total = this.childTotals.get(id)
    if (total !== undefined) return total > 0
    const node = this.nodes.get(id)
    return node ? canExpand(node.type) : false
  }

  // badged: er zit nog iets onder én het staat nog niet open.
  badged(id: string): boolean {
    return !this.revealed.has(id) && this.hasChildren(id)
  }

  // iconOf is het glyph van een knoop. De toewijzing per thema is blijvend
  // (parai-graph houdt hem bij op thema-id), dus dezelfde kennisbank levert
  // elke sessie dezelfde iconen op.
  iconOf(id: string): string {
    const node = this.nodes.get(id)
    return node ? iconKey(node.type, node.id, node.label) : 'graph'
  }

  // --- acties ---------------------------------------------------------------

  // load haalt het structuurniveau op: domeinen, thema's en subthema's, en de
  // banden daartussen.
  async load(): Promise<void> {
    if (this.disposed) return
    const gen = ++this.#navGen
    // Een nieuwe plattegrond maakt een lopende focus zinloos: hij zou zijn pad
    // uitklappen in een boom die niet meer bestaat.
    this.#focusGen++
    const current = (): boolean => gen === this.#navGen
    this.phase = 'loading'
    this.#inflight.clear()
    this.notice = null

    const body = await this.#get('/graph/overview', current)
    // undefined = afgebroken of verouderd; een nieuwere load() draagt dan ook
    // de openstaande focuswens, dus die blijft met opzet staan.
    if (body === undefined) return
    if (body === null) {
      // Een mislukte load draagt geen wens meer: zonder dit zou een focus van
      // nu pas bij een geslaagde poging van straks alsnog wegspringen.
      this.#pendingFocus = null
      this.phase = 'failed'
      return
    }

    const payload = parseOverview(body, this.lang)
    const nodes = new Map<string, GraphNode>()
    for (const node of payload.nodes) nodes.set(node.id, node)

    // De ouder-kindbanden van de overview; alleen de structuurrelaties
    // tellen mee, zodat een thema niet ineens 4067 standpunten "heeft".
    const childIds = new Map<string, string[]>()
    const hasParent = new Set<string>()
    for (const edge of payload.edges) {
      if (!OVERVIEW_PARENT_RELATIONS.includes(edge.type)) continue
      if (!nodes.has(edge.source) || !nodes.has(edge.target)) continue
      const list = childIds.get(edge.source) ?? []
      if (!list.includes(edge.target)) list.push(edge.target)
      childIds.set(edge.source, list)
      hasParent.add(edge.target)
    }

    // Wortels zijn wat geen ouder heeft: de domeinen.
    const roots = payload.nodes.filter((n) => !hasParent.has(n.id)).map((n) => n.id)

    // De kleurfamilies liggen vast op de thema-id's, gesorteerd: dezelfde
    // kennisbank geeft elke keer dezelfde tinten.
    const families = new Map<string, number>()
    const themas = payload.nodes
      .filter((n) => n.type === 'thema')
      .map((n) => n.id)
      .sort()
    themas.forEach((id, i) => families.set(id, i % FAMILY_COUNT))

    // Voor de structuurniveaus is de overview compleet: hun aantallen staan
    // meteen vast, zodat uitklappen daar geen verzoek meer kost.
    const childTotals = new Map<string, number>()
    for (const [parent, list] of childIds) childTotals.set(parent, list.length)

    this.nodes = nodes
    this.edges = payload.edges
    this.roots = roots
    this.childIds = childIds
    this.childTotals = childTotals
    this.nextOffsets = new Map()
    this.childCounts = payload.childCounts
    this.families = families
    // De kaart opent op het structuurniveau: de domeinen staan open, dus de
    // thema's zijn meteen te zien. Verder dan dat gaat één klik tegelijk. Dit
    // kost geen verzoek — de banden domein → thema kwamen met de overview mee.
    this.revealed = new Set(roots)
    this.loadingIds = new Set()
    this.totals = payload.totals
    this.kb = payload.kb
    this.phase = 'ready'

    const pending = this.#pendingFocus
    this.#pendingFocus = null
    if (pending) await this.focus(pending)
  }

  // invalidate zegt: wat er ligt is verouderd, maar er kijkt nu niemand naar.
  // De volgende keer dat de tab opengaat wordt er opnieuw geladen, in plaats
  // van de tak waar de gebruiker in zit onder zijn handen weg te trekken.
  invalidate(): void {
    if (this.disposed) return
    if (this.phase === 'ready' || this.phase === 'failed') this.phase = 'idle'
  }

  // expand klapt één niveau uit. Twee keer klikken deelt dezelfde belofte, en
  // een tak die al openstaat is een no-op — geen tweede verzoek.
  expand(id: string): Promise<void> {
    if (this.disposed || this.phase === 'loading' || !this.nodes.has(id)) return Promise.resolve()
    const running = this.#inflight.get(id)
    if (running) return running
    if (this.revealed.has(id)) return Promise.resolve()
    // De overview telde nul kinderen: opengaan zou een lege tak opleveren en
    // een verzoek dat niets oplevert.
    if (this.childCounts.get(id) === 0) return Promise.resolve()
    // childTotals kent deze ouder al: zijn kinderen kwamen met de overview mee
    // of zijn eerder opgehaald. Opnieuw openzetten kost dan geen verzoek — en
    // een blad waarvan we wéten dat het er nul heeft gaat niet open, want
    // aria-expanded="true" boven een lege group is een loze belofte.
    if (this.childTotals.has(id)) {
      if ((this.childIds.get(id)?.length ?? 0) > 0) this.revealed = new Set(this.revealed).add(id)
      return Promise.resolve()
    }
    const promise = this.#fetchChildren(id, 0)
    this.#inflight.set(id, promise)
    return promise
  }

  // more haalt de volgende pagina kinderen op (de server kapt op 150 af).
  more(id: string): Promise<void> {
    if (this.disposed) return Promise.resolve()
    const offset = this.nextOffsets.get(id)
    if (offset === undefined) return Promise.resolve()
    const running = this.#inflight.get(id)
    if (running) return running
    const promise = this.#fetchChildren(id, offset)
    this.#inflight.set(id, promise)
    return promise
  }

  // home is de beginstand: de domeinen met hun thema's, precies zoals load()
  // hem neerzet, zonder keuze en zonder zoekopdracht. Vanaf elk punt in de
  // graaf is dit de ene klik terug naar iets waar je opnieuw vanaf kunt kijken.
  //
  // Wat er níet in zit is het relatiefilter: dat is een bewuste instelling uit
  // de knoppenbalk met een eigen "alles tonen", en die achter iemands rug om
  // terugzetten maakt van één knop twee daden. Wat er ook niet in zit is een
  // verzoek: alles wat hier gebeurt staat al in het geheugen, dus de beginstand
  // is er meteen — ook zonder netwerk.
  home(): void {
    if (this.disposed) return
    // Een wachtende "open in de graaf" hoort de gebruiker straks niet alsnog
    // weg te zetten uit de stand waar hij net om vroeg.
    this.#pendingFocus = null
    this.#focusGen++
    this.selected = null
    this.hovered = null
    this.query = ''
    this.revealed = new Set(this.roots)
    this.selectionRequest++
  }

  collapse(id: string): void {
    if (!this.revealed.has(id)) return
    const next = new Set(this.revealed)
    next.delete(id)
    this.revealed = next
  }

  toggle(id: string): Promise<void> {
    if (this.revealed.has(id)) {
      this.collapse(id)
      return Promise.resolve()
    }
    return this.expand(id)
  }

  // select kiest een knoop die er echt is; een id uit een oude klik of een
  // vreemde bron verandert niets.
  select(id: string | null): void {
    // Wie zelf iets aanwijst, wil niet dat een wachtende "open in de graaf" van
    // daarnet hem straks alsnog wegzet.
    this.#pendingFocus = null
    this.#focusGen++
    this.selectionRequest++
    if (id === null) {
      this.selected = null
      return
    }
    if (!this.nodes.has(id)) return
    this.selected = id
  }

  // focus is "open dit standpunt in de graaf" vanuit een bronkaart: eerst de
  // plattegrond, dan het pad ernaartoe uitklappen, dan selecteren. Komt hij
  // binnen terwijl de overview nog laadt, dan wacht hij netjes op zijn beurt;
  // de laatste vraag wint.
  async focus(id: string): Promise<void> {
    if (this.disposed || id === '') return
    // An unavailable requested source must never display the previous item
    // as if it were the requested one. Keep the branches, clear the detail.
    this.selected = null
    if (this.phase === 'loading') {
      this.#pendingFocus = id
      return
    }
    if (this.phase === 'idle' || this.phase === 'failed') {
      this.#pendingFocus = id
      await this.load()
      return
    }

    // Een eigen teller: een uitklap die intussen binnenkomt hoort deze focus
    // niet te doden, en deze focus geen lopende uitklap.
    const gen = ++this.#focusGen
    const current = (): boolean => gen === this.#focusGen
    this.notice = null
    const body = await this.#get('/graph/nodes/' + encodeURIComponent(id), current)
    if (body === undefined || body === null) return
    const detail = parseNodeDetail(body, this.lang)
    if (!detail || detail.node.id !== id) {
      this.notice = { status: 0, reason: 'node_not_found' }
      return
    }

    this.#merge({ nodes: [detail.node, ...detail.trail], edges: [] })
    for (const step of detail.trail) {
      await this.expand(step.id)
      if (!current() || this.disposed) return
    }
    // Het standpunt zelf blijft dicht: de gebruiker vroeg om dít punt, niet om
    // zijn citaten.
    this.select(detail.node.id)
    // De boom mag hierop de aandacht naar zich toe halen: deze selectie kwam
    // van buiten, dus er staat nog geen cursor waar de gebruiker hem wil.
    if (this.selected === detail.node.id) this.focusRequest++
  }

  dispose(): void {
    if (this.disposed) return
    this.disposed = true
    this.#navGen++
    this.#focusGen++
    this.#pendingFocus = null
    this.#inflight.clear()
    this.#controller.abort()
  }

  // --- binnenwerk -----------------------------------------------------------

  // #get haalt JSON op. `undefined` = afgebroken of verouderd (de aanroeper
  // stopt zonder iets te schrijven), `null` = mislukt met een melding. `current`
  // is de vraag "is dit antwoord nog van deze wereld?": elke aanroeper bepaalt
  // zelf waaraan hij zijn geldigheid ophangt.
  async #get(path: string, current: () => boolean): Promise<unknown | null | undefined> {
    try {
      const res = await this.ports.fetch(API + path, { signal: this.#controller.signal })
      if (this.disposed || !current()) return undefined
      if (!res.ok) {
        const failure = await readFailure(res)
        if (this.disposed || !current()) return undefined
        this.notice = failure
        return null
      }
      const body = (await res.json()) as unknown
      return this.disposed || !current() ? undefined : body
    } catch {
      if (this.disposed || !current()) return undefined
      this.notice = { status: 0 }
      return null
    }
  }

  async #fetchChildren(id: string, offset: number): Promise<void> {
    // Een kinderfetch hoort bij één tak, niet bij de navigatie: zolang die tak
    // er nog staat is zijn antwoord bruikbaar. Zou hij aan #navGen hangen, dan
    // liet een focus() die er middenin komt een uitklap stil sterven — met een
    // verouderde belofte in #inflight als naschok.
    const gen = this.#navGen
    const current = (): boolean => gen === this.#navGen && this.nodes.has(id)
    this.loadingIds = new Set(this.loadingIds).add(id)
    try {
      const path = '/graph/nodes/' + encodeURIComponent(id) + '/children?offset=' + String(offset)
      const body = await this.#get(path, current)
      if (body === undefined || body === null) return
      const payload = parseChildren(body, this.lang)
      this.#merge(payload)

      const children = [...(this.childIds.get(id) ?? [])]
      // Een Set naast de lijst: bij "meer laden" op een tak van 150 zou
      // includes() per binnenkomend kind de hele lijst opnieuw aflopen.
      const seen = new Set(children)
      for (const node of payload.nodes) {
        if (node.id === id || seen.has(node.id)) continue
        seen.add(node.id)
        children.push(node.id)
      }
      this.childIds = new Map(this.childIds).set(id, children)
      this.childTotals = new Map(this.childTotals).set(id, Math.max(payload.total, children.length))
      const offsets = new Map(this.nextOffsets)
      if (payload.nextOffset !== null && children.length < payload.total) offsets.set(id, payload.nextOffset)
      else offsets.delete(id)
      this.nextOffsets = offsets
      // Een blad zonder kinderen klapt niet open; anders zou er een lege tak
      // met aria-expanded="true" blijven staan.
      if (children.length > 0) this.revealed = new Set(this.revealed).add(id)
    } finally {
      if (gen === this.#navGen) {
        const next = new Set(this.loadingIds)
        next.delete(id)
        this.loadingIds = next
        this.#inflight.delete(id)
      }
    }
  }

  // #merge voegt nieuwe knopen en banden toe zonder bestaande te verliezen;
  // een tweede keer dezelfde band levert geen dubbele lijn op.
  #merge(payload: Mutation): void {
    if (payload.nodes.length > 0) {
      const nodes = new Map(this.nodes)
      for (const node of payload.nodes) {
        const previous = nodes.get(node.id)
        nodes.set(node.id, previous ? { ...node, metadata: { ...previous.metadata, ...node.metadata } } : node)
      }
      this.nodes = nodes
    }
    if (payload.edges.length > 0) {
      const seen = new Set(this.edges.map(edgeKey))
      const added = payload.edges.filter((e) => !seen.has(edgeKey(e)))
      if (added.length > 0) this.edges = [...this.edges, ...added]
    }
  }
}

function edgeKey(edge: GraphEdge): string {
  return edge.source + ' ' + edge.target + ' ' + edge.type
}
