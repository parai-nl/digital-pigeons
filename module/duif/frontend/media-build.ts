import { createHash } from 'node:crypto'
import { existsSync, lstatSync, readFileSync, realpathSync, readdirSync, rmSync } from 'node:fs'
import path from 'node:path'
import type { Plugin } from 'vite'
import { SIGNAL_BANDS, type ExplainerVideo, type ImageAsset, type MediaAttribution, type ModelAsset, type MotionAsset, type PigeonIdentity, type PigeonView, type PublicMedia, type SignalBand, type SignalSounds } from './src/lib/media/types'

export type Asset = {
  id: string; kind: string; pigeon?: string; status: string; file: string | null; sha256: string | null;
  bytes: number | null; width?: number | null; height?: number | null; durationMs?: number | null; hasAudio?: boolean;
  focus?: number[]; captureSet?: string | null; language?: string; alt?: { en: string; nl: string }; origin?: { reference?: string; maker?: string } | null;
  rights?: { publicDistribution?: boolean } | null; approval?: { reviewedBy?: string; reviewedAt?: string } | null; attribution?: MediaAttribution
}
// Eén veldopname per band in plaats van één opname voor alles: het
// pakket is pas compleet als alle drie de banden een eigen opname hebben.
// Elke duif heeft een eigen GLB, rustposter en kijkhoek;
// per vergadering laadt de client alleen het model van de actieve duif.
export type NativePigeon = { model: Asset; poster: Asset; view: PigeonView }
export type NativePackage = { pigeons: Record<PigeonIdentity, NativePigeon>; sounds: Record<SignalBand, Asset> }
// De uitlegvideo bovenaan de help: video, poster en optioneel
// Engelse ondertitels. Een video met status `temporary` heet in de help een
// placeholder; install-video.mjs wisselt het pakket in één stap en zet met
// --approve de goedkeuring vast.
export type ExplainerPackage = { video: Asset; poster: Asset; captions?: Asset | null }
export type Manifest = { version: number; visual: Asset[]; sounds: Asset[]; native?: NativePackage; explainer?: ExplainerPackage }
const IDENTITIES: PigeonIdentity[] = ['biological', 'individualist', 'nonverbal']
const PORTRAIT_KINDS = ['portrait', 'portrait-head']
const SHA = /^[a-f0-9]{64}$/
function fail(message: string): never { throw new Error(`Duif media: ${message}`) }

function checkedFile(directory: string, file: string, record: Asset, maximum: number): void {
  if (existsSync(directory) && lstatSync(directory).isSymbolicLink()) fail(`${record.id}: linked export directory`)
  const absolute = path.join(directory, file)
  let segment = directory
  for (const part of file.split('/')) {
    segment = path.join(segment, part)
    if (existsSync(segment) && lstatSync(segment).isSymbolicLink()) fail(`${record.id}: linked export path`)
  }
  if (!existsSync(absolute) || lstatSync(absolute).isSymbolicLink() || !lstatSync(absolute).isFile()) fail(`${record.id}: missing or linked export`)
  const size = lstatSync(absolute).size
  if (!size || size > maximum || size !== record.bytes) fail(`${record.id}: export size does not match or exceeds limit`)
  const real = realpathSync(absolute)
  if (!real.startsWith(realpathSync(directory) + path.sep)) fail(`${record.id}: file outside export directory`)
  const data = readFileSync(real)
  if (!data.length || data.length > maximum || data.length !== record.bytes) fail(`${record.id}: export size does not match or exceeds limit`)
  if (!record.sha256 || !SHA.test(record.sha256) || createHash('sha256').update(data).digest('hex') !== record.sha256) fail(`${record.id}: export hash mismatch`)
}

