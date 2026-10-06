import * as THREE from 'three'
import { parseEmbeddedPigeon } from './embeddedPigeon'
import { RoomEnvironment } from 'three/addons/environments/RoomEnvironment.js'
import { boundedAssetLoad } from './boundedAssetLoad'
import { capturePigeonPoints, DEFAULT_AZIMUTH, DEFAULT_ELEVATION, DEFAULT_FOV, framePigeon, STAGE_COMPOSITION } from './pigeonFraming'
import type { PigeonView } from '../types'
import { createPigeonMotion } from './pigeonMotion'
import type { GestureKind, GestureVariant } from './gestureSequence'
import { convexSupport, prepareCameraEnvelope } from './cameraEnvelope'
import { disposePigeonResources } from './pigeonResources'

export type PigeonStage = ReturnType<typeof createPigeonStage>

// Slepen en pijltoetsen tonen een bescheiden ander standpunt rond de kijkhoek
// van de duif, niet een vrije rondgang: dezelfde marges als rond de oude
// driekwartstand (−0,90 tot −0,22 rond −0,57).
const AZIMUTH_REACH = .34
const ELEVATION_REACH: [number, number] = [-.08, .095]

/** A bare facing keeps the original three-quarter view. */
export function stageView(view: PigeonView | 'left' | 'right'): PigeonView {
  return typeof view === 'string' ? { azimuth: DEFAULT_AZIMUTH, elevation: DEFAULT_ELEVATION, fov: DEFAULT_FOV, facing: view } : view
}

/**
 * De camerabaan rond een kijkhoek. De kijkhoek staat in het ongespiegelde
 * GLB-frame; gespiegeld naar rechts kijkt de camera van de andere kant, dus
 * draait de azimut om. De camera zakt nooit onder de vloer.
 */
export function stageOrbit(view: PigeonView) {
  const home = view.facing === 'right' ? -view.azimuth : view.azimuth
  return {
    home, elevation: view.elevation,
    azimuth: [home - AZIMUTH_REACH, home + AZIMUTH_REACH] as [number, number],
    elevations: [Math.max(.01, view.elevation + ELEVATION_REACH[0]), view.elevation + ELEVATION_REACH[1]] as [number, number],
  }
}

