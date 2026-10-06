// Visuele controle van de vergadermodule: de achtergrond per duif op het
// startscherm, het transcript in de linkerkolom onder de duif en de iconen bij
// de bedieningsknoppen.
//
// Draait headless tegen een eigen ontwikkelserver (Vite met de
// DevShell, /api geproxied naar een eigen Go-instantie). De vergadering zelf is
// volledig synthetisch: elke /meeting/-route wordt onderschept en het
// sessiekanaal is een nagebootste EventSource. Er gaat geen enkele aanroep naar
// een taalmodel, spraakherkenning of spraaksynthese, en de microfoon is het
// nepapparaat van Chromium met een stil WAV-bestand — geen zaalmicrofoon.
//
// De herkomst van de devserver komt uit de omgeving, want het poortnummer is
// van wie hem start en niet van dit bestand; zonder variabele valt hij terug op
// de standaardpoort van Vite.
//
//   DUIF_VISUEEL_BASE=http://127.0.0.1:<poort> node tools/meeting-visueel-check.mjs
//
// Schrijft screenshots en summary.json naar het genegeerde
// module/bin/meeting-visueel/.
import { chromium, expect } from '@playwright/test'
import { createHash } from 'node:crypto'
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises'
import path from 'node:path'

const root = path.resolve(import.meta.dirname, '..')
const base = process.env.DUIF_VISUEEL_BASE ?? 'http://127.0.0.1:5173'
const output = path.join(root, 'module/bin/meeting-visueel')
await mkdir(output, { recursive: true, mode: 0o700 })

// Een stil WAV-bestand voor het nepapparaat: de microfoon opent echt, maar er
// staat niets op en er gaat niets naar een server die luistert.
const wav = Buffer.alloc(44 + 48000 * 2)
wav.write('RIFF'); wav.writeUInt32LE(wav.length - 8, 4); wav.write('WAVEfmt ', 8); wav.writeUInt32LE(16, 16)
wav.writeUInt16LE(1, 20); wav.writeUInt16LE(1, 22); wav.writeUInt32LE(48000, 24); wav.writeUInt32LE(96000, 28)
wav.writeUInt16LE(2, 32); wav.writeUInt16LE(16, 34); wav.write('data', 36); wav.writeUInt32LE(wav.length - 44, 40)
const scratch = await mkdtemp('/tmp/duiven-meeting-visueel-')
const wavPath = path.join(scratch, 'silence.wav')
await writeFile(wavPath, wav)

const PIGEONS = ['biological', 'individualist', 'nonverbal']
const SESSION = 'synthetic-visueel'
const CONFIGS = []
for (const lang of ['en', 'nl']) {
  for (const theme of ['light', 'dark']) {
    for (const width of [1280, 390]) CONFIGS.push({ lang, theme, width, height: width === 390 ? 844 : 900 })
  }
}

// WCAG-contrast uit twee opgehaalde rgb-kleuren. sRGB, geen alfa: de gemeten
// achtergronden zijn dekkend (color-mix levert een dekkende kleur op).
function channel(v) { const c = v / 255; return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4 }
function luminance([r, g, b]) { return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b) }
function contrast(a, b) { const l1 = luminance(a), l2 = luminance(b); return (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05) }
// De browser rekent color-mix(in oklch, ...) uit naar oklch()/color(srgb ...),
// niet naar rgb(). Laat het canvas de omzetting naar sRGB-bytes doen in plaats
// van zelf cijfers uit een tekenreeks te vissen.
const TO_RGB = `(value) => {
  const canvas = document.createElement('canvas'); canvas.width = 1; canvas.height = 1;
  const ctx = canvas.getContext('2d', { willReadFrequently: true });
  ctx.clearRect(0, 0, 1, 1); ctx.fillStyle = '#000'; ctx.fillStyle = value; ctx.fillRect(0, 0, 1, 1);
  const d = ctx.getImageData(0, 0, 1, 1).data; return [d[0], d[1], d[2]];
}`

