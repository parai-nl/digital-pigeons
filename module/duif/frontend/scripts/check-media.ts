// Run with the same Bun runtime as the frontend build. --probe additionally
// checks actual codecs, dimensions, tracks and durations using local ffprobe.
import { readFileSync } from 'node:fs'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { validateMedia, type Manifest } from '../media-build'
import { explainerProbeFault, type ExplainerProbe } from './explainer-probe'
import { SIGNAL_BANDS, type PigeonIdentity } from '../src/lib/media/types'
const PIGEONS: PigeonIdentity[] = ['biological', 'individualist', 'nonverbal']
const frontend = path.resolve(import.meta.dirname, '..')
const manifest = JSON.parse(readFileSync(path.resolve(frontend, '../config/media-manifest.v1.json'), 'utf8')) as Manifest
const catalogue = validateMedia(manifest, frontend)
const release = process.argv.includes('--release')
if (release && (Object.keys(catalogue.portraits).length !== 3 || !catalogue.rest || catalogue.motions.length === 0 || manifest.sounds.length === 0 || manifest.visual.some(asset => asset.status !== 'approved') || manifest.sounds.some(asset => asset.status !== 'approved'))) throw new Error('Media delivery is not final: three portraits, a rest pose, movement and field sound must be approved; pending or temporary media remain')
const models = PIGEONS.filter(pigeon => catalogue.models?.[pigeon])
if (process.argv.includes('--native-release') && (models.length !== PIGEONS.length || !catalogue.signalSounds)) throw new Error('Native media delivery is incomplete: a verified model and rest image per pigeon and one field recording per band must all be installed')
if (process.argv.includes('--probe')) {
  for (const asset of [...manifest.visual.filter(asset => asset.status === 'approved'), ...manifest.sounds, ...(models.length && manifest.native ? [...PIGEONS.map(pigeon => manifest.native!.pigeons[pigeon].poster), ...SIGNAL_BANDS.map(band => manifest.native!.sounds[band])] : [])]) {
    const backendSound = asset.file!.startsWith('sounds/')
    const sound = backendSound || asset.kind === 'field-recording'
    const file = path.resolve(frontend, backendSound ? '../config' : 'public/media', asset.file!)
    const probe = JSON.parse(execFileSync(process.env.FFPROBE ?? 'ffprobe', ['-v', 'error', '-show_streams', '-show_format', '-of', 'json', file], { encoding: 'utf8', maxBuffer: 1024*1024, timeout: 10000 })) as { format: { duration?: string }; streams: { codec_type: string; codec_name: string; width?: number; height?: number }[] }
    if (sound && (!probe.streams.some(stream => stream.codec_type === 'audio' && stream.codec_name === 'mp3') || probe.streams.some(stream => stream.codec_type === 'video'))) throw new Error(`${asset.id}: expected MP3 audio only`)
    if (!sound) {
      const video = probe.streams.find(stream => stream.codec_type === 'video')
      if (!video || video.width !== asset.width || video.height !== asset.height) throw new Error(`${asset.id}: dimensions differ from export record`)
      if (probe.streams.some(stream => stream.codec_type === 'audio')) throw new Error(`${asset.id}: visual exports must not carry audio`)
    }
    if (asset.durationMs != null && (!Number.isFinite(Number(probe.format.duration)) || Math.abs(Number(probe.format.duration) * 1000 - asset.durationMs) > 40)) throw new Error(`${asset.id}: duration differs by more than 40 ms`)
    console.log(`${asset.id}: decoded export checked`)
  }
}
// De uitlegvideo is geen stil beeld: zij hoort H.264 met AAC te zijn,
// met vierkante pixels en de moov-box vooraan zodat de help met
// preload="metadata" niet de hele video ophaalt. install-video.mjs zorgt
// daarvoor; dit is de controle achteraf (explainer-probe.ts).
if (process.argv.includes('--probe') && catalogue.explainer && manifest.explainer) {
  const record = manifest.explainer.video
  const file = path.resolve(frontend, 'public/media', record.file!)
  const probe = JSON.parse(execFileSync(process.env.FFPROBE ?? 'ffprobe', ['-v', 'error', '-show_streams', '-show_format', '-of', 'json', file], { encoding: 'utf8', maxBuffer: 1024*1024, timeout: 10000 })) as ExplainerProbe
  const fault = explainerProbeFault(record, probe, readFileSync(file).subarray(0, 1024 * 1024).toString('latin1'))
  if (fault) throw new Error(fault)
  console.log(`${record.id}: decoded explainer checked`)
}
console.log(`Media manifest valid: ${Object.keys(catalogue.portraits).length} approved portraits, ${catalogue.motions.length} approved movements, ${manifest.sounds.length} backend sound entries; meeting playback: ${catalogue.signalSounds ? `${SIGNAL_BANDS.length} selected field recordings, one per band` : catalogue.temporarySound ? 'temporary synthesis' : 'approved backend recordings'}; native models: ${models.length ? `${models.join(', ')} installed and verified` : 'not installed (fallback)'}; explainer video: ${catalogue.explainer ? manifest.explainer!.video.status === 'approved' ? 'approved' : 'temporary (not chosen yet)' : 'not installed'}`)
