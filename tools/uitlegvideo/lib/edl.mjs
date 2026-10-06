// De montagelijst: welke stukken van welke opname, in welke volgorde,
// en wanneer welke ondertitel. Alles hier is rekenwerk op de capture.json van
// een afspeelsessie; ffmpeg komt pas in lib/ffmpeg.mjs.

// resolveAnchor zet een ankertekst om in een tijdstip (ms, klok van de
// capture). Vormen:
//   mark:send+0.4          het eerste moment `send` plus 0,4 s
//   mark:select-nonverbal#2  het tweede moment met die naam
//   mic+11.2               11,2 s nadat de microfoon openging
//   audio:media-playing#2  het tweede veldgeluid dat begon te klinken
//   pcmEnd+1.2             1,2 s na het laatste geplande stukje duifstem
//   event:persona          het eerste kanaalbericht van dat type
//   chunk:artifact_delta#2 het tweede streambrok (chat) met zo'n frame; het
//                          moment dat de app die tekst kreeg
export function resolveAnchor(capture, anchor) {
  const m = /^([a-zA-Z]+)(?::([a-zA-Z0-9_-]+))?(?:#(\d+))?([+-]\d+(?:\.\d+)?)?$/.exec(anchor.trim())
  if (!m) throw new Error(`anker niet te lezen: ${anchor}`)
  const [, kind, name, nth, offset] = m
  const k = Number(nth ?? 1) - 1
  const off = Number(offset ?? 0) * 1000
  let t
  if (kind === 'mark') t = capture.marks.filter((x) => x.name === name)[k]?.t
  else if (kind === 'mic') t = capture.audio.find((x) => x.kind === 'mic-open')?.t
  else if (kind === 'audio') { const a = capture.audio.filter((x) => x.kind === name)[k]; t = a?.at ?? a?.t }
  else if (kind === 'pcmEnd') t = pcmSpan(capture)?.end
  else if (kind === 'pcmStart') t = pcmSpan(capture)?.start
  else if (kind === 'event') t = capture.events.filter((x) => x.kind === 'es-message' && x.type === name)[k]?.t
  else if (kind === 'chunk') t = capture.events.filter((x) => x.kind === 'http-chunk' && x.types?.includes(name))[k]?.t
  else if (kind === 'start') t = capture.frames[0]?.t
  if (t == null) throw new Error(`anker ${anchor} komt niet voor in de opname van ${capture.scene}`)
  return t + off
}

// pcmSpan: van het eerste tot het laatste geplande stukje duifstem.
export function pcmSpan(capture) {
  const pcm = capture.audio.filter((x) => x.kind === 'pcm-start')
  if (!pcm.length) return null
  return { start: Math.min(...pcm.map((p) => p.at)), end: Math.max(...pcm.map((p) => p.at + p.duration * 1000)) }
}

// frameIndices kiest voor elk beeld van de uitvoer (fps vast) het laatste
// opgenomen beeld dat op dat moment op het scherm stond. De screencast levert
// alleen een beeld als er iets verandert; dan blijft het vorige staan.
export function frameIndices(frames, from, to, fps) {
  const count = Math.max(1, Math.round(((to - from) / 1000) * fps))
  const out = []
  let i = 0
  for (let k = 0; k < count; k++) {
    const t = from + (k * 1000) / fps
    while (i + 1 < frames.length && frames[i + 1].t <= t) i++
    out.push(i)
  }
  return out
}

// pcmRuns bundelt de stukjes duifstem die de app aansluitend inplande tot
// één uiting per beurt.
export function pcmRuns(capture) {
  const chunks = capture.audio.filter((a) => a.kind === 'pcm-start').sort((a, b) => a.at - b.at)
  const runs = []
  for (const ch of chunks) {
    const end = ch.at + ch.duration * 1000
    const last = runs.at(-1)
    if (last && ch.at - last.end < 1000) last.end = Math.max(last.end, end)
    else runs.push({ start: ch.at, end })
  }
  return runs
}

// --- ondertitels ---------------------------------------------------------------

// sentences knipt tekst in zinnen. Met `mergeShort` gaat een zin van een
// paar woorden ("Right.") samen met de volgende: te kort om los te lezen.
export function sentences(text, mergeShort = false) {
  const out = []
  for (const part of String(text).split(/(?<=[.?!])\s+/).map((p) => p.trim()).filter(Boolean)) {
    if (mergeShort && out.length && out.at(-1).length < 16) out[out.length - 1] += ' ' + part
    else out.push(part)
  }
  return out
}

// timeSentences legt zinnen op een stuk spraak van `duration` ms dat op
// `start` begint. `pauses` zijn de stiltes in die spraak ([van, tot] in ms
// vanaf het begin, gemeten in de opname zelf); zijn er genoeg, dan vallen de
// zinsgrenzen op de langste pauzes, anders op de verhouding van het aantal
// tekens. Stilte vooraan en achteraan telt niet als pauze.
export function timeSentences(parts, start, duration, pauses = []) {
  const n = parts.length
  if (!n) return []
  const inner = pauses.filter(([a, b]) => a > 100 && b < duration - 100 && b > a)
  let bounds
  if (inner.length >= n - 1) {
    bounds = [...inner].sort((x, y) => (y[1] - y[0]) - (x[1] - x[0])).slice(0, n - 1).sort((x, y) => x[0] - y[0])
  } else {
    const total = parts.reduce((sum, p) => sum + p.length, 0)
    let t = 0
    bounds = parts.slice(0, -1).map((p) => { t += (duration * p.length) / total; return [t, t] })
  }
  return parts.map((text, i) => ({
    text,
    from: start + (i === 0 ? 0 : bounds[i - 1][1]),
    to: start + (i === n - 1 ? duration : bounds[i][0]),
  }))
}

// spokenCues verdeelt de uitgesproken tekst van de duif over haar stem: één
// ondertitel per zin, grenzen op de pauzes in haar stem.
export function spokenCues(text, start, duration, pauses = []) {
  return timeSentences(sentences(text), start, duration, pauses)
}
