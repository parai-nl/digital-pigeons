#!/usr/bin/env node
// Neemt de vergaderreacties voor de etalage op: per uitspraak, per duif
// en per taal een verse vergadering met "zelfstandig meedoen" aan, zodat de
// reactie niet afhangt van de volgorde waarin een bezoeker klikt.
//
// De uitspraak gaat het echte pad van de module in: de echte frontend in
// headless Chromium, een synthetische microfoon die de uitgesproken zin
// afspeelt (speak.mjs), de spraakherkenning van de server, de vergadering. Na
// het einde van de zin krijgt de duif een vast venster; wat daarin begint mag
// uitpraten. Er wordt nooit op "Laat de duif reageren" gedrukt: zwijgt de
// duif, dan is dat de opname.
//
//   node tools/etalage-opnames/record-meeting.mjs                         alles, één take per item
//   node tools/etalage-opnames/record-meeting.mjs --takes 3
//   node tools/etalage-opnames/record-meeting.mjs --statement poep --pigeon signalerend --lang nl
//
// De driver volgt de uitlegvideo (tools/uitlegvideo/lib/scenes.mjs en
// lib/session.mjs): dezelfde knoppen,
// dezelfde Chromium-vlaggen, dezelfde aanloop in de microfoon.
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { existsSync, mkdirSync, statSync } from 'node:fs'
import { readFile } from 'node:fs/promises'
import path from 'node:path'
import { parseArgs } from 'node:util'
import { chromium } from 'playwright'
import { BASE, GPU_FLAGS, LANGS, MEETING_PIGEONS, MEETING_WINDOW_MS, MIC_LEAD_MS, SITE_TO_MODULE, TOOL_DIR, readEnv, stamp } from './lib/config.mjs'
import { meetingEvidence } from './lib/evidence.mjs'
import { meetingReaction } from './lib/map.mjs'
import { pageRuntime } from './lib/page-runtime.js'
import { checkServer, sameBuild } from './lib/server-info.mjs'
import { TAKE_FORMAT, envSecrets, saveTake, secretProblems } from './lib/take.mjs'
import { buildMeetingTake } from './lib/meeting-take.mjs'
import { bandFiles } from './lib/media.mjs'

const { values: args } = parseArgs({
  options: {
    statement: { type: 'string', multiple: true },
    pigeon: { type: 'string', multiple: true },
    lang: { type: 'string', multiple: true },
    takes: { type: 'string', default: '1' },
    base: { type: 'string', default: BASE },
    vragen: { type: 'string', default: path.join(TOOL_DIR, 'vragen.json') },
  },
})
const rounds = Number(args.takes)
if (!Number.isInteger(rounds) || rounds < 1 || rounds > 5) throw new Error('--takes moet 1 t/m 5 zijn')
const vragen = JSON.parse(await readFile(args.vragen, 'utf8'))
const spoken = JSON.parse(await readFile(path.join(TOOL_DIR, 'uitspraken/uitspraken.json'), 'utf8'))
const statements = vragen.meeting.filter((s) => !args.statement || args.statement.includes(s.id))
const pigeons = MEETING_PIGEONS.filter((p) => !args.pigeon || args.pigeon.includes(p))
const langs = LANGS.filter((l) => !args.lang || args.lang.includes(l))
if (!statements.length || !pigeons.length || !langs.length) throw new Error('geen uitspraken, duiven of talen gekozen')
for (const s of statements) for (const lang of langs) {
  const line = spoken.lines[`${s.id}-${lang}`]
  if (!line || line.text !== s.text[lang]) throw new Error(`de uitspraak ${s.id}-${lang} is niet (of met een andere tekst) uitgesproken; draai eerst speak.mjs`)
}
const secrets = envSecrets(readEnv())
const bands = bandFiles()
const WORK = path.join(TOOL_DIR, '.work')
mkdirSync(WORK, { recursive: true })

