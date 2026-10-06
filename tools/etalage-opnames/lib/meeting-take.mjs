// Van de ruwe meldingen uit de pagina (lib/page-runtime.js) naar een
// vergadertake: de verzoeken van de module, het vergaderkanaal, de momenten
// van microfoon, geluid en gebaar. Alle tijden zijn milliseconden vanaf het
// begin van de take. De stem van de duif (tts_audio) gaat niet als base64 in
// take.json maar als stem.mp3 ernaast, in de vorm die de etalage afspeelt
// (mono, 64 kbit/s, bit-exact); de lengte en de hash van de oorspronkelijke
// PCM staan in take.voice. build.mjs kopieert dat bestand ongewijzigd, zodat
// git het één keer bewaart.
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import os from 'node:os'
import path from 'node:path'

export function buildMeetingTake({ reports, marks, t0, timeline }) {
  const rel = (t) => Math.round(t - t0)
  const http = new Map()
  const channels = new Map()
  const probes = []
  for (const r of reports) {
    switch (r.kind) {
      case 'http-request':
        http.set(r.id, { id: r.id, method: r.method, path: r.path.replace(/^\/api\/duif\/v1/, ''), requestBody: r.requestBody ?? null, t: rel(r.t), raw: [] })
        break
      case 'http-head': {
        const x = http.get(r.id)
        if (x) Object.assign(x, { headMs: rel(r.t) - x.t, status: r.status, headers: r.headers })
        break
      }
      case 'http-chunk': {
        const x = http.get(r.id)
        if (x) x.raw.push({ ms: rel(r.t) - x.t, text: r.text })
        break
      }
      case 'http-end': {
        const x = http.get(r.id)
        if (x) { x.endMs = rel(r.t) - x.t; if (r.aborted) x.aborted = r.aborted }
        break
      }
      case 'http-failed': {
        const x = http.get(r.id)
        if (x) x.failed = r.error
        break
      }
      case 'es-construct':
        channels.set(r.channel, { channel: r.channel, path: r.path.replace(/^\/api\/duif\/v1/, ''), t: rel(r.t), messages: [] })
        break
      case 'es-open':
        if (channels.has(r.channel)) channels.get(r.channel).openMs = rel(r.t)
        break
      case 'es-message':
        channels.get(r.channel)?.messages.push({ t: rel(r.t), data: r.data })
        break
      case 'es-error': {
        const c = channels.get(r.channel)
        if (c) c.errors = (c.errors ?? 0) + 1
        break
      }
      case 'es-close':
        if (channels.has(r.channel)) channels.get(r.channel).closeMs = rel(r.t)
        break
      case 'mic-open':
      case 'pcm-start':
      case 'media-play':
      case 'media-playing':
      case 'media-ended':
      case 'gesture':
        probes.push({ kind: r.kind, t: rel(r.t), ...(r.at ? { at: rel(r.at) } : {}), ...pick(r, ['duration', 'sampleRate', 'src', 'clip', 'audio']) })
        break
      default:
        break
    }
  }

  // Verzoeken. De microfoonblokken (POST …/audio, meerdere per seconde) worden
  // geteld in plaats van één voor één bewaard; hun inhoud is binair en hoort
  // niet in een take. De chatgeschiedenis die de app bij het laden ophaalt is
  // die van het opnameaccount en hoort niet bij deze vergadering.
  const exchanges = []
  const upload = { requests: 0, statuses: {}, firstMs: null, lastMs: null }
  for (const { raw, ...x } of http.values()) {
    if (x.method === 'POST' && /\/meeting\/sessions\/[^/]+\/audio$/.test(x.path)) {
      upload.requests++
      upload.statuses[x.status ?? 'geen'] = (upload.statuses[x.status ?? 'geen'] ?? 0) + 1
      upload.firstMs ??= x.t
      upload.lastMs = x.t
      continue
    }
    const text = raw.map((c) => c.text).join('')
    if (x.path.startsWith('/chat/')) exchanges.push({ ...x, bodyOmitted: 'chatgeschiedenis van het opnameaccount; hoort niet bij deze vergadering' })
    else exchanges.push({ ...x, body: text })
  }

  const open = exchanges.find((x) => x.method === 'POST' && x.path === '/meeting/sessions' && x.status >= 200 && x.status < 300)
  const request = parse(open?.requestBody)
  const response = parse(open?.body)
  const session = open ? {
    id: response?.sessionId ?? response?.id ?? null,
    lang: response?.lang ?? request?.lang ?? null,
    pigeonId: response?.pigeonId ?? request?.pigeonId ?? null,
    autonomous: response?.autonomous ?? response?.settings?.autonomous ?? null,
    eagerness: response?.eagerness ?? response?.settings?.eagerness ?? request?.eagerness ?? null,
    cooldownSeconds: response?.cooldownSeconds ?? response?.settings?.cooldownSeconds ?? request?.cooldownSeconds ?? null,
  } : null

  // Het vergaderkanaal van deze sessie, met de stem eruit gehaald.
  const events = [...channels.values()].filter((c) => /\/meeting\/sessions\/[^/]+\/events/.test(c.path))
  const files = []
  const voices = new Map()
  for (const channel of events) {
    for (const message of channel.messages) {
      const ev = parse(message.data)
      if (ev?.type !== 'tts_audio' || typeof ev.audio !== 'string') continue
      const bytes = Buffer.from(ev.audio, 'base64')
      const key = `${ev.turnId ?? 'x'}:${ev.format}`
      if (!voices.has(key)) voices.set(key, { turnId: ev.turnId ?? null, format: ev.format, sampleRate: ev.sampleRate ?? 24000, parts: [], length: 0 })
      const voice = voices.get(key)
      const { audio, ...meta } = ev
      delete message.data
      message.tts = meta
      message.audio = { turn: voice.turnId, offset: voice.length, length: bytes.length }
      voice.parts.push(bytes)
      voice.length += bytes.length
    }
  }
  const voice = []
  let n = 0
  for (const v of voices.values()) {
    const bytes = Buffer.concat(v.parts)
    const name = `stem${voices.size > 1 ? `-${++n}` : ''}.mp3`
    files.push({ name, data: v.format === 'pcm16' ? voiceMp3(bytes, v.sampleRate) : bytes })
    voice.push({
      file: name,
      turnId: v.turnId,
      format: v.format,
      sampleRate: v.sampleRate,
      bytes: bytes.length,
      sha256: createHash('sha256').update(bytes).digest('hex'),
      ms: v.format === 'pcm16' ? Math.round(bytes.length / 2 / v.sampleRate * 1000) : null,
      ...(v.format === 'pcm16' ? { encoded: VOICE_ENCODING } : {}),
    })
  }

  const transcript = []
  for (const channel of events) {
    for (const message of channel.messages) {
      const ev = message.tts ?? parse(message.data)
      if (ev?.type === 'transcript' && ev.final) transcript.push({ t: message.t, text: ev.text, ...(ev.speaker ? { speaker: ev.speaker } : {}) })
    }
  }

  return {
    take: { session, timeline, actions: marks, exchanges, audioUpload: upload, channels: events, probes, transcript, voice },
    files,
  }
}

