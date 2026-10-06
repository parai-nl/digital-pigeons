import { drawIcon } from '@parai/graph/lib/icons'
import type { Fetcher } from '../api'
// parseHue ontleedt de opgeloste tint van --primary/--destructive; de vorm
// hangt van de browser af (oklch blijft oklch, een hex wordt rgb).
import { parseHue } from './hue'

// Ports van de kennisgraaf: de enige plek waar browser-API's de graaf
// binnenkomen. De store, de layout en de tekenaar noemen nergens `window`,
// `document` of `requestAnimationFrame`; browserGraphPorts() doet dat één keer,
// en lib/testing/graph.ts levert de fake. Zo is de hele graaflogica in jsdom te
// toetsen zonder één globale te patchen.

// CanvasLike is precies het stuk 2D-context dat de tekenaar gebruikt. Pick<>
// (in plaats van een eigen interface) garandeert dat een echte
// CanvasRenderingContext2D er zonder cast in past.
export type CanvasLike = Pick<
  CanvasRenderingContext2D,
  | 'save'
  | 'restore'
  | 'setTransform'
  | 'translate'
  | 'scale'
  | 'clearRect'
  | 'fillRect'
  | 'beginPath'
  | 'closePath'
  | 'moveTo'
  | 'lineTo'
  | 'arc'
  | 'quadraticCurveTo'
  | 'fill'
  | 'stroke'
  | 'fillText'
  | 'fillStyle'
  | 'strokeStyle'
  | 'lineWidth'
  | 'globalAlpha'
  | 'font'
  | 'textAlign'
  | 'textBaseline'
>

// GraphTheme zijn de kleuren van de gastheer, opgelost tot echte kleurtekst:
// een canvas kent geen `var(--foreground)`. De hues van --primary en
// --destructive zijn er zodat de thema-familiekleuren er afstand van kunnen
// houden (verzadigde kleur blijft van het duifoog en de stopknop).
export interface GraphTheme {
  dark: boolean
  ink: string
  dim: string
  canvas: string
  halo: string
  edge: string
  primaryHue: number
  destructiveHue: number
}

export interface GraphPorts {
  fetch: Fetcher
  // frame plant één tekenbeurt en levert de annulering; de lus zelf staat in
  // de component, zodat er nooit een rAF-token op de store blijft hangen.
  frame(cb: () => void): () => void
  // observeSize meldt de nieuwe maat van het element; de component past hem
  // pas in de tekenlus toe (nooit synchroon vanuit de waarnemer).
  observeSize(el: Element, cb: (width: number, height: number) => void): () => void
  pixelRatio(): number
  reducedMotion(): boolean
  // onVisible meldt of de pagina zichtbaar is; een onzichtbare tab hoort geen
  // simulatie te draaien.
  onVisible(cb: (visible: boolean) => void): () => void
  readTheme(el: Element): GraphTheme
  context(canvas: HTMLCanvasElement): CanvasLike | null
  // drawGlyph tekent één icoontje van de kaart op een klein eigen doekje, zodat
  // de boom en het leespaneel hetzelfde teken kunnen dragen als de knoop op de
  // plattegrond. Kan er niet getekend worden (jsdom, een browser zonder
  // Path2D), dan gebeurt er niets: het doekje blijft leeg en de naam ernaast
  // draagt de betekenis, zoals altijd al.
  drawGlyph(canvas: HTMLCanvasElement, icon: string, ink: string, size: number): void
}

// De terugvalkleuren gelden alleen als de gastheer geen tokens levert (of in
// een omgeving zonder opgemaakte stylesheet); ze zijn neutraal grijs, zodat een
// ontbrekend token opvalt in plaats van toevallig goed te staan.
const FALLBACK: GraphTheme = {
  dark: false,
  ink: '#1c1c1c',
  dim: '#6b6b6b',
  canvas: '#f6f5f2',
  halo: '#2f6f5e',
  edge: '#b9b6b0',
  primaryHue: 160,
  destructiveHue: 25,
}

