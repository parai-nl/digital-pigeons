import { existsSync, readdirSync, rmSync } from 'node:fs'
import path from 'node:path'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import tailwindcss from '@tailwindcss/vite'
import { svelteTesting } from '@testing-library/svelte/vite'
import { sveltePhosphorOptimize } from 'phosphor-svelte/vite'
import { defineConfig, type Plugin, transformWithEsbuild } from 'vite'
import cssInjectedByJs from 'vite-plugin-css-injected-by-js'
import { pigeonMediaPlugin } from './media-build'
import { expandParaiUiTokens, usedParaiUiFiles } from './parai-ui-sources'
import { paraiGraphSrc, paraiUiDir } from './private-deps'
import { threeCspPlugin } from './three-csp-build'

// @parai/ui en parai-graph zijn private Parai-checkouts: importeren, niet
// kopiëren. Waar ze staan (PARAI_UI_DIR, PARAI_GRAPH_DIR, anders de siblings)
// bepaalt private-deps.ts, dezelfde bron die de typecheck gebruikt.
const UI = paraiUiDir()
const GRAPH_SRC = paraiGraphSrc()
const PHOSPHOR = path.resolve(import.meta.dirname, 'node_modules/phosphor-svelte/lib')

// app.css kent geen env-variabelen (Tailwinds @source en @import willen een
// letterlijk pad); dit plugin-etje vult het token __PARAI_UI_DIR__ in vóórdat
// Tailwind de CSS ziet, zodat één env-variabele de hele keten stuurt. De regel
// met __PARAI_UI_USED__ wordt één @source per parai-ui-bestand dat de duif
// laadt (parai-ui-sources.ts). De devserver rekent die lijst uit wanneer hij
// app.css inleest: wie daarna een nieuwe parai-ui-component importeert,
// herstart hem.
function paraiUiDirToken(): Plugin {
  return {
    name: 'duif:parai-ui-dir',
    enforce: 'pre',
    transform(code, id) {
      if (!id.endsWith('.css') || !code.includes('__PARAI_UI_')) return null
      const used = () => usedParaiUiFiles(UI, path.resolve(import.meta.dirname, 'src'))
      return { code: expandParaiUiTokens(code, UI, used), map: null }
    },
  }
}

// Remove only generated build artefacts in this module's output directory.
// The tracked .gitkeep survives; stale hashed chunks cannot accumulate across
// builds and accidentally remain reachable through the embedded assets route.
function cleanGeneratedOutput(): Plugin {
  const output = path.resolve(import.meta.dirname, 'dist')
  return {
    name: 'duif:clean-generated-output',
    apply: 'build',
    buildStart() {
      if (!existsSync(output)) return
      for (const entry of readdirSync(output, { withFileTypes: true })) {
        if (entry.isFile() && /\.(js|css|map)$/.test(entry.name)) rmSync(path.join(output, entry.name))
      }
      rmSync(path.join(output, '.vite', 'manifest.json'), { force: true })
    },
  }
}

// Vite laat de witruimte in een es-lib-build bewust staan (minifyWhitespace is
// daar hard uitgezet, ook als build.minify aanstaat): een bibliotheek wordt
// normaal door de consument opnieuw geminificeerd. Onze consument is de
// core-shell, die de bundle onbewerkt en zonder compressie doorgeeft — dus
// minificeren wij zelf, ná het inspuiten van de CSS zodat die mee gaat.
// Identifiers blijven met opzet leesbaar noch onleesbaar afgedwongen: esbuild
// doet hier hetzelfde werk als voor een gewone app-build.
function minifyEsLibBundle(): Plugin {
  return {
    name: 'duif:minify-bundle',
    apply: 'build',
    enforce: 'post',
    async generateBundle(_options, bundle) {
      for (const [fileName, chunk] of Object.entries(bundle)) {
        if (chunk.type !== 'chunk') continue
        // transformWithEsbuild is Vite's eigen esbuild: geen extra
        // afhankelijkheid in package.json voor een stap die de build al kent.
        const out = await transformWithEsbuild(chunk.code, fileName, { minify: true, target: 'esnext', format: 'esm' })
        chunk.code = out.code
      }
    },
  }
}

export default defineConfig({
  plugins: [
    cleanGeneratedOutput(),
    threeCspPlugin(),
    pigeonMediaPlugin(import.meta.dirname),
    paraiUiDirToken(),
    sveltePhosphorOptimize(),
    tailwindcss(),
    svelte({ emitCss: true }),
    cssInjectedByJs({ useStrictCSP: true, relativeCSSInjection: true }),
    // Ná cssInjectedByJs: beide draaien in generateBundle, en wie later in deze
    // lijst staat, ziet het resultaat van de vorige.
    minifyEsLibBundle(),
    svelteTesting(),
  ],
  resolve: {
    dedupe: ['svelte', 'bits-ui'],
    alias: [
      { find: /^phosphor-svelte\/lib\/(.*)$/, replacement: path.join(PHOSPHOR, '$1.svelte') },
      { find: /^@parai\/ui$/, replacement: path.join(UI, 'index.ts') },
      { find: /^@parai\/ui\/layouts$/, replacement: path.join(UI, 'layouts/index.ts') },
      { find: /^@parai\/ui\/patterns$/, replacement: path.join(UI, 'patterns/index.ts') },
      { find: /^@parai\/ui\/(.*)$/, replacement: path.join(UI, '$1') },
      { find: /^@parai\/graph\/(.*)$/, replacement: path.join(GRAPH_SRC, '$1') },
    ],
  },
  server: {
    // De devserver mag precies twee bomen serveren: deze frontend en de
    // @parai/ui-checkout. De map erboven (waar alle repo's van de gebruiker
    // naast elkaar staan) hoort daar niet bij — een devserver is geen
    // bestandsbrowser.
    fs: { allow: [import.meta.dirname, UI, GRAPH_SRC] },
    proxy: { '/api': process.env.DUIF_API_PROXY ?? 'http://localhost:8080' },
  },
  build: {
    lib: { entry: 'src/index.ts', formats: ['es'], fileName: 'bundle' },
    outDir: 'dist',
    manifest: true,
    cssCodeSplit: true,
    emptyOutDir: false,
    sourcemap: false,
  },
  test: {
    environment: 'jsdom',
    // De origin van de testomgeving staat hier expliciet. Op een opaque origin
    // (`about:blank`) gooit `localStorage` vanaf jsdom 28 een SecurityError, en
    // die vloog in de afterEach van App.test.ts door de teardown heen — de
    // gerenderde DOM bleef staan en de volgende test vond alles dubbel. Vitest
    // geeft jsdom vandaag zelf een gewone origin, dus dit repareert niets wat
    // nu stuk is: het legt vast dat wij die keuze maken en niet een default van
    // een ander. smoke.test.ts noemt deze origin letterlijk, zodat het weghalen
    // van deze regel daar omvalt en niet verderop in een willekeurige suite.
    environmentOptions: { jsdom: { url: 'http://localhost/' } },
    include: ['src/**/*.test.ts'],
    setupFiles: ['./vitest-setup.ts'],
  },
})
