#!/usr/bin/env node
// Maakt de uitlegvideo uit de gekozen takes, zonder één
// modelaanroep. Eén opdracht na een wissel van 3D-model of portretten:
// dezelfde gesprekken, de nieuwe duif.
//
//   node tools/uitlegvideo/render.mjs [--format 16x9|4x3|…] [--reuse-capture] [--base <url>] [--out <map>]
//
// De video is de kale app, altijd volledig in beeld, met harde knippen tussen
// de handelingen (storyboard.json). Het enige wat erover ligt is aan het eind
// de verplichte naamsvermelding van het 3D-model. De verzonnen
// vergaderdeelnemers zijn niet te horen (hun woorden staan in het verloop van
// de app); de duif wel: de stem van de Stakeholder en het veldgeluid van de
// signalerende duif.
//
// Stappen: (1) elke scène afspelen in de echte frontend en opnemen
// (capture.mjs), (2) per shot beeld en geluid, (3) aan elkaar met de
// naamsvermelding en de overvloeier van de loop, geluid naar −16 LUFS,
// (4) posterbeeld, WebVTT (de duif) en tijdlijn. Alles komt in out/.
import { execFile } from 'node:child_process'
import { existsSync } from 'node:fs'
import { mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { parseArgs, promisify } from 'node:util'
import { capture } from './capture.mjs'
import { appStyle, renderCards } from './lib/cards.mjs'
import { captionsVtt } from './lib/captions.mjs'
import { creditLines, modelAttribution } from './lib/credits.mjs'
import { frameIndices, pcmRuns, resolveAnchor, spokenCues } from './lib/edl.mjs'
import { concatFiles, ffmpeg, FPS, loudness, mixAudio, silence, videoFromFrames, X264 } from './lib/ffmpeg.mjs'
import { loadTake } from './lib/take.mjs'
import { resolveFormat } from './lib/format.mjs'

const here = import.meta.dirname
const run = promisify(execFile)
const { values: args } = parseArgs({ options: { format: { type: 'string' }, 'reuse-capture': { type: 'boolean' }, base: { type: 'string' }, out: { type: 'string' } } })
const config = JSON.parse(await readFile(path.join(here, 'video.config.json'), 'utf8'))
const board = JSON.parse(await readFile(path.join(here, 'storyboard.json'), 'utf8'))
const base = args.base || config.base
// Het beeldformaat (video.config.json, formats): het standaardformaat schrijft
// uitlegvideo.mp4, een ander formaat uitlegvideo-<formaat>.mp4.
const fmt = resolveFormat(config, args.format)
const size = fmt.out
const OUT = path.join(here, 'out')
const WORK = path.join(OUT, 'work', `video-${fmt.name}`)
// Waar de video, de poster, de ondertitels en de tijdlijn komen; captures en
// tussenbestanden blijven in out/.
const DEST = path.resolve(args.out ?? OUT)
await mkdir(DEST, { recursive: true })
await rm(WORK, { recursive: true, force: true })
await mkdir(WORK, { recursive: true })
const log = (s) => console.log(s)

// 1. Afspelen en opnemen.
const scenes = new Set(board.shots.map((s) => s.scene))
const captures = {}
for (const scene of scenes) {
  const file = path.join(OUT, 'capture', fmt.name, scene, 'capture.json')
  let cap = args['reuse-capture'] && existsSync(file) ? JSON.parse(await readFile(file, 'utf8')) : null
  const transcript = scene.startsWith('meeting') ? (config.capture?.fullTranscript ?? true) : undefined
  if (cap && (cap.take !== config.takes[scene] || cap.failure || cap.viewport?.width !== fmt.viewport.width || cap.viewport?.height !== fmt.viewport.height || cap.fullTranscript !== transcript)) cap = null
  if (!cap) {
    log(`afspelen en opnemen: ${scene} (${fmt.name})`)
    cap = await capture(scene, { base, config, format: fmt.name, log: () => {} })
  }
  log(`  ${scene}: take ${cap.take}, ${cap.fps.frames} beelden, ${cap.fps.average} fps gemiddeld, ${(cap.timeMap.savedMs / 1000).toFixed(1)} s wachttijd ingekort`)
  captures[scene] = cap
}
const takes = {}
for (const scene of scenes) takes[scene] = await loadTake(path.join(here, 'takes', config.takes[scene]))

// Lettertypen en kleuren voor de ondertitels komen uit de app. De
// naamsvermelding komt uit de mediacatalogus die video.config.json noemt, en
// moet overeenkomen met wat de afgespeelde app zelf bij het model vermeldt.
const style = await appStyle(base, takes.chat)
const manifestFile = path.resolve(here, '..', '..', config.credits.manifest)
const manifest = JSON.parse(await readFile(manifestFile, 'utf8'))
const credits = creditLines(manifest)
for (const scene of scenes) {
  const shown = captures[scene].marks.find((m) => m.name === 'app-credit')?.data.links
  if (!shown) continue
  const model = modelAttribution(manifest, takes[scene].input?.pigeon)
  if (!model || shown[0] !== model.source || shown[1] !== model.licenseUrl) throw new Error(`naamsvermelding: de app toont ${JSON.stringify(shown)}, ${path.relative(process.cwd(), manifestFile)} zegt ${model?.source} / ${model?.licenseUrl}. Staat de catalogus in video.config.json gelijk met de server?`)
}
log(`  naamsvermelding uit ${path.relative(process.cwd(), manifestFile)}: ${credits.map((c) => c.text).join(' | ')}`)

const loudnessCache = new Map()
async function levelled(file, target) {
  if (!loudnessCache.has(file)) loudnessCache.set(file, Number((await loudness(file)).input_i))
  return target - loudnessCache.get(file)
}
const mediaCache = new Map()
async function appMedia(url) {
  if (mediaCache.has(url)) return mediaCache.get(url)
  const file = path.join(OUT, 'work', 'media', path.basename(new URL(url).pathname))
  await mkdir(path.dirname(file), { recursive: true })
  const res = await fetch(url)
  if (!res.ok) throw new Error(`media ${url}: ${res.status}`)
  await writeFile(file, Buffer.from(await res.arrayBuffer()))
  mediaCache.set(url, file)
  return file
}

// 2. Shots.
const clips = []
let cursor = 0
for (const [i, shot] of board.shots.entries()) {
  const cap = captures[shot.scene]
  const from = resolveAnchor(cap, shot.from)
  const to = shot.seconds ? from + shot.seconds * 1000 : resolveAnchor(cap, shot.to)
  if (!(to > from)) throw new Error(`${shot.id}: einde ligt niet na begin`)
  const indices = frameIndices(cap.frames, from, to, FPS)
  const clip = { i, id: shot.id, name: `${String(i).padStart(2, '0')}-${shot.id}`, scene: shot.scene, cap, take: takes[shot.scene], from, seconds: indices.length / FPS, start: cursor, indices, spec: shot }
  clip.video = path.join(WORK, `${clip.name}.mp4`)
  clip.audio = path.join(WORK, `${clip.name}.wav`)
  const files = indices.map((k) => path.join(OUT, 'capture', fmt.name, shot.scene, cap.frames[k].file))
  await videoFromFrames({ files, out: clip.video, work: path.join(WORK, clip.name), size })
  const sounds = await pigeonSounds(clip)
  if (sounds.length) await mixAudio({ seconds: clip.seconds, sounds, out: clip.audio })
  else await silence(clip.seconds, clip.audio)
  log(`  ${clip.name}: ${clip.seconds.toFixed(2)} s vanaf ${clip.start.toFixed(2)} s`)
  clips.push(clip)
  cursor += clip.seconds
}
const total = cursor

// 3. Aan elkaar, met ondertitels.
const video = path.join(WORK, 'video.mp4')
await concatFiles(clips.map((c) => c.video), video, WORK, ['-c', 'copy'])
const audio = path.join(WORK, 'audio.wav')
await concatFiles(clips.map((c) => c.audio), audio, WORK, ['-c', 'copy'])
const measured = await loudness(audio)

// De mensen in de vergadering zijn verzonnen en niet te horen; wat zij zeggen
// staat in het verloop van de app zelf. De duif spreekt; haar woorden en haar
// geluid gaan ook in de WebVTT (voor wie met geluid kijkt).
const spoken = []
for (const c of clips.filter((x) => x.take.input?.dialogue)) {
  const within = (from, to) => ({ start: Math.max(c.start, c.start + (from - c.from) / 1000), end: Math.min(c.start + c.seconds, c.start + (to - c.from) / 1000) })
  // De stem van de duif, per zin getimed op de pauzes in haar eigen stem.
  const texts = c.take.channels.flatMap((ch) => ch.messages.map((m) => m.tts ?? JSON.parse(m.data))).filter((ev) => ev.type === 'text').map((ev) => ev.delta).join('')
  for (const [k, runSpan] of pcmRuns(c.cap).entries()) {
    const wav = path.join(here, 'takes', c.take.id, c.take.audio[k].file)
    for (const cue of spokenCues(texts, runSpan.start, runSpan.end - runSpan.start, await pauses(wav))) {
      const { start, end } = within(cue.from, cue.to)
      if (end - start >= 0.5) spoken.push({ speaker: pigeonName(c.take), text: cue.text.replace(/\s*\[\d+(?:\s*,\s*\d+)*\]/g, ''), start, end })
    }
  }
  for (const a of c.cap.audio.filter((x) => x.kind === 'media-playing')) {
    const ended = c.cap.audio.find((x) => x.kind === 'media-ended' && x.t > a.t)
    const { start, end } = within(a.t, ended?.t ?? a.t + 1500)
    if (end - start >= 0.5) spoken.push({ speaker: pigeonName(c.take), text: '[pigeon sound]', start, end })
  }
}
const overlays = []
const jobs = []
// De verplichte naamsvermelding (CC BY-SA 3.0, met de licentie): klein maar
// leesbaar, bovenin het midden van de kop (daar staat in de app niets), in de
// laatste seconden.
const creditPng = path.join(WORK, 'credit.png')
// De licentie met haar adres breekt nooit af ("CC BY-SA" / "3.0"): zij is één
// geheel, de rest van de regel mag omlopen.
const creditParts = credits.map((c) => ({
  text: c.text.slice(0, c.text.length - c.license.length).trimEnd(),
  keep: `${c.license} (${c.licenseUrl.replace(/^https?:\/\//, '').replace(/\/$/, '')})`,
}))
jobs.push({ kind: 'credit', data: { parts: creditParts }, out: creditPng, transparent: true })
const loopFade = board.loopCrossfade ?? 0.6
const creditSpan = { start: Math.max(0, total - loopFade - (board.creditSeconds ?? 3)), end: total }
overlays.push({ png: creditPng, ...creditSpan, fade: 0.25, fadeOut: false })
await renderCards(base, style, jobs, size)
// De loop: het laatste stukje vloeit over in het eerste beeld van de video
// (het startscherm), zodat de herhaling geen sprong is. Het overvloeien ligt
// over alles heen, ook over de naamsvermelding.
const firstFrame = path.join(WORK, 'first-frame.png')
await ffmpeg(['-i', video, '-vf', 'scale=in_color_matrix=bt709:in_range=tv:out_range=pc,format=rgb24', '-frames:v', '1', firstFrame])
overlays.push({ png: firstFrame, start: total - loopFade - 0.05, end: total + 1, fade: loopFade, fadeOut: false })

const TO_RGB = 'scale=in_color_matrix=bt709:in_range=tv:out_range=pc,format=gbrp'
const inputs = ['-i', video, '-i', audio]
const chains = [`[0:v]${TO_RGB}[base]`]
let label = 'base'
overlays.forEach((o, k) => {
  inputs.push('-loop', '1', '-t', total.toFixed(4), '-i', o.png)
  const fades = [`fade=t=in:st=${o.start.toFixed(3)}:d=${o.fade}:alpha=1`]
  if (o.fadeOut !== false) fades.push(`fade=t=out:st=${(o.end - o.fade).toFixed(3)}:d=${o.fade}:alpha=1`)
  chains.push(`[${k + 2}:v]format=rgba,${fades.join(',')}[o${k}]`)
  chains.push(`[${label}][o${k}]overlay=0:0:enable='between(t,${o.start.toFixed(3)},${o.end.toFixed(3)})':format=gbrp[v${k}]`)
  label = `v${k}`
})
chains.push(`[${label}]scale=out_color_matrix=bt709:out_range=tv,format=yuv420p,setparams=colorspace=bt709:color_primaries=bt709:color_trc=bt709:range=tv[vout]`)
chains.push(`[1:a]loudnorm=I=-16:TP=-1.5:LRA=11:measured_I=${measured.input_i}:measured_TP=${measured.input_tp}:measured_LRA=${measured.input_lra}:measured_thresh=${measured.input_thresh}:offset=${measured.target_offset}:linear=true,aresample=48000[a]`)
const final = path.join(DEST, `uitlegvideo${fmt.suffix}.mp4`)
await ffmpeg([
  ...inputs,
  '-filter_complex', chains.join(';'),
  '-map', '[vout]', '-map', '[a]',
  ...X264, '-profile:v', 'high', '-crf', '18', '-preset', 'slow', '-r', String(FPS), '-g', String(FPS * 2),
  '-c:a', 'aac', '-b:a', '192k', '-ac', '2', '-ar', '48000',
  '-t', total.toFixed(4), '-movflags', '+faststart',
  final,
])

// 4. Poster, ondertitels, tijdlijn.
const pc = clips.find((c) => c.id === board.poster.shot)
const posterAt = pc.start + (resolveAnchor(pc.cap, board.poster.at) - pc.from) / 1000
await ffmpeg(['-ss', posterAt.toFixed(3), '-i', final, '-frames:v', '1', path.join(DEST, `uitlegvideo${fmt.suffix}-poster.png`)])
await writeFile(path.join(DEST, `uitlegvideo${fmt.suffix}.vtt`), captionsVtt(spoken))
const after = await loudness(final)
const timeline = {
  format: { name: fmt.name, viewport: fmt.viewport, scale: fmt.scale, out: fmt.out },
  seconds: +total.toFixed(3),
  loudness: { integrated: Number(after.input_i), truePeak: Number(after.input_tp) },
  takes: Object.fromEntries([...scenes].map((s) => [s, captures[s].take])),
  supplements: config.supplements ?? {},
  bundles: Object.fromEntries([...scenes].map((s) => [s, captures[s].bundle])),
  replayApp: Object.fromEntries([...scenes].map((s) => [s, captures[s].app])),
  shots: clips.map((c) => ({ id: c.id, scene: c.scene, start: +c.start.toFixed(3), seconds: +c.seconds.toFixed(3) })),
  spoken: spoken.map((s) => ({ ...s, start: +s.start.toFixed(3), end: +s.end.toFixed(3) })),
  credits,
}
await writeFile(path.join(DEST, `uitlegvideo${fmt.suffix}.timeline.json`), JSON.stringify(timeline, null, 1) + '\n')
log(`  klaar: ${final} (${total.toFixed(1)} s, ${after.input_i} LUFS)`)

// pigeonSounds legt onder een vergadershot alleen wat de duif laat horen: de
// stem uit de take op het moment dat de app hem liet klinken, en het
// veldgeluid dat de app zelf koos. De mensen en de chat zijn stil.
async function pigeonSounds(c) {
  const rel = (t) => (t - c.from) / 1000
  const sounds = []
  for (const [k, span] of pcmRuns(c.cap).entries()) {
    const entry = c.take.audio?.[k]
    if (!entry) throw new Error(`${c.take.id}: meer stemmen in de opname dan in de take`)
    const played = (span.end - span.start) / 1000
    if (Math.abs(played - entry.seconds) > 0.25) throw new Error(`${c.take.id}: stem speelde ${played.toFixed(2)} s, take heeft ${entry.seconds} s`)
    const file = path.join(here, 'takes', c.take.id, entry.file)
    if (rel(span.end) > 0 && rel(span.start) < c.seconds) sounds.push({ file, at: rel(span.start), gain: await levelled(file, -18) })
  }
  for (const a of c.cap.audio.filter((x) => x.kind === 'media-playing')) {
    if (rel(a.t) >= c.seconds || rel(a.t) < -5) continue
    const file = await appMedia(a.src)
    sounds.push({ file, at: rel(a.t), gain: await levelled(file, -20) })
  }
  return sounds
}

// pauses zoekt de stiltes in een opgenomen stem (ms vanaf het begin).
async function pauses(file) {
  const { stderr } = await run('ffmpeg', ['-hide_banner', '-i', file, '-af', 'silencedetect=n=-40dB:d=0.25', '-f', 'null', '-'])
  const starts = [...stderr.matchAll(/silence_start: ([\d.]+)/g)].map((m) => Number(m[1]) * 1000)
  const ends = [...stderr.matchAll(/silence_end: ([\d.]+)/g)].map((m) => Number(m[1]) * 1000)
  return starts.map((s, i) => [s, ends[i] ?? s]).filter(([, e]) => e > 0)
}

function pigeonName(take) {
  return { biological: 'Knowledge Partner', individualist: 'Stakeholder', nonverbal: 'Signaling Pigeon' }[take.input?.pigeon] ?? 'Pigeon'
}
