#!/usr/bin/env node
import { createHash } from 'node:crypto'
import { existsSync, lstatSync, mkdirSync, readFileSync, writeFileSync, renameSync, rmSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const manifestPath = path.join(repo, 'module/duif/config/media-manifest.v1.json')
// De podiumcompositie van de runtime. Staat er een compositie in
// camera-views.json, dan moet die hier exact mee overeenkomen: anders zijn de
// posters met een andere kadrering gerenderd dan de vergadering gebruikt.
const compositionPath = path.join(repo, 'module/duif/frontend/src/lib/media/model/stage-composition.json')
// Elke duif heeft een eigen GLB, rustposter en kijkhoek. Model,
// poster en portretten van model v4 staan samen in één webmap van de
// assetmap; de bestandsnamen komen uit het manifest en worden op vorm
// gecontroleerd voordat ze een pad worden.
const PIGEONS = ['biological', 'individualist', 'nonverbal']
const webDirectory = 'model-v4/web'
const MODEL_FILE = /^[a-z0-9][a-z0-9-]*\.glb$/
const POSTER_FILE = /^[a-z0-9][a-z0-9-]*\.png$/
// Drie veldopnames, één per arousal-band, in plaats van één opname voor
// alles. De banden zijn technische variatie in het materiaal, geen betekenis.
const soundSelection = { calm: 'sounds/pigeon-field-coo.mp3', insistent: 'sounds/pigeon-field-coos-repeated.mp3', alarm: 'sounds/pigeon-field-loft-chorus.mp3' }
const soundFilenames = { calm: 'pigeon-field-coo.mp3', insistent: 'pigeon-field-coos-repeated.mp3', alarm: 'pigeon-field-loft-chorus.mp3' }
// De portretten volgen de goedgekeurde portretslots uit het manifest,
// zodat een slot toevoegen of een render vervangen op één plek gebeurt.
const PORTRAIT_KINDS = ['portrait', 'portrait-head']
const PORTRAIT_FILE = /^[a-z0-9][a-z0-9-]*\.(webp|avif|png|jpg|jpeg)$/

function noLinks(target) {
  let current = path.resolve(target)
  while (true) {
    if (existsSync(current) && lstatSync(current).isSymbolicLink()) throw new Error(`Linked export destination is not permitted: ${current}`)
    const parent = path.dirname(current)
    if (parent === current) return
    current = parent
  }
}

function readSelected(source) {
  if (!existsSync(source) || lstatSync(source).isSymbolicLink() || !lstatSync(source).isFile()) throw new Error(`Missing or linked selected asset: ${source}`)
  return readFileSync(source)
}

/** Width and height from a PNG or WebP header; the probe step re-checks them. */
export function imageSize(data) {
  if (data.length > 24 && data.readUInt32BE(0) === 0x89504e47 && data.toString('ascii', 12, 16) === 'IHDR') return { width: data.readUInt32BE(16), height: data.readUInt32BE(20) }
  if (data.length > 30 && data.toString('ascii', 0, 4) === 'RIFF' && data.toString('ascii', 8, 12) === 'WEBP') {
    const chunk = data.toString('ascii', 12, 16)
    if (chunk === 'VP8X') return { width: 1 + data.readUIntLE(24, 3), height: 1 + data.readUIntLE(27, 3) }
    if (chunk === 'VP8L') { const bits = data.readUInt32LE(21); return { width: 1 + (bits & 0x3fff), height: 1 + ((bits >> 14) & 0x3fff) } }
    if (chunk === 'VP8 ') return { width: data.readUInt16LE(26) & 0x3fff, height: data.readUInt16LE(28) & 0x3fff }
  }
  throw new Error('Unsupported image header')
}

function selection(manifest) {
  if (!manifest.native?.pigeons) throw new Error('Native media selection is missing from the manifest')
  const portraits = (manifest.visual ?? []).filter(asset => PORTRAIT_KINDS.includes(asset?.kind) && asset?.status === 'approved')
  const chosen = []
  for (const pigeon of PIGEONS) {
    const slot = manifest.native.pigeons[pigeon]
    for (const [kind, pattern] of [['model', MODEL_FILE], ['poster', POSTER_FILE]]) {
      const asset = slot?.[kind]
      if (typeof asset?.file !== 'string' || !pattern.test(asset.file)) throw new Error(`Invalid approved selection: ${kind}/${pigeon}`)
      chosen.push({ slot: `${kind}/${pigeon}`, relative: `${webDirectory}/${asset.file}`, asset, file: asset.file })
    }
  }
  for (const [band, relative] of Object.entries(soundSelection)) chosen.push({ slot: `sound/${band}`, relative, asset: manifest.native.sounds?.[band], file: soundFilenames[band] })
  for (const asset of portraits) {
    if (typeof asset.file !== 'string' || !PORTRAIT_FILE.test(asset.file)) throw new Error(`Invalid approved selection: portrait/${asset.id}`)
    chosen.push({ slot: `portrait/${asset.id}`, relative: `${webDirectory}/${asset.file}`, asset, file: asset.file })
  }
  return chosen
}

// Dezelfde grenzen als media-build.ts: een kijkhoek in graden of buiten de
// vloer faalt hier al, niet pas bij de build. Er wordt niet gespiegeld: een
// duif die naar rechts kijkt heeft een azimut aan de andere kant van het model
// (onder −π/2). Een `facing` of `rendered` in camera-views.json telt niet mee.
function checkedView(pigeon, view) {
  const within = (value, low, high) => typeof value === 'number' && Number.isFinite(value) && value >= low && value <= high
  if (!view || !within(view.azimuth, -Math.PI, Math.PI) || !within(view.elevation, 0, 0.6) || !within(view.fov, 15, 60)) throw new Error(`camera-views.json has an invalid view for ${pigeon}`)
  return { azimuth: view.azimuth, elevation: view.elevation, fov: view.fov }
}

function checkedComposition(composition) {
  if (composition === undefined) return
  const runtime = JSON.parse(readFileSync(compositionPath, 'utf8'))
  const same = ['envelope', 'fill'].every(part => ['horizontal', 'vertical'].every(axis => composition?.[part]?.[axis] === runtime[part][axis]))
  if (!same) throw new Error(`camera-views.json composition differs from the runtime (${path.relative(repo, compositionPath)}); the posters were framed for another stage`)
}

/**
 * Write the integrity record (sha256, bytes, image size) of every selected
 * model-v4 file and, when present, the camera views into a copy of the
 * manifest. A file whose bytes changed loses its review: the old approval
 * describes other bytes, so build and staging refuse it until a person records
 * a new review. Status, rights and attribution are never touched.
 */
export function describeSelection(sourceRoot, manifest) {
  const updated = structuredClone(manifest)
  const unreviewed = []
  for (const { slot, relative, asset } of selection(updated)) {
    if (!relative.startsWith(`${webDirectory}/`)) continue
    const data = readSelected(path.resolve(sourceRoot, relative))
    const sha256 = createHash('sha256').update(data).digest('hex')
    if (asset.sha256 !== sha256 || asset.bytes !== data.length) {
      asset.sha256 = sha256
      asset.bytes = data.length
      asset.approval = null
      unreviewed.push(slot)
    }
    if (!relative.endsWith('.glb')) {
      try { Object.assign(asset, imageSize(data)) } catch { throw new Error(`Unsupported image header for ${slot}; only PNG and WebP sizes are read`) }
    }
  }
  const viewsFile = path.resolve(sourceRoot, webDirectory, 'camera-views.json')
  if (existsSync(viewsFile)) {
    const { views, composition } = JSON.parse(readSelected(viewsFile).toString('utf8'))
    checkedComposition(composition)
    for (const pigeon of PIGEONS) {
      if (!views?.[pigeon]) throw new Error(`camera-views.json has no view for ${pigeon}`)
      updated.native.pigeons[pigeon].view = checkedView(pigeon, views[pigeon])
    }
  }
  return { manifest: updated, unreviewed }
}

/** Copy only the explicitly selected, hash-verified web exports. */
export function prepareMeetingMedia(sourceRoot, destination, manifest) {
  noLinks(destination)
  // Read and validate everything before touching the destination. This never
  // copies a directory, source footage, the editable .blend, or arbitrary files.
  const exports = selection(manifest).map(({ slot, relative, asset, file }) => {
    if (asset?.file !== file || asset.status !== 'approved' || !asset.rights?.publicDistribution || !asset.approval?.reviewedBy || !asset.approval?.reviewedAt) throw new Error(`Invalid approved selection: ${slot}`)
    const data = readSelected(path.resolve(sourceRoot, relative))
    if (data.length !== asset.bytes || createHash('sha256').update(data).digest('hex') !== asset.sha256) throw new Error(`Selected asset fails integrity check: ${slot}`)
    const target = path.join(destination, asset.file)
    noLinks(target)
    if (existsSync(target) && !lstatSync(target).isFile()) throw new Error(`Export target is not a file: ${target}`)
    return { target, data, sha256: asset.sha256 }
  })
  mkdirSync(destination, { recursive: true })
  for (const item of exports) {
    const temporary = `${item.target}.preparing-${process.pid}`
    try {
      writeFileSync(temporary, item.data, { flag: 'wx' })
      renameSync(temporary, item.target)
    } finally { rmSync(temporary, { force: true }) }
  }
  return exports.map(({ target, data, sha256 }) => ({ file: path.basename(target), bytes: data.length, sha256 }))
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const args = process.argv.slice(2)
  const update = args[0] === '--update-manifest'
  if (update) args.shift()
  const [sourceRoot, destination = path.join(repo, 'module/duif/frontend/public/media')] = args
  if (!sourceRoot || args.length > 2 || (update && args.length > 1)) {
    process.stderr.write('Usage: node tools/prepare-meeting-media.mjs --update-manifest <selected-assets-root>\n       node tools/prepare-meeting-media.mjs <selected-assets-root> [export-directory]\n')
    process.exitCode = 1
  } else {
    try {
      const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'))
      if (update) {
        // Alleen het manifest: kopiëren gebeurt pas na een nieuwe beoordeling.
        const { manifest: updated, unreviewed } = describeSelection(sourceRoot, manifest)
        writeFileSync(manifestPath, JSON.stringify(updated, null, 2) + '\n')
        process.stdout.write(JSON.stringify({ manifest: path.relative(repo, manifestPath), needsReview: unreviewed }, null, 2) + '\n')
        if (unreviewed.length) process.stderr.write(`${unreviewed.length} slot(s) changed and lost their approval; record a review in the manifest, then run without --update-manifest to stage.\n`)
      } else {
        process.stdout.write(JSON.stringify(prepareMeetingMedia(sourceRoot, path.resolve(destination), manifest), null, 2) + '\n')
      }
    } catch (error) { process.stderr.write(`${error.message}\n`); process.exitCode = 1 }
  }
}
