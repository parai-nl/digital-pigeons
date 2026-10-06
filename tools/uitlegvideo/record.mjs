#!/usr/bin/env node
// Neemt één take op voor de uitlegvideo: de echte app, het
// verplichte model (lib/model.mjs) en de echte spraakdiensten, met alleen onze eigen
// vaste invoer — de chatvraag of het verzonnen vergaderfragment door een
// synthetische microfoon. Dit is de enige stap die modelaanroepen doet; alles
// daarna speelt de take af.
//
//   node tools/uitlegvideo/record.mjs --scene chat
//   node tools/uitlegvideo/record.mjs --scene meeting-nonverbal
//   node tools/uitlegvideo/record.mjs --scene meeting-individualist
//
// Opties: --base <url> (standaard http://127.0.0.1:8092, de opnameserver).
// Elke run levert een nieuwe map in takes/; welke take in de video komt staat
// in video.config.json.
import { readFile } from 'node:fs/promises'
import path from 'node:path'
import { parseArgs } from 'node:util'
import { chatScene, meetingScene, QUESTION, SCENES } from './lib/scenes.mjs'
import { recordPolicyError } from './lib/model.mjs'
import { appProvenance } from './lib/provenance.mjs'
import { micInput, openBrowser, preparePage, VIEWPORT } from './lib/session.mjs'
import { buildTake, externalizeAudio, sanitizeTake, saveTake, sseFrames, TAKE_FORMAT } from './lib/take.mjs'

const here = import.meta.dirname
const { values: args } = parseArgs({ options: { scene: { type: 'string' }, base: { type: 'string' } } })
const base = args.base || 'http://127.0.0.1:8092'
const scene = SCENES[args.scene]
if (!scene) throw new Error(`--scene moet een van ${Object.keys(SCENES).join(', ')} zijn`)
const dialogue = JSON.parse(await readFile(path.join(here, 'meeting-audio/meeting-dialogue.json'), 'utf8'))
const dialogueEndMs = Math.round(Math.max(...dialogue.lines.map((l) => (l.start + l.duration) * 1000)))
// De spraakherkenning heeft na het openen even nodig om te verbinden; zonder
// aanloop viel de eerste zin weg. Het fragment
// krijgt daarom stilte vooraf; opnemen en afspelen gebruiken hetzelfde bestand.
const MIC_LEAD_MS = 4000
// Zo lang na het laatste woord mag de duif nog zelf reageren. De detector van
// de signalerende duif had in de proeven tot vijftien seconden nodig; een
// pratende duif die dan nog zwijgt krijgt de knop.
const QUIET_MS = { nonverbal: 28000, individualist: 12000 }
// Met de knop: zo lang na het laatste woord, zodat de laatste regel in het
// verloop staat voordat de duif gevraagd wordt.
const RESPOND_GAP_MS = 1800

// Vooraf: de server moet het verplichte model gebruiken, via het HTTPS-adres
// uit PARAI_LLM_BASE_URL (lib/model.mjs). Een andere stand is geen take voor deze video. Het beleid
// gaat mee in de take als zijn herkomst.
const policy = (await (await fetch(base + '/api/duif/v1/duiven', { signal: AbortSignal.timeout(5000) })).json()).modelPolicy
const refused = recordPolicyError(policy)
if (refused) throw new Error(`de server op ${base} ${refused}`)
const knowledge = await (await fetch(base + '/api/duif/v1/knowledge/status')).json()
const app = await appProvenance(base)

const reports = []
let waiters = []
const onReport = (kind, data) => {
  const r = { kind, ...data }
  if (kind === 'es-message') { try { r.type = JSON.parse(data.data).type } catch { r.type = '' } }
  reports.push(r)
  for (const w of waiters) w()
}
const ctl = {
  mode: 'record',
  base,
  question: QUESTION,
  mark(name, data) { reports.push({ kind: 'mark', t: Date.now(), name, data }); console.log(`  · ${name}`) },
  pace: (key) => new Promise((r) => setTimeout(r, key === 'settle' ? 1500 : 400)),
  since: () => reports.length,
  find: (pred, from = 0) => reports.slice(from).find(pred),
  all: (pred, from = 0) => reports.slice(from).filter(pred),
  waitFor(pred, ms, label, from = 0) {
    return new Promise((resolve, reject) => {
      const check = () => { const hit = reports.slice(from).find(pred); if (hit) { done(); resolve(hit) } }
      const timer = setTimeout(() => { done(); reject(new Error(`wachten op ${label} duurde langer dan ${ms} ms`)) }, ms)
      const done = () => { clearTimeout(timer); waiters = waiters.filter((w) => w !== check) }
      waiters.push(check)
      check()
    })
  },
  sleepUntil: (t) => new Promise((r) => setTimeout(r, Math.max(0, t - Date.now()))),
  pcmEnd: () => Math.max(0, ...reports.filter((r) => r.kind === 'pcm-start').map((r) => r.at + r.duration * 1000)),
}

