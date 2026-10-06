import { createHash } from 'node:crypto'
import { realpathSync } from 'node:fs'
import { createRequire } from 'node:module'
import path from 'node:path'
import type { Plugin } from 'vite'

// Three 0.186.0. Upgrading the loader requires inspecting the new source and
// explicitly updating this fingerprint; an unfamiliar fallback must not ship.
const reviewedLoaderHash = '131c0f78c01d19368ae495caa65b3adaa10487810a36a05bb5901b769a35ac16'
const deniedCall = 'sourceURI = URL.createObjectURL( blob );'
const failClosed = 'throw new Error( "Embedded image decoder required" );'

export function restrictGltfLoaderSource(code: string, id: string, loaderFile: string): string | null {
  if (path.normalize(id.split('?')[0]) !== path.normalize(loaderFile)) return null
  const hash = createHash('sha256').update(code).digest('hex')
  if (hash !== reviewedLoaderHash || code.split(deniedCall).length !== 2) {
    throw new Error('Unreviewed Three image loader: inspect its source before updating the pinned fingerprint')
  }
  // Our embedded texture plugin decodes bytes directly. Disable only Three's
  // unused URL-producing fallback instead of exempting vendor code from the
  // compiled privacy checks or weakening the host application's CSP.
  return code.replace(deniedCall, failClosed)
}

export function threeCspPlugin(): Plugin {
  const loaderFile = realpathSync(createRequire(import.meta.url).resolve('three/addons/loaders/GLTFLoader.js'))
  return {
    name: 'duif:three-embedded-images-only',
    enforce: 'pre',
    transform(code, id) {
      const restricted = restrictGltfLoaderSource(code, id, loaderFile)
      return restricted === null ? null : { code: restricted, map: null }
    },
  }
}
