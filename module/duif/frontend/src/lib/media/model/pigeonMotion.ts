import { AnimationClip, AnimationMixer, LoopOnce, Mesh, Object3D } from 'three'
import { createGestureSequence, type GestureKind, type GestureVariant } from './gestureSequence'

export const MAX_REACTION_SECONDS = 11.5
const names: Record<GestureKind, string> = { peck: 'Peck', ruffle: 'WingWave' }

/** Baked, anatomically reviewed movement. Never substitute the old tiny rig pulse. */
export function createPigeonMotion(model: Object3D, animations: AnimationClip[], random: () => number = Math.random) {
  const mixer = new AnimationMixer(model), choose = createGestureSequence(random)
  const clips = new Map<GestureKind, AnimationClip>()
  for (const kind of ['peck', 'ruffle'] as const) {
    const clip = animations.find(item => item.name === names[kind])
    if (clip && clip.validate() && Number.isFinite(clip.duration) && clip.duration > .1 && clip.duration / .9 + .12 < MAX_REACTION_SECONDS) clips.set(kind, clip)
  }
  const nodes: Object3D[] = []; model.traverse(node => nodes.push(node))
  const rest = nodes.map(node => ({ node, position: node.position.clone(), quaternion: node.quaternion.clone(), scale: node.scale.clone(), morph: node instanceof Mesh ? node.morphTargetInfluences?.slice() : undefined }))
  const restorePose = () => rest.forEach(({ node, position, quaternion, scale, morph }) => {
    node.position.copy(position); node.quaternion.copy(quaternion); node.scale.copy(scale)
    if (morph && node instanceof Mesh && node.morphTargetInfluences) node.morphTargetInfluences.splice(0, morph.length, ...morph)
  })
  let current: { kind: GestureKind; rate: number; duration: number } | null = null
  let activeKind: GestureKind | null = null
  const restore = () => { mixer.stopAllAction(); activeKind = null; current = null; restorePose(); model.updateMatrixWorld(true) }
  function inspect(kind: GestureKind, time: number) {
    const clip = clips.get(kind)
    if (!clip) return
    if (activeKind !== kind) {
      mixer.stopAllAction(); restorePose()
      const action = mixer.clipAction(clip); action.reset(); action.setLoop(LoopOnce, 1); action.clampWhenFinished = true; action.play()
      activeKind = kind
    }
    mixer.clipAction(clip).paused = false
    mixer.setTime(Math.max(0, Math.min(time, clip.duration)))
    model.updateMatrixWorld(true)
  }
  const prepare = (variant: GestureVariant = 'auto') => {
    restore()
    const choice = choose(variant), clip = clips.get(choice.kind)
    if (!clip) return null
    current = { ...choice, duration: clip.duration / choice.rate + .12 }
    inspect(choice.kind, 0)
    return { ...current }
  }
  const sample = (time: number) => {
    if (!current) return
    if (!Number.isFinite(time) || time < 0 || time >= current.duration) { restore(); return }
    const clip = clips.get(current.kind)!
    if (time * current.rate >= clip.duration) { mixer.stopAllAction(); activeKind = null; restorePose(); return }
    inspect(current.kind, time * current.rate)
  }
  // De uiterste houdingen van een clip, in seconden. Een model kan ze
  // meegeven als extras.peaks van de animatie (glTF); zonder staan de
  // momenten van v3 er (pik op 1,0 s, vleugelpieken op 34/30 en 58/30 s).
  const peaks = (kind: GestureKind, clip: AnimationClip) => {
    const given: unknown = clip.userData?.peaks
    const listed = Array.isArray(given) ? given.filter((time): time is number => typeof time === 'number' && Number.isFinite(time) && time >= 0 && time <= clip.duration) : []
    return listed.length ? listed : (kind === 'peck' ? [1] : [34 / 30, 58 / 30]).filter(time => time < clip.duration)
  }
  const cameraSamples = () => Array.from(clips, ([kind, clip]) => ({ kind, duration: clip.duration, peaks: peaks(kind, clip) }))
  return {
    restore, prepare, sample, inspect, cameraSamples, canReact: clips.size === 2,
    dispose: () => { restore(); mixer.uncacheRoot(model) },
  }
}
