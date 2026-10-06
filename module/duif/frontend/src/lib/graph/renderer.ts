import { drawIcon } from '@parai/graph/lib/icons'
import { badgeGeometry, wrapLabel } from '@parai/graph/lib/renderer'
import type { Palette } from './colors'
import type { CanvasLike } from './ports'
import type { NodeType } from './types'
import { nodeRadius, nodeShape, relationStyle } from './visuals'

// De tekenaar is puur: context in, tekening uit. Hij houdt niets vast, kent
// geen tijd (de klok komt als `now` mee in de scène) en vraagt de DOM niets —
// daardoor is hij met een neptekencontext regel voor regel te toetsen, en kan
// de tekenlus in de component blijven.
//
// De vormtaal: domein een tegel, thema en subthema een schijf, bron een
// kaartje, bevinding en citaat een kleine schijf. Kleur zegt bij
// welk thema iets hoort, vorm zegt wát het is, het icoontje waar het over gaat.
// Alleen de selectie krijgt een verzadigde halo — dat is de enige plek waar de
// kaart echt kleur bekent.
//
// Twee dingen komen uit parai-graph omdat ze daar al kloppen: `wrapLabel` (de
// woordafbreking met zichtbare afkapping) en `badgeGeometry` (de plek van de
// "+"-badge, gedeeld met de raaktest zodat het bolletje ook echt aanklikbaar
// is). De rest van de tekenbeurt is van deze module, omdat zij het eigen
// thema en de tabellen van deze kennisbank volgt.

// DrawContext is CanvasLike plus wat een echte 2D-context extra kan. Die extra's
// zijn optioneel: de neptekencontext van de tests kent ze niet, en een streepje
// of een tekstschaduw minder maakt een tekening niet fout. Wat de tekening wél
// draagt (vorm, kleur, plaats, tekst) staat in CanvasLike en wordt dus overal
// getekend, ook in jsdom.
export type DrawContext = CanvasLike &
  Partial<Pick<CanvasRenderingContext2D, 'measureText' | 'setLineDash'>>

export interface SceneNode {
  id: string
  type: NodeType
  // label is de korte tekst van de wire; full is de volledige tekst erachter
  // (de samenvatting van een bevinding, het citaat zelf). Leeg als er niets
  // meer te lezen valt dan het label.
  label: string
  full: string
  x: number
  y: number
  family: number
  depth: number
  icon: string
  selected: boolean
  hovered: boolean
  // trail: op het kruimelpad naar de selectie.
  trail: boolean
  // matched: treffer van de zoekopdracht.
  matched: boolean
  // dim: buiten de zoekresultaten of buiten de selectie; blijft staan, zakt weg.
  dim: boolean
  // badged: er zit nog een niveau onder dat nog niet is uitgeklapt.
  badged: boolean
  // enterAt is het moment waarop de knoop verscheen; 0 = stond er al.
  enterAt: number
}

export interface SceneEdge {
  x1: number
  y1: number
  x2: number
  y2: number
  type: string
  trail: boolean
  // incident: raakt de selectie, maar ligt niet op het spoor omhoog.
  incident: boolean
  dim: boolean
  // label is de naam van de band, alleen gevuld voor banden aan de selectie.
  label: string
  enterAt: number
}

export interface Scene {
  width: number
  height: number
  dpr: number
  transform: { x: number; y: number; k: number }
  nodes: readonly SceneNode[]
  edges: readonly SceneEdge[]
  palette: Palette
  // now is de klok van dit frame; hij stuurt de intocht van nieuwe knopen.
  now: number
  // labels gaan uit bij ver uitzoomen: onleesbare tekst is ruis.
  labels: boolean
  // reserved zijn stukken doek waar de component zelf iets overheen legt — de
  // bedieningshint linksonder, de zoomknoppen rechtsboven. De tekenaar ziet die
  // opmaak niet, dus krijgt hij de vakken mee: een naam onder een vaste tekst
  // is net zo onleesbaar als een naam over een knoop.
  reserved?: readonly { x: number; y: number; w: number; h: number }[]
}

// Onder deze breedte is er geen ruimte voor tekst naast de knopen. De
// knoppenbalk leest hem mee: de boomkolom mag nooit zó breed worden dat de
// kaart eronder zakt, want dan verdwijnt elk label tegelijk.
export const COMPACT_WIDTH = 420

