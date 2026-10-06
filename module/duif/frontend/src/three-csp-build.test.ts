import { readFileSync, realpathSync } from 'node:fs'
import { createRequire } from 'node:module'
import { describe, expect, it } from 'vitest'
import { restrictGltfLoaderSource, threeCspPlugin } from '../three-csp-build'

const loaderFile = realpathSync(createRequire(import.meta.url).resolve('three/addons/loaders/GLTFLoader.js'))
const source = readFileSync(loaderFile, 'utf8')
const deniedCall = 'sourceURI = URL.createObjectURL( blob );'

describe('reviewed Three CSP boundary', () => {
  it('disables only the reviewed fallback call in the installed loader', () => {
    const output = restrictGltfLoaderSource(source, loaderFile, loaderFile)!
    expect(output).not.toContain('createObjectURL')
    expect(output).toContain('throw new Error( "Embedded image decoder required" );')
    expect(output.replace('throw new Error( "Embedded image decoder required" );', deniedCall)).toBe(source)
    expect(restrictGltfLoaderSource(source, `${loaderFile}?v=build`, loaderFile)).toBe(output)
  })

  it('rejects source drift, absent calls and duplicate calls', () => {
    for (const changed of [source + '\n', source.replace(deniedCall, ''), source.replace(deniedCall, deniedCall + deniedCall)]) {
      expect(() => restrictGltfLoaderSource(changed, loaderFile, loaderFile)).toThrow('Unreviewed Three image loader')
    }
  })

  it('does not hide the same API in unrelated application or vendor files', () => {
    for (const otherFile of [`${loaderFile}.other`, '/application/GLTFLoader.js', '/application/meeting.ts']) {
      expect(restrictGltfLoaderSource(deniedCall, otherFile, loaderFile)).toBeNull()
    }
  })

  it('runs before other transforms without excluding development builds', () => {
    const plugin = threeCspPlugin()
    expect(plugin.enforce).toBe('pre')
    expect(plugin.apply).toBeUndefined()
  })
})
