#!/usr/bin/env node
// Beelden van de echte kennisgraaf voor de methode op de etalage
// (site/etalage/media/beeld/kennisgraaf-licht.webp en -donker.webp), gemaakt
// met de frontend van de opnameserver. Geen modelaanroepen: de graaf leest
// alleen de kennisbank.
//
// In beeld komt alleen de graaf zelf: het canvas met de knopen, hun namen en
// de verbindingen. Boom, leespaneel, knoppenbalk en de zoomknoppen op de kaart
// blijven buiten beeld; de zoomknoppen en de hint onderaan het canvas worden
// daarvoor alleen tijdens de opname verborgen. Het leespaneel valt weg: op
// ±900 pixels breed in de methode-sectie zou het onleesbaar klein worden. Na
// "Passend maken" wordt het beeld bijgesneden tot wat er op het canvas staat
// (knopen, namen, verbindingen) met een rand; dat is ongeveer 760 CSS-pixels
// breed, zodat de namen op 900 pixels iets groter staan dan in de module
// zelf. Vastgelegd op dubbele pixeldichtheid, zodat het beeld scherp blijft.
//
// Geselecteerd is één domein (standaard "Milieu en dierenwelzijn", waar
// vuurwerk, festivals en lawaai onder vallen): zijn veertien thema's staan
// gekleurd om het domein, de rest gedimd erachter. Een domein en geen thema:
// bij een thema tekent de module de naam van de band naar zijn domein midden
// op die band, dus altijd met een lijn erdoor; bij een domein met meer dan
// acht thema's tekent zij geen bandnamen.
//
//   node tools/etalage-opnames/graph-shots.mjs                 Nederlands (de twee bestanden van het contract)
//   node tools/etalage-opnames/graph-shots.mjs --lang en       Engels, als kennisgraaf-licht-en.webp en -donker-en.webp
//   node tools/etalage-opnames/graph-shots.mjs --select ""     de beginstand, zonder selectie
//
// De herkomst (module-commit, tijdstip, maat, hash) komt in
// kennisgraaf.json naast dit script.
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { parseArgs } from 'node:util'
import { chromium } from 'playwright'
import { BASE, GPU_FLAGS, SITE_DIR, TOOL_DIR } from './lib/config.mjs'
import { contentBox } from './lib/image.mjs'
import { checkServer } from './lib/server-info.mjs'

const SELECT = { nl: 'Milieu en dierenwelzijn', en: 'Environment and animal welfare' }
const { values: args } = parseArgs({
  options: {
    base: { type: 'string', default: BASE },
    lang: { type: 'string', default: 'nl' },
    select: { type: 'string' },
    viewport: { type: 'string', default: '1600x1000' },
    dpr: { type: 'string', default: '2' },
    zoom: { type: 'string', default: '0' },
    pad: { type: 'string', default: '24' },
    threshold: { type: 'string', default: '18' },
    max: { type: 'string', default: String(250 * 1024) },
  },
})
const select = args.select ?? SELECT[args.lang]
const suffix = args.lang === 'nl' ? '' : `-${args.lang}`
const [vw, vh] = args.viewport.split('x').map(Number)
const dpr = Number(args.dpr)
const MAX = Number(args.max)
const OUT = path.join(SITE_DIR, 'etalage/media/beeld')
const RECORD = path.join(TOOL_DIR, 'kennisgraaf.json')
const server = await checkServer(args.base)
mkdirSync(OUT, { recursive: true })

// De schil volgt de taal van het account; zie record-meeting.mjs.
const me = await (await fetch(`${args.base}/api/auth/me`)).json()
if (me.locale !== args.lang) {
  const res = await fetch(`${args.base}/api/auth/profile`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: me.name ?? '', locale: args.lang }) })
  if (!res.ok) throw new Error(`taal van het opnameaccount: HTTP ${res.status}`)
}