const stamp = new Date().toISOString().replace(/[-:]/g, '').replace(/\.\d+Z$/, 'Z')
const id = `${args.scene}-${stamp}`
const dir = path.join(here, 'takes', id)
console.log(`take ${id} tegen ${base}`)
const micWav = scene.kind === 'meeting' ? await micInput(here, MIC_LEAD_MS) : undefined
const { browser, context } = await openBrowser({ micWav, viewport: VIEWPORT })
let failure = null
const t0 = Date.now()
try {
  const page = await preparePage(context, { mode: 'record', onReport, freshSince: t0 })
  page.on('pageerror', (err) => reports.push({ kind: 'mark', t: Date.now(), name: 'pageerror', data: { message: String(err.message).slice(0, 300) } }))
  if (scene.kind === 'chat') await chatScene(page, ctl)
  else await meetingScene(page, ctl, { pigeon: scene.pigeon, autonomous: scene.autonomous, respond: scene.respond, timing: { dialogueEndMs: MIC_LEAD_MS + dialogueEndMs, quietMs: QUIET_MS[scene.pigeon], respondGapMs: RESPOND_GAP_MS } })
  await page.waitForTimeout(1500)
} catch (err) {
  failure = String(err.message || err)
  console.error('opname mislukt:', failure)
} finally {
  await browser.close()
}

const take = { format: TAKE_FORMAT, id, scene: args.scene, recordedAt: new Date(t0).toISOString(), ...buildTake(reports, t0) }
const removed = sanitizeTake(take)
const audio = externalizeAudio(take, scene.pigeon ?? 'chat')
const provenance = new Map()
for (const x of take.exchanges) for (const f of sseFrames((x.chunks ?? []).map((c) => c.text).join(''))) if (f.provenance) provenance.set(JSON.stringify(f.provenance), f.provenance)
for (const c of take.channels) for (const m of c.messages) { try { const ev = JSON.parse(m.data ?? '{}'); if (ev.provenance) provenance.set(JSON.stringify(ev.provenance), ev.provenance) } catch { /* audio */ } }
Object.assign(take, {
  app: { ...app, server: base, modelPolicy: policy, knowledge: { kbId: knowledge.kbId, bundleSchema: knowledge.bundleSchema, importedAt: knowledge.importedAt } },
  provenance: [...provenance.values()],
  input: scene.kind === 'chat'
    ? { question: QUESTION, pigeon: 'biological', thenAskOther: 'individualist' }
    : { pigeon: scene.pigeon, autonomous: scene.autonomous, respond: Boolean(scene.respond), microphone: 'meeting-audio/meeting-dialogue.wav', micLeadInMs: MIC_LEAD_MS, dialogue: 'meeting-audio/meeting-dialogue.json', dialogueEndMs },
  selection: 'Kandidaat. De keuze staat in video.config.json; alle takes blijven bewaard.',
  sanitized: { freshVisitor: 'GET /chat/sessions tijdens de opname beperkt tot gesprekken uit deze opname', chatSessionsRemoved: removed, headers: 'alleen content-type en x-duif-*; geen cookies of autorisatie' },
  failure,
})
await saveTake(dir, take, audio)
console.log(JSON.stringify(summary(take), null, 1))
console.log(`opgeslagen in ${path.relative(process.cwd(), dir)}`)
process.exitCode = failure ? 1 : 0

function summary(take) {
  const out = { id: take.id, failure: take.failure, provenance: take.provenance.map((p) => p.model), actions: take.actions.map((a) => `${a.name}@${a.t}`) }
  for (const x of take.exchanges) {
    if (!x.chunks) continue
    const frames = sseFrames(x.chunks.map((c) => c.text).join(''))
    const answer = frames.filter((f) => f.type === 'artifact_delta').map((f) => f.delta).join('')
    const sources = frames.find((f) => f.type === 'sources')?.sources?.length ?? 0
    if (answer) out[`answer#${x.id}`] = { persona: frames.find((f) => f.type === 'artifact')?.persona, sources, text: answer, ms: x.endMs }
  }
  for (const c of take.channels) {
    const types = {}
    let text = ''
    for (const m of c.messages) {
      const ev = m.tts ?? JSON.parse(m.data)
      types[ev.type] = (types[ev.type] ?? 0) + 1
      if (ev.type === 'text') text += ev.delta ?? ''
      if (ev.type === 'transcript' && ev.final) out[`transcript@${m.ms}`] = ev.text
      if (ev.type === 'sound') out[`sound@${m.ms}`] = ev.sound
    }
    out[`channel${c.channel}`] = { types, text }
  }
  return out
}