// Alle zichtbare tekst in een gebied, elk stuk gemeten tegen de kleur waar het
// echt op ligt. De achtergrond wordt van buiten naar binnen opgebouwd — html,
// dan elke voorouder, dan het element zelf — zodat een doorzichtige of
// half-doorzichtige laag meetelt zoals het scherm hem toont. Het canvas doet de
// omzetting: color-mix() komt uit getComputedStyle als color(srgb ...) en niet
// als rgb(), en van een tekenreeks cijfers vissen gaat een keer mis.
//
// De drempel is die van WCAG AA: 4.5 voor gewone tekst, 3.0 voor grote tekst
// (24px, of 18.66px vanaf gewicht 700).
const textContrast = (selector) => {
  const root = document.querySelector(selector);
  if (!root) return null;
  const canvas = document.createElement('canvas'); canvas.width = 1; canvas.height = 1;
  const ctx = canvas.getContext('2d', { willReadFrequently: true });
  // Legt value met dekking alpha op base en geeft terug wat er dan staat.
  const over = (value, base, alpha) => {
    ctx.globalAlpha = 1; ctx.clearRect(0, 0, 1, 1);
    ctx.fillStyle = '#000'; ctx.fillStyle = 'rgb(' + base.join(',') + ')'; ctx.fillRect(0, 0, 1, 1);
    ctx.globalAlpha = alpha;
    // Een ongeldige waarde laat fillStyle op de vorige staan; daarom eerst zwart
    // zetten en daarna pas de echte waarde proberen.
    ctx.fillStyle = '#000'; ctx.fillStyle = value; ctx.fillRect(0, 0, 1, 1);
    ctx.globalAlpha = 1;
    const d = ctx.getImageData(0, 0, 1, 1).data; return [d[0], d[1], d[2]];
  };
  const chainOf = (el) => { const chain = []; for (let n = el; n; n = n.parentElement) chain.push(n); return chain.reverse(); };
  // Een naam waar een mens iets aan heeft in het rapport: de testid als die er
  // is, anders de eigen klasse. De s-…-klassen van Svelte zeggen niets — die
  // zijn het scope-merk van de component — dus die slaan we over.
  const name = (el) => {
    const id = el.dataset && el.dataset.testid;
    if (id) return '[' + id + ']';
    const cls = (el.getAttribute('class') ?? '').trim().split(/\s+/).filter((c) => c && !/^s-/.test(c));
    return el.tagName.toLowerCase() + (cls.length > 0 ? '.' + cls[0] : '');
  };
  // De gedempte tekst apart herkenbaar houden: dat is de kleur die het tinten
  // van het podium het eerst pijn doet, dus die wil het rapport los noemen.
  // Vergelijken gebeurt op de uitgerekende kleur, niet op de tekenreeks: dezelfde
  // kleur komt uit color-mix() anders geschreven terug dan uit een hex.
  const dimRaw = getComputedStyle(root).getPropertyValue('--duif-dim').trim();
  const dimKey = dimRaw ? over(dimRaw, [255, 255, 255], 1).join(',') : 'geen';
  const rows = [];
  for (const el of [root, ...root.querySelectorAll('*')]) {
    const own = [...el.childNodes].filter((n) => n.nodeType === 3).map((n) => n.textContent).join(' ').replace(/\s+/g, ' ').trim();
    if (!own) continue;
    const style = getComputedStyle(el);
    if (style.visibility !== 'visible') continue;
    const rect = el.getBoundingClientRect();
    if (rect.width < 1 || rect.height < 1) continue;
    let background = [255, 255, 255];
    let opacity = 1;
    for (const node of chainOf(el)) {
      const s = getComputedStyle(node);
      const a = Number(s.opacity);
      opacity *= Number.isFinite(a) ? a : 1;
      background = over(s.backgroundColor, background, Number.isFinite(a) ? a : 1);
    }
    if (opacity < 0.05) continue;
    const color = over(style.color, background, opacity);
    const size = parseFloat(style.fontSize);
    const weight = Number(style.fontWeight) || 400;
    const large = size >= 24 || (size >= 18.66 && weight >= 700);
    const dim = over(style.color, [255, 255, 255], 1).join(',') === dimKey;
    rows.push({ where: name(el), text: own.slice(0, 48), color, background, size, weight, dim, min: large ? 3 : 4.5 });
  }
  return rows;
}

