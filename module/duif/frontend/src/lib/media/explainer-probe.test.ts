import { describe, expect, it } from 'vitest'
import type { Asset } from '../../../media-build'
import { explainerProbeFault, type ExplainerProbe } from '../../../scripts/explainer-probe'

// De uitlegvideo voor de stand is 4:3 (1440×1080). De help reserveert haar
// vlak met breedte / hoogte uit het manifest; dat klopt alleen bij vierkante
// pixels. De probe hieronder heeft de vorm die ffprobe -of json teruggeeft.
const RECORD: Asset = { id: 'explainer-video', kind: 'explainer-video', status: 'temporary', file: 'uitlegvideo.mp4', sha256: null, bytes: null, width: 1440, height: 1080, durationMs: 85000 }
const HEAD = 'ftypisommoov……mdat'
function probe(sar?: string): ExplainerProbe {
  return {
    format: { duration: '85.000000' },
    streams: [
      { codec_type: 'video', codec_name: 'h264', pix_fmt: 'yuv420p', width: 1440, height: 1080, ...(sar === undefined ? {} : { sample_aspect_ratio: sar }) },
      { codec_type: 'audio', codec_name: 'aac' },
    ],
  }
}

describe('explainer probe', () => {
  it('accepts the recorded size with square pixels', () => {
    expect(explainerProbeFault(RECORD, probe('1:1'), HEAD)).toBeNull()
  })

  // Een echt voorgekomen geval: 1440×1080 met setsar=4/3 is op het scherm
  // 16:9, terwijl manifest en help 4:3 aannemen.
  it('refuses non-square pixels, and a file that does not say', () => {
    expect(explainerProbeFault(RECORD, probe('4:3'), HEAD)).toMatch(/square pixels .*found 4:3/)
    expect(explainerProbeFault(RECORD, probe('0:1'), HEAD)).toMatch(/square pixels .*found 0:1/)
    expect(explainerProbeFault(RECORD, probe(), HEAD)).toMatch(/square pixels .*found none/)
  })
})
