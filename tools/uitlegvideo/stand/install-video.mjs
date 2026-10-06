#!/usr/bin/env node
// Zet de uitlegvideo in één stap op zijn plek: de MP4, een
// poster en eventueel Engelse ondertitels gaan naar de publieke mediamap van de
// module, en het manifest krijgt de nieuwe hashes, bytes, afmetingen en duur.
// De build controleert daarna hetzelfde pakket opnieuw (media-build.ts).
//
//   node tools/uitlegvideo/stand/install-video.mjs <video.mp4> [--poster beeld.png|jpg] [--poster-at 2]
//        [--vtt ondertitels.vtt] [--approve "namen" --date 2026-09-30]
//   node tools/uitlegvideo/stand/install-video.mjs --placeholder
//
// Zonder --approve blijft het pakket `temporary`: het draait gewoon, maar
// draagt geen beoordeling die niemand gedaan heeft. Zonder --poster haalt het
// script een beeld uit de video (seconde --poster-at, standaard 2). Zonder
// --vtt verdwijnen de oude ondertitels: die horen bij een andere video.
// --placeholder maakt de neutrale placeholder van vijf seconden opnieuw.
import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { constants, copyFileSync, existsSync, lstatSync, mkdtempSync, openSync, readFileSync, readSync, closeSync, renameSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../..')
const media = path.join(repo, 'module/duif/frontend/public/media')
const manifestPath = path.join(repo, 'module/duif/config/media-manifest.v1.json')
const FFMPEG = process.env.FFMPEG ?? 'ffmpeg'
const FFPROBE = process.env.FFPROBE ?? 'ffprobe'
// Dezelfde grenzen als media-build.ts; wie hier slaagt, slaagt daar ook.
const MAX_VIDEO = 48 * 1024 * 1024
const MAX_POSTER = 2 * 1024 * 1024
const MAX_CAPTIONS = 256 * 1024
const MIN_SIZE = [320, 180]
const MAX_SIZE = [3840, 2160]
const NAMES = { video: 'uitlegvideo.mp4', poster: 'uitlegvideo-poster', captions: 'uitlegvideo-en.vtt' }
// Achtergrond van de placeholder: --duif-ground uit app.css.
const PLACEHOLDER_GROUND = '0xf5f7f6'
const PLACEHOLDER_INK = '0x252e2b'

function fail(message) { console.error(`install-video: ${message}`); process.exit(1) }
function option(name) {
  const index = process.argv.indexOf(name)
  if (index < 0) return undefined
  const value = process.argv[index + 1]
  if (!value || value.startsWith('--')) fail(`${name} needs a value`)
  return value
}
function checkSize(label, width, height) {
  if (!Number.isInteger(width) || !Number.isInteger(height) || width < MIN_SIZE[0] || height < MIN_SIZE[1] || width > MAX_SIZE[0] || height > MAX_SIZE[1]) fail(`${label} is ${width}×${height}; it must be between ${MIN_SIZE.join('×')} and ${MAX_SIZE.join('×')}`)
}
// Eerst naast het doel schrijven en dan hernoemen, zoals prepare-meeting-media:
// een afgebroken kopie laat nooit een half bestand onder de echte naam achter.
function install(from, target) {
  const temporary = `${target}.installing-${process.pid}`
  try {
    copyFileSync(from, temporary, constants.COPYFILE_EXCL)
    renameSync(temporary, target)
  } finally { rmSync(temporary, { force: true }) }
}
const digest = (file) => createHash('sha256').update(readFileSync(file)).digest('hex')

function probe(file) {
  const out = execFileSync(FFPROBE, ['-v', 'error', '-show_streams', '-show_format', '-of', 'json', file], { encoding: 'utf8', maxBuffer: 4 * 1024 * 1024 })
  return JSON.parse(out)
}

// Staat de moov-box vóór de mdat-box, dan kan de browser met preload="metadata"
// de duur en het eerste beeld tonen zonder de hele video op te halen.
function faststart(file) {
  const handle = openSync(file, 'r')
  try {
    const size = statSync(file).size
    const header = Buffer.alloc(16)
    for (let offset = 0; offset + 8 <= size;) {
      readSync(handle, header, 0, 16, offset)
      let length = header.readUInt32BE(0)
      const type = header.toString('latin1', 4, 8)
      if (type === 'moov') return true
      if (type === 'mdat') return false
      if (length === 1) length = Number(header.readBigUInt64BE(8))
      if (length === 0 || length < 8) return false
      offset += length
    }
    return false
  } finally { closeSync(handle) }
}

function checkVideo(file) {
  const info = probe(file)
  const video = info.streams.find(stream => stream.codec_type === 'video')
  const audio = info.streams.find(stream => stream.codec_type === 'audio')
  // H.264 in yuv420p met AAC speelt in Chrome, Safari en Firefox, ook vanaf
  // file:// op de standlaptop.
  if (!video || video.codec_name !== 'h264' || video.pix_fmt !== 'yuv420p') fail('video must be H.264 (yuv420p)')
  // Vierkante pixels, dezelfde eis als check-media --probe
  // (module/duif/frontend/scripts/explainer-probe.ts): de help reserveert het
  // vlak met breedte / hoogte, en met een andere SAR toont de browser een
  // andere verhouding (1440×1080 met SAR 4:3 is beeldvullend 16:9). Een
  // ontbrekende SAR telt ook als fout.
  if (video.sample_aspect_ratio !== '1:1') fail(`video must have square pixels (sample aspect ratio 1:1), found ${video.sample_aspect_ratio ?? 'none'}`)
  if (!audio || audio.codec_name !== 'aac') fail('video must carry an AAC sound track (silent is fine, absent is not)')
  const durationMs = Math.round(Number(info.format.duration) * 1000)
  if (!Number.isFinite(durationMs) || durationMs < 1000 || durationMs > 180000) fail('duration must be 1–180 s')
  checkSize('video', video.width, video.height)
  return { width: video.width, height: video.height, durationMs }
}

function makePlaceholder(directory) {
  const font = ['/System/Library/Fonts/Supplemental/Georgia.ttf', '/usr/share/fonts/truetype/dejavu/DejaVuSerif.ttf'].find(existsSync)
  const text = `drawtext=text='Explainer video — coming soon':fontcolor=${PLACEHOLDER_INK}:fontsize=64:x=(w-text_w)/2:y=(h-text_h)/2${font ? `:fontfile=${font}` : ''}`
  const video = path.join(directory, 'placeholder.mp4')
  const poster = path.join(directory, 'placeholder.png')
  const captions = path.join(directory, 'placeholder.vtt')
  execFileSync(FFMPEG, ['-v', 'error', '-y', '-f', 'lavfi', '-i', `color=c=${PLACEHOLDER_GROUND}:s=1920x1080:r=25:d=5`, '-f', 'lavfi', '-i', 'anullsrc=channel_layout=stereo:sample_rate=48000',
    '-vf', text, '-t', '5', '-c:v', 'libx264', '-pix_fmt', 'yuv420p', '-preset', 'slow', '-crf', '28', '-c:a', 'aac', '-b:a', '64k', '-movflags', '+faststart', '-map_metadata', '-1', video])
  execFileSync(FFMPEG, ['-v', 'error', '-y', '-i', video, '-frames:v', '1', poster])
  writeFileSync(captions, 'WEBVTT\n\n00:00:00.000 --> 00:00:05.000\nExplainer video — coming soon\n')
  return { video, poster, captions }
}

const placeholder = process.argv.includes('--placeholder')
const scratch = mkdtempSync(path.join(tmpdir(), 'duif-uitlegvideo-'))
// Ook fail() ruimt de werkmap op: process.exit slaat een finally over.
process.on('exit', () => rmSync(scratch, { recursive: true, force: true }))
{
  const source = placeholder ? makePlaceholder(scratch) : { video: process.argv[2], poster: option('--poster'), captions: option('--vtt') }
  if (!source.video || source.video.startsWith('--') || !existsSync(source.video)) fail('usage: install-video.mjs <video.mp4> [--poster file] [--poster-at seconds] [--vtt file] [--approve "names" --date YYYY-MM-DD] | --placeholder')
  for (const file of [source.video, source.poster, source.captions].filter(Boolean)) {
    if (!existsSync(file) || lstatSync(file).isSymbolicLink() || !lstatSync(file).isFile()) fail(`not a regular file: ${file}`)
  }
  const approvedBy = option('--approve')
  const approvedAt = option('--date')
  if (Boolean(approvedBy) !== Boolean(approvedAt) || (approvedAt && !/^\d{4}-\d{2}-\d{2}$/.test(approvedAt))) fail('--approve and --date (YYYY-MM-DD) go together')
  if (placeholder && approvedBy) fail('a placeholder is never approved')
  const posterAt = Number(option('--poster-at') ?? 2)
  if (!Number.isFinite(posterAt) || posterAt < 0) fail('--poster-at must be a number of seconds, 0 or more')

  const facts = checkVideo(source.video)
  let video = source.video
  if (!faststart(video)) {
    // Alleen de boxen verplaatsen, geen hercodering: beeld en geluid blijven
    // bit voor bit wat beoordeeld is.
    video = path.join(scratch, 'faststart.mp4')
    execFileSync(FFMPEG, ['-v', 'error', '-y', '-i', source.video, '-map', '0', '-c', 'copy', '-movflags', '+faststart', video])
    console.log('install-video: moved the moov box to the front (stream copy, no re-encode)')
  }
  if (statSync(video).size > MAX_VIDEO) fail(`video is ${(statSync(video).size / 1048576).toFixed(1)} MB; the limit is ${MAX_VIDEO / 1048576} MB (re-encode with a higher CRF)`)

  let poster = source.poster
  if (!poster) {
    poster = path.join(scratch, 'poster.jpg')
    const at = Math.min(posterAt, facts.durationMs / 1000 - 0.1)
    execFileSync(FFMPEG, ['-v', 'error', '-y', '-ss', String(Math.max(at, 0)), '-i', video, '-frames:v', '1', '-q:v', '3', poster])
  }
  const posterExtension = path.extname(poster).toLowerCase().replace('.jpeg', '.jpg')
  if (!['.png', '.jpg', '.webp'].includes(posterExtension)) fail('poster must be PNG, JPEG or WebP')
  if (statSync(poster).size > MAX_POSTER) fail('poster exceeds 2 MB; use JPEG')
  const posterStream = probe(poster).streams.find(stream => stream.codec_type === 'video')
  checkSize('poster', posterStream?.width, posterStream?.height)
  if (source.captions) {
    if (statSync(source.captions).size > MAX_CAPTIONS) fail('captions exceed 256 KB')
    if (!readFileSync(source.captions, 'utf8').replace(/^﻿/, '').startsWith('WEBVTT')) fail('captions must be a WebVTT file')
  }

  const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'))
  const previous = manifest.explainer
  const status = approvedBy ? 'approved' : 'temporary'
  const videoHash = digest(video)
  // Wie de al geïnstalleerde video goedkeurt, geeft het bestand in de mediamap
  // mee; de herkomst blijft dan de oorspronkelijke take, niet "uitlegvideo.mp4".
  const kept = previous?.video?.sha256 === videoHash ? previous.video.origin?.reference : undefined
  const reference = placeholder ? 'tools/uitlegvideo/stand/install-video.mjs --placeholder' : kept ?? path.basename(source.video)
  const provenance = (extra = {}) => ({
    status,
    origin: { reference, maker: 'Digitale Duiven' },
    // Rechten en beoordeling legt alleen --approve vast; een tijdelijk pakket
    // claimt geen van beide.
    ...(approvedBy ? { rights: { publicDistribution: true }, approval: { reviewedBy: approvedBy, reviewedAt: approvedAt, basis: 'Gekozen montage van de uitlegvideo' } } : {}),
    ...extra,
  })
  const target = { video: NAMES.video, poster: `${NAMES.poster}${posterExtension}`, captions: NAMES.captions }
  const explainer = {
    video: { id: 'explainer-video', kind: 'explainer-video', ...provenance(), file: target.video, sha256: videoHash, bytes: statSync(video).size, width: facts.width, height: facts.height, durationMs: facts.durationMs, hasAudio: true, language: 'en' },
    poster: { id: 'explainer-poster', kind: 'explainer-poster', ...provenance(), file: target.poster, sha256: digest(poster), bytes: statSync(poster).size, width: posterStream.width, height: posterStream.height },
    captions: source.captions ? { id: 'explainer-captions', kind: 'explainer-captions', ...provenance(), file: target.captions, sha256: digest(source.captions), bytes: statSync(source.captions).size, language: 'en' } : null,
  }

  // Eerst alles gelezen en gecontroleerd, dan pas de mediamap aangeraakt; oude
  // pakketbestanden die niet meer in het manifest staan gaan weg, anders weigert
  // de build ze als niet-gedeclareerd.
  for (const old of [previous?.video?.file, previous?.poster?.file, previous?.captions?.file]) {
    if (old && !Object.values(target).includes(old) && /^[a-z0-9][a-z0-9-]*\.[a-z0-9]+$/.test(old)) rmSync(path.join(media, old), { force: true })
  }
  if (!source.captions) rmSync(path.join(media, target.captions), { force: true })
  install(video, path.join(media, target.video))
  install(poster, path.join(media, target.poster))
  if (source.captions) install(source.captions, path.join(media, target.captions))
  manifest.explainer = explainer
  const manifestDraft = path.join(scratch, 'media-manifest.v1.json')
  writeFileSync(manifestDraft, JSON.stringify(manifest, null, 2) + '\n')
  install(manifestDraft, manifestPath)

  console.log(`install-video: ${status} explainer installed — ${facts.width}×${facts.height}, ${(facts.durationMs / 1000).toFixed(1)} s, ${(explainer.video.bytes / 1048576).toFixed(1)} MB${source.captions ? ', with captions' : ''}`)
  if (!approvedBy && !placeholder) console.log('install-video: not approved yet — fine for a local look, not for a deploy; rerun with --approve "names" --date YYYY-MM-DD once chosen')
  console.log('Check it:   cd module/duif/frontend && bun scripts/check-media.ts --probe')
  console.log(`Stand copy: cp module/duif/frontend/public/media/${NAMES.video} tools/uitlegvideo/stand/`)
}