const reports = []
let context
let activePage
try {
  context = await chromium.launch({
    headless: true,
    channel: 'chromium',
    args: ['--use-fake-device-for-media-stream', '--use-fake-ui-for-media-stream', `--use-file-for-fake-audio-capture=${wavPath}`],
  })

  // De ontwikkelserver bundelt afhankelijkheden pas wanneer een scherm ze voor
  // het eerst opvraagt, en herlaadt de pagina daarna. Eén opwarmronde haalt de
  // vergadering (3D-duif, keuzelijst) één keer op, zodat de metingen daarna niet
  // midden in een herlaadbeurt vallen.
  {
    const warm = await context.newContext({ viewport: { width: 1280, height: 900 }, permissions: ['microphone'] })
    const page = await warm.newPage()
    await page.goto(base, { waitUntil: 'domcontentloaded' })
    const tab = page.getByRole('tab', { name: 'Meeting' })
    await expect(async () => { await tab.click(); await expect(tab).toHaveAttribute('aria-selected', 'true') }).toPass({ timeout: 20000 })
    await expect(page.getByTestId('meeting-view')).toBeVisible()
    await page.waitForTimeout(4000)
    await warm.close()
  }

  for (const config of CONFIGS) {
    const browserContext = await context.newContext({
      viewport: { width: config.width, height: config.height },
      permissions: ['microphone'],
      deviceScaleFactor: 1,
    })
    const page = await browserContext.newPage()
    activePage = page
    const errors = []
    const external = []
    const findings = { config, identity: {}, overflow: [], contrast: [], stageContrast: {}, layout: {}, icons: {} }
    page.on('pageerror', (e) => errors.push(e.message))
    if (process.env.DUIF_VISUEEL_DEBUG) {
      page.on('framenavigated', (f) => console.error('NAV', f.url()))
      page.on('console', (m) => console.error('LOG', m.type(), m.text().slice(0, 120)))
    }
    page.on('console', (m) => { if (m.type() === 'error') errors.push(`console: ${m.text()}`) })

    await page.addInitScript(({ lang, session }) => {
      localStorage.setItem('duif.ui-language', lang)
      const NativeEvents = window.EventSource
      const channels = []
      window.EventSource = class {
        static CLOSED = 2
        constructor(url) {
          if (!String(url).includes(`/meeting/sessions/${session}/events`)) return new NativeEvents(url)
          this.readyState = 1
          channels.push(this)
        }
        close() { this.readyState = 2 }
      }
      window.visueelFixture = {
        emit: (ev) => channels.at(-1)?.onmessage?.({ data: JSON.stringify(ev) }),
        open: () => channels.filter((c) => c.readyState !== 2).length,
      }
    }, { lang: config.lang, session: SESSION })

    let turn = 0
    const settings = () => ({ turnId: String(turn), autonomous: false, autonomyPaused: false, playbackTracking: true, eagerness: 'normal', cooldownSeconds: 180 })
    await page.route('**/*', async (route) => {
      const request = route.request()
      const url = new URL(request.url())
      // Niets verlaat deze machine: elk verzoek naar een andere herkomst wordt
      // hier afgevangen en leeg beantwoord. In de dev-index staat één
      // lettertypeverwijzing naar Google Fonts; die wordt dus nooit opgehaald en
      // de pagina valt terug op de systeemletters. Dat staat in het rapport.
      if (url.origin !== new URL(base).origin) {
        if (!external.includes(url.origin)) external.push(url.origin)
        return route.fulfill({ status: 204, body: '', contentType: 'text/plain' })
      }
      if (url.pathname.includes('/v1/meeting/sessions')) {
        const json = (body, status = 202) => route.fulfill({ json: body, status })
        if (request.method() === 'DELETE') return route.fulfill({ status: 204 })
        if (url.pathname.endsWith('/sessions')) {
          const body = request.postDataJSON() ?? {}
          return json({ sessionId: SESSION, pigeonId: body.pigeonId, lang: body.lang, listening: 'listening', voice: 'off', playbackTracking: true, settings: settings() }, 201)
        }
        if (url.pathname.endsWith('/audio')) return json({ streamed: 1, dropped: 0, listening: 'listening' })
        if (url.pathname.endsWith('/respond') || url.pathname.endsWith('/intervene') || url.pathname.endsWith('/stop')) { turn += 1; return json({ accepted: true, settings: settings() }) }
        return json({ accepted: true, settings: settings() })
      }
      return route.continue()
    })

    await page.goto(base, { waitUntil: 'domcontentloaded' })
    // Donker zetten we met de knop van de DevShell zelf, niet met een truc:
    // dat is dezelfde weg die de gastheer-schil in het echt bewandelt.
    if (config.theme === 'dark') {
      const toggle = page.getByRole('button', { name: 'dark', exact: true })
      await expect(async () => { await toggle.click(); await expect(page.locator('html')).toHaveClass(/dark/) }).toPass({ timeout: 15000 })
    }
    const meetingTab = page.getByRole('tab', { name: config.lang === 'en' ? 'Meeting' : 'Vergadering' })
    await expect(meetingTab).toBeVisible()
    // De schil hydrateert na de eerste verf; klik tot de tab ook echt staat.
    await expect(async () => { await meetingTab.click(); await expect(meetingTab).toHaveAttribute('aria-selected', 'true') }).toPass({ timeout: 15000 })
    await expect(page.getByTestId('meeting-view')).toBeVisible()

    // ---- elke duif haar eigen achtergrond op het startscherm ----
    for (const pigeon of PIGEONS) {
      await page.locator(`.pigeon-choice[data-pigeon="${pigeon}"]`).click()
      await expect(page.getByTestId('meeting-view')).toHaveAttribute('data-pigeon', pigeon)
      const measured = await page.evaluate((source) => {
        const toRgb = eval(source)
        const view = document.querySelector('[data-testid="meeting-view"]')
        const identity = document.querySelector('.setup-identity')
        const title = document.querySelector('#meeting-setup-title')
        const body = document.querySelector('.setup-identity p')
        const eyebrow = document.querySelector('.setup-identity .duif-eyebrow')
        const style = (el) => getComputedStyle(el)
        return {
          screen: toRgb(style(view).backgroundColor).join(','),
          card: toRgb(style(identity).backgroundColor).join(','),
          titleColor: toRgb(style(title).color).join(','),
          bodyColor: toRgb(style(body).color).join(','),
          eyebrowColor: toRgb(style(eyebrow).color).join(','),
          titleSize: style(title).fontSize,
        }
      }, TO_RGB)
      findings.identity[pigeon] = measured
      // Tekst op de gekleurde kaart blijft leesbaar: de kop is groot (AA 3.0),
      // de lopende tekst houdt de gewone eis van 4.5.
      const nums = (v) => v.split(',').map(Number)
      const cardBg = nums(measured.card)
      const titleRatio = contrast(nums(measured.titleColor), cardBg)
      const bodyRatio = contrast(nums(measured.bodyColor), cardBg)
      const eyebrowRatio = contrast(nums(measured.eyebrowColor), cardBg)
      findings.contrast.push({ pigeon, title: Number(titleRatio.toFixed(2)), body: Number(bodyRatio.toFixed(2)), eyebrow: Number(eyebrowRatio.toFixed(2)) })
      expect(titleRatio, `kop op de kaart van ${pigeon}`).toBeGreaterThanOrEqual(3)
      expect(bodyRatio, `tekst op de kaart van ${pigeon}`).toBeGreaterThanOrEqual(4.5)
      expect(eyebrowRatio, `bovenschrift op de kaart van ${pigeon}`).toBeGreaterThanOrEqual(4.5)

      const overflow = await page.evaluate(() => ({ doc: document.documentElement.scrollWidth - document.documentElement.clientWidth }))
      findings.overflow.push({ where: `setup:${pigeon}`, ...overflow })
      expect(overflow.doc, `horizontale overloop op het startscherm van ${pigeon}`).toBeLessThanOrEqual(0)

      await page.screenshot({ path: path.join(output, `setup-${pigeon}-${config.lang}-${config.theme}-${config.width}.png`) })
    }
    // De drie achtergronden moeten ook echt uit elkaar liggen.
    const cards = PIGEONS.map((p) => findings.identity[p].card)
    expect(new Set(cards).size, `drie verschillende kaartachtergronden: ${cards.join(' | ')}`).toBe(3)
    const screens = PIGEONS.map((p) => findings.identity[p].screen)
    expect(new Set(screens).size, `drie verschillende schermachtergronden: ${screens.join(' | ')}`).toBe(3)

    // Elke zichtbare tekstregel op het lopende podium tegen de kleur waar zij op
    // ligt. Het podium tint zijn achtergrond met de kleur van de duif, en dat
    // kost contrast: daarom meet dit niet alleen de kaart maar echt alles wat er
    // staat, in elke stand waarin het podium tekst laat zien.
    const measureStage = async (pigeon, phase) => {
      const rows = await page.evaluate(textContrast, '[data-testid="live-view"]')
      expect(rows, `podium van ${pigeon} gevonden (${phase})`).not.toBeNull()
      expect(rows.length, `zichtbare tekst op het podium van ${pigeon} (${phase})`).toBeGreaterThan(3)
      const measured = rows.map((row) => ({ ...row, ratio: Number(contrast(row.color, row.background).toFixed(2)) }))
      for (const row of measured) {
        expect(
          row.ratio,
          `${pigeon}/${phase} ${row.where} "${row.text}" (${row.size}px, gewicht ${row.weight}) op rgb(${row.background})`,
        ).toBeGreaterThanOrEqual(row.min)
      }
      const bucket = (findings.stageContrast[pigeon] ??= { rows: [] })
      bucket.rows.push(...measured.map(({ where, size, weight, dim, min, ratio }) => ({ phase, where, size, weight, dim, min, ratio })))
      bucket.min = Math.min(...bucket.rows.map((r) => r.ratio))
      const dimmed = bucket.rows.filter((r) => r.dim).map((r) => r.ratio)
      bucket.dim = dimmed.length > 0 ? Math.min(...dimmed) : null
      return measured
    }

    // ---- het lopende vergaderscherm: verloop en iconen ----
    for (const pigeon of PIGEONS) {
      await expect(async () => {
        await page.locator(`.pigeon-choice[data-pigeon="${pigeon}"]`).click()
        await expect(page.getByTestId('meeting-view')).toHaveAttribute('data-pigeon', pigeon)
      }).toPass({ timeout: 10000 })
      await expect(async () => {
        await page.getByTestId('seat-button').click()
        await expect(page.getByTestId('live-view')).toBeVisible({ timeout: 3000 })
      }).toPass({ timeout: 20000 })
      await page.waitForFunction(() => Boolean(window.visueelFixture) && window.visueelFixture.open() > 0)
      await page.evaluate(({ pigeon, lang }) => window.visueelFixture.emit({ type: 'state', turnId: '0', sessionId: 'synthetic-visueel', pigeonId: pigeon, lang, listening: 'listening', voice: 'off', speaking: false }), { pigeon, lang: config.lang })

      // Het stille podium: de uitnodiging, de wachtzin en het lege verloop.
      await measureStage(pigeon, 'wachtend')

      // Een bijdrage via de fixtureroute: getypte tekst, geen model, geen stem.
      await page.evaluate(({ pigeon, lang, speaks }) => {
        const emit = window.visueelFixture.emit
        emit({ type: 'transcript', text: 'SYNTHETIC: we are deciding what to do about the birds on the square.', speaker: '2', final: true })
        emit({ type: 'transcript', text: 'SYNTHETIC: and the noise early in the morning.', speaker: '3', final: true })
        if (!speaks) return
        emit({ type: 'persona', turnId: '1', pigeonId: pigeon, lang })
        emit({ type: 'text', turnId: '1', delta: 'SYNTHETIC contribution for layout only. '.repeat(24) })
        emit({ type: 'response_done', turnId: '1', kbStatus: 'gap', sources: [] })
      }, { pigeon, lang: config.lang, speaks: pigeon !== 'nonverbal' })
      if (pigeon !== 'nonverbal') await expect(page.getByTestId('latest-answer')).toBeVisible()

      // Het sprekende podium met het verloop dicht (de gezegde regels staan er)
      // en daarna uitgeklapt (kop, log, geheugenregel, eerdere bijdragen).
      await measureStage(pigeon, 'antwoord')
      await page.getByTestId('transcript-toggle').click()
      await expect(page.getByTestId('transcript-log')).toBeVisible()
      await measureStage(pigeon, 'verloop-open')
      await page.getByTestId('transcript-toggle').click()
      await expect(page.getByTestId('transcript-preview')).toBeVisible()

      const boxes = await page.evaluate(() => {
        const box = (sel) => { const el = document.querySelector(sel); if (!el) return null; const r = el.getBoundingClientRect(); return { left: Math.round(r.left), right: Math.round(r.right), top: Math.round(r.top), bottom: Math.round(r.bottom), width: Math.round(r.width) } }
        const scroll = document.querySelector('.stage-scroll')
        return {
          pigeon: box('.pigeon-stage'),
          answer: box('.stage-answer'),
          transcript: box('.stage-transcript'),
          credits: box('.model-credit'),
          stageOverflow: scroll ? scroll.scrollWidth - scroll.clientWidth : null,
          docOverflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
        }
      })
      findings.layout[`${pigeon}-live`] = boxes
      expect(boxes.stageOverflow, 'horizontale overloop in het podium').toBeLessThanOrEqual(0)
      expect(boxes.docOverflow, 'horizontale overloop van de pagina').toBeLessThanOrEqual(0)
      if (pigeon !== 'nonverbal') {
        if (config.width >= 1280) {
          // Twee kolommen: het verloop staat links onder de duif, het antwoord rechts.
          expect(boxes.transcript.right, 'verloop blijft links van het antwoord').toBeLessThanOrEqual(boxes.answer.left)
          expect(Math.abs(boxes.transcript.left - boxes.pigeon.left), 'verloop lijnt uit met de duif').toBeLessThanOrEqual(2)
          expect(boxes.transcript.width, 'verloop houdt de breedte van de duifkolom').toBeLessThanOrEqual(boxes.pigeon.width + 2)
          expect(boxes.transcript.top, 'verloop staat onder de duif').toBeGreaterThanOrEqual(boxes.pigeon.bottom - 2)
        } else {
          // Smal: het verloop valt ónder het antwoord, niet ertussen.
          expect(boxes.transcript.top, 'verloop staat onder het antwoord').toBeGreaterThanOrEqual(boxes.answer.bottom - 2)
        }
      }

      // Elk bedieningsonderdeel draagt een icoon; de microfoon toont de stand.
      const controls = pigeon === 'nonverbal'
        ? [['stop-button', 'stop'], ['intervene-button', 'intervene'], ['call-control-mute', 'microphone-live'], ['call-control-end', 'end']]
        : [['stop-button', 'stop'], ['respond-button', 'respond'], ['call-control-mute', 'microphone-live'], ['call-control-end', 'end']]
      const seen = {}
      for (const [testid, icon] of controls) {
        const mark = page.getByTestId(testid).locator('.btn-icon')
        await expect(mark).toHaveAttribute('data-icon', icon)
        seen[testid] = await mark.evaluate((el) => ({ icon: el.dataset.icon, svg: Boolean(el.querySelector('svg')), width: Math.round(el.getBoundingClientRect().width) }))
        expect(seen[testid].svg, `${testid} heeft een getekend icoon`).toBe(true)
        expect(seen[testid].width, `${testid} icoon heeft breedte`).toBeGreaterThan(6)
      }
      findings.icons[`${pigeon}-live`] = seen

      await page.screenshot({ path: path.join(output, `live-${pigeon}-${config.lang}-${config.theme}-${config.width}.png`) })

      // Gedempt: het icoon wisselt zichtbaar mee met de stand.
      await page.getByTestId('call-control-mute').click()
      await expect(page.getByTestId('call-control-mute')).toHaveAttribute('data-mic', 'muted')
      await expect(page.getByTestId('call-control-mute').locator('.btn-icon')).toHaveAttribute('data-icon', 'microphone-muted')
      if (pigeon === 'biological') await page.screenshot({ path: path.join(output, `live-muted-${config.lang}-${config.theme}-${config.width}.png`) })
      await page.getByTestId('call-control-mute').click()

      await page.getByTestId('call-control-end').click()
      // Beëindigen vraagt één keer na zolang de duif nog spreekt. Na de
      // klik komt dus de vraag óf de opzetkaart; wacht op wat het eerst komt in
      // plaats van op een telling, die niet wacht en dus nul kan zien voordat er
      // iets staat.
      {
        const dialog = page.getByTestId('end-dialog'), seat = page.getByTestId('seat-button')
        await expect.poll(async () => (await dialog.count()) + (await seat.count())).toBeGreaterThan(0)
        if (await dialog.count()) await page.getByTestId('end-dialog-confirm').click()
      }
      await expect(page.getByTestId('seat-button')).toBeVisible()
    }

    expect(errors, 'geen fouten in de pagina').toEqual([])
    // Alleen de lettertypeverwijzing van de dev-index mag onderschept zijn.
    expect(external.filter((o) => !['https://fonts.googleapis.com', 'https://fonts.gstatic.com'].includes(o)), 'geen verkeer naar derden').toEqual([])
    findings.blockedOrigins = external
    reports.push(findings)
    await page.close()
    await browserContext.close()
    activePage = undefined
  }

  // Het lopende podium in één tabel: per duif, per thema en breedte de laagste
  // gemeten waarde van alle zichtbare tekst, en de laagste van de gedempte
  // tekst apart — dat is de regel die het tinten van het podium het eerst raakt.
  const stage = []
  for (const report of reports) {
    for (const [pigeon, bucket] of Object.entries(report.stageContrast)) {
      stage.push({ lang: report.config.lang, theme: report.config.theme, width: report.config.width, pigeon, samples: bucket.rows.length, min: bucket.min, dim: bucket.dim })
    }
  }
  const summary = {
    browser: context.version(),
    base,
    scope: 'DevShell op Vite met eigen Go-API; vergadering volledig synthetisch (onderschepte HTTP + nagebootste SSE); nepmicrofoon met stil WAV; geen taalmodel, geen spraak, geen zaalmicrofoon, geen verkeer naar derden',
    checks: ['achtergrond per duif', 'verloop in de duifkolom', 'iconen bij elke bediening', 'contrast van alle zichtbare tekst op het lopende podium'],
    configs: CONFIGS.length,
    stage,
    reports,
  }
  await writeFile(path.join(output, 'summary.json'), JSON.stringify(summary, null, 2), { mode: 0o600 })
  console.log(JSON.stringify({ ...summary, reports: undefined, digest: createHash('sha256').update(JSON.stringify(reports)).digest('hex').slice(0, 16) }, null, 2))
  for (const row of stage) console.log(`podium ${row.pigeon.padEnd(14)} ${row.theme.padEnd(5)} ${row.lang} ${String(row.width).padStart(4)}  laagste ${row.min}  gedempt ${row.dim}  (${row.samples} regels)`)
} catch (error) {
  if (activePage && !activePage.isClosed()) {
    await activePage.screenshot({ path: path.join(output, 'failure.png') }).catch(() => {})
    console.error('laatste toestand:', JSON.stringify(await activePage.evaluate(() => ({ fixture: Boolean(window.visueelFixture), channels: window.visueelFixture?.open?.() ?? null, pigeon: document.querySelector('[data-testid="meeting-view"]')?.dataset.pigeon ?? null, live: Boolean(document.querySelector('[data-testid="live-view"]')) })).catch(() => null)))
  }
  throw error
} finally {
  await context?.close()
  await rm(scratch, { recursive: true, force: true })
}
