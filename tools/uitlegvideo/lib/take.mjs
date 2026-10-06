// Het takeformaat van de uitlegvideo. Een take is één live opname
// van een scène tegen de echte app: elk verzoek aan /api/duif/v1 met zijn
// antwoord (gestreamde antwoorden per brok, met het moment ten opzichte van het
// verzoek), elk bericht op het vergaderkanaal, de klikken van de driver en de
// momenten waarop geluid begon. Alle tijden zijn milliseconden vanaf het begin
// van de take.
//
// De stem van de duif (tts_audio, PCM16) staat niet als base64 in take.json
// maar als wav ernaast; afspelen zet dezelfde bytes terug in hetzelfde bericht.
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { buildTimeMap, capGaps, mergeWindows } from './timemap.mjs'

export const TAKE_FORMAT = 'duiven-uitlegvideo-take/1'

// buildTake maakt van de ruwe meldingen uit de pagina een take. `t0` is het
// begin van de take (vlak voor het laden van de pagina).
export function buildTake(reports, t0) {
  const rel = (t) => Math.round(t - t0)
  const http = new Map()
  const channels = new Map()
  const actions = []
  const probes = []
  for (const r of reports) {
    switch (r.kind) {
      case 'mark':
        actions.push({ name: r.name, t: rel(r.t), ...(r.data ?? {}) })
        break
      case 'http-request':
        http.set(r.id, { id: r.id, method: r.method, path: r.path, requestBody: r.requestBody ?? null, t: rel(r.t), raw: [] })
        break
      case 'http-head': {
        const x = http.get(r.id)
        Object.assign(x, { headMs: r.t - t0 - x.t, status: r.status, statusText: r.statusText, headers: r.headers })
        break
      }
      case 'http-chunk':
        http.get(r.id).raw.push({ ms: Math.round(r.t - t0 - http.get(r.id).t), text: r.text })
        break
      case 'http-end': {
        const x = http.get(r.id)
        x.endMs = Math.round(r.t - t0 - x.t)
        if (r.aborted) x.aborted = r.aborted
        break
      }
      case 'http-failed':
        http.get(r.id).failed = r.error
        break
      case 'es-construct':
        channels.set(r.channel, { channel: r.channel, path: r.path, t: rel(r.t), messages: [] })
        break
      case 'es-open':
        channels.get(r.channel).openMs = Math.round(r.t - t0 - channels.get(r.channel).t)
        break
      case 'es-message':
        channels.get(r.channel).messages.push({ ms: Math.round(r.t - t0 - channels.get(r.channel).t), data: r.data })
        break
      case 'es-error':
        channels.get(r.channel).errors = (channels.get(r.channel).errors ?? 0) + 1
        break
      case 'es-close':
        channels.get(r.channel).closeMs = Math.round(r.t - t0 - channels.get(r.channel).t)
        break
      case 'mic-open':
      case 'pcm-start':
      case 'media-play':
      case 'media-playing':
      case 'media-ended':
        probes.push({ kind: r.kind, t: rel(r.t), ...(r.at ? { at: rel(r.at) } : {}), ...pick(r, ['duration', 'frames', 'sampleRate', 'src', 'audio']) })
        break
      default:
        break
    }
  }
  const exchanges = [...http.values()].map(({ raw, ...x }) => {
    const sse = (x.headers ?? []).some(([k, v]) => k === 'content-type' && v.includes('text/event-stream'))
    return sse ? { ...x, chunks: raw } : { ...x, body: raw.map((c) => c.text).join('') }
  })
  return { exchanges, channels: [...channels.values()], actions, probes }
}

function pick(o, keys) {
  return Object.fromEntries(keys.filter((k) => o[k] !== undefined).map((k) => [k, o[k]]))
}

// frames leest de JSON-frames uit een SSE-tekst.
export function sseFrames(text) {
  return text.split(/\n\n/).flatMap((block) => {
    const data = block.split('\n').filter((l) => l.startsWith('data:')).map((l) => l.slice(5).replace(/^ /, '')).join('\n')
    if (!data) return []
    try { return [JSON.parse(data)] } catch { return [] }
  })
}

