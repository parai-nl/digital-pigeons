// Kleine, synthetische takes voor de tests: dezelfde vorm als een echte
// opname, met verzonnen tekst. Geen modeluitvoer en geen sleutels.
import { MODEL } from '../lib/config.mjs'

// Een verzonnen HTTPS-endpoint: de takes leggen het echte vast.
export const ENDPOINT = 'https://llm.example.invalid'
import { chatTurn } from '../lib/map.mjs'

export const SERVER = {
  base: 'http://127.0.0.1:8094',
  modelPolicy: { model: MODEL, detectionModel: MODEL, endpoint: ENDPOINT },
  knowledge: { kbId: 'Duiven', bundleSchema: 'duiven/3' },
  database: { name: 'opname_kopie', copyOf: 'kennisbank' },
  module: { commit: 'a'.repeat(40), moduleTree: 'b'.repeat(40), dirty: false, paraiCore: { commit: 'c'.repeat(40) }, builtAt: '2026-09-23T14:00:00.000Z', bundleSha256: 'd'.repeat(64) },
}

export const VRAGEN = {
  placeholder: true,
  chat: [{ id: 'q1', text: { nl: 'Vraag één?', en: 'Question one?' } }],
  meeting: [{ id: 's1', speaker: { nl: 'Voorzitter', en: 'Chair' }, text: { nl: 'Zin één.', en: 'Sentence one.' } }],
}

export const SOURCE = {
  number: 1,
  type: 'destillaat',
  id: 'destillaat:1',
  text: 'Samenvatting.',
  grounding: 'corpus_grounded',
  ref: {
    id: 'interview:1',
    citation: 'Doe, J., & Roe, R. (2019). Pigeons in towns: A study. Journal of Birds, 1(2), 3–4. https://doi.org/10.1000/xyz',
    paper: 'doe2019',
    doi: '10.1000/xyz',
    quote: 'Pigeons are found in all towns.',
    passages: [{ id: 'quote:1', text: 'Pigeons are found in all towns and on the squares.' }],
  },
}

const sse = (frame) => `data: ${JSON.stringify(frame)}\n\n`

// chatExchange: een geslaagde POST /chat/turn met kader, duifblok en
// bemiddeling, verdeeld over brokken zoals over de lijn.
export function chatExchange({ persona = 'individualist', kbStatus = 'sources', related = false, deltas = ['Eerste deel [1]. ', 'Tweede deel.'], framing = 'Kaderzin.', mediation = 'Bemiddeling.', stopReason, doneStop = 'natural', sources = [SOURCE], sessionId = 's-1', model = MODEL } = {}) {
  const chunks = [
    { ms: 5, text: sse({ type: 'phase', phase: 'facilitator' }) },
    { ms: 900, text: sse({ type: 'tool', id: 't1', name: 'ask_pigeon' }) + (framing ? sse({ type: 'text', delta: framing }) : '') },
    { ms: 950, text: sse({ type: 'phase', phase: 'pigeon_grounding' }) },
    { ms: 1200, text: sse({ type: 'artifact', id: 'pigeon-1', kind: 'pigeon_answer', persona, kbStatus, ...(related ? { related: true } : {}) }) + sse({ type: 'sources', id: 'pigeon-1', sources }) + sse({ type: 'phase', phase: 'pigeon' }) },
  ]
  let ms = 3000
  for (const delta of deltas) {
    // Het eerste frame van een brok valt half in het vorige brok.
    const frame = sse({ type: 'artifact_delta', id: 'pigeon-1', delta })
    chunks.push({ ms, text: frame.slice(0, 10) })
    chunks.push({ ms: ms + 40, text: frame.slice(10) })
    ms += 200
  }
  chunks.push({ ms, text: sse({ type: 'artifact', id: 'pigeon-1', done: true, kind: 'pigeon_answer', persona, ...(stopReason ? { stopReason } : {}) }) + sse({ type: 'tool_result', id: 't1', name: 'ask_pigeon' }) })
  if (mediation) chunks.push({ ms: ms + 500, text: sse({ type: 'phase', phase: 'mediation' }) + sse({ type: 'text', delta: mediation }) })
  chunks.push({ ms: ms + 900, text: sse({ type: 'done', sessionId, kbStatus, stopReason: doneStop, provenance: { model } }) })
  return { id: 1, method: 'POST', path: '/chat/turn', requestBody: {}, t: 10, status: 200, headers: [['content-type', 'text/event-stream'], ['x-duif-session-id', sessionId]], chunks, endMs: ms + 900 }
}

// serverWith: de server van SERVER, maar met een ander model voor chat en
// detectie (bijvoorbeeld een historische take van een eerder verplicht model).
export const serverWith = (model) => (model === MODEL ? SERVER : { ...SERVER, modelPolicy: { model, detectionModel: model, endpoint: ENDPOINT } })