const record = existsSync(RECORD) ? JSON.parse(readFileSync(RECORD, 'utf8')) : { images: {} }
const browser = await chromium.launch({ headless: true, args: [...GPU_FLAGS, '--hide-scrollbars'] })
try {
  for (const [name, scheme] of [['licht', 'light'], ['donker', 'dark']]) {
    const context = await browser.newContext({ viewport: { width: vw, height: vh }, deviceScaleFactor: dpr, colorScheme: scheme, locale: args.lang === 'nl' ? 'nl-NL' : 'en-GB', reducedMotion: 'reduce' })
    const page = await context.newPage()
    await page.addInitScript(([l, s]) => {
      try { localStorage.setItem('PARAGLIDE_LOCALE', l); localStorage.setItem('duif.ui-language', l); localStorage.setItem('mode-watcher-mode', s) } catch { /* geen opslag */ }
    }, [args.lang, scheme])
    await page.route(/\/api\/(pageview|csp-report)$/, (route) => route.fulfill({ status: 204 }))
    await page.goto(args.base + '/duif', { waitUntil: 'domcontentloaded' })
    await page.locator('.pigeon-choice[data-pigeon="biological"]').waitFor({ timeout: 30000 })
    await page.locator('.duif-nav-views button').first().click()
    await page.getByTestId('graph-view').waitFor({ timeout: 30000 })
    await page.getByTestId('graph-shown').waitFor({ timeout: 30000 })
    // Tijd voor de layout van de kaart en de lettertypen.
    await page.evaluate(() => document.fonts.ready)
    await page.waitForTimeout(2500)
    if (select) {
      // Een klik op een rij selecteert én klapt in of uit. Het domein staat
      // bij binnenkomst uitgeklapt, met zijn thema's op de kaart; blijft het
      // na de klik ingeklapt, dan klapt een tweede klik het weer uit (de
      // selectie blijft).
      const item = page.getByTestId('graph-tree').locator('[data-testid="tree-item"]', { has: page.locator('.graph-tree-row', { hasText: select }) }).first()
      const before = await item.getAttribute('aria-expanded')
      await item.locator('.graph-tree-row').first().click()
      await page.waitForTimeout(1500)
      if (before === 'true' && (await item.getAttribute('aria-expanded')) === 'false') await item.locator('.graph-tree-row').first().click()
      await page.waitForTimeout(2500)
    }
    // Passend maken (de derde knop op de kaart), dan eventueel inzoomen, en
    // de knoppen en de hint voor het beeld verbergen.
    const controls = page.locator('.graph-map-controls button')
    await controls.nth(2).click()
    for (let i = 0; i < Number(args.zoom); i++) await controls.nth(1).click()
    await page.waitForTimeout(2000)
    await page.addStyleTag({ content: '.graph-map-controls, .graph-map-hint, .graph-tip { visibility: hidden !important; }' })
    await page.waitForTimeout(300)
    const box = await page.getByTestId('graph-canvas').boundingBox()
    const clip = { x: Math.round(box.x), y: Math.round(box.y), width: Math.floor(box.width), height: Math.floor(box.height) }
    const tmp = mkdtempSync(path.join(os.tmpdir(), 'etalage-graaf-'))
    try {
      const png = path.join(tmp, 'graaf.png')
      await page.screenshot({ path: png, clip })
      // Het lege canvas eromheen gaat eraf: bijsnijden tot alles wat van de
      // achtergrondkleur afwijkt (knopen, namen, verbindingen), met een rand.
      const crop = contentBox(png, clip.width * dpr, clip.height * dpr, Number(args.pad) * dpr, Number(args.threshold))
      const file = `kennisgraaf-${name}${suffix}.webp`
      const target = path.join(OUT, file)
      let quality = 82
      for (;;) {
        execFileSync('cwebp', ['-quiet', '-q', String(quality), '-m', '6', '-metadata', 'none', '-crop', String(crop.x), String(crop.y), String(crop.width), String(crop.height), png, '-o', target])
        if (statSync(target).size <= MAX || quality <= 40) break
        quality -= 6
      }
      const size = statSync(target).size
      const pixels = { width: crop.width, height: crop.height }
      console.log(`${path.relative(process.cwd(), target)}: ${pixels.width}×${pixels.height} (${Math.round(crop.width / dpr)}×${Math.round(crop.height / dpr)} CSS-pixels ×${dpr}), ${Math.round(size / 1024)} KB (q${quality})${size > MAX ? ' — TE GROOT' : ''}`)
      if (size > MAX) process.exitCode = 1
      record.images[file] = {
        lang: args.lang,
        scheme,
        selection: select || null,
        crop: 'graafcanvas, bijgesneden tot de knopen, namen en verbindingen (zonder boom, leespaneel en knoppen)',
        width: pixels.width,
        height: pixels.height,
        cssWidth: Math.round(crop.width / dpr),
        cssHeight: Math.round(crop.height / dpr),
        devicePixelRatio: dpr,
        quality,
        bytes: size,
        sha256: createHash('sha256').update(readFileSync(target)).digest('hex'),
        madeAt: new Date().toISOString(),
        moduleCommit: server.module.commit,
        moduleTree: server.module.moduleTree,
        bundleSha256: server.module.bundleSha256,
        knowledgeBase: { kbId: server.knowledge.kbId, importedAt: server.knowledge.importedAt },
        tool: server.tool,
      }
    } finally {
      rmSync(tmp, { recursive: true, force: true })
    }
    await context.close()
  }
} finally {
  await browser.close()
}
writeFileSync(RECORD, JSON.stringify(record, null, 1) + '\n')

