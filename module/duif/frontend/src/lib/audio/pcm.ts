// Zuivere signaalbewerking voor de microfoonkant. Geen browser-API's,
// geen toestand buiten de doorgegeven objecten: dit bestand is met een paar
// getallen te toetsen.

// De STT-brug wil PCM16 op 16 kHz mono; één blok is 4096 monsters, oftewel
// 256 ms.
export const TARGET_RATE = 16000
export const BLOCK_SAMPLES = 4096
export const BLOCK_MS = Math.round((BLOCK_SAMPLES / TARGET_RATE) * 1000)

// floatToInt16 zet −1…1 om naar PCM16 en knipt af op de randen. De asymmetrie
// (0x8000 negatief, 0x7fff positief) is de juiste: −1 is −32768, 1 is 32767.
export function floatToInt16(samples: Float32Array, out?: Int16Array): Int16Array {
  const pcm = out ?? new Int16Array(samples.length)
  for (let i = 0; i < samples.length; i++) {
    const v = samples[i] < -1 ? -1 : samples[i] > 1 ? 1 : samples[i]
    pcm[i] = v < 0 ? Math.round(v * 0x8000) : Math.round(v * 0x7fff)
  }
  return pcm
}

// Decimator brengt de sample rate van de browsercontext terug naar 16 kHz.
//
// Waarom niet gewoon een 16 kHz-context vragen (wat een eerdere versie deed):
// Firefox vóór 148 weigert dat op een 48 kHz-stroom, en 44,1 kHz geeft een
// gebroken verhouding (2,75625). Daarom accepteren we de contextrate en
// bemonsteren we zelf, met een driepuntsgemiddelde als anti-alias-filter:
// simpel, goedkoop, en genoeg voor spraak die tóch al door een STT-model gaat.
//
// De toestand (de gebroken leespositie en de laatste twee monsters van het
// vorige blok) blijft in het object staan, zodat blokgrenzen geen klik of
// sprong geven.
export class Decimator {
  readonly ratio: number
  private readonly antialias: boolean
  private pos = 0
  private h1 = 0
  private h2 = 0

  constructor(inputRate: number, targetRate: number = TARGET_RATE) {
    const rate = inputRate > 0 ? inputRate : targetRate
    this.ratio = rate / targetRate
    // Bij gelijke of lagere invoerrate is er niets weg te filteren; dan zou het
    // gemiddelde alleen maar hoge tonen kosten.
    this.antialias = this.ratio > 1
  }

  reset(): void {
    this.pos = 0
    this.h1 = 0
    this.h2 = 0
  }

  process(block: Float32Array): Float32Array {
    const n = block.length
    if (n === 0) return new Float32Array(0)
    // Bovengrens op het aantal uitvoermonsters; de echte lengte volgt uit de lus.
    const out = new Float32Array(Math.ceil(n / this.ratio) + 1)
    let count = 0
    while (this.pos < n) {
      const i = Math.floor(this.pos)
      out[count++] = this.antialias ? this.average(block, i) : block[i]
      this.pos += this.ratio
    }
    this.pos -= n
    this.h1 = block[n - 1]
    this.h2 = n > 1 ? block[n - 2] : this.h1
    return out.subarray(0, count)
  }

  // average is het driepuntsgemiddelde over monster i en zijn twee voorgangers;
  // vóór de blokgrens komen die uit het vorige blok.
  private average(block: Float32Array, i: number): number {
    const s1 = i >= 1 ? block[i - 1] : this.h1
    const s2 = i >= 2 ? block[i - 2] : i === 1 ? this.h1 : this.h2
    return (block[i] + s1 + s2) / 3
  }
}