// De zoom waarboven een soort zijn label uit zichzelf laat zien. Daaronder
// alleen als de knoop gekozen, aangewezen of een zoektreffer is. Zonder deze
// trap staan er bij het openen 475 labels over elkaar heen.
const LABEL_ZOOM: Readonly<Record<NodeType, number>> = {
  domein: 0,
  thema: 0,
  subthema: 0.8,
  interview: 1.15,
  destillaat: 1.3,
  quote: 1.6,
}

// Wie eerst gaat, houdt zijn plek: hoger in de boom = eerder aan de beurt bij
// het verdelen van de ruimte.
const LABEL_RANK: Readonly<Record<NodeType, number>> = {
  domein: 6,
  thema: 5,
  subthema: 3,
  interview: 2,
  destillaat: 1,
  quote: 0,
}

const LINE_HEIGHT = 15
// De volledige tekst van een bevinding is een hele zin; een citaat kan een
// alinea zijn. Dit is het plafond: zonder plafond schildert een kennisbank met
// een bron van 5 000 tekens een kolom over de hele kaart.
const FULL_MAX_LINES = 16
const SHORT_MAX_LINES = 2

const ENTER_MS = 900

// entry is de intocht van een nieuwe knoop: eerst niets, dan groeit en verschijnt
// hij. Smoothstep (langzaam in én uit) in plaats van een ease-out, want een
// tros citaten die tegelijk opkomt leest anders als een klap.
function entry(enterAt: number, now: number): number {
  if (enterAt <= 0) return 1
  const t = Math.min(1, Math.max(0, (now - enterAt) / ENTER_MS))
  return t * t * (3 - 2 * t)
}

function roundedRect(ctx: CanvasLike, x: number, y: number, w: number, h: number, r: number): void {
  const radius = Math.min(r, w / 2, h / 2)
  ctx.beginPath()
  ctx.moveTo(x + radius, y)
  ctx.lineTo(x + w - radius, y)
  ctx.quadraticCurveTo(x + w, y, x + w, y + radius)
  ctx.lineTo(x + w, y + h - radius)
  ctx.quadraticCurveTo(x + w, y + h, x + w - radius, y + h)
  ctx.lineTo(x + radius, y + h)
  ctx.quadraticCurveTo(x, y + h, x, y + h - radius)
  ctx.lineTo(x, y + radius)
  ctx.quadraticCurveTo(x, y, x + radius, y)
  ctx.closePath()
}

function shapePath(ctx: CanvasLike, type: NodeType, x: number, y: number, r: number): void {
  switch (nodeShape(type)) {
    case 'tile':
      roundedRect(ctx, x - r, y - r, r * 2, r * 2, r * 0.32)
      return
    case 'card':
      roundedRect(ctx, x - r * 0.82, y - r, r * 1.64, r * 2, r * 0.22)
      return
    default:
      ctx.beginPath()
      ctx.arc(x, y, r, 0, Math.PI * 2)
      ctx.closePath()
  }
}

// Path2D bestaat in de browser en niet in jsdom. Een ontbrekend icoontje maakt
// de tekening niet fout — de vorm en de kleur dragen de betekenis al — dus de
// glyph slaat stil over in plaats van de hele tekenbeurt te laten vallen.
function drawGlyph(ctx: CanvasLike, key: string, x: number, y: number, size: number, color: string): void {
  if (typeof Path2D !== 'function') return
  // drawIcon vraagt een volledige 2D-context, maar gebruikt er alleen save,
  // translate, scale, fillStyle, fill(Path2D) en restore van — precies wat
  // CanvasLike belooft. Deze ene cast is de hele overbrugging.
  drawIcon(ctx as CanvasRenderingContext2D, key, x, y, size, color)
}

function textWidth(ctx: DrawContext, text: string): number {
  const measured = ctx.measureText?.(text)
  return measured ? measured.width : text.length * 6.2
}

export function drawScene(ctx: DrawContext, scene: Scene): void {
  const { palette, transform, now } = scene
  const dpr = scene.dpr > 0 ? scene.dpr : 1
  const k = transform.k

  ctx.save()
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
  ctx.globalAlpha = 1
  ctx.fillStyle = palette.canvas
  // clearRect zou de doorzichtige gastheer laten zien; de plattegrond heeft
  // een eigen papier — en dat papier komt uit de tokens van de gastheer, dus
  // licht en donker volgen vanzelf.
  ctx.clearRect(0, 0, scene.width, scene.height)
  ctx.fillRect(0, 0, scene.width, scene.height)
  ctx.translate(transform.x, transform.y)
  ctx.scale(k, k)

  drawEdges(ctx, scene)
  drawNodes(ctx, scene, now)

  ctx.restore()

  // De tekst gaat in schermruimte: even groot bij elke zoom, en dus leesbaar op
  // afstand. De dpr-transform moet blijven staan — op identiteit landt elk label
  // op een retina-scherm op de helft van zijn plek en zweeft het weg van zijn
  // knoop.
  ctx.save()
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
  if (scene.labels) {
    drawEdgeLabels(ctx, scene)
    drawLabels(ctx, scene)
  }
  ctx.restore()
}

