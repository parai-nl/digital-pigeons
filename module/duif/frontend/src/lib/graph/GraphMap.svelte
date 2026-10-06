<script lang="ts">
  // De plattegrond zelf: het eerste wat de graaf laat zien. Alles wat hier per
  // exemplaar leeft — de simulatie, het rAF-token, de maatwaarnemer, de
  // tekencontext, de camera — hangt aan deze component en wordt in de opruiming
  // van onMount weer neergelegd. Niets daarvan staat op de store: twee kaarten
  // zouden anders elkaars klok stoppen.
  //
  // De camera komt uit parai-graph (`Viewport`): pannen, zoomen, passend maken,
  // een getweende vlucht naar een knoop, en per tik meebewegen met een groep die
  // nog uit elkaar valt. Dat laatste is waarom een uitklap niet wegloopt: een
  // eenmalige vlucht mikt op een plek die de simulatie meteen weer verplaatst.
  //
  // De rest — de invoerlus, de tekenklok, de raaktest — is van deze module,
  // omdat de duivenkaart alle browser-API's (requestAnimationFrame,
  // ResizeObserver, Image) via GraphPorts krijgt en haar eigen store heeft,
  // en het canvascomponent van parai-graph dat niet zo ontvangt.
  import { badgeGeometry } from '@parai/graph/lib/renderer'
  import { isClickGesture } from '@parai/graph/lib/nodeStyle'
  import { Viewport } from '@parai/graph/lib/viewport'
  import { onMount, untrack } from 'svelte'
  import { makePalette, type Palette } from './colors'
  import type { GraphStore } from './graphStore.svelte'
  import { endpoints, GraphLayout, type GraphControls } from './layout'
  import type { CanvasLike } from './ports'
  import { graphTranslator } from './readingCopy'
  import { drawScene, type Scene, type SceneEdge, type SceneNode } from './renderer'
  import type { UiLocale } from '../i18n.svelte'
  import { nodeRadius } from './visuals'
  import type { NodeType } from './types'

  let {
    store,
    locale,
    controls = $bindable(null),
    active = true,
    overlays = [],
  }: {
    store: GraphStore
    locale: UiLocale
    controls?: GraphControls | null
    active?: boolean
    // overlays zijn de stukken opmaak die de gastheer over het doek legt (de
    // zoomknoppen). De kaart legt er zijn eigen hint bij en houdt die vakken
    // vrij van namen.
    overlays?: readonly (HTMLElement | undefined)[]
  } = $props()

  const t = $derived(graphTranslator(locale))
  // Onder deze zoom is tekst ruis in plaats van taal.
  const LABEL_ZOOM = 0.32
  // Boven zoveel banden aan de selectie stoppen de namen op de lijnen.
  const EDGE_LABEL_MAX = 8

  let host: HTMLDivElement | undefined = $state()
  let canvasEl: HTMLCanvasElement | undefined = $state()
  // De twee stukken opmaak die over het doek liggen: de bedieningshint
  // linksonder en de zoomknoppen rechtsboven. Ze worden bij elke maatwissel
  // opgemeten en als bezet gebied doorgegeven, zodat er geen naam onder
  // verdwijnt.
  let hintEl: HTMLParagraphElement | undefined = $state()
  let reserved: { x: number; y: number; w: number; h: number }[] = []
  let nodeCount = $state(0)
  let edgeCount = $state(0)
  let tip = $state<{ x: number; y: number; label: string; type: NodeType } | null>(null)

  // Buiten de runes: dit is het binnenwerk van de tekenlus, geen UI-stand. De
  // layout wordt hier gemaakt en niet in onMount, want het sync-$effect draait
  // eerder — met een layout die dan nog null is blijft een kaart die op een al
  // gevulde store gemonteerd wordt leeg tot de volgende storewijziging.
  const layout = untrack(() => new GraphLayout(store.ports.reducedMotion()))
  let ctx: CanvasLike | null = null
  let palette: Palette | null = null
  let cancelFrame: (() => void) | null = null
  let stopped = false
  let pageVisible = true
  let pending = { width: 0, height: 0 }
  let size = { width: 0, height: 0, dpr: 0 }
  let dragging: string | null = null
  let down: { x: number; y: number } | null = null
  // followIds is de groep waar de camera achteraan gaat: een net uitgeklapte
  // knoop mét zijn nieuwe kinderen. Losgelaten zodra de gebruiker zelf iets
  // aanwijst, of zodra de simulatie tot rust komt.
  let followIds: Set<string> | null = null
  let fitted = false

  const vp = new Viewport(() => schedule())

  function draw(): void {
    if (!ctx || !palette) return
    if (size.width === 0 || size.height === 0) return
    const now = performance.now()
    const selected = store.selected
    const searching = store.searching
    const matches = store.matchesLoaded

    // De buren van de selectie blijven staan. Zonder dat zakt precies wat je net
    // hebt uitgeklapt mee weg met de rest van de kaart — de klik zou het niveau
    // eronder openen en meteen onleesbaar maken.
    const connected = new Set<string>()
    if (selected !== null) {
      for (const link of layout.links) {
        const from = typeof link.source === 'object' ? link.source.id : ''
        const to = typeof link.target === 'object' ? link.target.id : ''
        if (from === selected) connected.add(to)
        else if (to === selected) connected.add(from)
      }
    }

    const nodes: SceneNode[] = []
    for (const node of layout.nodes) {
      const data = store.nodes.get(node.id)
      if (!data) continue
      const trail = store.trailIds.has(node.id)
      const matched = searching && matches.has(node.id)
      nodes.push({
        id: node.id,
        type: node.type,
        label: data.label,
        full: readingText(node.id),
        x: node.x,
        y: node.y,
        family: node.family,
        depth: node.depth,
        icon: store.iconOf(node.id),
        selected: selected === node.id,
        hovered: store.hovered === node.id,
        trail,
        matched,
        // Wegzakken doet wie noch gekozen, noch op het spoor, noch een treffer
        // is. Zonder selectie en zonder zoekopdracht zakt er niets weg: dan is
        // de hele kaart het onderwerp.
        dim: searching
          ? !matched && !trail && selected !== node.id
          : selected !== null && !trail && selected !== node.id && !connected.has(node.id),
        badged: store.badged(node.id),
        enterAt: node.enterAt,
      })
    }

    // Namen op de lijnen leggen uit wat een band is — tot er te veel van zijn.
    // Een uitgeklapt thema hangt aan 27 kinderen, en 27 keer "Subthema" naast
    // elkaar is een muur, geen uitleg. Boven de drempel blijven alleen het spoor
    // en de band onder de muis benoemd; de legenda houdt het volledige verhaal.
    const hovered = store.hovered
    let selectionEdges = 0
    let hoveredEdges = 0
    for (const link of layout.links) {
      const from = typeof link.source === 'object' ? link.source.id : ''
      const to = typeof link.target === 'object' ? link.target.id : ''
      if (selected !== null && (from === selected || to === selected)) selectionEdges++
      if (hovered !== null && (from === hovered || to === hovered)) hoveredEdges++
    }
    const nameSelectionEdges = selectionEdges > 0 && selectionEdges <= EDGE_LABEL_MAX
    // Dezelfde drempel geldt onder de muis: wie het uitgeklapte thema zelf
    // aanwijst, krijgt niet alsnog 27 keer "Subthema" te lezen.
    const nameHoveredEdges = hoveredEdges > 0 && hoveredEdges <= EDGE_LABEL_MAX

    const edges: SceneEdge[] = []
    for (const link of layout.links) {
      const ends = endpoints(link)
      if (!ends) continue
      const from = typeof link.source === 'object' ? link.source.id : ''
      const to = typeof link.target === 'object' ? link.target.id : ''
      const onTrail = store.trailIds.has(from) && store.trailIds.has(to)
      const incident = selected !== null && (from === selected || to === selected)
      const underPointer = hovered !== null && (from === hovered || to === hovered)
      // Het anker is de kant waar je vanaf leest: de knoop onder de muis wint,
      // want die wees de lezer zojuist zelf aan.
      const anchor = underPointer ? hovered : incident ? selected : null
      const named = anchor !== null && (onTrail || (underPointer && nameHoveredEdges) || (incident && nameSelectionEdges))
      edges.push({
        ...ends,
        type: link.type,
        trail: onTrail,
        incident: incident && !onTrail,
        dim: (searching || selected !== null) && !onTrail && !incident && !underPointer,
        // (een band tussen twee buren van de selectie zakt mee weg; de knopen
        //  zelf blijven staan, dat is wat de lezer moet kunnen aanwijzen)
        label: named ? relationName(link.type, from === anchor ? to : from, anchor) : '',
        enterAt: 0,
      })
    }

    const scene: Scene = {
      width: size.width,
      height: size.height,
      dpr: size.dpr,
      transform: { x: vp.x, y: vp.y, k: vp.k },
      nodes,
      edges,
      palette,
      now,
      labels: vp.k >= LABEL_ZOOM,
      reserved,
    }
    drawScene(ctx, scene)
  }

  // De volledige tekst achter een afgekapt label. Alleen een bevinding en een
  // citaat krijgen die op de kaart: hun label ís een afkapping van hun tekst, dus
  // zonder dit lees je een halve zin. Een thema of domein heeft een echte titel;
  // daar zou de beschrijving een alinea over de kaart schilderen, en die staat
  // toch al in het leespaneel.
  function readingText(id: string): string {
    const node = store.nodes.get(id)
    if (!node || (node.type !== 'destillaat' && node.type !== 'quote')) return ''
    const text = node.metadata.text ?? ''
    return text.trim() === '' || text.trim() === node.label.trim() ? '' : text
  }

  // De naam van een band zoals je hem vanaf de selectie leest: hij heet naar wat
  // er aan de andere kant staat ("Citaten", "Bron"). Wijst hij naar zijn eigen
  // soort, dan draagt de relatie zelf de naam.
  function relationName(type: string, otherId: string, selfId: string): string {
    const other = store.nodes.get(otherId)
    const self = store.nodes.get(selfId)
    if (!other || !self) return ''
    if (other.type === self.type) return t('graph.rel.other')
    return t(`graph.type.${other.type}`)
  }

  // applySize gebeurt in de lus, nooit in de waarnemer zelf: een canvas dat
  // midden in een tekening van maat verandert wist zichzelf.
  function applySize(): void {
    const dpr = store.ports.pixelRatio()
    if (pending.width === size.width && pending.height === size.height && dpr === size.dpr) return
    if (pending.width <= 0 || pending.height <= 0) return
    // Een echte maatwissel — het venster gaat open, de scheiding verschuift —
    // hoort de tekening opnieuw te kaderen: een kaart die na het versmallen
    // half buiten beeld hangt is geen kaart meer. Van nul naar iets is geen
    // wissel maar de eerste tekening, en die kadert toch al.
    const resized = size.width > 0 && size.height > 0
    size = { width: pending.width, height: pending.height, dpr }
    if (!canvasEl) return
    canvasEl.width = Math.max(1, Math.round(size.width * dpr))
    canvasEl.height = Math.max(1, Math.round(size.height * dpr))
    if (vp.x === 0 && vp.y === 0) {
      vp.x = size.width / 2
      vp.y = size.height / 2
    }
    measureOverlays()
    // De lus kadert hieronder zelf, mét de nieuwe maat in de hand.
    if (resized) fitted = false
  }

  // Eén meting per maatwissel, nooit per tekenbeurt: de opmaak vragen naar zijn
  // plek kost een herberekening van de bladspiegel.
  function measureOverlays(): void {
    const frame = host?.getBoundingClientRect()
    if (!frame) return
    const boxes: { x: number; y: number; w: number; h: number }[] = []
    for (const el of [hintEl, ...overlays]) {
      const at = el?.getBoundingClientRect()
      if (!at || at.width === 0) continue
      boxes.push({ x: at.left - frame.left - 4, y: at.top - frame.top - 4, w: at.width + 8, h: at.height + 8 })
    }
    reserved = boxes
  }

  function frame(): void {
    cancelFrame = null
    if (stopped) return
    applySize()
    const moving = layout.step()
    // De eerste tekening wordt één keer passend gemaakt; daarna is de camera van
    // de gebruiker.
    if (!fitted && size.width > 0 && layout.nodes.length > 0) {
      fitted = true
      fit()
    }
    if (followIds) {
      const box = layout.boundsOf(followIds)
      // Ruimer kaderen dan de standaard: een uitgeklapte knoop met een tros
      // kinderen mag niet tot aan de rand van het doek staan.
      if (box) vp.followBounds(box, size.width, size.height, 0.18, 220)
      if (!moving) followIds = null
    }
    draw()
    if (moving || followIds) schedule()
  }

  function schedule(): void {
    if (stopped || cancelFrame) return
    // Een gesloten tabblad of een verborgen pagina tekent niet: dat is hier de
    // hele "sim.stop()" — er is maar één klok, en die staat dan stil.
    if (!active || !store.visible || !pageVisible) return
    cancelFrame = store.ports.frame(frame)
  }

  // Het enige data-$effect: het leest de zichtbare graaf en legt hem op de
  // layout. untrack rond de synchronisatie, zodat d3's muteren van x/y nooit als
  // afhankelijkheid terugkomt.
  $effect(() => {
    const nodes = [...store.visibleIds].map((id) => ({
      id,
      type: store.nodes.get(id)?.type ?? 'thema',
      family: store.view.family.get(id) ?? -1,
      depth: store.view.depth.get(id) ?? 0,
    }))
    const edges = store.visibleEdges.map((e) => ({ source: e.source, target: e.target, type: e.type }))
    untrack(() => {
      nodeCount = nodes.length
      edgeCount = edges.length
      layout.sync({ nodes, edges, now: performance.now() })
      schedule()
    })
  })

  $effect(() => {
    const awake = active && store.visible && pageVisible
    untrack(() => {
      if (awake) schedule()
      else {
        cancelFrame?.()
        cancelFrame = null
      }
    })
  })

  // De selectie tekent anders (halo, spoor, volledige tekst), dus één frame
  // extra.
  $effect(() => {
    store.selected
    store.hovered
    store.query
    store.hiddenRelations
    untrack(() => schedule())
  })

  // Een keuze brengt de camera naar de knoop. Alleen vliegen, niet volgen: wat
  // gekozen wordt staat er meestal al, en meebewegen met een groep zou dan
  // ongevraagd uitzoomen.
  $effect(() => {
    store.selectionRequest
    store.focusRequest
    const id = store.selected
    untrack(() => {
      if (id !== null) flyToNode(id)
    })
  })

  function flyToNode(id: string): void {
    if (!fitted || size.width === 0) return
    const at = layout.position(id)
    if (!at) return
    vp.flyToPoint(at.x, at.y, Math.max(vp.k, 0.9), size.width, size.height)
    schedule()
  }

  // Na een uitklap volgt de camera de knoop mét zijn nieuwe kinderen, per tik,
  // tot de simulatie stil ligt. Een eenmalige vlucht zou mikken op een plek die
  // de simulatie meteen weer verplaatst — dan loopt de uitklap van het scherm.
  function followExpanded(id: string): void {
    const group = new Set<string>([id])
    for (const child of store.childIds.get(id) ?? []) group.add(child)
    followIds = group
    schedule()
  }

  onMount(() => {
    const ports = store.ports
    if (host) palette = makePalette(ports.readTheme(host))
    if (canvasEl) ctx = ports.context(canvasEl)
    const unobserve = host
      ? ports.observeSize(host, (width, height) => {
          pending = { width, height }
          schedule()
        })
      : () => {}
    const unwatch = ports.onVisible((visible) => {
      pageVisible = visible
      if (visible) schedule()
      else {
        cancelFrame?.()
        cancelFrame = null
      }
    })
    if (host) pending = { width: host.clientWidth, height: host.clientHeight }
    controls = {
      zoomIn: () => vp.zoomAt(size.width / 2, size.height / 2, 1.3),
      zoomOut: () => vp.zoomAt(size.width / 2, size.height / 2, 0.77),
      reset: () => fit(),
      home: () => {
        // De store heeft net alle takken dichtgeklapt; wat overblijft staat nog
        // op de plek die het had toen er honderden knopen omheen stonden. Eerst
        // opnieuw uitvouwen, dan kaderen — anders kadert de camera een tekening
        // die zichzelf een tel later opnieuw ordent.
        vp.cancelFly()
        layout.warm()
        fit()
      },
    }
    schedule()
    return () => {
      stopped = true
      cancelFrame?.()
      cancelFrame = null
      vp.cancelFly()
      unobserve()
      unwatch()
      layout.stop()
      ctx = null
      controls = null
    }
  })

  function fit(): void {
    followIds = null
    const box = layout.bounds()
    if (!box || size.width === 0) {
      vp.x = size.width / 2
      vp.y = size.height / 2
      vp.k = 1
      schedule()
      return
    }
    // Ruimer kaderen dan de knopen zelf: de namen staan naast, boven of onder
    // hun knoop, en zonder deze marge valt de buitenste naam net buiten beeld.
    vp.fit(
      { minX: box.minX - 64, minY: box.minY - 64, maxX: box.maxX + 64, maxY: box.maxY + 64 },
      size.width,
      size.height,
    )
  }

  function at(event: PointerEvent | WheelEvent): { x: number; y: number; px: number; py: number } {
    const box = canvasEl?.getBoundingClientRect()
    const px = event.clientX - (box?.left ?? 0)
    const py = event.clientY - (box?.top ?? 0)
    const point = vp.toGraph(px, py)
    return { ...point, px, py }
  }

  // De raaktest kijkt eerst naar de knoop zelf en dan naar zijn "+"-badge. De
  // badge zit buiten de straal van de knoop, dus zonder deze tweede vraag zou
  // een klik erop als achtergrond tellen en de selectie juist loslaten. De
  // meetkunde komt uit dezelfde functie als de tekening, zodat de twee nooit uit
  // elkaar lopen.
  function hit(x: number, y: number): { id: string; badge: boolean } | null {
    const near = layout.find(x, y, 70 / vp.k)
    if (!near) return null
    const r = nodeRadius(near.type)
    const dx = x - near.x
    const dy = y - near.y
    // Zes schermpixels speling rond de rand: een muis landt zelden precies op
    // een schijf, en een citaat is ook na de vergroting nog een klein doel.
    if (dx * dx + dy * dy <= (r + 6 / vp.k) ** 2) return { id: near.id, badge: false }
    if (store.badged(near.id)) {
      const b = badgeGeometry(near.x, near.y, r)
      const bx = x - b.x
      const by = y - b.y
      if (bx * bx + by * by <= b.r * b.r) return { id: near.id, badge: true }
    }
    return null
  }

  function onPointerDown(event: PointerEvent): void {
    if (!canvasEl) return
    // Optioneel aangeroepen: jsdom kent pointer capture niet, en de kaart hoort
    // ook zonder dat te werken — het is comfort tijdens een sleep, geen gedrag.
    canvasEl.setPointerCapture?.(event.pointerId)
    // Wie zelf de kaart pakt, neemt de camera over.
    followIds = null
    vp.cancelFly()
    const point = at(event)
    down = { x: event.clientX, y: event.clientY }
    const target = hit(point.x, point.y)
    if (target && !target.badge) {
      dragging = target.id
      layout.pin(target.id, point.x, point.y)
    } else {
      dragging = null
    }
    schedule()
  }

  function onPointerMove(event: PointerEvent): void {
    // Een pointerup die buiten het venster viel bereikt ons nooit. De eerste
    // beweging zonder ingedrukte knop is dan alsnog het einde van de sleep.
    if (event.buttons === 0) {
      if (dragging) onPointerUp(event)
      hover(event)
      return
    }
    const point = at(event)
    if (dragging) {
      layout.pin(dragging, point.x, point.y)
      layout.reheat(0.1)
    } else {
      vp.panBy(event.movementX, event.movementY)
    }
    schedule()
  }

  function hover(event: PointerEvent): void {
    const point = at(event)
    const target = hit(point.x, point.y)
    const node = target ? store.nodes.get(target.id) : undefined
    store.hovered = node ? node.id : null
    tip = node ? { x: point.px, y: point.py, label: node.label, type: node.type } : null
  }

  function onPointerUp(event: PointerEvent): void {
    if (canvasEl?.hasPointerCapture?.(event.pointerId)) canvasEl.releasePointerCapture?.(event.pointerId)
    if (dragging) layout.release(dragging)
    const start = down
    down = null
    const wasDragging = dragging
    dragging = null
    if (!start || !isClickGesture(start.x, start.y, event.clientX, event.clientY)) return

    const point = at(event)
    const target = hit(point.x, point.y)
    if (!target) {
      if (!wasDragging) store.select(null)
      return
    }
    // Eén klik is één niveau: kiezen, en wat eronder zit openen. Staat het al
    // open, dan blijft het bij kiezen — dieper gaat de volgende klik.
    store.select(target.id)
    if (store.badged(target.id)) void openLevel(target.id)
  }

  async function openLevel(id: string): Promise<void> {
    await store.expand(id)
    if (stopped) return
    followExpanded(id)
  }

  function onPointerLeave(): void {
    store.hovered = null
    tip = null
    schedule()
  }

  function onWheel(event: WheelEvent): void {
    event.preventDefault()
    followIds = null
    const point = at(event)
    vp.zoomAt(point.px, point.py, event.deltaY < 0 ? 1.12 : 0.89)
  }