/** A selected model may never fetch files outside the approved export package. */
export function validateEmbeddedGlb(data: Buffer): void {
  if (data.length < 28 || data.toString('ascii', 0, 4) !== 'glTF' || data.readUInt32LE(4) !== 2 || data.readUInt32LE(8) !== data.length) fail('invalid native GLB header')
  const jsonLength = data.readUInt32LE(12)
  if (data.readUInt32LE(16) !== 0x4e4f534a || jsonLength % 4 !== 0 || jsonLength > data.length - 28) fail('invalid native GLB JSON chunk')
  let document: { buffers?: { uri?: unknown; byteLength?: number }[]; images?: { uri?: unknown; bufferView?: number; mimeType?: string }[]; bufferViews?: { buffer?: number; byteOffset?: number; byteLength?: number }[] }
  try { document = JSON.parse(data.toString('utf8', 20, 20 + jsonLength)) } catch { return fail('invalid native GLB JSON') }
  const binary = 20 + jsonLength
  if (data.readUInt32LE(binary + 4) !== 0x004e4942 || data.readUInt32LE(binary) + binary + 8 !== data.length) fail('invalid native GLB binary chunk')
  if (!Array.isArray(document.buffers) || document.buffers.length !== 1 || document.buffers.some(buffer => buffer.uri !== undefined || !Number.isInteger(buffer.byteLength) || buffer.byteLength! < 1 || buffer.byteLength! > data.readUInt32LE(binary))) fail('native GLB must use one embedded buffer')
  if (!Array.isArray(document.bufferViews) || document.bufferViews.some(view => view.buffer !== 0 || !Number.isInteger(view.byteLength) || view.byteLength! < 1 || !Number.isInteger(view.byteOffset ?? 0) || (view.byteOffset ?? 0) < 0 || (view.byteOffset ?? 0) + view.byteLength! > document.buffers![0].byteLength!)) fail('invalid native GLB buffer views')
  if (document.images && (!Array.isArray(document.images) || document.images.some(image => image.uri !== undefined || !Number.isInteger(image.bufferView) || image.bufferView! < 0 || image.bufferView! >= document.bufferViews!.length || !['image/png', 'image/jpeg', 'image/webp'].includes(image.mimeType ?? '')))) fail('native GLB images must be embedded buffer views')
}

function publicAttribution(asset: Asset): MediaAttribution {
  if (asset.status !== 'approved' || !asset.rights?.publicDistribution || !asset.origin?.reference || !asset.origin?.maker || !asset.approval?.reviewedBy || !asset.approval?.reviewedAt) fail(`${asset.id}: native export requires recorded provenance, rights and review`)
  const credit = asset.attribution
  if (!credit || ![credit.title, credit.creator, credit.source, credit.license, credit.licenseUrl, credit.changes].every(value => typeof value === 'string' && value.trim())) fail(`${asset.id}: public attribution missing`)
  for (const value of [credit.source, credit.licenseUrl]) {
    let url: URL
    try { url = new URL(value) } catch { return fail(`${asset.id}: invalid public attribution URL`) }
    if (url.protocol !== 'https:' || url.username || url.password) fail(`${asset.id}: invalid public attribution URL`)
  }
  // Deliberate projection: internal review records, source paths, hashes and
  // unrecognized metadata never leave the build. License attribution does.
  return { title: credit.title, creator: credit.creator, source: credit.source, license: credit.license, licenseUrl: credit.licenseUrl, changes: credit.changes }
}

// De kijkhoek is data uit camera-views.json; een onzinnige hoek zou de
// kadrering stil laten mislukken, dus de grenzen staan hier en niet pas in de
// browser. Azimut binnen één omwenteling, elevatie tussen vlak en schuin van
// boven, en een beeldhoek waarin een duif niet vertekent.
function checkedView(pigeon: string, view: PigeonView | undefined): PigeonView {
  const finite = (value: unknown, low: number, high: number) => typeof value === 'number' && Number.isFinite(value) && value >= low && value <= high
  // Geen spiegeling in de vergadering: naar rechts kijken is een azimut aan
  // de andere kant van het model. Een oude spiegelvlag wordt geweigerd
  // in plaats van stil genegeerd.
  if (!view || !finite(view.azimuth, -Math.PI, Math.PI) || !finite(view.elevation, 0, .6) || !finite(view.fov, 15, 60) || (view.facing !== undefined && view.facing !== 'left')) fail(`${pigeon}: invalid camera view`)
  return { azimuth: view.azimuth, elevation: view.elevation, fov: view.fov }
}

