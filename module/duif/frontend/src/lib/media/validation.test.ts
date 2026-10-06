import { mkdtempSync, mkdirSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { validateMedia, type Manifest } from '../../../media-build'
const roots: string[] = []
afterEach(() => { for (const root of roots.splice(0)) rmSync(root, { recursive: true, force: true }) })
function fixture() {
  const root = mkdtempSync(path.join(tmpdir(), 'duif-media-')); roots.push(root)
  const frontend = path.join(root, 'frontend'); mkdirSync(path.join(frontend, 'public/media'), { recursive: true })
  const data = Buffer.from('fixture-only image bytes; file verification is tested independently of decode')
  writeFileSync(path.join(frontend, 'public/media/rest.webp'), data)
  const asset = { id: 'rest', pigeon: 'nonverbal', kind: 'rest', status: 'approved', file: 'rest.webp', sha256: createHash('sha256').update(data).digest('hex'), bytes: data.length, width: 1000, height: 1000, focus: [.5,.5], captureSet: 'fixture-set', alt: { en: 'Pigeon at rest', nl: 'Duif in rust' }, origin: { reference: 'synthetic test fixture', maker: 'test' }, rights: { publicDistribution: true }, approval: { reviewedBy: 'test-only', reviewedAt: '2026-09-07' } }
  const manifest: Manifest = { version: 1, visual: [asset], sounds: [] }
  return { root, frontend, manifest, asset }
}
describe('public export boundary', () => {
  it('ships only URL/crop metadata; master/provenance details stay private', () => {
    const { frontend, manifest } = fixture(); const result = validateMedia(manifest, frontend)
    expect(result.rest?.url).toBe('/modules/duif/media/rest.webp')
    expect(JSON.stringify(result)).not.toMatch(/reviewedBy|test-only|origin|reference|sha256/)
  })
  it('leaves pending slots on the original fallback without requiring imaginary files', () => {
    const { frontend, manifest } = fixture(); rmSync(path.join(frontend, 'public/media/rest.webp'))
    manifest.visual[0] = { ...manifest.visual[0], status: 'pending', file: null, sha256: null, approval: null }
    expect(validateMedia(manifest, frontend).rest).toBeNull()
  })
  for (const mutation of ['missing', 'hash', 'size', 'oversize', 'rights', 'review', 'path', 'duplicate', 'symlink', 'linked-directory', 'unlisted']) it(`rejects ${mutation} exports before public distribution`, () => {
    const { root, frontend, manifest, asset } = fixture(); const file = path.join(frontend, 'public/media/rest.webp')
    if (mutation === 'missing') rmSync(file)
    if (mutation === 'hash') asset.sha256 = '0'.repeat(64)
    if (mutation === 'size') asset.bytes++
    if (mutation === 'oversize') { writeFileSync(file, Buffer.alloc(2*1024*1024+1)); asset.bytes=2*1024*1024+1 }
    if (mutation === 'rights') asset.rights.publicDistribution = false
    if (mutation === 'review') asset.approval.reviewedBy = ''
    if (mutation === 'path') asset.file = '../rest.webp'
    if (mutation === 'duplicate') manifest.visual.push({ ...asset })
    if (mutation === 'symlink') { const outside = path.join(root, 'master.webp'); writeFileSync(outside, readFileSync(file)); rmSync(file); symlinkSync(outside, file) }
    if (mutation === 'linked-directory') { rmSync(path.join(frontend, 'public'), { recursive: true }); symlinkSync(root, path.join(frontend, 'public')) }
    if (mutation === 'unlisted') writeFileSync(path.join(frontend, 'public/media/unreviewed-master.mov'), 'private master')
    expect(() => validateMedia(manifest, frontend)).toThrow(/Duif media/)
  })
  // Het kaartportret en de kopuitsnede zijn twee slots per duif en
  // worden net als het native pakket gestaged, dus alles of niets.
  it('keeps the head crop in its own slot and stages the portrait package whole', () => {
    const { frontend, manifest, asset } = fixture()
    const card = Buffer.from('test-only portrait bytes'), head = Buffer.from('test-only head crop bytes')
    const digest = (data: Buffer) => createHash('sha256').update(data).digest('hex')
    writeFileSync(path.join(frontend, 'public/media/portret.webp'), card)
    writeFileSync(path.join(frontend, 'public/media/portret-kop.webp'), head)
    const base = { ...asset, pigeon: 'biological', captureSet: null }
    manifest.visual.push({ ...base, id: 'portret', kind: 'portrait', file: 'portret.webp', bytes: card.length, sha256: digest(card) })
    manifest.visual.push({ ...base, id: 'portret-kop', kind: 'portrait-head', file: 'portret-kop.webp', bytes: head.length, sha256: digest(head) })
    const staged = validateMedia(manifest, frontend)
    expect(staged.portraits.biological?.url).toBe('/modules/duif/media/portret.webp')
    expect(staged.headshots.biological?.url).toBe('/modules/duif/media/portret-kop.webp')
    manifest.visual.push({ ...base, id: 'portret-tweede', kind: 'portrait', file: 'portret.webp', bytes: card.length, sha256: digest(card) })
    expect(() => validateMedia(manifest, frontend)).toThrow(/duplicate export file/)
    manifest.visual.pop()
    rmSync(path.join(frontend, 'public/media/portret-kop.webp'))
    expect(() => validateMedia(manifest, frontend)).toThrow(/incomplete portrait package/)
    rmSync(path.join(frontend, 'public/media/portret.webp'))
    const drawing = validateMedia(manifest, frontend)
    expect(drawing.portraits).toEqual({}); expect(drawing.headshots).toEqual({})
    expect(drawing.rest?.url).toBe('/modules/duif/media/rest.webp')
  })
  it('refuses movement without a matching rest pose, silence declaration or bounded duration', () => {
    const {frontend,manifest,asset}=fixture();const bytes=Buffer.from('test-only motion bytes')
    writeFileSync(path.join(frontend,'public/media/movement.mp4'),bytes)
    const motion={...asset,id:'movement',kind:'movement',file:'movement.mp4',bytes:bytes.length,sha256:createHash('sha256').update(bytes).digest('hex'),durationMs:1200,hasAudio:false}
    manifest.visual.push(motion); expect(validateMedia(manifest,frontend).motions).toHaveLength(1)
    motion.hasAudio=true; expect(()=>validateMedia(manifest,frontend)).toThrow(/silent/)
    motion.hasAudio=false;motion.durationMs=16000;expect(()=>validateMedia(manifest,frontend)).toThrow(/200–8000/)
    motion.durationMs=1200; motion.captureSet='other-set'; expect(()=>validateMedia(manifest,frontend)).toThrow(/capture set/); motion.captureSet='fixture-set'; manifest.visual.shift();rmSync(path.join(frontend,'public/media/rest.webp'));expect(()=>validateMedia(manifest,frontend)).toThrow(/matching rest/)
  })
})
