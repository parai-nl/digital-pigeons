// Gedrag van stand/install-video.mjs. Het script leidt de repo af
// van zijn eigen plek; de test draait daarom een kopie in een nagebouwde repo
// in een tijdelijke map, zodat de echte mediamap en het echte manifest nooit
// geraakt worden, ook niet als het script stuk is.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { execFileSync, spawnSync } from 'node:child_process'
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'

const repo = path.resolve(import.meta.dirname, '../../..')

function fakeRepo() {
  const root = mkdtempSync(path.join(tmpdir(), 'install-video-'))
  mkdirSync(path.join(root, 'tools/uitlegvideo/stand'), { recursive: true })
  mkdirSync(path.join(root, 'module/duif/config'), { recursive: true })
  mkdirSync(path.join(root, 'module/duif/frontend/public/media'), { recursive: true })
  copyFileSync(path.join(repo, 'tools/uitlegvideo/stand/install-video.mjs'), path.join(root, 'tools/uitlegvideo/stand/install-video.mjs'))
  copyFileSync(path.join(repo, 'module/duif/config/media-manifest.v1.json'), path.join(root, 'module/duif/config/media-manifest.v1.json'))
  return root
}

function clip(dir, sar) {
  const out = path.join(dir, `sar-${sar.replace('/', '-')}.mp4`)
  execFileSync('ffmpeg', ['-v', 'error', '-y', '-f', 'lavfi', '-i', 'color=c=gray:s=640x480:r=25:d=2', '-f', 'lavfi', '-i', 'anullsrc=channel_layout=stereo:sample_rate=48000',
    '-vf', `setsar=${sar}`, '-t', '2', '-c:v', 'libx264', '-pix_fmt', 'yuv420p', '-c:a', 'aac', '-movflags', '+faststart', out])
  return out
}

test('install-video weigert een video zonder vierkante pixels en installeert er een met', () => {
  const root = fakeRepo()
  try {
    const script = path.join(root, 'tools/uitlegvideo/stand/install-video.mjs')
    const manifest = path.join(root, 'module/duif/config/media-manifest.v1.json')
    const before = readFileSync(manifest, 'utf8')
    const wide = spawnSync(process.execPath, [script, clip(root, '4/3')], { encoding: 'utf8' })
    assert.equal(wide.status, 1)
    assert.match(wide.stderr, /square pixels \(sample aspect ratio 1:1\), found 4:3/)
    assert.equal(readFileSync(manifest, 'utf8'), before, 'een geweigerde video raakt het manifest niet')
    const square = spawnSync(process.execPath, [script, clip(root, '1/1')], { encoding: 'utf8' })
    assert.equal(square.status, 0, square.stderr)
    const explainer = JSON.parse(readFileSync(manifest, 'utf8')).explainer
    assert.equal(explainer.video.status, 'temporary')
    assert.deepEqual([explainer.video.width, explainer.video.height], [640, 480])
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
})