function nativePackage(native: NativePackage | undefined, directory: string, files: Set<string>, ids: Set<string>, catalogue: PublicMedia): void {
  catalogue.models = {}
  catalogue.signalSounds = null
  if (!native) return
  if (!native.pigeons || IDENTITIES.some(pigeon => !native.pigeons[pigeon]) || Object.keys(native.pigeons).some(key => !IDENTITIES.includes(key as PigeonIdentity))) fail('native package needs one model per pigeon')
  if (!native.sounds || SIGNAL_BANDS.some(band => !native.sounds[band])) fail('native package needs one recording per band')
  const recordings = SIGNAL_BANDS.map(band => native.sounds[band])
  const views = IDENTITIES.map(pigeon => checkedView(pigeon, native.pigeons[pigeon].view))
  const entries = [...IDENTITIES.flatMap(pigeon => [native.pigeons[pigeon].model, native.pigeons[pigeon].poster]), ...recordings]
  const models = IDENTITIES.map(() => ({ extension: /^[a-z0-9][a-z0-9-]*\.glb$/, kind: 'model', maximum: 12 * 1024 * 1024 }))
  const posters = IDENTITIES.map(() => ({ extension: /^[a-z0-9][a-z0-9-]*\.png$/, kind: 'poster', maximum: 2 * 1024 * 1024 }))
  const slots = [...models.flatMap((model, index) => [model, posters[index]]), ...recordings.map(() => ({ extension: /^[a-z0-9][a-z0-9-]*\.mp3$/, kind: 'field-recording', maximum: 1024 * 1024 }))]
  const credits = entries.map((asset, index) => {
    if (!asset || !/^[a-z][a-z0-9-]*$/.test(asset.id) || ids.has(asset.id)) fail('invalid or duplicate native ID')
    ids.add(asset.id)
    const slot = slots[index]
    if (asset.kind !== slot.kind || !asset.file || !slot.extension.test(asset.file) || files.has(asset.file)) fail(`${asset.id}: invalid native export slot or filename`)
    if (!asset.sha256 || !SHA.test(asset.sha256) || !Number.isInteger(asset.bytes) || asset.bytes! <= 0 || asset.bytes! > slot.maximum) fail(`${asset.id}: invalid native integrity record`)
    files.add(asset.file)
    return publicAttribution(asset)
  })
  for (const recording of recordings) {
    if (!Number.isInteger(recording.durationMs) || recording.durationMs! < 200 || recording.durationMs! > 15000) fail('native recording duration outside playback limits')
  }
  // The approved package is optional delivery, never partial delivery. A fresh
  // checkout builds honestly without it; corruption or partial copying fails.
  // Drie duiven met elk een model zijn één pakket: een half gekopieerde set zou
  // één duif stil op haar portret laten staan zonder dat iemand het merkt.
  const present = entries.map(asset => existsSync(path.join(directory, asset.file!)))
  if (present.every(value => !value)) return
  if (!present.every(Boolean)) fail('incomplete native package; run prepare-meeting-media.mjs with all approved exports')
  entries.forEach((asset, index) => checkedFile(directory, asset.file!, asset, slots[index].maximum))
  IDENTITIES.forEach((pigeon, index) => {
    const { model, poster } = native.pigeons[pigeon]
    validateEmbeddedGlb(readFileSync(path.join(directory, model.file!)))
    catalogue.models![pigeon] = { id: model.id, url: `/modules/duif/media/${model.file}`, posterUrl: `/modules/duif/media/${poster.file}`, view: views[index], attribution: credits[index * 2] } satisfies ModelAsset
  })
  const offset = IDENTITIES.length * 2
  catalogue.signalSounds = Object.fromEntries(SIGNAL_BANDS.map((band, index) => [band, {
    id: recordings[index].id, url: `/modules/duif/media/${recordings[index].file}`,
    durationMs: recordings[index].durationMs!, attribution: credits[offset + index],
  }])) as SignalSounds
}

const EXPLAINER_SLOTS = [
  { kind: 'explainer-video', file: /^[a-z0-9][a-z0-9-]*\.mp4$/, maximum: 48 * 1024 * 1024 },
  { kind: 'explainer-poster', file: /^[a-z0-9][a-z0-9-]*\.(png|jpg|jpeg|webp)$/, maximum: 2 * 1024 * 1024 },
  { kind: 'explainer-captions', file: /^[a-z0-9][a-z0-9-]*\.vtt$/, maximum: 256 * 1024 },
]

