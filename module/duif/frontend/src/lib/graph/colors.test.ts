import { describe, expect, it } from 'vitest'
import { familyColor, FAMILY_COUNT, familyHues, HUE_GAP, makePalette, parseHue } from './colors'
import { FAKE_THEME } from '../testing/graph'

describe('parseHue', () => {
  it('reads the hue from the colour forms a browser hands back', () => {
    expect(parseHue('oklch(0.62 0.14 152)')).toBeCloseTo(152, 5)
    expect(parseHue('oklch(62% 0.14 152deg / 0.5)')).toBeCloseTo(152, 5)
    expect(parseHue('hsl(200, 60%, 50%)')).toBeCloseTo(200, 5)
    expect(parseHue('rgb(255, 0, 0)')).toBeCloseTo(0, 5)
    expect(parseHue('rgb(0, 255, 0)')).toBeCloseTo(120, 5)
    expect(parseHue('#0000ff')).toBeCloseTo(240, 5)
    expect(parseHue('#00f')).toBeCloseTo(240, 5)
  })

  it('has no opinion when there is no hue to read', () => {
    expect(parseHue('')).toBeNull()
    expect(parseHue('transparent')).toBeNull()
    expect(parseHue('lab(50% 0 0)')).toBeNull()
    // Een grijs heeft geen betekenisvolle tint — in welke schrijfwijze ook.
    expect(parseHue('oklch(0.5 0 0)')).toBeNull()
    expect(parseHue('rgb(128, 128, 128)')).toBeNull()
    expect(parseHue('rgba(0, 0, 0, 0)')).toBeNull()
    expect(parseHue('#777')).toBeNull()
    expect(parseHue('#ffffff')).toBeNull()
  })
})

describe('familyHues', () => {
  it('keeps every family away from the reserved hues', () => {
    const hues = familyHues([160, 25])
    expect(hues).toHaveLength(FAMILY_COUNT)
    for (const hue of hues) {
      for (const reserved of [160, 25]) {
        const distance = Math.min(Math.abs(hue - reserved), 360 - Math.abs(hue - reserved))
        expect(distance, `hue ${hue} vs ${reserved}`).toBeGreaterThanOrEqual(HUE_GAP - 0.001)
      }
    }
  })

  it('spreads them out and keeps them distinct', () => {
    const hues = familyHues([160, 25])
    expect(new Set(hues.map((h) => h.toFixed(1))).size).toBe(FAMILY_COUNT)
  })

  it('handles a reserved band that wraps past zero', () => {
    const hues = familyHues([5])
    for (const hue of hues) {
      const distance = Math.min(Math.abs(hue - 5), 360 - Math.abs(hue - 5))
      expect(distance).toBeGreaterThanOrEqual(HUE_GAP - 0.001)
    }
  })

  it('falls back to the full circle when nothing is left', () => {
    const hues = familyHues([0, 90, 180, 270], 4, 180)
    expect(hues).toHaveLength(4)
  })
})

describe('familyColor', () => {
  it('gets lighter and paler the deeper it sits', () => {
    const shallow = familyColor(120, 0, false)
    const deep = familyColor(120, 3, false)
    const lightness = (css: string) => Number.parseFloat(css.slice('oklch('.length))
    const chroma = (css: string) => Number.parseFloat(css.split(' ')[1])
    expect(lightness(deep)).toBeGreaterThan(lightness(shallow))
    expect(chroma(deep)).toBeLessThan(chroma(shallow))
    // Potloodtinten: nooit verzadigd — die kleur is van het duifoog.
    expect(chroma(shallow)).toBeLessThan(0.1)
  })
})

describe('makePalette', () => {
  it('passes the host colours through and gives structure a neutral tint', () => {
    const palette = makePalette(FAKE_THEME)
    expect(palette.ink).toBe(FAKE_THEME.ink)
    expect(palette.halo).toBe(FAKE_THEME.halo)
    expect(palette.hues).toHaveLength(FAMILY_COUNT)
    expect(palette.fill(-1, 0)).toContain('0.004')
    expect(palette.fill(0, 0)).toBe(familyColor(palette.hues[0], 0, false))
    // Een familie-index buiten de tabel loopt rond in plaats van te breken.
    expect(palette.fill(FAMILY_COUNT + 1, 0)).toBe(palette.fill(1, 0))
  })

  it('uses darker, deeper tints on a dark host', () => {
    const light = makePalette(FAKE_THEME).fill(0, 0)
    const dark = makePalette({ ...FAKE_THEME, dark: true }).fill(0, 0)
    expect(dark).not.toBe(light)
  })

  // Het icoontje ín een knoop is dertien pixels op de plattegrond en in de
  // boom. Blijft het contrast met zijn eigen vulling onder de drie op één, dan
  // is het een vlek. Deze grens geldt voor elke familie, elke diepte en beide
  // thema's van de gastheer — dus wordt hij hier over de hele tabel nagerekend
  // in plaats van op één steekproef.
  it('keeps every glyph readable on the fill it sits on, light and dark', () => {
    for (const dark of [false, true]) {
      const palette = makePalette({ ...FAKE_THEME, dark })
      for (let family = -1; family < FAMILY_COUNT; family++) {
        for (let depth = 0; depth <= 5; depth++) {
          const found = contrast(palette.fill(family, depth), palette.glyph(family))
          expect(found, `familie ${family}, diepte ${depth}, ${dark ? 'donker' : 'licht'}`).toBeGreaterThanOrEqual(
            CONTRAST_MIN,
          )
        }
      }
    }
  })
})

// De ondergrens uit WCAG voor grafische onderdelen (1.4.11): een icoontje dat
// betekenis draagt moet drie op één halen tegen wat eromheen ligt.
const CONTRAST_MIN = 3

// De rekenweg van oklch naar een contrastverhouding: oklab → lineair sRGB →
// relatieve helderheid → de verhouding uit WCAG. De browser doet dit zelf, maar
// een test kan hem daar niet naar vragen — en een grens die niemand narekent is
// geen grens.
function contrast(a: string, b: string): number {
  const [high, low] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (high + 0.05) / (low + 0.05)
}

function luminance(color: string): number {
  const [l, chroma, hue] = readOklch(color)
  const rad = (hue * Math.PI) / 180
  const a = chroma * Math.cos(rad)
  const b = chroma * Math.sin(rad)
  const long = (l + 0.3963377774 * a + 0.2158037573 * b) ** 3
  const medium = (l - 0.1055613458 * a - 0.0638541728 * b) ** 3
  const short = (l - 0.0894841775 * a - 1.291485548 * b) ** 3
  const channels = [
    4.0767416621 * long - 3.3077115913 * medium + 0.2309699292 * short,
    -1.2684380046 * long + 2.6097574011 * medium - 0.3413193965 * short,
    -0.0041960863 * long - 0.7034186147 * medium + 1.707614701 * short,
  ].map((v) => Math.min(1, Math.max(0, v)))
  return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2]
}

function readOklch(color: string): [number, number, number] {
  const parts = /^oklch\(([\d.]+) ([\d.]+) ([\d.]+)\)$/.exec(color)
  if (!parts) throw new Error('geen oklch-kleur: ' + color)
  return [Number(parts[1]), Number(parts[2]), Number(parts[3])]
}
