import { createHash } from 'node:crypto'
import { mkdtempSync, mkdirSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { validateMedia, validateEmbeddedGlb, type Asset, type Manifest } from '../../../media-build'
import type { PigeonIdentity, PigeonView, SignalBand } from './types'

const roots: string[] = []
afterEach(() => roots.splice(0).forEach(root => rmSync(root, { recursive: true, force: true })))
const attribution = { title: 'Fixture', creator: 'Synthetic test', source: 'https://example.org/source', license: 'CC0 1.0', licenseUrl: 'https://creativecommons.org/publicdomain/zero/1.0/', changes: 'Synthetic fixture only' }
function glb(document: object = { buffers: [{ byteLength: 4 }], bufferViews: [{ buffer: 0, byteLength: 4 }], images: [{ bufferView: 0, mimeType: 'image/png' }] }) {
  const json = Buffer.from(JSON.stringify(document).padEnd(Math.ceil(JSON.stringify(document).length / 4) * 4, ' '))
  const data = Buffer.alloc(28 + json.length + 4)
  data.write('glTF'); data.writeUInt32LE(2, 4); data.writeUInt32LE(data.length, 8)
  data.writeUInt32LE(json.length, 12); data.writeUInt32LE(0x4e4f534a, 16); json.copy(data, 20)
  data.writeUInt32LE(4, 20 + json.length); data.writeUInt32LE(0x004e4942, 24 + json.length)
  return data
}
const VIEWS: Record<PigeonIdentity, PigeonView> = {
  biological: { azimuth: -Math.PI / 2, elevation: .1, fov: 32 },
  individualist: { azimuth: -1.8416, elevation: .1, fov: 30 },
  nonverbal: { azimuth: -.57, elevation: .105, fov: 32 },
}
function fixture(install = true) {
  const root = mkdtempSync(path.join(tmpdir(), 'duif-native-')); roots.push(root)
  const frontend = path.join(root, 'frontend'), directory = path.join(frontend, 'public/media')
  mkdirSync(directory, { recursive: true })
  const make = (kind: string, file: string, data: Buffer, id = kind): Asset => {
    if (install) writeFileSync(path.join(directory, file), data)
    return { id, kind, file, status: 'approved', bytes: data.length, sha256: createHash('sha256').update(data).digest('hex'), origin: { reference: 'private master path', maker: 'fixture' }, rights: { publicDistribution: true }, approval: { reviewedBy: 'fixture-only', reviewedAt: '2026-09-09' }, attribution }
  }
  const recording = (band: SignalBand, file: string, durationMs: number): Asset =>
    ({ ...make('field-recording', file, Buffer.from(`synthetic recording fixture ${band}`), `coo-${band}`), durationMs })
  // Elke duif een eigen GLB, rustposter en kijkhoek. De bytes
  // verschillen per duif, zodat een verwisseling in de projectie opvalt.
  const pigeon = (id: PigeonIdentity, view: PigeonView) => ({
    model: make('model', `pigeon-${id}.glb`, glb({ buffers: [{ byteLength: 4 }], bufferViews: [{ buffer: 0, byteLength: 4 }], asset: { generator: id } }), `model-${id}`),
    poster: make('poster', `pigeon-${id}.png`, Buffer.from(`synthetic poster ${id}`), `poster-${id}`),
    view,
  })
  const manifest: Manifest = { version: 1, visual: [], sounds: [], native: {
    pigeons: {
      biological: pigeon('biological', VIEWS.biological),
      individualist: pigeon('individualist', VIEWS.individualist),
      nonverbal: pigeon('nonverbal', VIEWS.nonverbal),
    },
    sounds: { calm: recording('calm', 'coo.mp3', 4529), insistent: recording('insistent', 'coo-urgent.mp3', 3950), alarm: recording('alarm', 'loft.mp3', 3600) },
  } }
  return { frontend, directory, manifest }
}
describe('native package export boundary', () => {
  it('allows absent delivery without selecting imaginary media', () => {
    const { frontend, manifest } = fixture(false)
    expect(validateMedia(manifest, frontend)).toMatchObject({ models: {}, signalSounds: null })
  })
  it('projects only selected same-origin files and public attribution', () => {
    const { frontend, manifest } = fixture()
    const media = validateMedia(manifest, frontend)
    for (const pigeon of ['biological', 'individualist', 'nonverbal'] as const) {
      expect(media.models?.[pigeon]).toEqual({ id: `model-${pigeon}`, url: `/modules/duif/media/pigeon-${pigeon}.glb`, posterUrl: `/modules/duif/media/pigeon-${pigeon}.png`, view: VIEWS[pigeon], attribution })
    }
    expect(media.signalSounds).toMatchObject({
      calm: { url: '/modules/duif/media/coo.mp3', durationMs: 4529 },
      insistent: { url: '/modules/duif/media/coo-urgent.mp3', durationMs: 3950 },
      alarm: { url: '/modules/duif/media/loft.mp3', durationMs: 3600 },
    })
    expect(JSON.stringify(media)).not.toMatch(/private master|fixture-only|reviewedBy|sha256/)
  })
  it('refuses a package that leaves a band without its own recording', () => {
    const { frontend, manifest } = fixture()
    delete (manifest.native!.sounds as Partial<Record<SignalBand, Asset>>).alarm
    expect(() => validateMedia(manifest, frontend)).toThrow(/one recording per band/)
  })
  it('refuses a package that leaves a pigeon without her own model', () => {
    const { frontend, manifest } = fixture()
    delete (manifest.native!.pigeons as Partial<Record<PigeonIdentity, unknown>>).individualist
    expect(() => validateMedia(manifest, frontend)).toThrow(/one model per pigeon/)
  })
  it('refuses two pigeons sharing one exported model file', () => {
    const { frontend, manifest } = fixture()
    manifest.native!.pigeons.individualist.model = { ...manifest.native!.pigeons.biological.model, id: 'model-copy' }
    expect(() => validateMedia(manifest, frontend)).toThrow(/invalid native export slot or filename/)
  })
  for (const view of [{ azimuth: 7 }, { elevation: Number.NaN }, { elevation: -.1 }, { fov: 120 }, { facing: 'up' }, { facing: 'right' }, null]) it(`rejects the camera view ${JSON.stringify(view)}`, () => {
    const { frontend, manifest } = fixture()
    const pigeon = manifest.native!.pigeons.biological
    pigeon.view = (view === null ? undefined : { ...pigeon.view, ...view }) as PigeonView
    expect(() => validateMedia(manifest, frontend)).toThrow(/biological: invalid camera view/)
  })
  for (const failure of ['partial', 'partial-model', 'partial-sound', 'hash', 'pending', 'rights', 'review', 'path', 'unlisted', 'linked', 'unsafe-credit']) it(`rejects ${failure} native exports`, () => {
    const { frontend, directory, manifest } = fixture()
    const model = manifest.native!.pigeons.nonverbal.model
    if (failure === 'partial') rmSync(path.join(directory, 'coo.mp3'))
    if (failure === 'partial-model') rmSync(path.join(directory, 'pigeon-individualist.glb'))
    if (failure === 'partial-sound') rmSync(path.join(directory, 'loft.mp3'))
    if (failure === 'hash') model.sha256 = '0'.repeat(64)
    if (failure === 'pending') model.status = 'pending'
    if (failure === 'rights') model.rights!.publicDistribution = false
    if (failure === 'review') model.approval = null
    if (failure === 'path') model.file = '../pigeon.glb'
    if (failure === 'unlisted') writeFileSync(path.join(directory, 'master.blend'), 'private')
    if (failure === 'linked') { const target = path.join(directory, 'pigeon-nonverbal.png'); const bytes = readFileSync(target); const external = path.join(frontend, 'master.png'); writeFileSync(external, bytes); rmSync(target); symlinkSync(external, target) }
    if (failure === 'unsafe-credit') model.attribution = { ...attribution, source: 'javascript:alert(1)' }
    const reasons: Record<string, RegExp> = { partial: /incomplete native package/, 'partial-model': /incomplete native package/, 'partial-sound': /incomplete native package/, hash: /hash mismatch/, pending: /provenance, rights and review/, rights: /provenance, rights and review/, review: /provenance, rights and review/, path: /invalid native export slot/, unlisted: /unlisted file/, linked: /linked export/, 'unsafe-credit': /attribution URL/ }
    expect(() => validateMedia(manifest, frontend)).toThrow(reasons[failure])
  })
  it('rejects external image/buffer references and malformed GLB chunks', () => {
    expect(() => validateEmbeddedGlb(glb())).not.toThrow()
    expect(() => validateEmbeddedGlb(glb({ buffers: [{ uri: 'https://example.org/model.bin', byteLength: 4 }], bufferViews: [] }))).toThrow(/embedded buffer/)
    expect(() => validateEmbeddedGlb(glb({ buffers: [{ byteLength: 4 }], bufferViews: [{ buffer: 0, byteLength: 4 }], images: [{ uri: 'https://example.org/texture.png' }] }))).toThrow(/embedded buffer views/)
    const data = glb(); data.writeUInt32LE(0xffffffff, 12)
    expect(() => validateEmbeddedGlb(data)).toThrow(/JSON chunk/)
  })
})