function explainerPackage(explainer: ExplainerPackage | undefined, directory: string, files: Set<string>, ids: Set<string>, catalogue: PublicMedia): void {
  catalogue.explainer = null
  if (!explainer) return
  const entries = [explainer.video, explainer.poster, ...(explainer.captions ? [explainer.captions] : [])]
  entries.forEach((asset, index) => {
    const slot = EXPLAINER_SLOTS[index]
    if (!asset || !/^[a-z][a-z0-9-]*$/.test(asset.id) || ids.has(asset.id)) fail('invalid or duplicate explainer ID')
    ids.add(asset.id)
    if (asset.kind !== slot.kind || !asset.file || !slot.file.test(asset.file) || files.has(asset.file)) fail(`${asset.id}: invalid explainer slot or filename`)
    files.add(asset.file)
    // Een placeholder is eerlijk een placeholder: hij draagt geen beoordeling
    // die niemand gedaan heeft. De gekozen video draagt die wel, en het pakket
    // is één geheel: geen goedgekeurde video met een placeholderposter.
    if (asset.status !== explainer.video.status) fail(`${asset.id}: explainer package mixes placeholder and approved files`)
    if (asset.status === 'temporary') { if (asset.approval) fail(`${asset.id}: a placeholder must not carry a review`) }
    else if (asset.status !== 'approved' || !asset.rights?.publicDistribution || !asset.origin?.reference || !asset.origin?.maker || !asset.approval?.reviewedBy || !asset.approval?.reviewedAt) fail(`${asset.id}: explainer export requires recorded provenance, rights and review`)
    if (index !== 1 && asset.language !== 'en') fail(`${asset.id}: the explainer and its captions are English`)
    if (!asset.sha256 || !SHA.test(asset.sha256) || !Number.isInteger(asset.bytes) || asset.bytes! <= 0 || asset.bytes! > slot.maximum) fail(`${asset.id}: invalid explainer integrity record`)
  })
  const { video, poster } = explainer
  if (video.hasAudio !== true || !Number.isInteger(video.durationMs) || video.durationMs! < 1000 || video.durationMs! > 180000) fail(`${video.id}: explainer video needs a sound track and 1–180 s`)
  for (const asset of [video, poster]) {
    if (!Number.isInteger(asset.width) || !Number.isInteger(asset.height) || asset.width! < 320 || asset.height! < 180 || asset.width! > 3840 || asset.height! > 2160) fail(`${asset.id}: invalid dimensions`)
  }
  // Net als het native pakket: afwezig bouwt eerlijk zonder videosectie, half
  // gekopieerd of beschadigd houdt de build tegen.
  const present = entries.map(asset => existsSync(path.join(directory, asset.file!)))
  if (present.every(value => !value)) return
  if (!present.every(Boolean)) fail('incomplete explainer package; run tools/uitlegvideo/stand/install-video.mjs')
  entries.forEach((asset, index) => checkedFile(directory, asset.file!, asset, EXPLAINER_SLOTS[index].maximum))
  const captions = explainer.captions
  if (captions && !readFileSync(path.join(directory, captions.file!), 'utf8').replace(/^\uFEFF/, '').startsWith('WEBVTT')) fail(`${captions.id}: captions must be WebVTT`)
  catalogue.explainer = {
    id: video.id, url: `/modules/duif/media/${video.file}`, posterUrl: `/modules/duif/media/${poster.file}`,
    captionsUrl: captions ? `/modules/duif/media/${captions.file}` : null,
    width: video.width!, height: video.height!, durationMs: video.durationMs!, language: 'en', temporary: video.status === 'temporary',
  } satisfies ExplainerVideo
}

