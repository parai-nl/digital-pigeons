import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { mkdtempSync, mkdirSync, readFileSync, readdirSync, realpathSync, rmSync, symlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { describeSelection, imageSize, prepareMeetingMedia } from './prepare-meeting-media.mjs'

const PIGEONS = { biological: 'biologisch', individualist: 'individualistisch', nonverbal: 'signalerend' }
// Een PNG-kop van 1400 × 1100 en een verliesloze WebP-kop van 512 × 512: meer
// leest describeSelection niet, en de probe controleert het echte beeld.
const png = (width, height) => { const data = Buffer.alloc(33); data.writeUInt32BE(0x89504e47, 0); data.write('IHDR', 12); data.writeUInt32BE(width, 16); data.writeUInt32BE(height, 20); return data }
const webp = (width, height) => { const data = Buffer.alloc(40); data.write('RIFF'); data.write('WEBP', 8); data.write('VP8L', 12); data.writeUInt8(0x2f, 20); data.writeUInt32LE((width - 1) | ((height - 1) << 14), 21); return data }
// cwebp met alfa schrijft een uitgebreide kop (VP8X): zo zien de echte portretten eruit.
const webpExtended = (width, height) => { const data = Buffer.alloc(40); data.write('RIFF'); data.write('WEBP', 8); data.write('VP8X', 12); data.writeUInt8(0x10, 20); data.writeUIntLE(width - 1, 24, 3); data.writeUIntLE(height - 1, 27, 3); return data }

function fixture() {
  const root = mkdtempSync(path.join(realpathSync(tmpdir()), 'duif-stage-'))
  const source = path.join(root, 'source'), output = path.join(root, 'output'), web = path.join(source, 'model-v4/web')
  mkdirSync(web, { recursive: true }); mkdirSync(path.join(source, 'sounds'))
  const approve = (data, file) => ({ file, bytes: data.length, sha256: createHash('sha256').update(data).digest('hex'), status: 'approved', rights: { publicDistribution: true }, approval: { reviewedBy: 'fixture', reviewedAt: '2026-09-23' } })
  const manifest = { native: { pigeons: {}, sounds: {} }, visual: [] }
  const staged = []
  for (const [pigeon, dutch] of Object.entries(PIGEONS)) {
    // Per duif een eigen model en poster met eigen bytes.
    const model = Buffer.from(`synthetic ${pigeon} model`), poster = Buffer.concat([png(1400, 1100), Buffer.from(pigeon)])
    writeFileSync(path.join(web, `stadsduif-v4-${dutch}.glb`), model)
    writeFileSync(path.join(web, `stadsduif-v4-${dutch}-rust.png`), poster)
    manifest.native.pigeons[pigeon] = { model: approve(model, `stadsduif-v4-${dutch}.glb`), poster: approve(poster, `stadsduif-v4-${dutch}-rust.png`), view: { azimuth: -.57, elevation: .105, fov: 32 } }
    staged.push(`stadsduif-v4-${dutch}.glb`, `stadsduif-v4-${dutch}-rust.png`)
  }
  // Er staat één veldopname per arousal-band in het pakket.
  for (const [band, file] of [['calm', 'pigeon-field-coo.mp3'], ['insistent', 'pigeon-field-coos-repeated.mp3'], ['alarm', 'pigeon-field-loft-chorus.mp3']]) {
    const data = Buffer.from(`synthetic ${band} fixture`)
    writeFileSync(path.join(source, 'sounds', file), data)
    manifest.native.sounds[band] = approve(data, file)
    staged.push(file)
  }
  // De portretten volgen de goedgekeurde slots uit het manifest.
  for (const [id, kind, file] of [['portrait-biological', 'portrait', 'stadsduif-v4-biologisch-kaart.webp'], ['portrait-head-biological', 'portrait-head', 'stadsduif-v4-biologisch-kop.webp']]) {
    const data = webp(kind === 'portrait' ? 512 : 256, kind === 'portrait' ? 512 : 256)
    writeFileSync(path.join(web, file), data)
    manifest.visual.push({ id, kind, pigeon: 'biological', ...approve(data, file) })
    staged.push(file)
  }
  // Een slot dat nog niet geleverd is wordt niet gestaged en blokkeert niet.
  manifest.visual.push({ id: 'portrait-nonverbal', kind: 'portrait', pigeon: 'nonverbal', status: 'pending', file: null })
  writeFileSync(path.join(web, 'stadsduif-v4.blend'), 'must not leak')
  writeFileSync(path.join(web, 'kaart-bron.psd'), 'must not leak')
  return { root, source, output, web, manifest, staged }
}

test('stages exactly the selected per-pigeon exports and validates before replacing them', () => {
  const { root, source, output, manifest, staged } = fixture()
  try {
    assert.equal(prepareMeetingMedia(source, output, manifest).length, staged.length)
    assert.deepEqual(readdirSync(output).sort(), [...staged].sort())
    const before = readFileSync(path.join(output, 'stadsduif-v4-biologisch.glb'))
    writeFileSync(path.join(source, 'sounds/pigeon-field-loft-chorus.mp3'), 'corrupt')
    assert.throws(() => prepareMeetingMedia(source, output, manifest), /integrity/)
    assert.deepEqual(readFileSync(path.join(output, 'stadsduif-v4-biologisch.glb')), before)
    const linked = path.join(root, 'linked'); symlinkSync(output, linked)
    assert.throws(() => prepareMeetingMedia(source, linked, manifest), /Linked export/)
    // Een bestandsnaam uit het manifest mag nooit een pad worden.
    manifest.native.pigeons.individualist.model.file = '../stadsduif-v4-individualistisch.glb'
    assert.throws(() => prepareMeetingMedia(source, output, manifest), /Invalid approved selection: model\/individualist/)
    manifest.visual[0].file = '../stadsduif-v4-biologisch-kaart.webp'
    assert.throws(() => prepareMeetingMedia(source, output, manifest), /Invalid approved selection/)
  } finally { rmSync(root, { recursive: true, force: true }) }
})

test('refuses a package that leaves a pigeon without her model', () => {
  const { root, source, output, manifest } = fixture()
  try {
    delete manifest.native.pigeons.nonverbal
    assert.throws(() => prepareMeetingMedia(source, output, manifest), /Invalid approved selection: model\/nonverbal/)
  } finally { rmSync(root, { recursive: true, force: true }) }
})

test('records integrity and camera views of a new delivery and withdraws the old approval', () => {
  const { root, source, output, web, manifest } = fixture()
  try {
    // Een nieuwe levering: andere bytes, een andere maat en een kijkhoek per duif.
    const model = Buffer.from('new biological model')
    writeFileSync(path.join(web, 'stadsduif-v4-biologisch.glb'), model)
    writeFileSync(path.join(web, 'stadsduif-v4-biologisch-kaart.webp'), webpExtended(640, 640))
    // Niet spiegelen maar draaien: een `facing` of `rendered` in
    // camera-views.json telt niet mee en belandt niet in het manifest.
    const views = { biological: { azimuth: -1.5708, elevation: .1, fov: 30, facing: 'left', note: 'dropped' }, individualist: { azimuth: -1, elevation: .1, fov: 32 }, nonverbal: { azimuth: -.57, elevation: .105, fov: 32, rendered: 'unmirrored' } }
    const composition = JSON.parse(readFileSync(path.resolve(import.meta.dirname, '../module/duif/frontend/src/lib/media/model/stage-composition.json'), 'utf8'))
    writeFileSync(path.join(web, 'camera-views.json'), JSON.stringify({ version: 1, views, composition }))
    const { manifest: updated, unreviewed } = describeSelection(source, manifest)
    const slot = updated.native.pigeons.biological
    assert.equal(slot.model.sha256, createHash('sha256').update(model).digest('hex'))
    assert.equal(slot.model.bytes, model.length)
    assert.deepEqual(slot.view, { azimuth: -1.5708, elevation: .1, fov: 30 })
    assert.deepEqual([updated.visual[0].width, updated.visual[0].height], [640, 640])
    assert.deepEqual([slot.poster.width, slot.poster.height], [1400, 1100])
    // Wat van bytes veranderde, verliest zijn beoordeling; de rest houdt haar.
    assert.deepEqual(unreviewed.sort(), ['model/biological', 'portrait/portrait-biological'])
    assert.equal(slot.model.approval, null)
    assert.equal(updated.visual[0].approval, null)
    assert.equal(slot.poster.approval.reviewedBy, 'fixture')
    assert.equal(slot.model.status, 'approved', 'status and rights stay a human record')
    // Zonder nieuwe beoordeling wordt er niets gestaged.
    assert.throws(() => prepareMeetingMedia(source, output, updated), /Invalid approved selection: model\/biological/)
    assert.equal(manifest.native.pigeons.biological.view.fov, 32, 'the input manifest stays untouched')
    // Posters met een andere kadrering dan de runtime worden geweigerd.
    writeFileSync(path.join(web, 'camera-views.json'), JSON.stringify({ version: 1, views, composition: { ...composition, fill: { ...composition.fill, vertical: .78 } } }))
    assert.throws(() => describeSelection(source, manifest), /composition differs from the runtime/)
    writeFileSync(path.join(web, 'camera-views.json'), JSON.stringify({ version: 1, views: { ...views, individualist: { ...views.individualist, azimuth: -57 } } }))
    assert.throws(() => describeSelection(source, manifest), /invalid view for individualist/)
    delete views.nonverbal
    writeFileSync(path.join(web, 'camera-views.json'), JSON.stringify({ version: 1, views }))
    assert.throws(() => describeSelection(source, manifest), /no view for nonverbal/)
  } finally { rmSync(root, { recursive: true, force: true }) }
})

test('reads PNG and WebP dimensions from their headers', () => {
  assert.deepEqual(imageSize(png(1400, 1100)), { width: 1400, height: 1100 })
  assert.deepEqual(imageSize(webp(256, 256)), { width: 256, height: 256 })
  assert.deepEqual(imageSize(webpExtended(512, 512)), { width: 512, height: 512 })
  assert.throws(() => imageSize(Buffer.from('not an image at all, not at all')), /Unsupported/)
})