const server = await checkServer(args.base)
console.log(`opname tegen ${args.base}: ${server.modelPolicy.model} via ${server.modelPolicy.endpoint}, commit ${server.module.commit.slice(0, 10)}, kennisbank ${server.knowledge.kbId} (${server.database.name})`)
let failures = 0
for (const s of statements) {
  for (const lang of langs) {
    for (const pigeon of pigeons) {
      for (let round = 1; round <= rounds; round++) await meetingTake(s, lang, pigeon)
    }
  }
}
process.exitCode = failures ? 1 : 0

async function meetingTake(statement, lang, pigeon) {
  const line = spoken.lines[`${statement.id}-${lang}`]
  const audioFile = path.join(TOOL_DIR, 'uitspraken', line.file)
  const mic = micInput(audioFile, `${statement.id}-${lang}`)
  const t0 = Date.now()
  const id = `meeting-${statement.id}-${lang}-${pigeon}-${stamp(new Date(t0))}`
  console.log(`take ${id}`)
  const reports = []
  const marks = []
  const mark = (name, data) => { marks.push({ name, t: Date.now() - t0, ...(data ?? {}) }); console.log(`  · ${name}`) }
  const since = (pred, from = 0) => reports.slice(from).find(pred)
  let failure = null
  const timeline = {}
  await setAccountLocale(lang)
  const browser = await chromium.launch({
    headless: true,
    args: [
      '--mute-audio',
      '--autoplay-policy=no-user-gesture-required',
      '--use-fake-ui-for-media-stream',
      '--use-fake-device-for-media-stream',
      `--use-file-for-fake-audio-capture=${mic}%noloop`,
      // WebGL via de GPU (alleen macOS), zodat de 3D-duif laadt en haar gebaar maakt.
      ...GPU_FLAGS,
    ],
  })
  try {
    const context = await browser.newContext({
      viewport: { width: 1280, height: 800 },
      locale: lang === 'nl' ? 'nl-NL' : 'en-GB',
      colorScheme: 'light',
      reducedMotion: 'no-preference',
    })
    const page = await context.newPage()
    await page.exposeFunction('__etReport', (kind, data) => { reports.push({ kind, ...data }) })
    // De taal van de schil en van de module; de module volgt de schil.
    await page.addInitScript((l) => {
      try { localStorage.setItem('PARAGLIDE_LOCALE', l); localStorage.setItem('duif.ui-language', l) } catch { /* geen opslag */ }
    }, lang)
    await page.addInitScript({ content: `(${pageRuntime.toString()})();` })
    // Paginabezoek- en CSP-meldingen gaan over echte bezoekers.
    await page.route(/\/api\/(pageview|csp-report)$/, (route) => route.fulfill({ status: 204 }))
    page.on('pageerror', (err) => mark('pageerror', { message: String(err.message).slice(0, 300) }))

    mark('load')
    await page.goto(args.base + '/duif', { waitUntil: 'domcontentloaded' })
    await page.locator('.pigeon-choice[data-pigeon="biological"]').waitFor({ timeout: 30000 })
    await page.locator('#duif-tab-meeting').click()
    await page.getByTestId('seat-button').waitFor()
    await page.locator(`.pigeon-choice[data-pigeon="${SITE_TO_MODULE[pigeon]}"]`).click()
    await page.locator(`[data-testid="meeting-view"][data-pigeon="${SITE_TO_MODULE[pigeon]}"]`).waitFor()
    const toggle = page.getByTestId('autonomy-field').getByRole('switch')
    if (!(await toggle.isVisible())) await page.getByTestId('meeting-view').locator('summary').first().click()
    if ((await toggle.getAttribute('aria-checked')) !== 'true') { mark('autonomy-on'); await toggle.click() }
    if ((await toggle.getAttribute('aria-checked')) !== 'true') throw new Error('zelfstandig meedoen staat niet aan')
    await page.waitForTimeout(800)

    const opened = reports.length
    mark('seat')
    await page.getByTestId('seat-button').click()
    await page.getByTestId('live-view').waitFor({ timeout: 30000 })
    const micOpen = await waitFor(() => since((r) => r.kind === 'mic-open', opened), 30000, 'microfoon')
    timeline.micOpenMs = micOpen.t - t0
    timeline.utteranceStartMs = timeline.micOpenMs + MIC_LEAD_MS
    timeline.utteranceEndMs = timeline.utteranceStartMs + line.durationMs
    timeline.windowEndMs = timeline.utteranceEndMs + MEETING_WINDOW_MS
    // Het venster: tot 25 s na de zin. Begint de duif daarin, dan mag zij
    // uitpraten; anders is het stilte.
    const reactionStart = (r) => r.kind === 'es-message' && ['persona', 'sound'].includes(typeOf(r))
    await waitFor(() => since(reactionStart, opened) ?? (Date.now() - t0 >= timeline.windowEndMs ? 'venster' : null), timeline.windowEndMs - (Date.now() - t0) + 5000, 'venster')
    const started = since(reactionStart, opened)
    if (started && started.t - t0 <= timeline.windowEndMs) {
      mark('reaction', { type: typeOf(started) })
      await settle(reports, opened, t0)
      mark('reaction-done')
    } else {
      mark('window-closed')
    }
    await page.waitForTimeout(1500)
    mark('end-click')
    await page.getByTestId('call-control-end').click()
    const dialog = page.getByTestId('end-dialog-confirm')
    await Promise.race([dialog.waitFor({ timeout: 5000 }).then(() => dialog.click()), page.getByTestId('seat-button').waitFor({ timeout: 5000 })]).catch(() => {})
    await page.getByTestId('seat-button').waitFor({ timeout: 15000 })
    mark('ended')
    await page.waitForTimeout(800)
    const now = await checkServer(args.base)
    if (!sameBuild(server, now)) throw new Error('de server is tijdens de opname vervangen door een andere build')
  } catch (err) {
    failure = String(err.message || err)
  } finally {
    await browser.close()
  }

  const { take, files } = buildMeetingTake({ reports, marks, t0, timeline })
  Object.assign(take, {
    format: TAKE_FORMAT,
    id,
    kind: 'meeting',
    item: { statement: statement.id, lang, pigeon, modulePigeon: SITE_TO_MODULE[pigeon] },
    input: {
      speaker: statement.speaker[lang],
      text: statement.text[lang],
      audio: `uitspraken/${line.file}`,
      audioSha256: createHash('sha256').update(await readFile(audioFile)).digest('hex'),
      audioMs: line.durationMs,
      voice: `${line.voice} (${spoken.provider} ${line.model})`,
      micLeadMs: MIC_LEAD_MS,
      windowMs: MEETING_WINDOW_MS,
      placeholder: vragen.placeholder === true,
    },
    recordedAt: new Date(t0).toISOString(),
    finishedAt: new Date().toISOString(),
    server,
    failure,
  })
  // Volgorde in het bestand: kop eerst, dan de ruwe opname.
  const ordered = { format: take.format, id: take.id, kind: take.kind, item: take.item, input: take.input, recordedAt: take.recordedAt, finishedAt: take.finishedAt, server: take.server, failure: take.failure, ...take }
  if (!failure) {
    try {
      // Een leesbare samenvatting; kiezen en bouwen leiden dit opnieuw af uit
      // de ruwe opname (lib/evidence.mjs) en vertrouwen haar niet.
      const reaction = meetingReaction(ordered, { bands })
      const e = meetingEvidence(ordered)
      ordered.checks = { reaction: reaction.type, delayMs: reaction.delayMs ?? null, sessionLang: e.lang, langMatches: e.lang === lang, pigeonMatches: e.pigeonId === SITE_TO_MODULE[pigeon], autonomous: e.autonomous, finalTranscript: e.finalTranscripts.length > 0, channelErrors: e.errors.length, stopped: e.stopped }
      if (e.lang !== lang) ordered.failure = `de vergadering liep in ${e.lang}, niet in ${lang}`
      else if (e.autonomous !== true) ordered.failure = 'zelfstandig meedoen stond niet aan bij het openen'
    } catch (err) {
      ordered.failure = `niet te mappen: ${err.message}`
    }
  }
  const leaks = secretProblems(ordered, secrets)
  if (leaks.length) throw new Error(`take ${id} niet opgeslagen: ${leaks.join('; ')}`)
  await saveTake(ordered, files)
  summarize(ordered)
  if (ordered.failure) failures++
}

