import type { GraphTheme } from './ports'

// De kleurleer van de plattegrond. Twee regels uit de ontwerprichting:
//  1. De 26 thema-families zijn potloodtinten: lage chroma, en dieper in de
//     boom lichter — een tekening, geen dashboard.
//  2. Verzadigde kleur blijft van het duifoog (--primary) en de stopknop
//     (--destructive). Familietinten houden daarom afstand van die twee hues,
//     zodat de selectie-halo de enige kleur is die echt roept.
// Alles hier is puur: kleuren in, kleurtekst uit. Geen DOM, geen tokens —
// die komen opgelost via GraphPorts.readTheme().

import { FAMILY_COUNT } from './types'
import { normalizeHue as norm } from './hue'
export { FAMILY_COUNT } from './types'
export { parseHue } from './hue'

// De minimale hoekafstand (in graden) tot een gereserveerde tint.
export const HUE_GAP = 20


function clamp(value: number, min: number, max: number): number {
  return value < min ? min : value > max ? max : value
}


type Range = [number, number]

// allowedArcs is de cirkel min de verboden banden rond de gereserveerde tinten.
function allowedArcs(avoid: readonly number[], gap: number): Range[] {
  const blocked: Range[] = []
  for (const raw of avoid) {
    if (!Number.isFinite(raw)) continue
    const center = norm(raw)
    const from = center - gap
    const to = center + gap
    if (from < 0) {
      blocked.push([0, to], [norm(from), 360])
    } else if (to > 360) {
      blocked.push([from, 360], [0, to - 360])
    } else {
      blocked.push([from, to])
    }
  }
  blocked.sort((a, b) => a[0] - b[0])
  const merged: Range[] = []
  for (const range of blocked) {
    const last = merged.at(-1)
    if (last && range[0] <= last[1]) last[1] = Math.max(last[1], range[1])
    else merged.push([range[0], range[1]])
  }
  const free: Range[] = []
  let cursor = 0
  for (const [from, to] of merged) {
    if (from > cursor) free.push([cursor, from])
    cursor = Math.max(cursor, to)
  }
  if (cursor < 360) free.push([cursor, 360])
  return free
}

// familyHues verdeelt `count` tinten gelijkmatig over wat er van de cirkel
// overblijft. Blijft er niets over (een gastheer met twee tinten die alles
// beslaan), dan verdeelt hij ze alsnog over de hele cirkel: liever een tint te
// dicht bij --primary dan 26 keer dezelfde kleur.
export function familyHues(avoid: readonly number[], count = FAMILY_COUNT, gap = HUE_GAP): number[] {
  const arcs = allowedArcs(avoid, gap)
  const total = arcs.reduce((sum, [from, to]) => sum + (to - from), 0)
  const hues: number[] = []
  if (total <= 0 || arcs.length === 0) {
    for (let i = 0; i < count; i++) hues.push(norm((i * 360) / count))
    return hues
  }
  for (let i = 0; i < count; i++) {
    let offset = ((i + 0.5) * total) / count
    for (const [from, to] of arcs) {
      const span = to - from
      if (offset <= span) {
        hues.push(norm(from + offset))
        break
      }
      offset -= span
    }
    // Afrondingsrest aan het einde van de laatste boog.
    if (hues.length === i) hues.push(norm(arcs[arcs.length - 1][1]))
  }
  return hues
}

// familyColor is één potloodtint: dieper in de boom lichter en bleker, zodat
// een uitgeklapte tak vanzelf naar de achtergrond zakt.
//
// De basistint is een slag donkerder dan in de eerste versie. De kaart wordt op
// een klasscherm gedeeld, en een thema van oklch(0.74 …) op papierwit stond daar
// nauwelijks van de achtergrond af. De chroma blijft onder de 0.1: verzadigde
// kleur is nog steeds van het duifoog, het verschil zit in helderheid.
export function familyColor(hue: number, depth: number, dark: boolean): string {
  const step = clamp(depth, 0, 5)
  const lightness = dark ? clamp(0.5 + step * 0.055, 0, 0.92) : clamp(0.68 + step * 0.05, 0, 0.97)
  const chroma = clamp(0.095 - step * 0.013, 0.008, 0.2)
  return `oklch(${lightness.toFixed(3)} ${chroma.toFixed(3)} ${norm(hue).toFixed(1)})`
}

// glyphColor is de inkt van het icoontje ín een knoop: dezelfde tint, veel
// donkerder. Eén regel voor licht en donker, want beide vullingen zijn licht
// genoeg om donkere inkt te dragen — een witte glyph zou op de bleke diepere
// tinten verdwijnen.
//
// De donkere waarde was 0.24 en haalde op de donkerste vulling (een thema op
// diepte nul) 2,7 op één: te weinig om een icoontje van dertien pixels te
// onderscheiden. Op 0.16 is het slechtste geval 3,1 op één, en dat is een
// verschuiving van de inkt alleen — de vullingen van de kaart blijven precies
// zoals ze waren. Zie CONTRAST_MIN in colors.test.ts.
export function glyphColor(hue: number, dark: boolean): string {
  return `oklch(${dark ? 0.16 : 0.3} 0.05 ${norm(hue).toFixed(1)})`
}

export interface Palette {
  readonly dark: boolean
  readonly ink: string
  readonly dim: string
  readonly canvas: string
  readonly halo: string
  readonly edge: string
  readonly hues: readonly number[]
  // fill levert de vulkleur van een knoop; family < 0 is "geen thema"
  // (domein, bron) en krijgt een neutrale, kleurloze tint.
  fill(family: number, depth: number): string
  // glyph levert de inktkleur van het icoontje op die vulling.
  glyph(family: number): string
}

export function makePalette(theme: GraphTheme): Palette {
  const hues = familyHues([theme.primaryHue, theme.destructiveHue])
  // De neutrale vulling (domein, bron — alles zonder thema) was in
  // donker 0.42 en droeg zijn eigen icoontje op 2,1 op één: het glyph viel er
  // niet uit te halen. Iets lichter grijs plus donkerder inkt brengt het
  // slechtste geval op 3,3 op één, zonder dat de kaart een andere kleur krijgt:
  // het blijft hetzelfde kleurloze grijs, een halve stap lichter.
  const neutralLight = theme.dark ? 0.5 : 0.88
  return {
    dark: theme.dark,
    ink: theme.ink,
    dim: theme.dim,
    canvas: theme.canvas,
    halo: theme.halo,
    edge: theme.edge,
    hues,
    fill(family, depth) {
      if (family < 0 || hues.length === 0) {
        const step = clamp(depth, 0, 5)
        const lightness = clamp(neutralLight + step * (theme.dark ? 0.05 : 0.02), 0, 0.97)
        return `oklch(${lightness.toFixed(3)} 0.004 90)`
      }
      return familyColor(hues[family % hues.length], depth, theme.dark)
    },
    glyph(family) {
      if (family < 0 || hues.length === 0) return theme.dark ? 'oklch(0.14 0.004 90)' : 'oklch(0.32 0.004 90)'
      return glyphColor(hues[family % hues.length], theme.dark)
    },
  }
}
