import { Box3, Object3D, Vector3 } from 'three'
import { ConvexHull } from 'three/addons/math/ConvexHull.js'
import { capturePigeonPoints } from './pigeonFraming'
import type { createPigeonMotion } from './pigeonMotion'

/** Preserve the support surface exactly, without retaining all interior vertices. */
export function convexSupport(points: Float32Array) {
  const vertices: Vector3[] = []
  for (let i = 0; i < points.length; i += 3) vertices.push(new Vector3(points[i], points[i + 1], points[i + 2]))
  if (vertices.length < 4) return points
  const hull = new ConvexHull().setFromPoints(vertices), used = new Set<Vector3>()
  for (const face of hull.faces) {
    let edge = face.edge
    do { used.add(edge.head().point); edge = edge.next } while (edge !== face.edge)
  }
  if (!used.size) return points
  const result = new Float32Array(used.size * 3)
  let index = 0
  for (const vertex of used) { result[index++] = vertex.x; result[index++] = vertex.y; result[index++] = vertex.z }
  return result
}

/** Loading includes pose preparation; no hidden perpetual sampling in the scene. */
export async function prepareCameraEnvelope(model: Object3D, motion: ReturnType<typeof createPigeonMotion>, signal: AbortSignal) {
  const supports: number[] = []
  try {
    for (const clip of motion.cameraSamples()) {
      // The clip's own extreme poses (extras.peaks, or the v3 moments)
      // supplement evenly spaced interpolation checks.
      const times = [...new Set([...Array.from({ length: 13 }, (_, i) => clip.duration * i / 12), ...clip.peaks])].sort((a, b) => a - b)
      for (const time of times) {
        if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
        motion.inspect(clip.kind, time)
        const hull = convexSupport(capturePigeonPoints(model))
        for (const value of hull) supports.push(value)
        // Let Stop/navigation and the 10-second load watchdog run between poses.
        await new Promise<void>(resolve => setTimeout(resolve, 0))
      }
    }
  } finally { motion.restore() }
  if (!supports.length) return capturePigeonPoints(model)
  const envelope = convexSupport(new Float32Array(supports))
  const bounds = new Box3()
  for (let i = 0; i < envelope.length; i += 3) bounds.expandByPoint(new Vector3(envelope[i], envelope[i + 1], envelope[i + 2]))
  const center = bounds.getCenter(new Vector3())
  // Small guard for extrema between inspected poses and normal floating-point error.
  for (let i = 0; i < envelope.length; i += 3) {
    envelope[i] = center.x + (envelope[i] - center.x) * 1.035
    envelope[i + 1] = center.y + (envelope[i + 1] - center.y) * 1.035
    envelope[i + 2] = center.z + (envelope[i + 2] - center.z) * 1.035
  }
  return envelope
}