export function validateMedia(manifest: Manifest, frontend: string): PublicMedia {
  if (manifest.version !== 1 || !Array.isArray(manifest.visual) || !Array.isArray(manifest.sounds)) fail('unsupported manifest')
  const catalogue: PublicMedia = { portraits: {}, headshots: {}, rest: null, motions: [], temporarySound: false }
  const ids = new Set<string>()
  const files = new Set<string>()
  const directory = path.join(frontend, 'public/media')
  for (const folder of [path.join(frontend, 'public'), directory]) {
    if (existsSync(folder) && lstatSync(folder).isSymbolicLink()) fail('media export directory must not be a symlink')
  }
  // De portretten worden net als het native pakket gestaged, niet gecommit
  // (.gitignore). Een verse checkout zonder die exports moet eerlijk bouwen op
  // de getekende duif; half gekopieerd mag nooit stilletjes doorgaan. Rust- en
  // bewegingsbeeld blijven wel hard: die zijn geen gestaged pakket.
  const staged = manifest.visual.filter(asset => PORTRAIT_KINDS.includes(asset.kind) && asset.status === 'approved' && asset.file)
  const installed = staged.filter(asset => existsSync(path.join(directory, asset.file!))).length
  if (installed && installed !== staged.length) fail('incomplete portrait package; run prepare-meeting-media.mjs with all approved exports')
  const portraitsInstalled = staged.length > 0 && installed === staged.length
  for (const asset of manifest.visual) {
    if (!/^[a-z][a-z0-9-]*$/.test(asset.id) || ids.has(asset.id)) fail('invalid or duplicate visual ID')
    ids.add(asset.id)
    if (!IDENTITIES.includes(asset.pigeon as PigeonIdentity) || !['portrait', 'portrait-head', 'rest', 'movement'].includes(asset.kind)) fail(`${asset.id}: unknown visual slot`)
    const portrait = PORTRAIT_KINDS.includes(asset.kind)
    if (asset.status === 'pending') {
      if (asset.file || asset.sha256 || asset.approval) fail(`${asset.id}: pending slots must not select an export`)
      continue
    }
    if (asset.status !== 'approved' || !asset.rights?.publicDistribution || !asset.origin?.reference || !asset.origin?.maker || !asset.approval?.reviewedBy || !asset.approval?.reviewedAt) fail(`${asset.id}: public export requires recorded provenance, rights and review`)
    if (!asset.alt?.en?.trim() || !asset.alt?.nl?.trim()) fail(`${asset.id}: bilingual image description missing`)
    const motion = asset.kind === 'movement'
    if (!asset.file || !(motion ? /^[a-z0-9][a-z0-9-]*\.(mp4|webm)$/ : /^[a-z0-9][a-z0-9-]*\.(webp|avif|png|jpg|jpeg)$/).test(asset.file)) fail(`${asset.id}: invalid export filename`)
    if (files.has(asset.file)) fail(`${asset.id}: duplicate export file`)
    files.add(asset.file)
    if (!portrait || portraitsInstalled) checkedFile(directory, asset.file, asset, motion ? 6 * 1024 * 1024 : 2 * 1024 * 1024)
    if (!Number.isInteger(asset.width) || !Number.isInteger(asset.height) || asset.width! < 256 || asset.height! < 256 || asset.width! > 4096 || asset.height! > 4096) fail(`${asset.id}: invalid dimensions`)
    if (asset.focus?.length !== 2 || asset.focus.some(value => !Number.isFinite(value) || value < 0 || value > 1)) fail(`${asset.id}: invalid crop focus`)
    const image: ImageAsset = { id: asset.id, url: `/modules/duif/media/${asset.file}`, width: asset.width!, height: asset.height!, focus: asset.focus as [number, number] }
    if (motion) {
      if (asset.pigeon !== 'nonverbal' || asset.hasAudio !== false || !Number.isInteger(asset.durationMs) || asset.durationMs! < 200 || asset.durationMs! > 8000) fail(`${asset.id}: movement must be silent, signaling-only and 200–8000 ms`)
      catalogue.motions.push({ ...image, durationMs: asset.durationMs! } as MotionAsset)
    } else if (asset.kind === 'rest') {
      if (asset.pigeon !== 'nonverbal' || catalogue.rest) fail(`${asset.id}: duplicate or invalid rest pose`)
      catalogue.rest = image
    } else {
      // De kopuitsnede is een eigen slot per duif, geen tweede portret: beide
      // gaan door dezelfde controle op herkomst, rechten en hash.
      const pigeon = asset.pigeon as PigeonIdentity
      const slots = asset.kind === 'portrait-head' ? catalogue.headshots : catalogue.portraits
      if (slots[pigeon]) fail(`${asset.id}: duplicate portrait slot`)
      slots[pigeon] = image
    }
  }
  // Zonder gestagede exports blijven de slots wel op dubbele bezetting
  // gecontroleerd, maar wijst er geen URL naar een bestand dat er niet is.
  if (!portraitsInstalled) { catalogue.portraits = {}; catalogue.headshots = {} }
  if (catalogue.motions.length && !catalogue.rest) fail('movement requires an approved matching rest pose')
  const rest = manifest.visual.find(asset => asset.kind === 'rest' && asset.status === 'approved')
  for (const asset of manifest.visual.filter(asset => asset.kind === 'movement' && asset.status === 'approved')) {
    if (!asset.captureSet || asset.captureSet !== rest?.captureSet || asset.width !== rest.width || asset.height !== rest.height || asset.focus?.join(',') !== rest.focus?.join(',')) fail(`${asset.id}: movement and rest must share a capture set, dimensions and crop`)
  }
  nativePackage(manifest.native, directory, files, ids, catalogue)
  explainerPackage(manifest.explainer, directory, files, ids, catalogue)
  if (existsSync(directory)) for (const entry of readdirSync(directory)) {
    if (entry === 'README.md' || entry === '.gitkeep') continue
    if (!files.has(entry)) fail(`unlisted file in public media: ${entry}`)
  }
  const soundIds = new Set<string>()
  for (const sound of manifest.sounds) {
    if (!/^[a-z][a-z0-9-]*$/.test(sound.id) || soundIds.has(sound.id)) fail('invalid or duplicate sound ID')
    soundIds.add(sound.id)
    if (!sound.file || !/^sounds\/[a-z0-9][a-z0-9-]*\.mp3$/.test(sound.file)) fail(`${sound.id}: invalid sound filename`)
    checkedFile(path.resolve(frontend, '../config'), sound.file, sound, 1024 * 1024)
    if (!Number.isInteger(sound.durationMs) || sound.durationMs! < 200 || sound.durationMs! > 15000) fail(`${sound.id}: duration outside playback limits`)
    if (sound.status === 'temporary' && sound.kind === 'synthetic') catalogue.temporarySound = true
    else if (sound.status !== 'approved' || sound.kind !== 'field-recording' || !sound.rights?.publicDistribution || !sound.origin?.maker || !sound.origin?.reference || !sound.approval?.reviewedBy || !sound.approval?.reviewedAt) fail(`${sound.id}: field sound requires provenance, rights and review`)
  }
  // Het native pakket dekt alle drie de banden met echte opnames; dan hoort de
  // gebruiker geen synthetisch materiaal meer en vervalt de notitie. Zonder dat
  // pakket blijft zij staan zolang er een placeholder in de set zit.
  if (catalogue.signalSounds) catalogue.temporarySound = false
  return catalogue
}

