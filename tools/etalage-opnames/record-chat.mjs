#!/usr/bin/env node
// Neemt de chat voor de etalage op: per vraag en per taal een direct
// gesprek met de Kennispartner en met de Stakeholder, en de vergelijking in
// beide richtingen via precies de route van de hoekknop "Vraag het ook aan de
// andere duif": het brongesprek tot en met die beurt gaat mee
// (POST /chat/comparisons), daarna dezelfde vraag als gewone beurt in het
// nieuwe gesprek. Dezelfde endpoints, bodies en volgorde als de frontend
// (chatStore.svelte.ts: send, beginComparison, #askOtherPigeon).
//
//   node tools/etalage-opnames/record-chat.mjs                 alles, één take per item
//   node tools/etalage-opnames/record-chat.mjs --takes 3       drie rondes per vraag en taal
//   node tools/etalage-opnames/record-chat.mjs --question afschot --lang nl
//   node tools/etalage-opnames/record-chat.mjs --redo <take-id>   hetzelfde item opnieuw
//
// --redo neemt precies dat item opnieuw op: een direct gesprek als nieuw
// direct gesprek, een vergelijking als nieuwe vergelijking vanuit hetzelfde
// brongesprek (dezelfde `basedOn`), zodat een afgebroken antwoord vervangen
// kan worden zonder de rest van de ronde te veranderen.
//
// Eén ronde levert vier takes: twee directe gesprekken en de twee
// vergelijkingen die op precies die gesprekken voortbouwen (`basedOn`).
import { randomBytes } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import path from 'node:path'
import { parseArgs } from 'node:util'
import { createLimiter, createRecorder } from './lib/api.mjs'
import { BASE, LANGS, SITE_TO_MODULE, TOOL_DIR, readEnv, routeSlug, stamp } from './lib/config.mjs'
import { chatEvidence } from './lib/evidence.mjs'
import { chatTurn } from './lib/map.mjs'
import { checkServer, sameBuild } from './lib/server-info.mjs'
import { timedFrames } from './lib/sse.mjs'
import { TAKE_FORMAT, envSecrets, loadTake, saveTake, secretProblems } from './lib/take.mjs'

const { values: args } = parseArgs({
  options: {
    question: { type: 'string', multiple: true },
    lang: { type: 'string', multiple: true },
    takes: { type: 'string', default: '1' },
    base: { type: 'string', default: BASE },
    vragen: { type: 'string', default: path.join(TOOL_DIR, 'vragen.json') },
    redo: { type: 'string', multiple: true },
  },
})
const rounds = Number(args.takes)
if (!Number.isInteger(rounds) || rounds < 1 || rounds > 5) throw new Error('--takes moet 1 t/m 5 zijn')
const vragen = JSON.parse(await readFile(args.vragen, 'utf8'))
const questions = vragen.chat.filter((q) => !args.question || args.question.includes(q.id))
const langs = LANGS.filter((l) => !args.lang || args.lang.includes(l))
if (!questions.length || !langs.length) throw new Error('geen vragen of talen gekozen')
const secrets = envSecrets(readEnv())

const server = await checkServer(args.base)
console.log(`opname tegen ${args.base}: ${server.modelPolicy.model} via ${server.modelPolicy.endpoint}, commit ${server.module.commit.slice(0, 10)}, kennisbank ${server.knowledge.kbId}`)
const limiter = createLimiter(10)
let failures = 0

for (const id of args.redo ?? []) {
  const old = await loadTake(id)
  const q = vragen.chat.find((v) => v.id === old.item.question)
  if (!q || q.text[old.item.lang] !== old.input.text) throw new Error(`${id}: de vraag in vragen.json is niet meer dezelfde; neem een hele ronde op`)
  if (old.item.route.includes('>')) {
    const source = await loadTake(old.item.basedOn)
    if (source.failure) throw new Error(`${id}: het brongesprek ${source.id} is mislukt`)
    await comparisonTake(q, old.item.lang, old.item.from, old.item.pigeon, source)
  } else {
    await directTake(q, old.item.lang, old.item.pigeon)
  }
}
for (const q of args.redo ? [] : questions) {
  for (const lang of langs) {
    for (let round = 1; round <= rounds; round++) {
      const direct = {}
      for (const pigeon of ['kennispartner', 'stakeholder']) direct[pigeon] = await directTake(q, lang, pigeon)
      for (const [from, to] of [['kennispartner', 'stakeholder'], ['stakeholder', 'kennispartner']]) {
        if (direct[from].failure) { console.log(`  ${from}>${to} overgeslagen: het brongesprek mislukte`); continue }
        await comparisonTake(q, lang, from, to, direct[from])
      }
    }
  }
}
process.exitCode = failures ? 1 : 0