function drawEdges(ctx: DrawContext, scene: Scene): void {
  const k = scene.transform.k
  for (const edge of scene.edges) {
    const style = relationStyle(edge.type)
    let alpha = 0.42
    let width = 1.2
    if (edge.trail) {
      alpha = 1
      width = 3
    } else if (edge.incident) {
      alpha = 0.9
      width = 2
    } else if (edge.dim) {
      alpha = 0.08
    }
    ctx.globalAlpha = alpha * entry(edge.enterAt, scene.now)
    ctx.strokeStyle = edge.trail ? scene.palette.halo : style.color
    ctx.lineWidth = width / k
    ctx.setLineDash?.(style.dash.map((d) => d / k))
    ctx.beginPath()
    ctx.moveTo(edge.x1, edge.y1)
    ctx.lineTo(edge.x2, edge.y2)
    ctx.stroke()
  }
  ctx.setLineDash?.([])
  ctx.globalAlpha = 1
}

function drawNodes(ctx: DrawContext, scene: Scene, now: number): void {
  const { palette } = scene
  const k = scene.transform.k
  for (const node of scene.nodes) {
    const e = entry(node.enterAt, now)
    const r = nodeRadius(node.type) * (0.62 + 0.38 * e)

    // De halo van de selectie ligt eronder en is de enige verzadigde kleur op
    // de kaart. Een zoektreffer krijgt hem bleker, zodat "gevonden" en "gekozen"
    // niet hetzelfde roepen.
    if (node.selected || node.matched) {
      ctx.globalAlpha = (node.selected ? 0.34 : 0.18) * e
      ctx.fillStyle = palette.halo
      ctx.beginPath()
      ctx.arc(node.x, node.y, r * (node.selected ? 1.9 : 1.55), 0, Math.PI * 2)
      ctx.fill()
    }

    ctx.globalAlpha = (node.dim ? 0.22 : 1) * e
    ctx.fillStyle = palette.fill(node.family, node.depth)
    shapePath(ctx, node.type, node.x, node.y, r)
    ctx.fill()
    ctx.lineWidth = (node.selected ? 2.6 : node.hovered || node.trail ? 1.8 : 1) / k
    ctx.strokeStyle = node.selected || node.hovered ? palette.halo : palette.edge
    ctx.stroke()

    drawGlyph(ctx, node.icon, node.x, node.y, r * 1.15, palette.glyph(node.family))

    if (node.badged) drawBadge(ctx, scene, node, r)
    ctx.globalAlpha = 1
  }
}

// De "+"-badge zit op de rechterschouder van de knoop en zegt: hier zit nog een
// niveau onder. Zijn plek komt uit parai-graph, zodat de raaktest en de tekening
// het over dezelfde cirkel hebben en het bolletje ook echt aanklikbaar is.
function drawBadge(ctx: DrawContext, scene: Scene, node: SceneNode, r: number): void {
  const { palette } = scene
  const k = scene.transform.k
  const b = badgeGeometry(node.x, node.y, r)
  ctx.beginPath()
  ctx.arc(b.x, b.y, b.r, 0, Math.PI * 2)
  ctx.fillStyle = palette.ink
  ctx.fill()
  ctx.lineWidth = 1.4 / k
  ctx.strokeStyle = palette.canvas
  ctx.stroke()
  ctx.fillStyle = palette.canvas
  ctx.font = `700 ${(b.r * 1.5).toFixed(1)}px ui-sans-serif, system-ui, sans-serif`
  ctx.textAlign = 'center'
  ctx.textBaseline = 'middle'
  ctx.fillText('+', b.x, b.y)
  ctx.textBaseline = 'alphabetic'
}

