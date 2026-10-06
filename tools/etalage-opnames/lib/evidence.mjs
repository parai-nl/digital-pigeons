// Wat een take bewijst, afgeleid uit de ruwe opname zelf: de frames van de
// beurt, de teruglezing van het gesprek, de verzoeken en het vergaderkanaal.
// De samenvatting die de opname in take.checks schreef wordt hier niet
// vertrouwd; kiezen en bouwen gebruiken alleen wat hieruit komt.
import { chatTurn } from './map.mjs'
import { timedFrames } from './sse.mjs'

const parse = (value) => {
  if (value && typeof value === 'object') return value
  if (typeof value !== 'string' || value === '') return null
  try { return JSON.parse(value) } catch { return null }
}

// chatEvidence: het model uit het done-frame van de beurt, of de opgeslagen
// duifantwoorden gelijk zijn aan de gestreamde, en wat er gevraagd is.
export function chatEvidence(take) {
  const turn = [...(take.exchanges ?? [])].reverse().find((x) => x.method === 'POST' && x.path === '/chat/turn' && x.status === 200 && x.chunks)
  if (!turn) return { turn: null }
  const done = timedFrames(turn.chunks).find((f) => f.frame.type === 'done')?.frame
  const streamed = chatTurn(take).messages.filter((m) => m.author !== 'facilitator').map((m) => m.text)
  const readBack = [...take.exchanges].reverse().find((x) => x.method === 'GET' && x.status === 200 && x.path.endsWith('/messages') && x.t > turn.t)
  const rows = parse(readBack?.body)?.messages ?? []
  const stored = rows.filter((r) => r.kind === 'pigeon_answer').slice(-streamed.length).map((r) => r.content)
  const request = parse(turn.requestBody) ?? {}
  const comparison = take.exchanges.find((x) => x.method === 'POST' && x.path === '/chat/comparisons')
  const header = (turn.headers ?? []).find(([k]) => k === 'x-duif-session-id')?.[1]
  return {
    turn,
    sessionId: header ?? done?.sessionId ?? null,
    model: done?.provenance?.model ?? null,
    readBack: Boolean(readBack),
    storedEqualsStreamed: Boolean(readBack) && stored.length === streamed.length && stored.every((text, i) => text === streamed[i]),
    request: { pigeonId: request.pigeonId, message: request.message, lang: request.lang, sessionId: request.sessionId ?? null },
    ...(comparison ? { comparison: { status: comparison.status, ...(parse(comparison.requestBody) ?? {}), session: parse(comparison.body)?.session ?? null } } : {}),
  }
}

// meetingEvidence: hoe de vergadering geopend werd (duif, taal, zelfstandig
// meedoen, volgens verzoek én antwoord van de server), wat de
// spraakherkenning definitief verstond, en of er een fout of een afbreking
// op het kanaal stond.
export function meetingEvidence(take) {
  const open = (take.exchanges ?? []).find((x) => x.method === 'POST' && x.path === '/meeting/sessions' && x.status >= 200 && x.status < 300)
  const request = parse(open?.requestBody) ?? {}
  const response = parse(open?.body) ?? {}
  const events = (take.channels ?? []).flatMap((c) => c.messages.map((m) => m.tts ?? parse(m.data) ?? {}))
  const count = (type) => events.filter((e) => e.type === type).length
  return {
    opened: Boolean(open),
    pigeonId: response.pigeonId ?? request.pigeonId ?? null,
    requestedPigeonId: request.pigeonId ?? null,
    lang: response.lang ?? request.lang ?? null,
    autonomous: response.autonomous ?? response.settings?.autonomous ?? null,
    finalTranscripts: events.filter((e) => e.type === 'transcript' && e.final === true).map((e) => e.text),
    errors: events.filter((e) => e.type === 'error').map((e) => e.reason ?? e.error ?? 'error'),
    stopped: count('stopped'),
    ttsFrames: count('tts_audio'),
    responses: count('response_done'),
  }
}