export const VOICE_ENCODING = 'mp3, mono, 64 kbit/s (libmp3lame, bit-exact)'

// voiceMp3 zet PCM16 mono om naar de mp3 van de etalage. Zonder metadata en
// bit-exact: dezelfde PCM geeft dezelfde bytes.
export function voiceMp3(pcm, sampleRate) {
  const dir = mkdtempSync(path.join(os.tmpdir(), 'etalage-'))
  try {
    const raw = path.join(dir, 'in.raw')
    const out = path.join(dir, 'out.mp3')
    writeFileSync(raw, pcm)
    execFileSync('ffmpeg', ['-v', 'error', '-f', 's16le', '-ar', String(sampleRate), '-ac', '1', '-i', raw, '-map_metadata', '-1', '-fflags', '+bitexact', '-flags:a', '+bitexact', '-codec:a', 'libmp3lame', '-b:a', '64k', out])
    return readFileSync(out)
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
}

// mediaMs: de speelduur van een audiobestand volgens ffprobe.
export function mediaMs(file) {
  return Math.round(Number(execFileSync('ffprobe', ['-v', 'error', '-show_entries', 'format=duration', '-of', 'csv=p=0', file], { encoding: 'utf8' }).trim()) * 1000)
}

function parse(text) {
  if (typeof text !== 'string' || text === '') return null
  try { return JSON.parse(text) } catch { return null }
}

function pick(o, keys) {
  return Object.fromEntries(keys.filter((k) => o[k] !== undefined).map((k) => [k, o[k]]))
}
