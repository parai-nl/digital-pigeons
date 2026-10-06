// De controle achteraf op de uitlegvideo: wat ffprobe over het bestand
// zegt, naast wat het manifest belooft. Hij staat los van check-media.ts, zodat
// de regels zonder ffprobe en zonder geïnstalleerde video te toetsen zijn.
// `probe` is de JSON van `ffprobe -show_streams -show_format`, `head` het begin
// van het bestand.
import type { Asset } from '../media-build'

export type ExplainerProbe = {
  format: { duration?: string }
  streams: { codec_type: string; codec_name: string; pix_fmt?: string; width?: number; height?: number; sample_aspect_ratio?: string }[]
}

export function explainerProbeFault(record: Asset, probe: ExplainerProbe, head: string): string | null {
  const video = probe.streams.find(stream => stream.codec_type === 'video')
  if (!video || video.codec_name !== 'h264' || video.pix_fmt !== 'yuv420p' || video.width !== record.width || video.height !== record.height) return `${record.id}: expected H.264 yuv420p at the recorded dimensions`
  // De help reserveert het vlak met breedte / hoogte uit het manifest. Met
  // niet-vierkante pixels toont de browser een andere verhouding dan die maat
  // (1440×1080 met SAR 4:3 is beeldvullend 16:9), en krijgt de speler alsnog
  // balken. Een ontbrekende SAR is ook een fout: dan zegt het bestand niet wat
  // de help aanneemt.
  if (video.sample_aspect_ratio !== '1:1') return `${record.id}: expected square pixels (sample aspect ratio 1:1), found ${video.sample_aspect_ratio ?? 'none'}`
  if (!probe.streams.some(stream => stream.codec_type === 'audio' && stream.codec_name === 'aac')) return `${record.id}: expected an AAC sound track`
  if (Math.abs(Number(probe.format.duration) * 1000 - record.durationMs!) > 100) return `${record.id}: duration differs by more than 100 ms`
  if (!head.includes('moov') || (head.includes('mdat') && head.indexOf('mdat') < head.indexOf('moov'))) return `${record.id}: moov box is not at the front; rerun install-video.mjs`
  return null
}
