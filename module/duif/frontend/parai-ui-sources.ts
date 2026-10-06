import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs'
import path from 'node:path'

// Welke @parai/ui-bestanden deze frontend werkelijk laadt: elke import naar
// @parai/ui in src/ (tests niet, die gaan niet mee in de bundle), en daarna wat
// die bestanden op hun beurt binnen parai-ui importeren. Tailwind scant alleen
// die bestanden. De hele componentmap scannen gaf utilities voor ruim honderd
// componenten die de duif nooit toont, en die CSS zat in de eerste keten (ruim
// 41 KB; zie het budget in frontend_bundle_test.go). De lijst komt uit de
// checkout zelf, dus een andere parai-ui-checkout brengt haar eigen
// importketen mee. Een import via de barrel '@parai/ui' trekt alles
// mee wat die exporteert: dan scant Tailwind weer (bijna) alles, nooit te weinig.
const IMPORT = /(?:\bfrom|\bimport)\s*\(?\s*['"]([^'"]+)['"]/g
const UI_ENTRIES: Record<string, string> = {
  '@parai/ui': 'index.ts',
  '@parai/ui/layouts': 'layouts/index.ts',
  '@parai/ui/patterns': 'patterns/index.ts',
}
const USED_TOKEN = '__PARAI_UI_USED__'
const USED_LINE = `@source "${USED_TOKEN}";`

function resolveUiImport(ui: string, spec: string, from: string): string | null {
  let base: string
  if (spec in UI_ENTRIES) base = path.join(ui, UI_ENTRIES[spec])
  else if (spec.startsWith('@parai/ui/')) base = path.join(ui, spec.slice('@parai/ui/'.length))
  else if (spec.startsWith('.') && from.startsWith(ui + path.sep)) base = path.resolve(path.dirname(from), spec)
  else return null
  for (const file of [base, `${base}.ts`, `${base}.svelte`, path.join(base, 'index.ts')]) {
    if (existsSync(file) && statSync(file).isFile()) return file
  }
  return null
}

export function usedParaiUiFiles(ui: string, srcDir: string): string[] {
  const used = new Set<string>()
  const follow = (file: string): void => {
    for (const [, spec] of readFileSync(file, 'utf8').matchAll(IMPORT)) {
      const target = resolveUiImport(ui, spec, file)
      if (!target || used.has(target)) continue
      used.add(target)
      follow(target)
    }
  }
  const walk = (dir: string): void => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const file = path.join(dir, entry.name)
      if (entry.isDirectory()) walk(file)
      else if (/\.(svelte|ts)$/.test(entry.name) && !entry.name.endsWith('.test.ts')) follow(file)
    }
  }
  walk(srcDir)
  return [...used].sort()
}

// expandParaiUiTokens vult de twee tokens van app.css in: __PARAI_UI_DIR__ met
// het pad van de checkout, en de regel @source "__PARAI_UI_USED__"; met één
// @source per gebruikt bestand. Dat tweede mag nooit stil mislukken: blijft het
// token staan (een andere schrijfwijze) of is de lijst leeg, dan scant Tailwind
// geen enkel parai-ui-bestand, vallen al hun utilities weg en wordt de bundel
// alleen maar kleiner — het budget zou het belonen. Dan stopt de build hier.
export function expandParaiUiTokens(code: string, ui: string, used: () => string[]): string {
  let out = code
  if (out.includes(USED_LINE)) {
    const files = used()
    if (files.length === 0) {
      throw new Error(`${USED_TOKEN}: geen enkel @parai/ui-bestand gevonden dat src/ importeert; Tailwind zou geen parai-ui-klassen meer maken`)
    }
    out = out.replace(USED_LINE, files.map((file) => `@source "${file.replaceAll('\\', '/')}";`).join('\n'))
  }
  if (out.includes(USED_TOKEN)) {
    throw new Error(`${USED_TOKEN} niet ingevuld: schrijf de regel precies als ${USED_LINE}`)
  }
  return out.replaceAll('__PARAI_UI_DIR__', ui.replaceAll('\\', '/'))
}