export function pigeonMediaPlugin(frontend: string): Plugin {
  const id = '\0virtual:duif-media'
  const manifestPath = path.resolve(frontend, '../config/media-manifest.v1.json')
  let build = false
  return {
    name: 'duif:public-media',
    configResolved(config) { build = config.command === 'build' },
    buildStart() {
      this.addWatchFile(manifestPath)
      validateMedia(JSON.parse(readFileSync(manifestPath, 'utf8')) as Manifest, frontend)
      if (build) rmSync(path.join(frontend, 'dist/media'), { recursive: true, force: true })
    },
    configureServer(server) {
      server.middlewares.use((request, response, next) => {
        const pathname = request.url?.split('?')[0] ?? ''
        if (!pathname.startsWith('/modules/duif/media/') && !pathname.startsWith('/media/')) { next(); return }
        try {
          const catalogue = validateMedia(JSON.parse(readFileSync(manifestPath, 'utf8')) as Manifest, frontend)
          const allowed = new Set([...Object.values(catalogue.portraits), ...Object.values(catalogue.headshots), ...(catalogue.rest ? [catalogue.rest] : []), ...catalogue.motions].map(asset => asset.url))
          for (const model of Object.values(catalogue.models ?? {})) { allowed.add(model.url); allowed.add(model.posterUrl) }
          for (const sound of Object.values(catalogue.signalSounds ?? {})) allowed.add(sound.url)
          if (catalogue.explainer) for (const url of [catalogue.explainer.url, catalogue.explainer.posterUrl, catalogue.explainer.captionsUrl]) if (url) allowed.add(url)
          const canonical = pathname.startsWith('/media/') ? `/modules/duif${pathname}` : pathname
          if (!allowed.has(canonical)) { response.statusCode = 404; response.end('Media not available'); return }
          request.url = canonical.replace('/modules/duif', '')
          next()
        } catch { response.statusCode = 503; response.end('Media not available') }
      })
    },
    resolveId(source) { if (source === 'virtual:duif-media') return id },
    load(source) {
      if (source !== id) return
      const catalogue = validateMedia(JSON.parse(readFileSync(manifestPath, 'utf8')) as Manifest, frontend)
      // Only selected URLs, display metadata and explicit public license credits
      // leave the build; review records and source master references stay private.
      return `export default ${JSON.stringify(catalogue)};`
    },
  }
}
