export function normalizeHue(hue: number): number {
  const h = hue % 360
  return h < 0 ? h + 360 : h
}


// Een grijs (r == g == b) heeft geen tint: 0° teruggeven zou een neutrale
// gastheerkleur als "rood" laten gelden en de 26 familietinten daar onnodig van
// wegduwen. Geen tint is null, en de aanroeper valt terug op zijn eigen waarde.
function hueFromRgb(r: number, g: number, b: number): number | null {
  const max = Math.max(r, g, b)
  const min = Math.min(r, g, b)
  const span = max - min
  if (span === 0) return null
  let hue: number
  if (max === r) hue = ((g - b) / span) % 6
  else if (max === g) hue = (b - r) / span + 2
  else hue = (r - g) / span + 4
  return normalizeHue(hue * 60)
}

// parseHue leest de tint uit een opgeloste kleur. Welke vorm de browser
// teruggeeft verschilt (oklch blijft oklch, een hex wordt rgb), dus alle
// gangbare vormen komen langs; wat niet te lezen is, is geen tint (null) —
// de aanroeper valt dan terug op zijn eigen waarde in plaats van te gokken.
export function parseHue(css: string): number | null {
  const text = css.trim().toLowerCase()
  if (text === '') return null

  const hex = /^#([0-9a-f]{3,8})$/.exec(text)
  if (hex) {
    const digits = hex[1]
    const short = digits.length === 3 || digits.length === 4
    const size = short ? 1 : 2
    const at = (i: number) => {
      const part = digits.slice(i * size, i * size + size)
      const value = Number.parseInt(short ? part + part : part, 16)
      return Number.isNaN(value) ? 0 : value / 255
    }
    if (digits.length === 3 || digits.length === 4 || digits.length === 6 || digits.length === 8) {
      return hueFromRgb(at(0), at(1), at(2))
    }
    return null
  }

  const call = /^([a-z]+)\(([^)]*)\)$/.exec(text)
  if (!call) return null
  const name = call[1]
  const parts = call[2]
    .replaceAll('/', ' ')
    .replaceAll(',', ' ')
    .split(/\s+/)
    .filter((p) => p !== '')
  const num = (index: number): number | null => {
    const raw = parts[index]
    if (raw === undefined) return null
    const value = Number.parseFloat(raw.replace(/(deg|%|grad|rad|turn)$/, ''))
    if (Number.isNaN(value)) return null
    if (raw.endsWith('turn')) return value * 360
    if (raw.endsWith('rad')) return (value * 180) / Math.PI
    return value
  }

  if (name === 'rgb' || name === 'rgba') {
    const r = num(0)
    const g = num(1)
    const b = num(2)
    if (r === null || g === null || b === null) return null
    const scale = (v: number, raw: string) => (raw.endsWith('%') ? v / 100 : v / 255)
    return hueFromRgb(scale(r, parts[0]), scale(g, parts[1]), scale(b, parts[2]))
  }
  if (name === 'hsl' || name === 'hsla') {
    const h = num(0)
    return h === null ? null : normalizeHue(h)
  }
  if (name === 'oklch' || name === 'lch') {
    const chroma = num(1)
    const h = num(2)
    if (h === null) return null
    // Een grijs (chroma 0) heeft geen betekenisvolle tint; die zou de
    // familiekleuren onnodig van 0° wegduwen.
    if (chroma !== null && chroma <= 0) return null
    return normalizeHue(h)
  }
  return null
}