// resolveColor laat de browser het rekenwerk doen: een sondespan krijgt de
// tokenwaarde als `color` — een echte kleur-eigenschap, dus de berekende waarde
// is een opgeloste kleur en geen color-mix()-tekst die een canvas niet kent.
//
// De terugvalkleur gaat mee ín de var(): een ontbrekend token maakt de
// declaratie pas bij het berekenen ongeldig, en dan erft de sonde de kleur van
// zijn ouder in plaats van leeg te blijven — de JS-terugval hieronder zou dan
// nooit afgaan en de graaf tekende met de tekstkleur van de gastheer.
function resolveToken(probe: HTMLElement, token: string, fallback: string): string {
  probe.style.color = ''
  probe.style.color = `var(${token}, ${fallback})`
  const value = getComputedStyle(probe).color
  // Een omgeving die var() niet oplost (jsdom) geeft de tekst onveranderd
  // terug; een canvas kan daar niets mee, dus dan alsnog de eigen terugval.
  return value !== '' && !value.includes('var(') ? value : fallback
}

// De schermdichtheid, met een plafond: boven de drie is elke extra pixel
// rekenwerk zonder zichtbaar verschil. Hij staat los van de port zodat zowel
// pixelRatio() als drawGlyph() dezelfde waarde gebruiken.
function devicePixelRatio(): number {
  const dpr = globalThis.devicePixelRatio
  return typeof dpr === 'number' && dpr > 0 ? Math.min(dpr, 3) : 1
}

export function browserGraphPorts(fetcher: Fetcher): GraphPorts {
  return {
    fetch: fetcher,
    frame(cb) {
      const id = requestAnimationFrame(() => cb())
      return () => cancelAnimationFrame(id)
    },
    observeSize(el, cb) {
      const observer = new ResizeObserver((entries) => {
        const box = entries[0]?.contentRect
        if (box) cb(box.width, box.height)
      })
      observer.observe(el)
      return () => observer.disconnect()
    },
    pixelRatio: devicePixelRatio,
    reducedMotion() {
      return globalThis.matchMedia?.('(prefers-reduced-motion: reduce)').matches === true
    },
    onVisible(cb) {
      const handler = () => cb(document.visibilityState !== 'hidden')
      document.addEventListener('visibilitychange', handler)
      return () => document.removeEventListener('visibilitychange', handler)
    },
    readTheme(el) {
      const probe = document.createElement('span')
      probe.setAttribute('aria-hidden', 'true')
      probe.style.position = 'absolute'
      probe.style.width = '0'
      probe.style.height = '0'
      el.appendChild(probe)
      try {
        const theme: GraphTheme = {
          // .dark hangt bij core op een voorouder, niet op onze wortel.
          dark: !!el.closest('.dark'),
          ink: resolveToken(probe, '--duif-graph-ink', FALLBACK.ink),
          dim: resolveToken(probe, '--duif-graph-dim', FALLBACK.dim),
          canvas: resolveToken(probe, '--duif-graph-canvas', FALLBACK.canvas),
          halo: resolveToken(probe, '--duif-graph-halo', FALLBACK.halo),
          edge: resolveToken(probe, '--duif-graph-edge', FALLBACK.edge),
          // Een gastheer zonder --primary levert geen tint op; dan blijft de
          // eigen terugvaltint staan in plaats van een gegokte 0°.
          primaryHue: parseHue(resolveToken(probe, '--primary', 'transparent')) ?? FALLBACK.primaryHue,
          destructiveHue: parseHue(resolveToken(probe, '--destructive', 'transparent')) ?? FALLBACK.destructiveHue,
        }
        return theme
      } finally {
        probe.remove()
      }
    },
    context(canvas) {
      return canvas.getContext('2d')
    },
    drawGlyph(canvas, icon, ink, size) {
      // Path2D is er in de browser en niet in jsdom; zonder Path2D tekent
      // drawIcon niets.
      if (typeof Path2D !== 'function') return
      const dpr = devicePixelRatio()
      canvas.width = Math.max(1, Math.round(size * dpr))
      canvas.height = Math.max(1, Math.round(size * dpr))
      const ctx = canvas.getContext('2d')
      if (!ctx) return
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      ctx.clearRect(0, 0, size, size)
      drawIcon(ctx, icon, size / 2, size / 2, size, ink)
    },
  }
}
