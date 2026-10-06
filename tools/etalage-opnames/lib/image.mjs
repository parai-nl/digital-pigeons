// Beeldbewerking voor de kennisgraafbeelden (graph-shots.mjs).
import { execFileSync } from 'node:child_process'

// contentBox: het kleinste kader om alles wat van de achtergrond van het
// canvas afwijkt, plus `pad` pixels rand, binnen het beeld. De achtergrond is
// de kleur van de linkerbovenhoek (het canvas is daar leeg: de zoomknoppen
// staan rechtsboven en zijn verborgen).
export function contentBox(png, width, height, pad, threshold) {
  const raw = execFileSync('ffmpeg', ['-v', 'error', '-i', png, '-f', 'rawvideo', '-pix_fmt', 'rgb24', '-'], { maxBuffer: 512 * 1024 * 1024 })
  const at = (x, y) => (y * width + x) * 3
  const bg = [raw[0], raw[1], raw[2]]
  let left = width, right = -1, top = height, bottom = -1
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      const i = at(x, y)
      if (Math.abs(raw[i] - bg[0]) + Math.abs(raw[i + 1] - bg[1]) + Math.abs(raw[i + 2] - bg[2]) <= threshold) continue
      if (x < left) left = x
      if (x > right) right = x
      if (y < top) top = y
      if (y > bottom) bottom = y
    }
  }
  if (right < 0) return { x: 0, y: 0, width, height }
  const x = Math.max(0, left - pad)
  const y = Math.max(0, top - pad)
  return { x, y, width: Math.min(width, right + pad + 1) - x, height: Math.min(height, bottom + pad + 1) - y }
}