</script>

<div
  bind:this={host}
  class="relative min-h-0 flex-1 overflow-hidden rounded-[var(--duif-radius-stage)] bg-[var(--duif-graph-canvas)]"
  role="img"
  aria-label={t('graph.map.title')}
  data-testid="graph-canvas"
  data-node-count={nodeCount}
  data-edge-count={edgeCount}
>
  <!-- Het canvas is een plaatje: de bedienbare, voorleesbare versie van
       dezelfde graaf is de lijstweergave in de knoppenbalk.
       absolute inset-0: de width/height-attributen van een canvas zijn een
       intrinsieke maat. Stond het canvas in de stroom, dan zou die maat zijn
       container groter maken, de ResizeObserver een grotere maat melden en het
       canvas dus opnieuw groeien — een lus die het beeld tot een lange strook
       uitrekt. Buiten de stroom kan het nooit zijn eigen doos duwen. -->
  <canvas
    bind:this={canvasEl}
    class="absolute inset-0 h-full w-full touch-none"
    aria-hidden="true"
    onpointerdown={onPointerDown}
    onpointermove={onPointerMove}
    onpointerup={onPointerUp}
    onpointercancel={onPointerUp}
    onpointerleave={onPointerLeave}
    onwheel={onWheel}
  ></canvas>
  {#if tip}
    <div class="graph-tip" style="left:{tip.x}px; top:{tip.y}px" aria-hidden="true">
      <span>{t(`graph.type.${tip.type}`)}</span>{tip.label}
    </div>
  {/if}
  <p bind:this={hintEl} class="graph-map-hint" aria-hidden="true">{t('graph.map.hint')}</p>
</div>

<style>
  .graph-tip {
    position: absolute;
    transform: translate(-50%, calc(-100% - 14px));
    max-width: 20rem;
    padding: 0.4rem 0.6rem;
    border-radius: 0.5rem;
    background: var(--duif-ink);
    color: var(--duif-paper);
    font-size: 0.78rem;
    line-height: 1.4;
    pointer-events: none;
    z-index: 2;
  }
  .graph-tip span {
    display: block;
    font-size: 0.65rem;
    text-transform: uppercase;
    letter-spacing: 0.07em;
    opacity: 0.7;
  }
  .graph-map-hint {
    position: absolute;
    left: 0.75rem;
    bottom: 0.6rem;
    margin: 0;
    font-size: 0.72rem;
    color: var(--duif-dim);
    pointer-events: none;
  }
</style>