// sanitizeTake haalt uit de take wat niet over deze opname gaat: de lijst met
// eerdere gesprekken van het opnameaccount. Alleen gesprekken die in deze
// take zelf voorkomen blijven staan.
export function sanitizeTake(take) {
  const own = new Set()
  for (const x of take.exchanges) {
    for (const [k, v] of x.headers ?? []) if (k === 'x-duif-session-id') own.add(v)
    for (const m of x.path.matchAll(/\/chat\/sessions\/([^/?]+)/g)) own.add(decodeURIComponent(m[1]))
    if (x.path.endsWith('/chat/comparisons') && x.body) {
      try { const id = JSON.parse(x.body)?.session?.id; if (id) own.add(id) } catch { /* geen JSON */ }
    }
  }
  let removed = 0
  for (const x of take.exchanges) {
    if (x.method !== 'GET' || !/\/chat\/sessions$/.test(x.path) || !x.body) continue
    const body = JSON.parse(x.body)
    const before = body.sessions?.length ?? 0
    body.sessions = (body.sessions ?? []).filter((s) => own.has(s.id))
    removed += before - body.sessions.length
    x.body = JSON.stringify(body)
    x.sanitized = 'eerdere gesprekken van het opnameaccount verwijderd'
  }
  return removed
}

// externalizeAudio haalt de PCM van elke tts_audio-frame uit de kanaalberichten
// en geeft per beurt één wav terug. In het bericht blijft een verwijzing staan.
export function externalizeAudio(take, prefix) {
  const files = new Map()
  for (const channel of take.channels) {
    for (const message of channel.messages) {
      let ev
      try { ev = JSON.parse(message.data) } catch { continue }
      if (ev.type !== 'tts_audio' || typeof ev.audio !== 'string') continue
      if (ev.format !== 'pcm16') throw new Error(`onverwacht audioformaat ${ev.format}`)
      const bytes = Buffer.from(ev.audio, 'base64')
      if (Buffer.from(bytes).toString('base64') !== ev.audio) throw new Error('base64 van tts_audio is niet canoniek; terugzetten zou de bytes veranderen')
      const name = `${prefix}-turn${ev.turnId ?? 'x'}.wav`
      if (!files.has(name)) files.set(name, { sampleRate: ev.sampleRate ?? 24000, parts: [], length: 0 })
      const file = files.get(name)
      const { audio, ...rest } = ev
      delete message.data
      message.tts = rest
      message.pcm = { file: name, offset: file.length, length: bytes.length }
      file.parts.push(bytes)
      file.length += bytes.length
    }
  }
  return [...files].map(([name, f]) => ({ name, sampleRate: f.sampleRate, pcm: Buffer.concat(f.parts) }))
}

export function wav(pcm, sampleRate) {
  const header = Buffer.alloc(44)
  header.write('RIFF', 0); header.writeUInt32LE(36 + pcm.length, 4); header.write('WAVEfmt ', 8)
  header.writeUInt32LE(16, 16); header.writeUInt16LE(1, 20); header.writeUInt16LE(1, 22)
  header.writeUInt32LE(sampleRate, 24); header.writeUInt32LE(sampleRate * 2, 28); header.writeUInt16LE(2, 32)
  header.writeUInt16LE(16, 34); header.write('data', 36); header.writeUInt32LE(pcm.length, 40)
  return Buffer.concat([header, pcm])
}

export async function saveTake(dir, take, audioFiles) {
  await mkdir(dir, { recursive: true })
  for (const f of audioFiles) await writeFile(path.join(dir, f.name), wav(f.pcm, f.sampleRate))
  take.audio = audioFiles.map((f) => ({ file: f.name, sampleRate: f.sampleRate, bytes: f.pcm.length, seconds: +(f.pcm.length / 2 / f.sampleRate).toFixed(3) }))
  await writeFile(path.join(dir, 'take.json'), JSON.stringify(take, null, 1) + '\n')
}

// loadTake leest een take en zet de stem terug in de berichten, byte voor byte.
export async function loadTake(dir) {
  const take = JSON.parse(await readFile(path.join(dir, 'take.json'), 'utf8'))
  if (take.format !== TAKE_FORMAT) throw new Error(`${dir}: onbekend takeformaat ${take.format}`)
  const pcm = new Map()
  for (const a of take.audio ?? []) pcm.set(a.file, (await readFile(path.join(dir, a.file))).subarray(44))
  for (const channel of take.channels) {
    for (const message of channel.messages) {
      if (!message.pcm) continue
      const bytes = pcm.get(message.pcm.file).subarray(message.pcm.offset, message.pcm.offset + message.pcm.length)
      message.data = JSON.stringify({ ...message.tts, audio: bytes.toString('base64') })
    }
  }
  take.dir = dir
  return take
}

