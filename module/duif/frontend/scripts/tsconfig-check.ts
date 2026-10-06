// Schrijft tsconfig.check.json (gitignored): tsconfig.json met de paden naar
// @parai/ui en @parai/graph uit dezelfde bron als de Vite-build
// (private-deps.ts, dus PARAI_UI_DIR en PARAI_GRAPH_DIR). tsconfig.json houdt
// de sibling-paden voor editors; `bun run check` typeert via dit bestand, zodat
// svelte-check ook in een checkout zonder siblings dezelfde
// checkouts ziet als de build. Draai met dezelfde Bun-runtime als de build.
import { writeFileSync } from 'node:fs'
import path from 'node:path'
import { paraiGraphSrc, paraiUiDir } from '../private-deps'

const frontend = path.resolve(import.meta.dirname, '..')
const ui = paraiUiDir()
const graphSrc = paraiGraphSrc()

// `paths` uit een extends-bestand vervangt die van de basis in zijn geheel,
// dus de svelte-regels uit tsconfig.json gaan hier mee.
const config = {
  extends: './tsconfig.json',
  compilerOptions: {
    paths: {
      svelte: ['./node_modules/svelte'],
      'svelte/*': ['./node_modules/svelte/*'],
      '@parai/ui': [path.join(ui, 'index.ts')],
      '@parai/ui/layouts': [path.join(ui, 'layouts/index.ts')],
      '@parai/ui/patterns': [path.join(ui, 'patterns/index.ts')],
      '@parai/ui/*': [path.join(ui, '*')],
      '@parai/graph/*': [path.join(graphSrc, '*')],
    },
  },
}
writeFileSync(path.join(frontend, 'tsconfig.check.json'), `${JSON.stringify(config, null, 2)}\n`)
