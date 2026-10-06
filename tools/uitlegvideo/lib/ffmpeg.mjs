// ffmpeg-stappen voor de uitlegvideo. Elk shot wordt eerst een
// tussenbestand van precies zijn eigen lengte (beeld en geluid apart); de
// eindstap plakt ze aan elkaar, legt de ondertitels erover en normaliseert
// het geluid.
import { execFile } from 'node:child_process'
import { mkdir, rm, symlink, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { promisify } from 'node:util'

const run = promisify(execFile)
export const FPS = 30
export const RATE = 48000

export async function ffmpeg(args, { log } = {}) {
  if (log) log('ffmpeg ' + args.map((a) => (/\s/.test(a) ? JSON.stringify(a) : a)).join(' ').slice(0, 400))
  try {
    return await run('ffmpeg', ['-hide_banner', '-v', 'error', '-y', ...args], { maxBuffer: 64 * 1024 * 1024 })
  } catch (err) {
    throw new Error(`ffmpeg faalde: ${err.stderr || err.message}`)
  }
}

// JPEG uit Chromium is sRGB met de BT.601-matrix op vol bereik; H.264 voor
// schermen is BT.709 op tv-bereik. Eén plek voor die omzetting.
const COLOR_IN = 'in_color_matrix=bt601:in_range=pc:out_color_matrix=bt709:out_range=tv'
const X264 = ['-c:v', 'libx264', '-pix_fmt', 'yuv420p', '-colorspace', 'bt709', '-color_primaries', 'bt709', '-color_trc', 'bt709', '-color_range', 'tv']

// videoFromFrames zet een reeks opgenomen beelden (één per uitvoerbeeld) om in
// een fragment van de videomaat. De opname heeft die maat al (venster maal
// schaal, lib/format.mjs); hier gebeurt alleen de kleuromzetting, geen
// uitsnede en geen zoom.
export async function videoFromFrames({ files, out, work, size = { width: 1920, height: 1080 }, log }) {
  await rm(work, { recursive: true, force: true })
  await mkdir(work, { recursive: true })
  await Promise.all(files.map((file, k) => symlink(file, path.join(work, `${String(k).padStart(6, '0')}${path.extname(file)}`))))
  const ext = path.extname(files[0])
  const vf = `scale=${size.width}:${size.height}:flags=lanczos:${COLOR_IN},format=yuv420p,setparams=colorspace=bt709:color_primaries=bt709:color_trc=bt709:range=tv`
  await ffmpeg(['-framerate', String(FPS), '-i', path.join(work, `%06d${ext}`), '-vf', vf, ...X264, '-crf', '12', '-preset', 'fast', '-r', String(FPS), out], { log })
  await rm(work, { recursive: true, force: true })
}

// silence maakt een stil geluidsfragment van precies `seconds`.
export async function silence(seconds, out) {
  await ffmpeg(['-f', 'lavfi', '-i', `anullsrc=r=${RATE}:cl=stereo`, '-t', seconds.toFixed(6), '-c:a', 'pcm_s16le', out])
}

// mixAudio legt geluiden op een tijdlijn van `seconds` lang. Elk geluid:
// { file, at (s, mag negatief: dan begint het halverwege), gain (dB) }.
export async function mixAudio({ seconds, sounds, out, log }) {
  if (!sounds.length) return silence(seconds, out)
  const inputs = []
  const chains = []
  sounds.forEach((s, i) => {
    inputs.push('-i', s.file)
    const f = [`aresample=${RATE}`, 'aformat=channel_layouts=stereo']
    if (s.at < 0) f.push(`atrim=start=${(-s.at).toFixed(4)}`, 'asetpts=PTS-STARTPTS')
    else if (s.at > 0) f.push(`adelay=${Math.round(s.at * 1000)}:all=1`)
    if (s.gain) f.push(`volume=${s.gain}dB`)
    chains.push(`[${i}:a]${f.join(',')}[s${i}]`)
  })
  const mix = `${sounds.map((_, i) => `[s${i}]`).join('')}amix=inputs=${sounds.length}:normalize=0:duration=longest,apad,atrim=end=${seconds.toFixed(6)},afade=t=in:d=0.02,afade=t=out:st=${Math.max(0, seconds - 0.03).toFixed(4)}:d=0.03[a]`
  await ffmpeg([...inputs, '-filter_complex', [...chains, mix].join(';'), '-map', '[a]', '-c:a', 'pcm_s16le', '-ar', String(RATE), out], { log })
}

// concatFiles plakt tussenbestanden van hetzelfde soort aan elkaar.
export async function concatFiles(files, out, work, extra = []) {
  const list = path.join(work, path.basename(out) + '.txt')
  await writeFile(list, files.map((f) => `file '${f.replaceAll("'", "'\\''")}'`).join('\n') + '\n')
  await ffmpeg(['-f', 'concat', '-safe', '0', '-i', list, ...extra, out])
}

// loudness meet de geïntegreerde luidheid (EBU R128) voor de lineaire
// normalisatie in de tweede stap.
export async function loudness(file) {
  const { stderr } = await run('ffmpeg', ['-hide_banner', '-i', file, '-af', 'loudnorm=I=-16:TP=-1.5:LRA=11:print_format=json', '-f', 'null', '-'], { maxBuffer: 64 * 1024 * 1024 })
  const json = stderr.match(/\{[^{}]*"input_i"[^{}]*\}/g)?.at(-1)
  if (!json) throw new Error(`geen luidheidsmeting voor ${file}`)
  return JSON.parse(json)
}

export { X264 }