// chatTake: een chattake zoals record-chat.mjs hem schrijft. Een vergelijking
// kopieert eerst uit `sourceSessionId` (POST /chat/comparisons) en stelt de
// vraag dan in het nieuwe gesprek; daarna wordt het gesprek teruggelezen.
// `model` is het model dat de server meldde en het done-frame noemt.
export function chatTake({ id = 'chat-q1-nl-stakeholder-20260923T100000Z', route = 'stakeholder', lang = 'nl', recordedAt = '2026-09-23T10:00:00.000Z', basedOn, sourceSessionId = 's-0', sessionId = `s-${id}`, exchange, text, stored, model = MODEL, ...rest } = {}) {
  const pigeon = route.includes('>') ? route.split('>')[1] : route
  const modulePigeon = { kennispartner: 'biological', stakeholder: 'individualist' }[pigeon]
  const item = { question: 'q1', lang, route, pigeon, modulePigeon }
  const question = text ?? VRAGEN.chat[0].text[lang]
  const exchanges = []
  if (route.includes('>')) {
    Object.assign(item, { from: route.split('>')[0], basedOn })
    exchanges.push({ id: 1, method: 'POST', path: '/chat/comparisons', requestBody: { requestId: 'r', sourceSessionId, questionId: 'q', pigeonId: modulePigeon }, t: 0, status: 201, headers: [['content-type', 'application/json']], body: JSON.stringify({ session: { id: sessionId, pigeonId: modulePigeon, lang } }) })
  }
  const x = { ...(exchange ?? chatExchange({ persona: modulePigeon, sessionId, model })), requestBody: { sessionId: route.includes('>') ? sessionId : null, pigeonId: modulePigeon, message: question, lang } }
  exchanges.push(x)
  const draft = { id, item, exchanges }
  const answers = stored ?? chatTurn(draft).messages.filter((m) => m.author !== 'facilitator').map((m) => m.text)
  exchanges.push({ id: 3, method: 'GET', path: `/chat/sessions/${sessionId}/messages`, requestBody: null, t: 40000, status: 200, headers: [['content-type', 'application/json']], body: JSON.stringify({ messages: [{ role: 'user', content: question }, ...answers.map((content) => ({ role: 'assistant', kind: 'pigeon_answer', content }))] }) })
  return {
    format: 'duiven-etalage-take/1',
    id,
    kind: 'chat',
    item,
    input: { text: question, placeholder: true },
    recordedAt,
    finishedAt: recordedAt.replace('00.000Z', '30.000Z'),
    server: serverWith(model),
    exchanges,
    provenance: { model },
    checks: { model, modelMatchesPolicy: true, storedEqualsStreamed: true },
    ...rest,
  }
}

// meetingTake: een vergadering waarin de uitspraak eindigt op 8000 ms en het
// venster op 33000 ms sluit.
export function meetingTake({ id = 'meeting-s1-nl-signalerend-20260923T100000Z', pigeon = 'signalerend', lang = 'nl', events = [], probes = [], voice = [], transcript = true, opened, recordedAt = '2026-09-23T10:00:00.000Z', model = MODEL, ...rest } = {}) {
  const modulePigeon = { kennispartner: 'biological', stakeholder: 'individualist', signalerend: 'nonverbal' }[pigeon]
  const request = { pigeonId: modulePigeon, playbackTracking: true, lang, autonomous: true, eagerness: 'normal', cooldownSeconds: 0 }
  const response = { autonomous: true, lang, pigeonId: modulePigeon, sessionId: 'm-1', ...opened }
  const heard = transcript ? [[9000, { type: 'transcript', text: VRAGEN.meeting[0].text[lang], final: true }]] : []
  return {
    format: 'duiven-etalage-take/1',
    id,
    kind: 'meeting',
    item: { statement: 's1', lang, pigeon, modulePigeon },
    input: { speaker: VRAGEN.meeting[0].speaker[lang], text: VRAGEN.meeting[0].text[lang], placeholder: true },
    recordedAt,
    finishedAt: recordedAt.replace('00.000Z', '45.000Z'),
    server: serverWith(model),
    failure: null,
    session: { id: 'm-1', lang, pigeonId: modulePigeon, autonomous: true },
    timeline: { micOpenMs: 1000, utteranceStartMs: 5000, utteranceEndMs: 8000, windowEndMs: 33000 },
    exchanges: [{ id: 1, method: 'POST', path: '/meeting/sessions', requestBody: JSON.stringify(request), t: 900, status: 201, headers: [['content-type', 'application/json']], body: JSON.stringify(response) }],
    channels: [{ channel: 1, path: '/meeting/sessions/m-1/events', t: 1000, messages: [...heard, ...events].sort((a, b) => a[0] - b[0]).map(([t, ev]) => ({ t, data: JSON.stringify(ev) })) }],
    probes,
    transcript: [],
    voice,
    checks: { reaction: null, langMatches: true, autonomous: true },
    ...rest,
  }
}
