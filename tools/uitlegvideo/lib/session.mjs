// De browser voor opnemen en afspelen. Beide standen draaien de
// echte frontend in dezelfde Chromium met dezelfde instellingen; alleen het
// init-script verschilt (zie replay/page-runtime.js). Zo is een afspeelsessie
// de opname zelf, niet een nabootsing ervan.
import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync } from 'node:fs'
import path from 'node:path'
import { chromium } from 'playwright'
import { pageRuntime } from '../replay/page-runtime.js'
import { createMatcher } from './match.mjs'

// Het venster van de app. Kleiner dan 1920×1080 met een hogere schaal: de hele
// app staat in beeld, groot genoeg om op drie meter te lezen, en het beeld is
// toch 1920×1080 pixels. 1280×720 bij 1,5 houdt de gewone desktopindeling.
export const VIEWPORT = { width: 1280, height: 720, scale: 1.5 }

// openBrowser start Chromium met een synthetische microfoon: het opgegeven
// wav-bestand speelt één keer af zodra de app de microfoon opent. Het geluid
// van de pagina zelf blijft stil (--mute-audio); de audio-API's lopen gewoon
// door, zodat afspeelbevestigingen en `ended` echt zijn.
export async function openBrowser({ micWav, viewport = { width: 1280, height: 720, scale: 1 } } = {}) {
  const scale = viewport.scale ?? 1
  const args = [
    '--mute-audio',
    '--autoplay-policy=no-user-gesture-required',
    '--use-fake-ui-for-media-stream',
    '--use-fake-device-for-media-stream',
    '--hide-scrollbars',
    // Headless Chromium rendert WebGL anders in software (SwiftShader): de
    // 3D-duif haalde op 3840×2160 maar 5–8 beelden per seconde. Via ANGLE op
    // Metal rekent de GPU van de Mac.
    '--use-angle=metal',
    '--enable-gpu',
    '--ignore-gpu-blocklist',
  ]
  if (micWav) args.push(`--use-file-for-fake-audio-capture=${micWav}%noloop`)
  // Zonder deze vlag levert de screencast in headless Chromium beelden op
  // CSS-maat, ook bij een hogere deviceScaleFactor.
  if (scale !== 1) args.push(`--force-device-scale-factor=${scale}`)
  const browser = await chromium.launch({ headless: true, args })
  const context = await browser.newContext({
    viewport: { width: viewport.width, height: viewport.height },
    deviceScaleFactor: scale,
    reducedMotion: 'no-preference',
    colorScheme: 'light',
    locale: 'en-GB',
  })
  return { browser, context }
}