async function directTake(q, lang, pigeon) {
  return record(q, lang, pigeon, async (rec, take) => {
    const turn = await rec.stream('POST', '/chat/turn', { sessionId: null, pigeonId: SITE_TO_MODULE[pigeon], message: q.text[lang], lang }, { costly: true })
    take.session = { id: sessionOf(turn) }
    if (take.session.id) await readBack(rec, take.session.id)
    return turn
  })
}

async function comparisonTake(q, lang, from, to, source) {
  const route = `${from}>${to}`
  return record(q, lang, route, async (rec, take) => {
    take.item.from = from
    take.item.basedOn = source.id
    const sourceSession = source.session.id
    // Zoals beginComparison: de opgeslagen id van de vraag opzoeken en de
    // tekst controleren.
    const rows = await readBack(rec, sourceSession)
    const question = rows.filter((m) => m.role === 'user').find((m) => m.content === q.text[lang])
    if (!question?.id) throw new Error('de vraag staat niet in het brongesprek')
    // #askOtherPigeon: nieuw doelgesprek met de vaste context, openen, dan de
    // vraag als gewone beurt.
    const requestId = randomBytes(16).toString('hex')
    const made = await rec.json('POST', '/chat/comparisons', { requestId, sourceSessionId: sourceSession, questionId: question.id, pigeonId: SITE_TO_MODULE[to] }, { costly: true })
    const session = made.body?.session
    if (made.status !== 200 && made.status !== 201) throw new Error(`vergelijking gaf ${made.status}`)
    if (!session?.id || session.pigeonId !== SITE_TO_MODULE[to]) throw new Error('vergelijking leverde geen gesprek van de andere duif')
    take.session = { id: session.id, lang: session.lang, comparison: session.comparison }
    const opened = await readBack(rec, session.id)
    const copied = session.comparison?.contextMessageIds?.length ?? 0
    if (opened.length !== copied) throw new Error(`het nieuwe gesprek heeft ${opened.length} rijen, verwacht ${copied} (alleen de kopie)`)
    const turn = await rec.stream('POST', '/chat/turn', { sessionId: session.id, pigeonId: SITE_TO_MODULE[to], message: q.text[lang], lang: session.lang ?? lang }, { costly: true })
    await readBack(rec, session.id)
    return turn
  })
}

// record draait één take: vastleggen, controleren, opslaan en samenvatten.
async function record(q, lang, route, body) {
  const t0 = Date.now()
  const pigeon = route.includes('>') ? route.split('>')[1] : route
  const id = `chat-${q.id}-${lang}-${routeSlug(route)}-${stamp(new Date(t0))}`
  const rec = createRecorder(args.base, { limiter, t0, log: console.log })
  const take = {
    format: TAKE_FORMAT,
    id,
    kind: 'chat',
    item: { question: q.id, lang, route, pigeon, modulePigeon: SITE_TO_MODULE[pigeon] },
    input: { text: q.text[lang], placeholder: vragen.placeholder === true },
    recordedAt: new Date(t0).toISOString(),
    server,
  }
  console.log(`take ${id}`)
  let turn = null
  try {
    turn = await body(rec, take)
    const now = await checkServer(args.base)
    if (!sameBuild(server, now)) throw new Error('de server is tijdens de opname vervangen door een andere build')
  } catch (err) {
    take.failure = String(err.message || err)
  }
  take.finishedAt = new Date().toISOString()
  take.exchanges = rec.exchanges
  take.failure ??= turnFailure(turn)
  if (!take.failure) {
    try {
      chatTurn(take)
      take.provenance = provenanceOf(turn)
      take.checks = checks(take)
    } catch (err) {
      take.failure = `niet te mappen: ${err.message}`
    }
  }
  const leaks = secretProblems(take, secrets)
  if (leaks.length) throw new Error(`take ${id} niet opgeslagen: ${leaks.join('; ')}`)
  await saveTake(take)
  summarize(take)
  if (take.failure) failures++
  return take
}

