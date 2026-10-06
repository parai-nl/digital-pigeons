import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { expandParaiUiTokens, usedParaiUiFiles } from '../parai-ui-sources'

const roots: string[] = []
afterEach(() => { for (const root of roots.splice(0)) rmSync(root, { recursive: true, force: true }) })

function fixture(files: Record<string, string>): string {
  const root = mkdtempSync(path.join(tmpdir(), 'duif-ui-sources-')); roots.push(root)
  for (const [name, body] of Object.entries(files)) {
    mkdirSync(path.dirname(path.join(root, name)), { recursive: true })
    writeFileSync(path.join(root, name), body)
  }
  return root
}

describe('parai-ui-bestanden die Tailwind scant', () => {
  it('volgt de imports van src/ en daarna die binnen parai-ui, zonder tests', () => {
    const root = fixture({
      'ui/index.ts': "export { default as Shown } from './components/Shown.svelte'",
      'ui/components/Shown.svelte': "<script>import { cn } from '../utils/cn'</script>",
      'ui/components/Unused.svelte': '<div></div>',
      'ui/components/OnlyInTests.svelte': '<div></div>',
      'ui/utils/cn.ts': 'export const cn = () => ""',
      'src/App.svelte': "<script>import Shown from '@parai/ui/components/Shown.svelte'</script>",
      'src/App.test.ts': "import '@parai/ui/components/OnlyInTests.svelte'",
    })
    const ui = path.join(root, 'ui')
    expect(usedParaiUiFiles(ui, path.join(root, 'src'))).toEqual([
      path.join(ui, 'components/Shown.svelte'),
      path.join(ui, 'utils/cn.ts'),
    ])
  })

  it('maakt van de tokenregel één @source per bestand en vult de map in', () => {
    const css = '@import "__PARAI_UI_DIR__/css/theme.css";\n@source "__PARAI_UI_USED__";\n@source "./**/*.svelte";'
    expect(expandParaiUiTokens(css, '/ui', () => ['/ui/a.svelte', '/ui/b.ts'])).toBe(
      '@import "/ui/css/theme.css";\n@source "/ui/a.svelte";\n@source "/ui/b.ts";\n@source "./**/*.svelte";',
    )
  })

  // Stil mislukken zou alle parai-ui-utilities weghalen en de bundel kleiner
  // maken: het budget zou het belonen. Dus stopt de build.
  it('weigert een token dat blijft staan of een lege lijst', () => {
    expect(() => expandParaiUiTokens("@source '__PARAI_UI_USED__';", '/ui', () => ['/ui/a.svelte'])).toThrow('__PARAI_UI_USED__ niet ingevuld')
    expect(() => expandParaiUiTokens('@source "__PARAI_UI_USED__" ;', '/ui', () => ['/ui/a.svelte'])).toThrow('__PARAI_UI_USED__ niet ingevuld')
    expect(() => expandParaiUiTokens('@source "__PARAI_UI_USED__";', '/ui', () => [])).toThrow('geen enkel @parai/ui-bestand')
  })

  it('laat CSS zonder het token met rust', () => {
    expect(expandParaiUiTokens('@import "__PARAI_UI_DIR__/x.css";', '/ui', () => [])).toBe('@import "/ui/x.css";')
  })
})
