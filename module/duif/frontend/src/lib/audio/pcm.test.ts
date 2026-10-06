import { describe, expect, it } from 'vitest'
import { BLOCK_SAMPLES, Decimator, floatToInt16, TARGET_RATE } from './pcm'

const fill = (n: number, f: (i: number) => number) => Float32Array.from({ length: n }, (_, i) => f(i))

describe('floatToInt16', () => {
  it('maps the full scale and clips beyond it', () => {
    const pcm = floatToInt16(Float32Array.from([-2, -1, 0, 0.5, 1, 2]))
    expect(Array.from(pcm)).toEqual([-32768, -32768, 0, 16384, 32767, 32767])
  })
})

describe('Decimator', () => {
  it('brings 48 kHz down to 16 kHz and carries the remainder across blocks', () => {
    const dec = new Decimator(48000)
    expect(dec.ratio).toBe(3)
    let total = 0
    for (let b = 0; b < 4; b++) total += dec.process(new Float32Array(8192)).length
    // Vier blokken van 8192 monsters zijn 32768 monsters; op een derde daarvan
    // mag hooguit één monster afwijking staan (de rest over de blokgrens).
    expect(Math.abs(total - 32768 / 3)).toBeLessThanOrEqual(1)
  })

  it('handles the fractional ratio of 44,1 kHz', () => {
    const dec = new Decimator(44100)
    expect(dec.ratio).toBeCloseTo(44100 / TARGET_RATE)
    let total = 0
    for (let b = 0; b < 8; b++) total += dec.process(new Float32Array(8192)).length
    expect(Math.abs(total - (8 * 8192) / dec.ratio)).toBeLessThanOrEqual(1)
  })

  it('leaves a constant signal constant', () => {
    const dec = new Decimator(48000)
    dec.process(fill(8192, () => 0.25))
    const out = dec.process(fill(8192, () => 0.25))
    for (const v of out) expect(v).toBeCloseTo(0.25, 6)
  })

  it('keeps a 1 kHz sine continuous across the block boundary', () => {
    const dec = new Decimator(48000)
    const sine = (i: number) => Math.sin((2 * Math.PI * 1000 * i) / 48000)
    const first = dec.process(fill(8192, sine))
    const second = dec.process(fill(8192, (i) => sine(i + 8192)))
    const seam = Math.abs(second[0] - first[first.length - 1])
    // Eén stap van 16 kHz op een sinus van 1 kHz is hooguit ~0,4; een sprong
    // zou daar ver overheen gaan.
    expect(seam).toBeLessThan(0.5)
  })

  it('passes a 16 kHz stream through untouched', () => {
    const dec = new Decimator(TARGET_RATE)
    const input = fill(BLOCK_SAMPLES, (i) => (i % 2 === 0 ? 0.5 : -0.5))
    expect(Array.from(dec.process(input))).toEqual(Array.from(input))
  })
})
