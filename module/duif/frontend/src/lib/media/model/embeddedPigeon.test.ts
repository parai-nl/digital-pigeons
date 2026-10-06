import { afterEach, expect, it, vi } from 'vitest'
import { Mesh } from 'three'
import { decodePigeonImage, parseEmbeddedPigeon, validateEmbeddedPigeon } from './embeddedPigeon'
import { disposePigeonResources } from './pigeonResources'

function glb(extra: Record<string, unknown> = {}) {
  const json = JSON.stringify({ asset: { version: '2.0' }, buffers: [{ byteLength: 16 }], ...extra })
  const padded = json.padEnd(Math.ceil(json.length / 4) * 4, ' ')
  const bytes = new ArrayBuffer(12 + 8 + padded.length + 8 + 16), view = new DataView(bytes)
  view.setUint32(0, 0x46546c67, true); view.setUint32(4, 2, true); view.setUint32(8, bytes.byteLength, true)
  view.setUint32(12, padded.length, true); view.setUint32(16, 0x4e4f534a, true)
  new Uint8Array(bytes, 20, padded.length).set(new TextEncoder().encode(padded))
  view.setUint32(20 + padded.length, 16, true); view.setUint32(24 + padded.length, 0x004e4942, true)
  return bytes
}
afterEach(() => vi.unstubAllGlobals())

it('rejects external resources and loader extensions before GLTF parsing', () => {
  expect(() => validateEmbeddedPigeon(glb())).not.toThrow()
  for (const extra of [
    { buffers: [{ uri: 'https://elsewhere.invalid/model.bin' }] },
    { images: [{ uri: 'https://elsewhere.invalid/texture.png' }] },
    { images: [{ bufferView: 0, mimeType: 'image/svg+xml' }] },
    { extensionsUsed: ['KHR_texture_basisu'] },
  ]) expect(() => validateEmbeddedPigeon(glb(extra))).toThrow()
  expect(() => validateEmbeddedPigeon(new ArrayBuffer(8))).toThrow()
})

it('decodes embedded textures directly and preserves GLTF material colour configuration', async () => {
  const bitmap = { width: 2, height: 2, close: vi.fn() }
  const decode = vi.fn(async () => bitmap)
  vi.stubGlobal('createImageBitmap', decode)
  const objectUrl = vi.fn(() => { throw new Error('CSP forbids blob URLs') })
  vi.stubGlobal('URL', class extends URL { static createObjectURL = objectUrl })
  const bytes = glb({
    bufferViews: [{ buffer: 0, byteOffset: 0, byteLength: 12 }, { buffer: 0, byteOffset: 12, byteLength: 4 }],
    accessors: [{ bufferView: 0, componentType: 5126, count: 1, type: 'VEC3', min: [0, 0, 0], max: [0, 0, 0] }],
    images: [{ bufferView: 1, mimeType: 'image/png' }], textures: [{ source: 0 }],
    materials: [{ pbrMetallicRoughness: { baseColorTexture: { index: 0 } } }],
    meshes: [{ primitives: [{ attributes: { POSITION: 0 }, material: 0 }] }],
    nodes: [{ mesh: 0 }], scenes: [{ nodes: [0] }], scene: 0,
  })
  const result = await parseEmbeddedPigeon(bytes, new AbortController().signal)
  const mesh = result.scene.children[0] as Mesh
  expect(decode).toHaveBeenCalledWith(expect.any(Blob), { colorSpaceConversion: 'none', premultiplyAlpha: 'none' })
  expect(objectUrl).not.toHaveBeenCalled()
  expect(mesh.material).toMatchObject({ map: { flipY: false, colorSpace: 'srgb' } })
  disposePigeonResources(result.scene)
})

it('does not decode after an already cancelled meeting load', async () => {
  const controller = new AbortController(); controller.abort()
  const decode = vi.fn(); vi.stubGlobal('createImageBitmap', decode)
  await expect(parseEmbeddedPigeon(glb(), controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
  expect(decode).not.toHaveBeenCalled()
})

it('supports the permitted local data-image fallback without object URLs', async () => {
  vi.stubGlobal('createImageBitmap', undefined)
  let assigned = ''
  vi.stubGlobal('Image', class { onload?: () => void; set src(value: string) { assigned = value; this.onload?.() } })
  await decodePigeonImage(new Blob(['test'], { type: 'image/png' }))
  expect(assigned).toMatch(/^data:image\/png;base64,/)
})
