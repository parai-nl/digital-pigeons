import { existsSync } from 'node:fs'
import path from 'node:path'

// Waar de private Parai-checkouts staan. Eén bron voor de Vite-build (en dus
// vitest) én voor de typecheck (scripts/tsconfig-check.ts), zodat svelte-check
// nooit een andere checkout typeert dan de build bundelt.

// @parai/ui is de private componentbibliotheek van Parai. Zij wordt niet
// gekopieerd maar via een alias uit de sibling-checkout geladen (zelfde
// constructie als de Go-import van parai-core: importeren mag, kopiëren niet).
// Het pad is overridebaar met PARAI_UI_DIR; default is de sibling naast deze
// repo. Ontbreekt de checkout, dan stopt de build hier met één duidelijke regel.
export function paraiUiDir(): string {
  const ui = path.resolve(process.env.PARAI_UI_DIR ?? path.resolve(import.meta.dirname, '../../../../parai-ui'))
  if (!existsSync(path.join(ui, 'index.ts'))) {
    throw new Error(
      `@parai/ui niet gevonden op ${ui}. Zet PARAI_UI_DIR naar een parai-ui-checkout of clone hem naast deze repo.`,
    )
  }
  return ui
}

// parai-graph is de private graafcomponent van Parai. Dezelfde regel als bij
// @parai/ui: importeren mag, kopiëren niet. `@parai/graph/*` wijst naar de
// bronmap van de graafcomponent; overridebaar met PARAI_GRAPH_DIR, default de
// sibling naast deze repo. Geeft de bronmap terug, niet de repo-root.
export function paraiGraphSrc(): string {
  const graph = path.resolve(process.env.PARAI_GRAPH_DIR ?? path.resolve(import.meta.dirname, '../../../../parai-graph'))
  const src = path.join(graph, 'module/graph/frontend/src')
  if (!existsSync(path.join(src, 'lib/viewport.ts'))) {
    throw new Error(
      `parai-graph niet gevonden op ${graph}. Zet PARAI_GRAPH_DIR naar een parai-graph-checkout of clone hem naast deze repo.`,
    )
  }
  return src
}