// preparePage zet de app in de stand waarin een bezoeker haar ziet: Engels,
// licht thema, en het init-script dat meeluistert of afspeelt. Paginabezoek-
// en CSP-meldingen van de schil gaan niet naar de server; die gaan over
// echte bezoekers.
export async function preparePage(context, { mode, http = [], channels = [], onReport, profile, freshSince, supplement = null, seqStart = 0, mediaDelays = [] }) {
  const page = await context.newPage()
  await page.exposeFunction('__uvReport', (kind, data) => onReport(kind, data))
  await page.addInitScript(() => {
    try {
      localStorage.setItem('PARAGLIDE_LOCALE', 'en')
      localStorage.setItem('mode-watcher-mode', 'light')
      localStorage.setItem('duif.ui-language', 'en')
    } catch { /* geen opslag: de app valt terug op Engels */ }
  })
  const cfg = { mode, http, channels, supplement, seqStart, mediaDelays }
  await page.addInitScript({ content: `(${pageRuntime.toString()})(${JSON.stringify(cfg)}, ${createMatcher.toString()});` })
  await page.route(/\/api\/(pageview|csp-report)$/, (route) => route.fulfill({ status: 204 }))
  // Opnemen gebeurt als verse bezoeker: het opnameaccount heeft honderden
  // proefgesprekken, en de app opent bij het kiezen van een duif haar laatste
  // gesprek. Een bezoeker op de stand heeft die geschiedenis niet; alleen
  // gesprekken uit deze opname blijven in de lijst.
  if (freshSince) {
    await page.route(/\/api\/duif\/v1\/chat\/sessions$/, async (route) => {
      if (route.request().method() !== 'GET') return route.continue()
      const response = await route.fetch()
      const body = await response.json()
      body.sessions = (body.sessions ?? []).filter((s) => Date.parse(s.createdAt) >= freshSince)
      await route.fulfill({ response, json: body })
    })
  }
  // Afspelen: wat de schil van de opnameserver toont maar pigeon.parai.nl in
  // de kop niet (of alleen persoonlijk) toont, blijft onzichtbaar. De
  // productieschil heeft geen modulepil en zet de taalkeuze in het accountmenu
  // (zo doet de gebouwde schil van pigeon.parai.nl het); het
  // accountrondje is persoonlijk. visibility houdt de opmaak op zijn plaats.
  if (profile) {
    // De muis: headless Chromium tekent geen cursor, dus deze pagina krijgt
    // er een die de echte muisbewegingen van de driver volgt, met een korte
    // ring bij elke klik. Hij doet niets met de app (pointer-events: none).
    await page.addInitScript(() => {
      const install = () => {
        const arrow = document.createElement('div')
        arrow.setAttribute('aria-hidden', 'true')
        arrow.style.cssText = 'position:fixed;left:0;top:0;width:22px;height:22px;z-index:2147483647;pointer-events:none;display:none;filter:drop-shadow(0 1px 2px rgb(0 0 0 / .35))'
        arrow.innerHTML = '<svg viewBox="0 0 22 22" width="22" height="22"><path d="M2 1.5 L2 18 L6.3 13.9 L9.1 20.2 L12 19 L9.3 12.8 L15.2 12.8 Z" fill="#111" stroke="#fff" stroke-width="1.4" stroke-linejoin="round"/></svg>'
        document.documentElement.append(arrow)
        window.addEventListener('mousemove', (e) => { arrow.style.display = 'block'; arrow.style.transform = `translate(${e.clientX - 2}px, ${e.clientY - 1}px)` }, true)
        window.addEventListener('mousedown', (e) => {
          const ring = document.createElement('div')
          ring.style.cssText = `position:fixed;left:${e.clientX - 18}px;top:${e.clientY - 18}px;width:36px;height:36px;border-radius:50%;border:2px solid rgb(37 46 43 / .55);background:rgb(37 46 43 / .12);z-index:2147483646;pointer-events:none;transform:scale(.4);opacity:1;transition:transform .35s ease-out, opacity .35s ease-out`
          document.documentElement.append(ring)
          requestAnimationFrame(() => { ring.style.transform = 'scale(1.3)'; ring.style.opacity = '0' })
          setTimeout(() => ring.remove(), 400)
        }, true)
      }
      if (document.documentElement) install()
      else document.addEventListener('DOMContentLoaded', install, { once: true })
    })
    await page.addInitScript(() => {
      const hide = () => {
        const style = document.createElement('style')
        style.textContent = '[data-testid="module-nav"],[data-testid="language-toggle"],[data-testid="user-avatar-trigger"]{visibility:hidden!important}'
        document.head.append(style)
      }
      if (document.head) hide()
      else document.addEventListener('DOMContentLoaded', hide, { once: true })
    })
  }
  // Het account van de opnameserver is een ontwikkelaarsaccount (skipauth,
  // rol admin, dev@localhost). In beeld hoort een gewone bezoeker; alleen de
  // accountknop van de schil ziet dit.
  if (profile) await page.route(/\/api\/auth\/me$/, (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(profile) }))
  return page
}

// Een neutraal profiel voor de accountknop in beeld (alleen bij afspelen).
export const VISITOR_PROFILE = {
  id: '00000000-0000-4000-8000-000000000130',
  email: 'visitor@example.org',
  name: 'Visitor',
  locale: 'en',
  role: 'user',
  avatar: '',
  status: 'active',
}

// dialogueSource geeft het vergaderfragment als wav. Dat bestand is afgeleid
// en staat niet in git (zie .gitignore); ontbreekt het, dan wordt het uit de
// vier regels en hun begintijden in meeting-dialogue.json opnieuw gemengd.
export function dialogueSource(root) {
  const own = path.join(root, 'meeting-audio/meeting-dialogue.wav')
  if (existsSync(own)) return own
  const out = path.join(root, 'out', 'work', 'meeting-dialogue.wav')
  if (existsSync(out)) return out
  const spec = JSON.parse(readFileSync(path.join(root, 'meeting-audio/meeting-dialogue.json'), 'utf8'))
  const inputs = spec.lines.flatMap((l) => ['-i', path.join(root, 'meeting-audio', l.file)])
  const delays = spec.lines.map((l, i) => `[${i}]adelay=${Math.round(l.start * 1000)}:all=1[l${i}]`)
  const mix = `${spec.lines.map((_, i) => `[l${i}]`).join('')}amix=inputs=${spec.lines.length}:normalize=0,apad,atrim=end=${spec.duration}`
  mkdirSync(path.dirname(out), { recursive: true })
  execFileSync('ffmpeg', ['-v', 'error', '-y', ...inputs, '-filter_complex', [...delays, mix].join(';'), '-ar', String(spec.sampleRate), '-ac', '1', '-c:a', 'pcm_s16le', out])
  return out
}

// micInput maakt de invoer van de synthetische microfoon: het vergaderfragment
// met `leadMs` stilte ervoor (zie record.mjs).
export function micInput(root, leadMs) {
  const out = path.join(root, 'out', 'work', `mic-lead${leadMs}.wav`)
  if (!existsSync(out)) {
    mkdirSync(path.dirname(out), { recursive: true })
    execFileSync('ffmpeg', ['-v', 'error', '-y', '-i', dialogueSource(root), '-af', `adelay=${leadMs}`, '-ar', '48000', '-ac', '1', '-c:a', 'pcm_s16le', out])
  }
  return out
}
