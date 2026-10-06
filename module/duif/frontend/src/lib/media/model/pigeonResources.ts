import { Material, Mesh, Object3D, Skeleton, SkinnedMesh, Texture } from 'three'

/** Dispose shared GPU objects once, including bitmap-backed loader images. */
export function disposePigeonResources(object: Object3D) {
  const materials = new Set<Material>(), textures = new Set<Texture>()
  const skeletons = new Set<Skeleton>()
  const geometries = new Set<Mesh['geometry']>()
  object.traverse(node => {
    if (!(node instanceof Mesh)) return
    if (node instanceof SkinnedMesh && node.skeleton) skeletons.add(node.skeleton)
    geometries.add(node.geometry)
    for (const material of Array.isArray(node.material) ? node.material : [node.material]) materials.add(material)
  })
  skeletons.forEach(skeleton => skeleton.dispose())
  geometries.forEach(geometry => geometry.dispose())
  materials.forEach(material => {
    for (const value of Object.values(material)) if (value instanceof Texture) textures.add(value)
    material.dispose()
  })
  const images = new Set<ImageBitmap>()
  textures.forEach(texture => {
    if (typeof ImageBitmap !== 'undefined' && texture.source.data instanceof ImageBitmap) images.add(texture.source.data)
    texture.dispose()
  })
  images.forEach(image => image.close())
}