// setAccountLocale zet de taal van het opnameaccount, via hetzelfde endpoint
// dat de schil gebruikt als iemand van taal wisselt (PUT /api/auth/profile).
// De schil volgt de taal van het account en herlaadt anders de pagina.
async function setAccountLocale(lang) {
  const me = await (await fetch(`${args.base}/api/auth/me`)).json()
  if (me.locale === lang) return
  const res = await fetch(`${args.base}/api/auth/profile`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: me.name ?? '', locale: lang }) })
  if (!res.ok) throw new Error(`de taal van het opnameaccount kon niet naar ${lang} (HTTP ${res.status})`)
}

function typeOf(r) {
  if (r.type) return r.type
  try { r.type = JSON.parse(r.data).type } catch { r.type = '' }
  return r.type
}

async function waitFor(check, ms, label) {
  const deadline = Date.now() + ms
  for (;;) {
    const hit = check()
    if (hit) return hit
    if (Date.now() > deadline) throw new Error(`wachten op ${label} duurde langer dan ${ms} ms`)
    await new Promise((r) => setTimeout(r, 150))
  }
}

// settle wacht tot wat de duif begon ook af is: een geluid tot zijn einde,
// een bijdrage tot haar laatste woord en haar laatste klank (uitlegvideo).
async function settle(reports, from, t0) {
  const deadline = Date.now() + 180000
  while (Date.now() < deadline) {
    const after = reports.slice(from)
    const sounds = after.filter((r) => r.kind === 'media-play').length
    const ended = after.filter((r) => r.kind === 'media-ended').length
    const personas = after.filter((r) => r.kind === 'es-message' && typeOf(r) === 'persona').length
    const done = after.filter((r) => r.kind === 'es-message' && ['response_done', 'error', 'stopped'].includes(typeOf(r))).length
    const pcmEnd = Math.max(0, ...after.filter((r) => r.kind === 'pcm-start').map((r) => r.at + r.duration * 1000))
    if (sounds <= ended && personas <= done && Date.now() > pcmEnd + 800) return
    await new Promise((r) => setTimeout(r, 250))
  }
  throw new Error(`de reactie was na 180 s nog niet af (${Date.now() - t0} ms in de take)`)
}

