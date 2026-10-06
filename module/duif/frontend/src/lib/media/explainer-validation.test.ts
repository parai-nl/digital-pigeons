import { createHash } from 'node:crypto'
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { validateMedia, type Asset, type Manifest } from '../../../media-build'

// De uitlegvideo gaat door dezelfde poort als de rest van
// public/media: gedeclareerd, gehasht, alles of niets. Een placeholder mag
// zonder beoordeling, de gekozen video niet.
const roots: string[] = []
afterEach(() => roots.splice(0).forEach(root => rmSync(root, { recursive: true, force: true })))
function fixture(install = true) {
  const root = mkdtempSync(path.join(tmpdir(), 'duif-explainer-')); roots.push(root)
  const frontend = path.join(root, 'frontend'), directory = path.join(frontend, 'public/media')
  mkdirSync(directory, { recursive: true })
  const make = (kind: string, file: string, data: Buffer, extra: Partial<Asset>): Asset => {
    if (install) writeFileSync(path.join(directory, file), data)
    return { id: kind, kind, file, status: 'temporary', bytes: data.length, sha256: createHash('sha256').update(data).digest('hex'), origin: { reference: 'private take path', maker: 'fixture' }, ...extra }
  }
  const manifest: Manifest = { version: 1, visual: [], sounds: [], explainer: {
    video: make('explainer-video', 'uitlegvideo.mp4', Buffer.from('synthetic video fixture'), { width: 1920, height: 1080, durationMs: 95000, hasAudio: true, language: 'en' }),
    poster: make('explainer-poster', 'uitlegvideo-poster.png', Buffer.from('synthetic poster fixture'), { width: 1920, height: 1080 }),
    captions: make('explainer-captions', 'uitlegvideo-en.vtt', Buffer.from('WEBVTT\n\n00:00.000 --> 00:01.000\nFixture\n'), { language: 'en' }),
  } }
  return { frontend, directory, manifest, explainer: manifest.explainer! }
}
const approve = (asset: Asset) => Object.assign(asset, { status: 'approved', rights: { publicDistribution: true }, approval: { reviewedBy: 'fixture-only', reviewedAt: '2026-09-23' } })

describe('explainer video export boundary', () => {
  it('projects only same-origin URLs and display facts, placeholder or approved', () => {
    const { frontend, manifest, explainer } = fixture()
    expect(validateMedia(manifest, frontend).explainer).toEqual({
      id: 'explainer-video', url: '/modules/duif/media/uitlegvideo.mp4', posterUrl: '/modules/duif/media/uitlegvideo-poster.png',
      captionsUrl: '/modules/duif/media/uitlegvideo-en.vtt', width: 1920, height: 1080, durationMs: 95000, language: 'en', temporary: true,
    })
    for (const asset of [explainer.video, explainer.poster, explainer.captions!]) approve(asset)
    const approved = validateMedia(manifest, frontend)
    expect(approved.explainer).toMatchObject({ url: '/modules/duif/media/uitlegvideo.mp4', temporary: false })
    expect(JSON.stringify(approved)).not.toMatch(/reviewedBy|fixture-only|private take path|sha256/)
  })

  it('builds without a video section when nothing is installed, and without a track when there are no captions', () => {
    const empty = fixture(false)
    expect(validateMedia(empty.manifest, empty.frontend).explainer).toBeNull()
    const { frontend, directory, manifest, explainer } = fixture()
    rmSync(path.join(directory, explainer.captions!.file!))
    explainer.captions = null
    expect(validateMedia(manifest, frontend).explainer?.captionsUrl).toBeNull()
    delete manifest.explainer
    rmSync(directory, { recursive: true })
    expect(validateMedia(manifest, frontend).explainer).toBeNull()
  })

  // Ook zonder geïnstalleerde bestanden moet het record zelf kloppen, net als
  // bij het native pakket: een kapot manifest wacht niet tot de levering.
  for (const failure of ['hash-format', 'oversize']) it(`rejects a ${failure} explainer record before any file is installed`, () => {
    const { frontend, manifest, explainer } = fixture(false)
    if (failure === 'hash-format') explainer.poster.sha256 = 'not-a-hash'
    if (failure === 'oversize') explainer.video.bytes = 48 * 1024 * 1024 + 1
    expect(() => validateMedia(manifest, frontend)).toThrow(/invalid explainer integrity record/)
  })

  for (const failure of ['partial', 'hash', 'mixed', 'placeholder-review', 'unreviewed', 'container', 'silent', 'length', 'language', 'not-webvtt', 'duplicate-id']) it(`rejects ${failure} explainer exports`, () => {
    const { frontend, directory, manifest, explainer } = fixture()
    if (failure === 'partial') rmSync(path.join(directory, explainer.poster.file!))
    if (failure === 'hash') explainer.video.sha256 = '0'.repeat(64)
    if (failure === 'mixed') approve(explainer.video)
    if (failure === 'placeholder-review') explainer.poster.approval = { reviewedBy: 'nobody', reviewedAt: '2026-09-23' }
    if (failure === 'unreviewed') for (const asset of [explainer.video, explainer.poster, explainer.captions!]) { approve(asset); asset.approval = null }
    if (failure === 'container') explainer.video.file = 'uitlegvideo.mov'
    if (failure === 'silent') explainer.video.hasAudio = false
    if (failure === 'length') explainer.video.durationMs = 181000
    if (failure === 'language') explainer.captions!.language = 'nl'
    if (failure === 'not-webvtt') { const text = Buffer.from('1\n00:00:00,000 --> 00:00:01,000\nSRT\n'); writeFileSync(path.join(directory, 'uitlegvideo-en.vtt'), text); Object.assign(explainer.captions!, { bytes: text.length, sha256: createHash('sha256').update(text).digest('hex') }) }
    if (failure === 'duplicate-id') explainer.poster.id = 'explainer-video'
    expect(() => validateMedia(manifest, frontend)).toThrow(/Duif media/)
  })
})