// De banden aan de selectie krijgen hun naam erbij: zonder die namen is een
// gekleurde lijn een raadsel dat alleen de legenda oplost.
function drawEdgeLabels(ctx: DrawContext, scene: Scene): void {
  const { transform: v } = scene
  if (scene.width < COMPACT_WIDTH || v.k < 0.45) return
  ctx.font = '10.5px ui-sans-serif, system-ui, sans-serif'
  ctx.textAlign = 'center'
  ctx.textBaseline = 'alphabetic'
  for (const edge of scene.edges) {
    if (edge.label === '') continue
    ctx.globalAlpha = 1
    ctx.fillStyle = relationStyle(edge.type).color
    ctx.fillText(edge.label, v.x + ((edge.x1 + edge.x2) / 2) * v.k, v.y + ((edge.y1 + edge.y2) / 2) * v.k - 4)
  }
}

interface Box {
  x: number
  y: number
  w: number
  h: number
}

function overlaps(a: Box, b: Box): boolean {
  return a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y
}

// Hoever een label van zijn knoop mag wegzakken als er naast hem nergens plek
// is. Verder dan dit hoort het label bij niets meer; dan is geen naam beter dan
// een naam die de verkeerde knoop lijkt aan te wijzen.
const MAX_DRIFT = 110

// nodeBox is het vak dat een knoop op het scherm bezet houdt, inclusief zijn
// "+"-badge: die zit buiten de straal, en een label eroverheen maakt precies de
// affordance onzichtbaar die zegt dat er nog een niveau onder zit. De meetkunde
// komt uit dezelfde functie als de tekening.
function nodeBox(node: SceneNode, scene: Scene): Box | null {
  const v = scene.transform
  const graphR = nodeRadius(node.type)
  const r = graphR * v.k
  const cx = v.x + node.x * v.k
  const cy = v.y + node.y * v.k
  if (cx < -200 || cx > scene.width + 200 || cy < -200 || cy > scene.height + 200) return null
  let minX = cx - r
  let minY = cy - r
  let maxX = cx + r
  let maxY = cy + r
  if (node.badged) {
    const badge = badgeGeometry(node.x, node.y, graphR)
    const bx = v.x + badge.x * v.k
    const by = v.y + badge.y * v.k
    const br = badge.r * v.k
    minX = Math.min(minX, bx - br)
    minY = Math.min(minY, by - br)
    maxX = Math.max(maxX, bx + br)
    maxY = Math.max(maxY, by + br)
  }
  return { x: minX, y: minY, w: maxX - minX, h: maxY - minY }
}

const FORCED_FONT = '600 13px ui-sans-serif, system-ui, sans-serif'
const PLAIN_FONT = '11.5px ui-sans-serif, system-ui, sans-serif'
const READING_FONT = '13px ui-serif, Georgia, serif'

// forcedLabel zegt of een knoop zijn naam hoe dan ook laat zien: de selectie,
// de knoop onder de muis, het spoor ernaartoe en elke zoektreffer. De rest van
// de kaart hangt aan de zoomtrap.
function forcedLabel(node: SceneNode): boolean {
  return node.selected || node.hovered || node.trail || node.matched
}

