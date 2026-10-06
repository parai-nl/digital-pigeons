import { ClampToEdgeWrapping, LinearFilter, LinearMipmapLinearFilter, LinearMipmapNearestFilter, MirroredRepeatWrapping, NearestFilter, NearestMipmapLinearFilter, NearestMipmapNearestFilter, RepeatWrapping, Texture } from 'three'
import { GLTFLoader, type GLTFParser } from 'three/addons/loaders/GLTFLoader.js'
import { disposePigeonResources } from './pigeonResources'

/** The approved asset is a self-contained GLB, never an external-resource graph. */
export function validateEmbeddedPigeon(bytes: ArrayBuffer): void {
  if (bytes.byteLength < 20 || bytes.byteLength > 32 * 1024 * 1024) throw new Error('Invalid pigeon GLB size')
  const view = new DataView(bytes)
  if (view.getUint32(0, true) !== 0x46546c67 || view.getUint32(4, true) !== 2 || view.getUint32(8, true) !== bytes.byteLength || view.getUint32(16, true) !== 0x4e4f534a) throw new Error('Invalid pigeon GLB header')
  const length = view.getUint32(12, true)
  if (length > bytes.byteLength - 20) throw new Error('Invalid pigeon GLB JSON')
  const json = JSON.parse(new TextDecoder().decode(new Uint8Array(bytes, 20, length)))
  // No loader extension is needed by v3. Keeping this format closed prevents
  // plugins from bypassing our embedded texture path or fetching other files.
  if (json.extensionsUsed?.length || json.extensionsRequired?.length) throw new Error('Unsupported pigeon GLB extension')
  if (!Array.isArray(json.buffers) || json.buffers.length !== 1 || json.buffers[0].uri !== undefined) throw new Error('Pigeon buffers must be embedded')
  for (const image of json.images ?? []) {
    if (image.uri !== undefined || !Number.isInteger(image.bufferView) || !['image/png', 'image/jpeg'].includes(image.mimeType)) throw new Error('Pigeon images must be embedded raster images')
  }
}

export async function decodePigeonImage(blob: Blob): Promise<ImageBitmap | HTMLImageElement> {
  if (typeof createImageBitmap === 'function') return createImageBitmap(blob, { colorSpaceConversion: 'none', premultiplyAlpha: 'none' })
  // Core permits data: images. Safari fallback remains local and does not
  // manufacture blob: URLs or require a broader Content-Security-Policy.
  const dataUrl = await new Promise<string>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result))
    reader.onerror = () => reject(new Error('Pigeon image could not be decoded'))
    reader.readAsDataURL(blob)
  })
  return new Promise((resolve, reject) => {
    const image = new Image()
    image.onload = () => resolve(image)
    image.onerror = () => reject(new Error('Pigeon image could not be decoded'))
    image.src = dataUrl
  })
}

const filters = { 9728: NearestFilter, 9729: LinearFilter, 9984: NearestMipmapNearestFilter, 9985: LinearMipmapNearestFilter, 9986: NearestMipmapLinearFilter, 9987: LinearMipmapLinearFilter } as const
const wraps = { 33071: ClampToEdgeWrapping, 33648: MirroredRepeatWrapping, 10497: RepeatWrapping } as const

export async function parseEmbeddedPigeon(bytes: ArrayBuffer, signal: AbortSignal) {
  validateEmbeddedPigeon(bytes)
  const textures = new Set<Texture>()
  let abandoned = false
  const release = (texture: Texture) => {
    const data = texture.source.data as { close?: () => void } | null
    if (data && typeof data.close === 'function') data.close()
    texture.dispose()
  }
  const abort = () => { abandoned = true; textures.forEach(release); textures.clear() }
  signal.addEventListener('abort', abort, { once: true })
  const loader = new GLTFLoader().register((parser: GLTFParser) => ({
    name: 'DUIf_embedded_raster',
    async loadTexture(index: number) {
      const definition = parser.json.textures[index]
      const source = parser.json.images[definition.source]
      const buffer = await parser.getDependency('bufferView', source.bufferView)
      const image = await decodePigeonImage(new Blob([buffer], { type: source.mimeType }))
      const texture = new Texture(image)
      if (abandoned || signal.aborted) { release(texture); throw new DOMException('Aborted', 'AbortError') }
      textures.add(texture)
      const sampler = parser.json.samplers?.[definition.sampler] ?? {}
      texture.flipY = false
      texture.name = definition.name || source.name || ''
      texture.magFilter = sampler.magFilter === 9728 ? NearestFilter : LinearFilter
      texture.minFilter = filters[sampler.minFilter as keyof typeof filters] ?? LinearMipmapLinearFilter
      texture.wrapS = wraps[sampler.wrapS as keyof typeof wraps] ?? RepeatWrapping
      texture.wrapT = wraps[sampler.wrapT as keyof typeof wraps] ?? RepeatWrapping
      texture.generateMipmaps = texture.minFilter !== NearestFilter && texture.minFilter !== LinearFilter
      texture.needsUpdate = true
      parser.associations.set(texture, { textures: index })
      return texture
    },
  }))
  try {
    if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
    const gltf = await loader.parseAsync(bytes, '')
    if (signal.aborted) { disposePigeonResources(gltf.scene); throw new DOMException('Aborted', 'AbortError') }
    // Ownership moves to the scene and its normal GPU cleanup path.
    textures.clear()
    return gltf
  } catch (error) { abort(); throw error }
  finally { signal.removeEventListener('abort', abort) }
}