/** A quiet local scene. Frames are rendered only for an event, resize or input. */
export function createPigeonStage(host: HTMLElement, reportContextLoss: () => void) {
  let disposed = false, model: THREE.Object3D | null = null
  let motion: ReturnType<typeof createPigeonMotion> | null = null
  let bounds = new THREE.Box3(new THREE.Vector3(-1, 0, -.4), new THREE.Vector3(1, 2.1, .4))
  let silhouette: Float32Array | undefined, still: Float32Array | undefined
  let cancelLoad = () => {}
  let generation = 0
  let view = stageView('right'), orbit = stageOrbit(view)
  let azimuth = orbit.home, elevation = orbit.elevation
  const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true, powerPreference: 'default' })
  renderer.setClearColor(0x000000, 0)
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2))
  renderer.outputColorSpace = THREE.SRGBColorSpace
  renderer.toneMapping = THREE.AgXToneMapping
  renderer.toneMappingExposure = 1.05
  renderer.shadowMap.enabled = true
  renderer.shadowMap.type = THREE.PCFShadowMap
  renderer.domElement.setAttribute('aria-hidden', 'true')
  host.appendChild(renderer.domElement)

  const scene = new THREE.Scene()
  const camera = new THREE.PerspectiveCamera(DEFAULT_FOV, 1, .1, 50)
  const sky = new THREE.HemisphereLight(0xf4f6fa, 0x8b8172, .65)
  const key = new THREE.DirectionalLight(0xfff4e5, 2.1)
  key.castShadow = true
  key.shadow.mapSize.set(1024, 1024)
  key.shadow.radius = 3
  const fill = new THREE.DirectionalLight(0xe7efff, .8)
  scene.add(sky, key, key.target, fill)

  // Studio reflections add quiet feather highlights without a visible backdrop.
  let studioMap: THREE.WebGLRenderTarget | undefined
  const room = new RoomEnvironment()
  const pmrem = new THREE.PMREMGenerator(renderer)
  try {
    studioMap = pmrem.fromScene(room, .06)
    scene.environment = studioMap.texture
    scene.environmentIntensity = .65
  } catch { /* Direct lighting remains usable on devices without PMREM support. */ }
  finally { room.dispose(); pmrem.dispose() }

  // A continuous contact shadow avoids polygonal feather silhouettes on the floor.
  // It is an explicitly designed soft contact, not a physically traced shadow.
  const pixels = new Uint8Array(128 * 128 * 4)
  for (let y = 0; y < 128; y++) for (let x = 0; x < 128; x++) {
    const radius = Math.hypot((x - 63.5) / 63.5, (y - 63.5) / 63.5)
    const opacity = Math.exp(-radius * radius * 5.5) * Math.max(0, 1 - radius ** 4)
    const index = (y * 128 + x) * 4
    pixels[index] = pixels[index + 1] = pixels[index + 2] = 255
    pixels[index + 3] = Math.round(Math.max(0, opacity) * 255)
  }
  const shadowTexture = new THREE.DataTexture(pixels, 128, 128, THREE.RGBAFormat)
  shadowTexture.needsUpdate = true
  shadowTexture.magFilter = THREE.LinearFilter
  const contact = new THREE.Mesh(new THREE.PlaneGeometry(1, 1), new THREE.MeshBasicMaterial({
    map: shadowTexture, color: 0x433d32, transparent: true, opacity: .25, depthWrite: false, toneMapped: false,
  }))
  contact.rotation.x = -Math.PI / 2
  contact.visible = false
  scene.add(contact)

  const render = () => { if (!disposed) renderer.render(scene, camera) }
  let viewportWidth = 0, viewportHeight = 0
  const frame = () => {
    if (disposed) return
    const width = host.clientWidth, height = host.clientHeight
    if (!width || !height) return
    if (width !== viewportWidth || height !== viewportHeight) {
      renderer.setSize(width, height); viewportWidth = width; viewportHeight = height
    }
    framePigeon(camera, bounds, width / height, azimuth, elevation, silhouette, view.fov, STAGE_COMPOSITION, still)
    render()
  }
  const observer = new ResizeObserver(frame)
  observer.observe(host)

  let drag: { id: number; x: number; y: number; azimuth: number; elevation: number } | null = null
  const pointDown = (event: PointerEvent) => {
    if (event.button !== 0 || !model) return
    host.focus({ preventScroll: true })
    drag = { id: event.pointerId, x: event.clientX, y: event.clientY, azimuth, elevation }
    renderer.domElement.setPointerCapture(event.pointerId)
  }
  const pointMove = (event: PointerEvent) => {
    if (!drag || event.pointerId !== drag.id) return
    azimuth = THREE.MathUtils.clamp(drag.azimuth - (event.clientX - drag.x) * .003, ...orbit.azimuth)
    elevation = THREE.MathUtils.clamp(drag.elevation + (event.clientY - drag.y) * .0015, ...orbit.elevations)
    frame()
  }
  const pointUp = () => { drag = null }
  const keyDown = (event: KeyboardEvent) => {
    if (!model || !['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home'].includes(event.key)) return
    event.preventDefault()
    if (event.key === 'Home') { azimuth = orbit.home; elevation = orbit.elevation }
    if (event.key === 'ArrowLeft') azimuth = Math.max(orbit.azimuth[0], azimuth - .045)
    if (event.key === 'ArrowRight') azimuth = Math.min(orbit.azimuth[1], azimuth + .045)
    if (event.key === 'ArrowUp') elevation = Math.min(orbit.elevations[1], elevation + .02)
    if (event.key === 'ArrowDown') elevation = Math.max(orbit.elevations[0], elevation - .02)
    frame()
  }
  renderer.domElement.addEventListener('pointerdown', pointDown)
  renderer.domElement.addEventListener('pointermove', pointMove)
  renderer.domElement.addEventListener('pointerup', pointUp)
  renderer.domElement.addEventListener('pointercancel', pointUp)
  renderer.domElement.addEventListener('lostpointercapture', pointUp)
  host.addEventListener('keydown', keyDown)

  const contextLost = (event: Event) => {
    event.preventDefault()
    if (disposed) return
    generation++; cancelLoad()
    reportContextLoss()
  }
  renderer.domElement.addEventListener('webglcontextlost', contextLost)
  frame()

  function unload() {
    motion?.dispose(); motion = null; silhouette = undefined; still = undefined
    if (model) { scene.remove(model); disposePigeonResources(model); model = null }
    contact.visible = false
  }

  function load(assetUrl: string, ready: (canReact: boolean) => void, failed: () => void, requested: PigeonView | 'left' | 'right' = 'right') {
    const current = ++generation
    cancelLoad(); unload(); view = stageView(requested); orbit = stageOrbit(view)
    const facing = view.facing
    azimuth = orbit.home; elevation = orbit.elevation
    let url: URL
    try { url = new URL(assetUrl, window.location.href) } catch { failed(); return () => {} }
    if (url.origin !== window.location.origin) { failed(); return () => {} }
    const cancelCurrent = boundedAssetLoad(async signal => {
      const response = await fetch(url, { signal, credentials: 'same-origin' })
      if (!response.ok) throw new Error('Model asset unavailable')
      const bytes = await response.arrayBuffer()
      if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
      const gltf = await parseEmbeddedPigeon(bytes, signal)
      const wrapper = new THREE.Group(); wrapper.name = 'PigeonFacing'
      wrapper.scale.x = facing === 'right' ? -1 : 1; wrapper.add(gltf.scene); wrapper.updateMatrixWorld(true)
      const controller = createPigeonMotion(gltf.scene, gltf.animations)
      try {
        const envelope = await prepareCameraEnvelope(wrapper, controller, signal)
        // The bird at rest sets her size on the stage; the envelope keeps
        // every sampled pose of both clips inside it (STAGE_COMPOSITION).
        const rest = convexSupport(capturePigeonPoints(wrapper))
        return { gltf, wrapper, controller, envelope, rest }
      } catch (error) { controller.dispose(); disposePigeonResources(wrapper); throw error }
    }, {
      loaded: prepared => {
        const { wrapper, controller, envelope, rest } = prepared
        if (disposed || current !== generation) { controller.dispose(); disposePigeonResources(wrapper); return }
        model = wrapper; motion = controller; silhouette = envelope; still = rest
        bounds = new THREE.Box3().setFromObject(model)
        if (bounds.isEmpty() || bounds.max.y - bounds.min.y < .0001 || !Number.isFinite(bounds.getSize(new THREE.Vector3()).length())) { motion.dispose(); motion = null; disposePigeonResources(model); model = null; failed(); return }
        const size = bounds.getSize(new THREE.Vector3()), center = bounds.getCenter(new THREE.Vector3())
        model.traverse(node => {
          if (!(node instanceof THREE.Mesh)) return
          node.castShadow = true; node.receiveShadow = true
          for (const material of Array.isArray(node.material) ? node.material : [node.material]) {
            // Preserve authored colours, maps, roughness and physical extensions.
            for (const value of Object.values(material)) if (value instanceof THREE.Texture) {
              value.anisotropy = Math.min(8, renderer.capabilities.getMaxAnisotropy())
              value.needsUpdate = true
            }
          }
        })
        scene.add(model)

        // Het licht staat aan de kant van de camera en van voren op de kop.
        // Kijkt de camera vanaf −Z (een duif die naar rechts kijkt),
        // dan verhuist het hoofdlicht mee naar die kant; gespiegeld (alleen nog
        // in ontwerpweergaven) spiegelt het mee met het model.
        const side = facing === 'right' ? -1 : 1
        const flank = Math.cos(orbit.home) < -1e-6 ? -1 : 1
        key.position.copy(center).add(new THREE.Vector3(-size.y * 1.4 * side, size.y * 2.5, size.y * 2.4 * flank))
        key.target.position.copy(center)
        fill.position.copy(center).add(new THREE.Vector3(size.y * 2 * side, size.y, -size.y * flank))
        const reach = size.length()
        key.shadow.camera.left = key.shadow.camera.bottom = -reach
        key.shadow.camera.right = key.shadow.camera.top = reach
        key.shadow.camera.near = .01
        key.shadow.camera.far = reach * 8
        key.shadow.normalBias = size.y * .002
        key.shadow.camera.updateProjectionMatrix()
        contact.position.set(center.x + size.x * .05 * side, bounds.min.y + size.y * .001, center.z)
        contact.scale.set(size.x * .85, Math.max(size.z * 1.5, size.y * .42), 1)
        contact.visible = true
        frame(); if (!disposed && current === generation) ready(motion.canReact)
      },
      failed: () => { if (!disposed && current === generation) failed() },
      disposeLate: prepared => { prepared.controller.dispose(); disposePigeonResources(prepared.wrapper) },
    })
    cancelLoad = cancelCurrent
    return () => { cancelCurrent(); if (current === generation) { generation++; unload() } }
  }

  return {
    load, render,
    beginReaction: (variant: GestureVariant) => motion?.prepare(variant) ?? null,
    restore: () => motion?.restore(),
    pose: (time: number) => motion?.sample(time),
    // Een vaste houding op een exact cliptijdstip, zonder het willekeurige
    // tempo van een reactie: voor controles en renders (meeting-poster-render).
    hold: (kind: GestureKind, time: number) => { motion?.restore(); motion?.inspect(kind, time) },
    dispose: () => {
      if (disposed) return
      disposed = true; generation++; cancelLoad(); observer.disconnect(); drag = null
      host.removeEventListener('keydown', keyDown)
      renderer.domElement.removeEventListener('pointerdown', pointDown)
      renderer.domElement.removeEventListener('pointermove', pointMove)
      renderer.domElement.removeEventListener('pointerup', pointUp)
      renderer.domElement.removeEventListener('pointercancel', pointUp)
      renderer.domElement.removeEventListener('lostpointercapture', pointUp)
      renderer.domElement.removeEventListener('webglcontextlost', contextLost)
      unload(); disposePigeonResources(contact)
      key.dispose(); fill.dispose(); studioMap?.dispose()
      // Elke duifwissel maakt een nieuwe renderer; geef de context meteen vrij
      // in plaats van te wachten tot de browser er te veel heeft.
      renderer.dispose(); renderer.forceContextLoss(); renderer.domElement.remove()
    },
  }
}
