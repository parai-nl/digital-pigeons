#!/usr/bin/env node
// Speelt een gekozen take af in de echte frontend en neemt het scherm op
// Er gaat geen verzoek naar een model: de app krijgt de opgenomen
// antwoorden (replay/page-runtime.js). De media — 3D-duif, portretten,
// veldgeluiden — laadt de app wél van de server, zodat een nieuw model of
// portret vanzelf in beeld komt.
//
//   node tools/uitlegvideo/capture.mjs --scene chat [--base http://127.0.0.1:8092]
//
// Levert out/capture/<formaat>/<scene>/: de beelden (JPEG in de videomaat van
// het formaat) met hun tijdstempel, en capture.json met de momenten, de
// geluidsmomenten, de indeling van het vergaderbeeld en de herkomst van de
// server die afspeelde.
import { mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { parseArgs } from 'node:util'
import { chatScene, meetingScene, SCENES } from './lib/scenes.mjs'
import { micInput, openBrowser, preparePage, VISITOR_PROFILE } from './lib/session.mjs'
import { buildPlan, buildTake, loadTake, saveTake, sseFrames, TAKE_FORMAT, timeWindows } from './lib/take.mjs'
import { buildTimeMap } from './lib/timemap.mjs'
import { resolveFormat } from './lib/format.mjs'
import { appProvenance } from './lib/provenance.mjs'

const here = import.meta.dirname
export { SCENES }

// Zoveel eerder dan het verzoek in de take gaat de knop in: drie beelden, zodat
// de klik in beeld zeker vóór de reactie van de duif valt, zoals in de take
// (daar kwam zij 6–9 ms na het verzoek). De klik zelf lag in de take tussen de
// markering van de driver en dat verzoek; 100 ms ervoor valt daar nog binnen.
const PRESS_LEAD_MS = 100

// capture speelt een take af en neemt het scherm op. Met `supplement` mag de
// app leesverzoeken (GET) die de take niet heeft echt doen; die worden dan als
// aanvullende take bewaard (de bronnen en de kennisgraaf achter een chattake
// die daar nog niet langs ging). Zonder `supplement` gaat er niets naar de
// server.
export async function capture(sceneName, { base, config, format, log = console.log, supplement = false } = {}) {
  const fmt = resolveFormat(config, format)
  const view = { ...fmt.viewport, scale: fmt.scale }
  const scale = fmt.scale
  const scene = SCENES[sceneName]
  if (!scene) throw new Error(`onbekende scène ${sceneName}`)
  const take = await loadTake(path.join(here, 'takes', config.takes[sceneName]))
  if (take.failure) throw new Error(`take ${take.id} is mislukt opgenomen: ${take.failure}`)
  const rules = scene.kind === 'chat' ? config.timeMap.chat : config.timeMap.meeting
  const windows = timeWindows(take, rules)
  // In een vergadering wordt niets ingekort vóór de knop: wat er gezegd wordt
  // en wanneer de duif reageert blijft op ware snelheid. Alleen het wachten
  // tussen de knop en haar eerste woord mag korter.
  const click = take.actions.find((a) => a.name === 'respond-click')
  if (scene.kind === 'meeting' && windows.some((w) => !click || w.from < click.t)) throw new Error(`${sceneName}: de tijdkaart zou de vergadering vóór de knop inkorten (${JSON.stringify(windows)}); dat mag niet`)
  const map = buildTimeMap(windows)
  const plan = buildPlan(take, map)
  // Aanvullende takes (alleen GET) horen bij deze take en worden achter zijn
  // verzoeken gezet; ze doen geen modelaanroep en hebben geen tijdkaart nodig.
  const extra = supplement ? [] : (config.supplements?.[sceneName] ?? [])
  let seqStart = Math.max(0, ...take.exchanges.map((x) => x.id))
  for (const id of extra) {
    const more = await loadTake(path.join(here, 'takes', id))
    if (more.supplementFor !== take.id) throw new Error(`${id} hoort niet bij ${take.id}`)
    for (const x of buildPlan(more).http) plan.http.push({ ...x, id: ++seqStart })
  }
  const pacing = config.pacing[scene.kind]
  const out = path.join(here, 'out', 'capture', fmt.name, sceneName)
  await rm(out, { recursive: true, force: true })
  await mkdir(path.join(out, 'frames'), { recursive: true })

  // De klikken in de vergadering gebeuren op de opgenomen momenten, gemeten
  // vanaf het openen van het kanaal en door dezelfde tijdkaart gehaald.
  let timing = null
  if (scene.kind === 'meeting') {
    const channel = take.channels[0]
    const at = (name) => { const a = take.actions.find((x) => x.name === name); return a ? Math.round(map(a.t) - map(channel.t)) : null }
    // Het moment van de knop is het moment waarop de app in de take haar
    // verzoek deed (respond of intervene), een paar beelden eerder: de driver
    // markeerde de klik vóór Playwright hem uitvoerde.
    const turn = click && take.exchanges.find((x) => x.method === 'POST' && /\/(respond|intervene)$/.test(x.path) && x.t >= click.t)
    timing = { pressAtMs: turn ? Math.round(map(turn.t) - map(channel.t)) - PRESS_LEAD_MS : null, endAtMs: at('end-click') }
  }

  const reports = []
  const misses = []
  let waiters = []
  const onReport = (kind, data) => {
    if (kind === 'replay-miss' || kind === 'replay-body-mismatch') misses.push({ kind, ...data })
    reports.push({ kind, ...data })
    for (const w of waiters) w()
  }
  const ctl = {
    mode: 'replay',
    base,
    typingDelay: pacing.typingDelayMs ?? 45,
    question: take.input.question,
    fullTranscript: config.capture?.fullTranscript ?? true,
    // Wat in beeld staat is wat het model schreef: de tekst van het antwoord
    // (zonder de bronnummers, die de app als knopjes tekent) moet gelijk zijn
    // aan de artifact_delta's van die beurt in de take.
    async checkAnswer(page, turn) {
      const turns = take.exchanges.filter((x) => x.path.endsWith('/chat/turn') && x.chunks)
      const expected = sseFrames(turns[turn].chunks.map((c) => c.text).join('')).filter((f) => f.type === 'artifact_delta').map((f) => f.delta).join('')
      const shown = await page.locator('[data-testid="assistant-message"]:not(.guidance-message) [data-testid="assistant-content"]').last().evaluate((el) => {
        const copy = el.cloneNode(true)
        copy.querySelectorAll('.duif-cite-hold sup, .duif-cursor').forEach((n) => n.remove())
        return copy.textContent
      })
      const norm = (t) => t.replace(/\s*\[\d+(?:\s*,\s*\d+)*\]/g, '').replace(/\s+/g, ' ').replace(/\s+([.,;:?!])/g, '$1').trim()
      if (norm(shown) !== norm(expected)) throw new Error(`antwoord ${turn + 1} in beeld wijkt af van de take:\n  beeld: ${norm(shown)}\n  take:  ${norm(expected)}`)
      reports.push({ kind: 'mark', t: Date.now(), name: `answer-check-${turn + 1}`, data: { ok: true } })
    },
    // Aan het eind van een vergadering: wat in beeld staat is wat het model
    // schreef, en de naamsvermelding die de app zelf bij het model toont.
    async checkMeeting(page) {
      const spoken = take.channels.flatMap((c) => c.messages).map((m) => m.tts ?? JSON.parse(m.data)).filter((ev) => ev.type === 'text').map((ev) => ev.delta).join('')
      if (spoken) {
        const shown = await page.locator('[data-testid="latest-answer"] [data-testid="answer-text"]').last().evaluate((el) => {
          const copy = el.cloneNode(true)
          copy.querySelectorAll('.duif-cite-hold sup, .duif-cursor').forEach((n) => n.remove())
          return copy.textContent
        })
        const norm = (t) => t.replace(/\s*\[\d+(?:\s*,\s*\d+)*\]/g, '').replace(/\s+/g, ' ').replace(/\s+([.,;:?!])/g, '$1').trim()
        if (norm(shown) !== norm(spoken)) throw new Error(`bijdrage in beeld wijkt af van de take:\n  beeld: ${norm(shown)}\n  take:  ${norm(spoken)}`)
        reports.push({ kind: 'mark', t: Date.now(), name: 'answer-check-meeting', data: { ok: true } })
      }
      const credit = await page.evaluate(() => [...document.querySelectorAll('[data-testid="media-credit"] p:first-of-type a')].map((a) => a.href))
      reports.push({ kind: 'mark', t: Date.now(), name: 'app-credit', data: { links: credit } })
    },
    mark(name, data) { reports.push({ kind: 'mark', t: Date.now(), name, data }); log(`  · ${name}`) },
    pace: (key) => new Promise((r) => setTimeout(r, pacing[key] ?? 400)),
    since: () => reports.length,
    find: (pred, from = 0) => reports.slice(from).find(pred),
    all: (pred, from = 0) => reports.slice(from).filter(pred),
    waitFor(pred, ms, label, from = 0) {
      return new Promise((resolve, reject) => {
        const check = () => {
          if (misses.length) { done(); reject(new Error(`afspelen wijkt af van de opname: ${JSON.stringify(misses[0])}`)); return }
          const hit = reports.slice(from).find(pred)
          if (hit) { done(); resolve(hit) }
        }
        const timer = setTimeout(() => { done(); reject(new Error(`wachten op ${label} duurde langer dan ${ms} ms`)) }, ms)
        const done = () => { clearTimeout(timer); waiters = waiters.filter((w) => w !== check) }
        waiters.push(check)
        check()
      })
    },
    sleepUntil: (t) => new Promise((r) => setTimeout(r, Math.max(0, t - Date.now()))),
    pcmEnd: () => Math.max(0, ...reports.filter((r) => r.kind === 'pcm-start').map((r) => r.at + r.duration * 1000)),
  }

  // Welke frontend speelt af? Verschilt de bundle van die bij de opname, dan
  // is dat geen fout (na een wissel van model of portret hoort dat zo), maar wel iets om te zien: een
  // andere frontend kan andere verzoeken doen of er anders uitzien.
  const replayApp = await appProvenance(base)
  const served = replayApp.bundleSha256
  const bundle = { replayed: served, recorded: take.app?.bundleSha256 ?? null, match: served === take.app?.bundleSha256 }
  if (!bundle.match) {
    const line = '!'.repeat(78)
    console.warn(`${line}\n! LET OP ${sceneName}: de frontend op ${base} is niet de frontend van take ${take.id}.\n! opname ${bundle.recorded}\n! nu     ${served}\n! Controleer de beelden.\n${line}`)
  }
  // Het veldgeluid klinkt pas als het geladen is. Bij de opname duurde dat
  // (media-play → media-playing); bij afspelen staat het meteen klaar. Dezelfde
  // wachttijd houdt het geluid op het moment waarop het toen klonk.
  const plays = take.probes.filter((p) => p.kind === 'media-play')
  const mediaDelays = plays.map((p) => Math.max(0, (take.probes.find((q) => q.kind === 'media-playing' && q.t >= p.t)?.t ?? p.t) - p.t))
  const micWav = scene.kind === 'meeting' ? micInput(here, take.input.micLeadInMs) : undefined
  const { browser, context } = await openBrowser({ micWav, viewport: view })
  const frames = []
  const writes = []
  const network = []
  let failure = null
  const started = Date.now()
  try {
    const page = await preparePage(context, { mode: 'replay', http: plan.http, channels: plan.channels, onReport, profile: VISITOR_PROFILE, supplement: supplement ? '^/api/duif/v1/graph/' : null, seqStart, mediaDelays })
    // Controle op het netwerk zelf: bij afspelen gaat er geen enkel verzoek
    // naar /api/duif/v1 de deur uit.
    page.on('request', (r) => {
      const u = new URL(r.url())
      if (!u.pathname.startsWith('/api/duif/v1')) return
      if (supplement && r.method() === 'GET' && u.pathname.startsWith('/api/duif/v1/graph/')) return
      network.push(r.method() + ' ' + u.pathname)
    })
    page.on('pageerror', (err) => reports.push({ kind: 'mark', t: Date.now(), name: 'pageerror', data: { message: String(err.message).slice(0, 300) } }))
    const cdp = await context.newCDPSession(page)
    cdp.on('Page.screencastFrame', (f) => {
      const file = `frames/${String(frames.length + 1).padStart(6, '0')}.jpg`
      frames.push({ file, t: Math.round(f.metadata.timestamp * 1000) })
      writes.push(writeFile(path.join(out, file), Buffer.from(f.data, 'base64')))
      void cdp.send('Page.screencastFrameAck', { sessionId: f.sessionId }).catch(() => {})
    })
    await cdp.send('Page.startScreencast', { format: 'jpeg', quality: config.capture?.jpegQuality ?? 92, maxWidth: Math.round(view.width * scale), maxHeight: Math.round(view.height * scale), everyNthFrame: 1 })
    if (scene.kind === 'chat') await chatScene(page, ctl)
    else await meetingScene(page, ctl, { pigeon: scene.pigeon, autonomous: scene.autonomous, respond: scene.respond, timing })
    await page.waitForTimeout(500)
    await cdp.send('Page.stopScreencast')
    if (misses.length) throw new Error(`afspelen wijkt af van de opname: ${JSON.stringify(misses)}`)
    if (network.length) throw new Error(`verzoek naar de server tijdens het afspelen: ${network.slice(0, 5).join(', ')}`)
    const errors = reports.filter((r) => r.name === 'pageerror')
    if (errors.length) throw new Error(`fout in de pagina: ${errors[0].data.message}`)
  } catch (err) {
    failure = String(err.message || err)
  } finally {
    await browser.close()
    await Promise.all(writes)
  }
  const span = (frames.at(-1)?.t ?? 0) - (frames[0]?.t ?? 0)
  const result = {
    scene: sceneName,
    take: take.id,
    format: fmt.name,
    viewport: view,
    output: fmt.out,
    fullTranscript: scene.kind === 'meeting' ? ctl.fullTranscript : undefined,
    startedAt: started,
    timeMap: { windows, savedMs: map.saved },
    bundle,
    app: replayApp,
    mediaDelays,
    network: network.length,
    frames,
    fps: { frames: frames.length, seconds: span / 1000, average: span ? +((frames.length - 1) / (span / 1000)).toFixed(1) : 0 },
    marks: reports.filter((r) => r.kind === 'mark').map(({ kind, ...m }) => m),
    events: reports.filter((r) => ['es-message', 'http-request', 'http-chunk', 'http-end', 'es-construct'].includes(r.kind)).map((r) => ({ kind: r.kind, t: r.t, type: r.type, types: r.types, path: r.path })),
    audio: reports.filter((r) => ['mic-open', 'pcm-start', 'media-play', 'media-playing', 'media-ended'].includes(r.kind)).map(({ kind, ...r }) => ({ kind, ...r })),
    failure,
  }
  await writeFile(path.join(out, 'capture.json'), JSON.stringify(result) + '\n')
  if (failure) throw new Error(`afspelen van ${sceneName} mislukt: ${failure}`)
  if (supplement) {
    // De echte leesverzoeken van deze sessie worden een aanvullende take.
    const live = reports.filter((r) => r.kind.startsWith('http-') && (r.live || reports.some((q) => q.kind === 'http-request' && q.live && q.id === r.id)))
    const extraTake = { format: TAKE_FORMAT, id: `${sceneName}-supplement-${new Date(started).toISOString().replace(/[-:]/g, '').replace(/\.\d+Z$/, 'Z')}`, scene: sceneName, supplementFor: take.id, recordedAt: new Date(started).toISOString(), ...buildTake(live, started), app: { server: base, ...replayApp, knowledge: await (await fetch(base + '/api/duif/v1/knowledge/status')).json() }, input: { note: 'alleen GET-verzoeken naar /api/duif/v1/graph/ tijdens het afspelen van de chattake; geen modelaanroep' } }
    extraTake.channels = []
    await saveTake(path.join(here, 'takes', extraTake.id), extraTake, [])
    result.supplement = extraTake.id
  }
  return result
}

if (import.meta.main) {
  const { values } = parseArgs({ options: { scene: { type: 'string' }, base: { type: 'string' }, format: { type: 'string' }, supplement: { type: 'boolean' } } })
  const config = JSON.parse(await readFile(path.join(here, 'video.config.json'), 'utf8'))
  const names = values.scene ? [values.scene] : Object.keys(SCENES)
  for (const name of names) {
    console.log(`afspelen en opnemen: ${name}`)
    const r = await capture(name, { base: values.base || config.base, config, format: values.format, supplement: values.supplement })
    if (r.supplement) console.log(`  aanvullende take opgeslagen: takes/${r.supplement}`)
    console.log(`  ${r.fps.frames} beelden in ${r.fps.seconds.toFixed(1)} s (${r.fps.average} fps gemiddeld), ${(r.timeMap.savedMs / 1000).toFixed(1)} s wachttijd ingekort`)
  }
}
