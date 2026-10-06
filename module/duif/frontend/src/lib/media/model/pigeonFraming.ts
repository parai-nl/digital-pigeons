import { Box3, MathUtils, Mesh, Object3D, PerspectiveCamera, Vector3 } from 'three'
import stage from './stage-composition.json'

export const DEFAULT_AZIMUTH = -.57
export const DEFAULT_ELEVATION = .105
export const DEFAULT_FOV = 32

/** Capture the real rest silhouette once, including the evaluated skin. */
export function capturePigeonPoints(model: Object3D) {
  model.updateMatrixWorld(true)
  const points: number[] = [], vertex = new Vector3()
  model.traverse(node => {
    if (!(node instanceof Mesh) || !node.geometry.attributes.position) return
    for (let index = 0; index < node.geometry.attributes.position.count; index++) {
      node.getVertexPosition(index, vertex).applyMatrix4(node.matrixWorld)
      points.push(vertex.x, vertex.y, vertex.z)
    }
  })
  return new Float32Array(points)
}

export type Composition = {
  /** Screen fraction (half-width, half-height) the moving envelope may reach. */
  envelope: { horizontal: number; vertical: number }
  /** Screen fraction the bird at rest should fill when the envelope allows. */
  fill?: { horizontal: number; vertical: number }
}
/** The original stage fit: the envelope within 86% of the width and 78% of the height. */
export const ENVELOPE_COMPOSITION: Composition = { envelope: { horizontal: .86, vertical: .78 } }
/**
 * The meeting stage: the bird at rest fills 80% of the height, as
 * long as every sampled pose of Peck and WingWave stays within 95% of the
 * width and 94% of the height. Equal rest height for the three pigeons, and
 * as large as their movement allows. The numbers live in
 * stage-composition.json: the one written source that the Blender poster
 * renderer and prepare-meeting-media.mjs read as well.
 */
export const STAGE_COMPOSITION: Composition = { envelope: stage.envelope, fill: stage.fill }

/** Exact perspective fit; actual vertices avoid over-padding empty box corners. */
export function framePigeon(camera: PerspectiveCamera, bounds: Box3, aspect: number, azimuth = DEFAULT_AZIMUTH, elevation = DEFAULT_ELEVATION, points?: Float32Array, fov = DEFAULT_FOV, composition: Composition = ENVELOPE_COMPOSITION, rest?: Float32Array) {
  const size = bounds.getSize(new Vector3()), target = bounds.getCenter(new Vector3())
  const outward = new Vector3(Math.sin(azimuth) * Math.cos(elevation), Math.sin(elevation), Math.cos(azimuth) * Math.cos(elevation))
  const right = new Vector3().crossVectors(new Vector3(0, 1, 0), outward).normalize()
  const up = new Vector3().crossVectors(outward, right).normalize()
  camera.aspect = Math.max(.1, aspect); camera.fov = fov
  const tangent = Math.tan(MathUtils.degToRad(camera.fov / 2))
  // Components relative to the bounds centre in camera axes: across, height, depth.
  const project = (source: Float32Array | undefined) => {
    const across: number[] = [], height: number[] = [], depth: number[] = []
    const add = (x: number, y: number, z: number) => {
      x -= target.x; y -= target.y; z -= target.z
      depth.push(x * outward.x + y * outward.y + z * outward.z)
      across.push(x * right.x + y * right.y + z * right.z)
      height.push(x * up.x + y * up.y + z * up.z)
    }
    if (source?.length) for (let i = 0; i < source.length; i += 3) add(source[i], source[i + 1], source[i + 2])
    else for (const x of [bounds.min.x, bounds.max.x]) for (const y of [bounds.min.y, bounds.max.y]) for (const z of [bounds.min.z, bounds.max.z]) add(x, y, z)
    return { across, height, depth }
  }
  // The closest distance at which the values fit within margin k:
  // for screen margin k the centre lies between max(p+k*z)-k*d and min(p-k*z)+k*d.
  const fit = (values: number[], depth: number[], k: number) => {
    let lower = -Infinity, upper = Infinity
    for (let i = 0; i < values.length; i++) { lower = Math.max(lower, values[i] + k * depth[i]); upper = Math.min(upper, values[i] - k * depth[i]) }
    return (lower - upper) / (2 * k)
  }
  const envelope = project(points), still = rest?.length ? project(rest) : null
  const horizontal = tangent * camera.aspect, vertical = tangent
  const { envelope: margin, fill } = composition
  let distance = Math.max(fit(envelope.across, envelope.depth, horizontal * margin.horizontal), fit(envelope.height, envelope.depth, vertical * margin.vertical), envelope.depth.reduce((most, value) => Math.max(most, value), 0) + size.length() * .01)
  if (still && fill) distance = Math.max(distance, fit(still.across, still.depth, horizontal * fill.horizontal), fit(still.height, still.depth, vertical * fill.vertical))
  // Center the projected silhouette symmetrically at the chosen distance. For
  // the axis that sets the distance this is the fit above; for the other axis
  // it makes the composition independent of the aspect ratio, so a rest poster
  // rendered at another aspect lies exactly over the live bird.
  const symmetric = (values: number[], depth: number[]) => {
    let low = Infinity, high = -Infinity
    for (const value of values) { low = Math.min(low, value); high = Math.max(high, value) }
    for (let step = 0; step < 40; step++) {
      const middle = (low + high) / 2
      let above = 0, below = 0
      for (let i = 0; i < values.length; i++) {
        const projected = (values[i] - middle) / (distance - depth[i])
        above = Math.max(above, projected); below = Math.max(below, -projected)
      }
      if (above > below) low = middle; else high = middle
    }
    return (low + high) / 2
  }
  let lift = symmetric(envelope.height, envelope.depth)
  if (still && fill) {
    // Vertically the bird at rest is centred, shifted only as far as the
    // envelope needs to stay within its margin. Both bounds are independent of
    // the aspect ratio, so the poster rule above still holds.
    let lowest = -Infinity, highest = Infinity
    const k = vertical * margin.vertical
    for (let i = 0; i < envelope.height.length; i++) {
      lowest = Math.max(lowest, envelope.height[i] - k * (distance - envelope.depth[i]))
      highest = Math.min(highest, envelope.height[i] + k * (distance - envelope.depth[i]))
    }
    lift = Math.min(Math.max(symmetric(still.height, still.depth), lowest), Math.max(lowest, highest))
  }
  target.addScaledVector(right, symmetric(envelope.across, envelope.depth)).addScaledVector(up, lift)
  camera.position.copy(target).addScaledVector(outward, distance)
  camera.near = Math.max(.001, distance - size.length() * 1.5)
  camera.far = distance + size.length() * 3
  camera.lookAt(target); camera.updateProjectionMatrix(); camera.updateMatrixWorld()
  return target
}