function drawLabels(ctx: DrawContext, scene: Scene): void {
  const { palette, transform: v } = scene
  if (scene.width < COMPACT_WIDTH) return

  // Alles in `occupied` is bezet gebied, en een label mag nergens overheen —
  // niet over een ander label en niet over een knoop of zijn "+"-badge. Tekst
  // over een schijf maakt allebei onleesbaar en verstopt de affordance die
  // zegt dat er nog iets onder zit; liever een naam een eindje van zijn knoop
  // dan een naam eroverheen.
  //
  // Wat wegzakt telt niet mee. Een knoop op tweeëntwintig procent is
  // achtergrond, en zou hij wél bezet gebied zijn, dan hield precies de knoop
  // die de lezer aanklikte — omringd door een tros weggezakte buren — geen plek
  // meer over voor zijn eigen tekst.
  const occupied: Box[] = []
  for (const area of scene.reserved ?? []) occupied.push({ ...area })
  for (const node of scene.nodes) {
    if (node.dim) continue
    const box = nodeBox(node, scene)
    if (box) occupied.push(box)
  }

  // De selectie wordt als eerste ingedeeld en heeft daarmee de eerste keus uit
  // de plekken rond haar eigen knoop; het spoor eromheen schuift weg voor haar,
  // niet andersom.
  const order = [...scene.nodes].sort((a, b) => rank(b) - rank(a))
  ctx.textAlign = 'center'
  ctx.textBaseline = 'top'

  for (const node of order) {
    const force = forcedLabel(node)
    if (!force && v.k < LABEL_ZOOM[node.type]) continue
    if (node.dim && !force) continue
    const cx = v.x + node.x * v.k
    const cy = v.y + node.y * v.k
    const r = nodeRadius(node.type) * v.k
    // De bovenkant van het papieren vlakje onder de tekst; de tekst zelf begint
    // drie pixels lager. Vlak en vak zijn hetzelfde rechthoekje, zodat wat er
    // getekend wordt ook precies is wat er bezet gehouden wordt.
    const below = cy + r + 2
    if (cx < -120 || cx > scene.width + 120 || below < -60 || below > scene.height + 60) continue

    // Wie gekozen is of op het spoor ligt, wordt gelezen: die krijgt de volledige
    // tekst, niet de afgekapte. Een bevinding is een hele zin — daar een
    // beletselteken achter zetten verbergt juist wat de gebruiker aanklikte.
    const reading = node.selected || node.trail
    const text = reading && node.full !== '' ? node.full : node.label
    ctx.font = reading ? READING_FONT : force ? FORCED_FONT : PLAIN_FONT
    const lines = wrapLabel(text, reading ? 30 : force ? 26 : 22, reading ? FULL_MAX_LINES : SHORT_MAX_LINES, reading)
    if (lines.length === 0) continue
    const width = Math.max(...lines.map((line) => textWidth(ctx, line))) + 10
    const height = lines.length * LINE_HEIGHT + 6
    const box: Box = { x: cx - width / 2, y: below, w: width, h: height }
    const blocker = (): Box | undefined => occupied.find((o) => overlaps(box, o))

    // Vier plekken rond de knoop, in de volgorde waarin een lezer ze zoekt:
    // eronder, erboven, rechts, links. De eerste die vrij ligt én helemaal in
    // beeld valt wint; een naam die aan de rand van het doek wordt afgesneden
    // leest net zo min als een naam over een schijf.
    const spots: readonly (readonly [number, number])[] = [
      [cx - width / 2, below],
      [cx - width / 2, cy - r - 2 - height],
      [cx + r + 6, cy - height / 2],
      [cx - r - 6 - width, cy - height / 2],
    ]
    const inFrame = (): boolean =>
      box.x >= 0 && box.x + box.w <= scene.width && box.y >= 0 && box.y + box.h <= scene.height
    let free = false
    for (const [x, y] of spots) {
      box.x = x
      box.y = y
      if (inFrame() && !blocker()) {
        free = true
        break
      }
    }

    if (!free) {
      // Nergens naast de knoop plek: dan zakt het label omlaag tot het vrij
      // ligt, met zijn x zo dicht mogelijk onder de knoop maar altijd binnen
      // het doek. In- en uitstapvoorwaarde zijn dezelfde vraag — anders schuift
      // een label door nadat het al vrij lag, tot acht sprongen van zijn knoop.
      box.x = Math.max(0, Math.min(cx - width / 2, scene.width - width))
      box.y = below
      const floor = below + MAX_DRIFT
      let hit = blocker()
      while (hit && box.y <= floor) {
        box.y = hit.y + hit.h + 3
        hit = blocker()
      }
      // Te ver weggezakt, buiten beeld, of nog altijd bezet: dan geen naam. Dat
      // geldt ook voor een afgedwongen label — over een knoop heen printen is
      // nooit de uitweg.
      // Een laatste sprong die vóórbij het plafond landt telt ook niet mee.
      if (hit || box.y > floor || !inFrame()) continue
    }

    occupied.push(box)
    const tx = box.x + box.w / 2

    // Een plaatje onder de tekst in plaats van een randje eromheen: op een
    // gedeeld scherm is een rustig vlak beter leesbaar dan een gloed, en het
    // werkt in licht en donker even goed omdat het de papierkleur gebruikt.
    ctx.globalAlpha = force ? 0.94 : 0.8
    ctx.fillStyle = palette.canvas
    roundedRect(ctx, box.x, box.y, box.w, box.h, 5)
    ctx.fill()
    ctx.globalAlpha = 1
    ctx.fillStyle = force ? palette.ink : palette.dim
    for (let i = 0; i < lines.length; i++) ctx.fillText(lines[i], tx, box.y + 3 + i * LINE_HEIGHT)
  }
  ctx.textBaseline = 'alphabetic'
}

function rank(node: SceneNode): number {
  return (node.selected ? 1000 : forcedLabel(node) ? 100 : 0) + LABEL_RANK[node.type]
}