export function messageType(message) {
  if (message.tts) return message.tts.type
  try { return JSON.parse(message.data).type } catch { return '' }
}

// timeWindows bepaalt uit de take en de regels in video.config.json welke
// stukken opnametijd korter mogen: in de chat de wachttijd vóór het eerste
// woord van de duif en haperingen tijdens het schrijven; in een vergadering
// alleen het wachten ná de knop "Let the pigeon respond" tot haar eerste
// woord (het denken van het model, net als in de chat), en het wachten tussen
// haar tekst en haar stem (de spraaksynthese: de tekst staat al in beeld, de
// stem komt later). Wat er vóór de knop gezegd en gedaan wordt, blijft op ware
// snelheid (capture.mjs bewaakt dat).
export function timeWindows(take, rules) {
  const lists = []
  for (const x of take.exchanges) {
    if (!x.chunks) continue
    const times = [x.t, ...x.chunks.map((c) => x.t + c.ms)]
    // Het eerste woord van de duif (artifact_delta); een beurt zonder
    // duifblok telt vanaf de eerste facilitatortekst. Een kaderzin van de
    // facilitator vóór het duifblok is geen eerste woord van de duif.
    const first = x.chunks.find((c) => c.text.includes('"type":"artifact_delta"')) ?? x.chunks.find((c) => c.text.includes('"type":"text"'))
    const firstAt = first ? x.t + first.ms : Infinity
    if (rules.waitGapMs) lists.push(capGaps(times, rules.waitGapMs, { from: x.t, to: firstAt }))
    if (rules.streamGapMs) lists.push(capGaps(times, rules.streamGapMs, { from: firstAt, to: Infinity }))
  }
  const click = take.actions?.find((a) => a.name === 'respond-click')
  if (rules.respondWaitMs && click) {
    for (const channel of take.channels) {
      const after = channel.messages.map((m) => ({ t: channel.t + m.ms, type: messageType(m) })).filter((m) => m.t >= click.t)
      const first = after.find((m) => m.type === 'text' || m.type === 'tts_audio' || m.type === 'sound')
      if (!first) continue
      const times = [click.t, ...after.filter((m) => m.t <= first.t && ['persona', 'sources', 'text', 'tts_audio', 'sound'].includes(m.type)).map((m) => m.t)]
      lists.push(capGaps(times, rules.respondWaitMs, { from: click.t, to: first.t }))
    }
  }
  if (rules.voiceWaitMs && click) {
    for (const channel of take.channels) {
      const after = channel.messages.map((m) => ({ t: channel.t + m.ms, type: messageType(m) })).filter((m) => m.t >= click.t)
      const text = after.find((m) => m.type === 'text')
      const voice = after.find((m) => m.type === 'tts_audio')
      if (!text || !voice || voice.t <= text.t) continue
      const times = after.filter((m) => m.t >= text.t && m.t <= voice.t && ['sources', 'text', 'text_done', 'tts_audio'].includes(m.type)).map((m) => m.t)
      lists.push(capGaps(times, rules.voiceWaitMs, { from: text.t, to: voice.t }))
    }
  }
  return mergeWindows(...lists)
}

// buildPlan zet de take om in wat de pagina afspeelt: per verzoek het
// antwoord met de tijden door de tijdkaart, per kanaal de berichten.
export function buildPlan(take, map = buildTimeMap([])) {
  const since = (start, t) => Math.max(0, Math.round(map(t) - map(start)))
  const http = take.exchanges.filter((x) => !x.failed && x.status).map((x) => ({
    id: x.id,
    method: x.method,
    path: x.path,
    status: x.status,
    statusText: x.statusText,
    headers: x.headers,
    headMs: since(x.t, x.t + x.headMs),
    // Voor de controle bij afspelen: alleen bij verzoeken die maar één keer
    // horen te komen (een beurt, een vergelijking, een vergadering openen).
    ...(typeof x.requestBody === 'string' && x.method !== 'GET' && !/\/(audio|playback|settings)$/.test(x.path) ? { requestBody: x.requestBody } : {}),
    ...(x.chunks ? { chunks: x.chunks.map((c) => ({ ms: since(x.t, x.t + c.ms), text: c.text })) } : { body: x.body }),
  }))
  const channels = take.channels.map((c) => ({
    path: c.path,
    openMs: since(c.t, c.t + (c.openMs ?? 0)),
    messages: c.messages.map((m) => ({ ms: since(c.t, c.t + m.ms), data: m.data, type: messageType(m) })),
  }))
  return { http, channels }
}
