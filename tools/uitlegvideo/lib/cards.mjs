// De naamsvermelding. Ze worden als HTML
// getekend door dezelfde Chromium, op de oorsprong van de app, met de
// lettertypen en kleuren die de app op dat moment zelf gebruikt; hier staat
// alleen de compositie.
import { chromium } from 'playwright'
import { openBrowser, preparePage, VISITOR_PROFILE } from './session.mjs'
import { buildPlan } from './take.mjs'

// appStyle leest uit de draaiende app wat een kaart nodig heeft: de
// @font-face-regels van de huisletter en de kleuren van inkt en gedempte tekst.
//
// Ook dit gebeurt als afspeelsessie: de app krijgt de antwoorden uit `take`
// (de chattake) en de neutrale bezoeker, en er gaat geen verzoek naar
// /api/duif/v1. Dat wordt hier gecontroleerd op het netwerk.
export async function appStyle(base, take) {
  const plan = buildPlan(take)
  const { browser, context } = await openBrowser({})
  try {
    const misses = []
    const page = await preparePage(context, { mode: 'replay', http: plan.http, channels: [], profile: VISITOR_PROFILE, onReport: (kind, data) => { if (kind === 'replay-miss') misses.push(data) } })
    const network = []
    page.on('request', (r) => { if (new URL(r.url()).pathname.startsWith('/api/duif/v1')) network.push(r.method() + ' ' + r.url()) })
    await page.goto(base + '/duif', { waitUntil: 'networkidle' })
    await page.locator('.pigeon-choice img').first().waitFor()
    if (misses.length || network.length) throw new Error(`stijl lezen deed verzoeken buiten de take: ${JSON.stringify({ misses, network })}`)
    return await page.evaluate(() => {
      const root = getComputedStyle(document.querySelector('.duif-root'))
      const v = (n) => root.getPropertyValue(n).trim()
      const faces = []
      for (const sheet of document.styleSheets) {
        let rules
        try { rules = sheet.cssRules } catch { continue }
        for (const rule of rules) {
          if (!(rule instanceof CSSFontFaceRule)) continue
          const family = rule.style.getPropertyValue('font-family').replaceAll('"', '')
          const range = rule.style.getPropertyValue('unicode-range')
          if (family !== 'Fustat') continue
          // Alleen de Latijnse delen; de rest van het schrift heeft een kaart niet nodig.
          if (range && !/U\+0000-00FF|U\+0-FF|U\+0000-007F/i.test(range)) continue
          faces.push(rule.cssText.replace(/url\("(\.\/[^"]+)"\)/g, (_, rel) => `url("${new URL(rel, sheet.href).href}")`))
        }
      }
      return {
        faces,
        sans: v('--font-sans'),
        tokens: { ink: v('--duif-ink'), dim: v('--duif-dim-base') },
      }
    })
  } finally {
    await browser.close()
  }
}

function css(style, size = { width: 1920, height: 1080 }) {
  const t = style.tokens
  return `${style.faces.join('\n')}
:root { --ink:${t.ink}; --dim:${t.dim}; --sans:${style.sans}; }
* { box-sizing: border-box; margin: 0; }
html, body { width: ${size.width}px; height: ${size.height}px; overflow: hidden; background: transparent; }
body { font-family: var(--sans); color: var(--ink); -webkit-font-smoothing: antialiased; }
`
}

const escape = (s) => String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c])

export function cardHtml(kind, data, style, size) {
  if (kind === 'credit') {
    // De verplichte naamsvermelding: klein maar leesbaar, in het lege midden
    // van de kop. Elk deel is { text, keep }: `keep` (de licentie) blijft op
    // één regel, de rest loopt om.
    const body = data.parts.map((p) => `${escape(p.text)} <span class="keep">${escape(p.keep)}</span>`).join(' — ')
    return `<style>${css(style, size)}
.credit { position: absolute; left: 50%; top: 14px; transform: translateX(-50%); width: max-content; max-width: ${Math.round(size.width * 0.62)}px;
  font: 400 22px/1.3 var(--sans); color: var(--dim); text-align: center; }
.keep { white-space: nowrap; }
</style><div class="credit">${body}</div>`
  }
  throw new Error(`onbekende kaart ${kind}`)
}

// renderCards tekent een lijst { kind, data, out, transparent } op de
// oorsprong van de app, zodat de lettertypen van de app zelf laden.
export async function renderCards(base, style, jobs, size = { width: 1920, height: 1080 }) {
  const browser = await chromium.launch({ args: ['--hide-scrollbars'] })
  try {
    for (const scale of [...new Set(jobs.map((j) => j.scale ?? 1))]) {
      const context = await browser.newContext({ viewport: size, deviceScaleFactor: scale })
      const page = await context.newPage()
      let html = ''
      await page.route(`${base}/__uitlegvideo/**`, async (route) => {
        const rel = new URL(route.request().url()).pathname.replace('/__uitlegvideo/', '')
        if (rel === 'card') return route.fulfill({ status: 200, contentType: 'text/html', body: `<!doctype html><meta charset="utf-8">${html}` })
        return route.fulfill({ status: 404 })
      })
      for (const job of jobs.filter((j) => (j.scale ?? 1) === scale)) {
        html = cardHtml(job.kind, job.data, style, size)
        await page.goto(`${base}/__uitlegvideo/card?${encodeURIComponent(job.out)}`)
        await page.evaluate(() => document.fonts.ready)
        await page.screenshot({ path: job.out, omitBackground: Boolean(job.transparent) })
      }
      await context.close()
    }
  } finally {
    await browser.close()
  }
}