// readBack leest een gesprek terug zoals de frontend dat doet (openSession,
// beginComparison) en bewaart het in de take zonder de volledige bronnen: die
// staan al in de stroom van de beurt (of in de take van het brongesprek).
// Per rij blijven id, rol, soort, auteur, tekst en stand staan, per bron het
// nummer en de id's; zo is na te gaan dat de opslag gelijk is aan de stroom.
async function readBack(rec, sessionId) {
  const { x, body } = await rec.json('GET', `/chat/sessions/${encodeURIComponent(sessionId)}/messages`)
  const messages = (body?.messages ?? []).map(compactRow)
  if (x.status === 200) {
    x.body = JSON.stringify({ ...body, messages })
    x.compacted = 'bronnen per rij ingekort tot nummer en id; de volledige bronnen staan in de stroom van de beurt'
  }
  return messages
}

function compactRow(row) {
  const out = {}
  for (const key of ['id', 'role', 'kind', 'persona', 'pigeonId', 'content', 'kbStatus', 'related', 'stopReason', 'partial', 'createdAt', 'provenance', 'comparison']) if (row[key] !== undefined) out[key] = row[key]
  if (Array.isArray(row.sources)) out.sources = row.sources.map((s) => ({ number: s.number, id: s.id, ref: s.ref?.id }))
  return out
}

function sessionOf(x) {
  const header = (x.headers ?? []).find(([k]) => k === 'x-duif-session-id')?.[1]
  if (header) return header
  const done = timedFrames(x.chunks ?? []).find((f) => f.frame.type === 'done')
  return done?.frame.sessionId ?? null
}

// turnFailure: wat een beurt onbruikbaar maakt. Een afwijkende stopreden is
// geen mislukking maar wordt gemeld (zie summarize); dan kies je een andere take.
function turnFailure(x) {
  if (!x) return 'geen beurt'
  if (x.status !== 200) return `beurt gaf HTTP ${x.status}: ${String(x.body ?? '').slice(0, 200)}`
  const frames = timedFrames(x.chunks ?? []).map((f) => f.frame)
  const error = frames.find((f) => f.type === 'error')
  if (error) return `foutframe ${error.reason ?? error.error ?? ''}`.trim()
  if (!frames.some((f) => f.type === 'done')) return 'geen done-frame'
  if (!frames.some((f) => f.type === 'artifact_delta')) return 'de duif schreef niets (geen artifact_delta)'
  return null
}

function provenanceOf(x) {
  return timedFrames(x.chunks ?? []).find((f) => f.frame.type === 'done')?.frame.provenance ?? null
}

// checks: een leesbare samenvatting van wat de opname bewijst, afgeleid uit de
// ruwe opname (lib/evidence.mjs). Kiezen en bouwen leiden het opnieuw af en
// vertrouwen deze samenvatting niet.
function checks(take) {
  const e = chatEvidence(take)
  return { model: e.model, modelMatchesPolicy: e.model === take.server.modelPolicy.model, storedEqualsStreamed: e.storedEqualsStreamed }
}

function summarize(take) {
  if (take.failure) { console.log(`  MISLUKT: ${take.failure}`); return }
  const turn = chatTurn(take)
  const answer = turn.messages.find((m) => m.author !== 'facilitator')
  const facilitator = turn.messages.filter((m) => m.author === 'facilitator').length
  console.log(`  ${answer.kind}, ${answer.sources.length} bronnen, ${answer.text.length} tekens, facilitator ${facilitator}, fasen ${turn.phases.join(' → ')}${turn.notes?.length ? `, let op: ${turn.notes.join('; ')}` : ''}`)
  console.log(`  « ${answer.text.slice(0, 160).replace(/\s+/g, ' ')}${answer.text.length > 160 ? ' …' : ''} »`)
}