// micInput: de uitspraak met stilte ervoor (de spraakherkenning moet eerst
// verbinden) en ruim stilte erna, zodat de microfoon het hele venster door
// stilte levert in plaats van op te houden.
function micInput(mp3, name) {
  const out = path.join(WORK, `mic-${name}-lead${MIC_LEAD_MS}.wav`)
  if (!existsSync(out) || statSync(out).mtimeMs < statSync(mp3).mtimeMs) {
    execFileSync('ffmpeg', ['-v', 'error', '-y', '-i', mp3, '-af', `adelay=${MIC_LEAD_MS}:all=1,apad=pad_dur=240`, '-ar', '48000', '-ac', '1', '-c:a', 'pcm_s16le', out])
  }
  return out
}

function summarize(take) {
  if (take.failure) { console.log(`  MISLUKT: ${take.failure}`); return }
  const r = meetingReaction(take, { bands })
  const heard = take.transcript?.map((s) => s.text).join(' ') || '(niets verstaan)'
  console.log(`  verstaan: « ${heard} »`)
  if (r.type === 'speech') console.log(`  spraak na ${r.delayMs} ms, ${r.sources.length} bronnen, ${Math.round((r.audioMs ?? 0) / 100) / 10} s stem, gebaar ${r.gesture ?? '-'}: « ${r.text.slice(0, 160)}${r.text.length > 160 ? ' …' : ''} »`)
  else if (r.type === 'signal') console.log(`  signaal na ${r.delayMs} ms: ${r.sound}, gebaar ${r.gesture ?? '-'}`)
  else console.log('  stilte')
}
