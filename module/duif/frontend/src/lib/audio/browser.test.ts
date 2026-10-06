import { afterEach, describe, expect, it, vi } from 'vitest'
import { browserPorts } from './browser'

afterEach(() => vi.unstubAllGlobals())

class AudioDouble {
  static current: AudioDouble
  static rejected = false
  onerror: (() => void) | null = null
  onended: (() => void) | null = null
  paused = false
  constructor(readonly src: string) { AudioDouble.current = this }
  play(): Promise<void> { return AudioDouble.rejected ? Promise.reject(new Error('blocked')) : Promise.resolve() }
  pause(): void { this.paused = true }
  removeAttribute(): void {}
}

describe('Browser sound seam', () => {
  it('reports blocked sound even when the failure precedes subscription', async () => {
    AudioDouble.rejected = true
    vi.stubGlobal('Audio', AudioDouble)
    const sound = browserPorts(vi.fn()).playSound('/api/duif/v1/sounds/test')
    await Promise.resolve()
    await Promise.resolve()
    const failed = vi.fn()
    const started = vi.fn()
    sound.onError?.(failed)
    sound.onStarted?.(started)
    expect(failed).toHaveBeenCalledOnce()
    expect(started).not.toHaveBeenCalled()
    sound.stop()
    expect(AudioDouble.current.paused).toBe(true)
  })

  it('reports start and actual media end and does not report a stopped handle again', async () => {
    AudioDouble.rejected = false
    vi.stubGlobal('Audio', AudioDouble)
    const sound = browserPorts(vi.fn()).playSound('/api/duif/v1/sounds/test')
    const started = vi.fn()
    const ended = vi.fn()
    sound.onStarted?.(started)
    sound.onEnded(ended)
    await Promise.resolve()
    expect(started).toHaveBeenCalledOnce()
    AudioDouble.current.onended?.()
    expect(ended).toHaveBeenCalledOnce()
    sound.stop()
    AudioDouble.current.onended?.()
    expect(ended).toHaveBeenCalledOnce()
  })
})

class BufferSourceDouble {
  buffer: unknown = null
  onended: (() => void) | null = null
  connect(): void {}
  disconnect(): void {}
  start(): void {}
  stop(): void {}
}
class AudioContextDouble {
  static current: AudioContextDouble
  state = 'running'
  currentTime = 0
  destination = {}
  sources: BufferSourceDouble[] = []
  constructor() { AudioContextDouble.current = this }
  createGain() { return { connect() {}, disconnect() {}, gain: { value: 1, setValueAtTime() {}, linearRampToValueAtTime() {} } } }
  createBuffer(_channels: number, length: number, sampleRate: number) { return { duration: length / sampleRate, getChannelData: () => new Float32Array(length) } }
  createBufferSource() { const source = new BufferSourceDouble(); this.sources.push(source); return source }
}

describe('Browser PCM drain', () => {
  it('finishes only after every scheduled source actually ends', () => {
    vi.stubGlobal('AudioContext', AudioContextDouble)
    const stream = browserPorts(vi.fn()).createAudioContext().openStream(24000)
    stream.write(new Int16Array(240))
    stream.write(new Int16Array(240))
    const ended = vi.fn()
    stream.finish?.(ended)
    expect(ended).not.toHaveBeenCalled()
    AudioContextDouble.current.sources[0].onended?.()
    expect(ended).not.toHaveBeenCalled()
    AudioContextDouble.current.sources[1].onended?.()
    expect(ended).toHaveBeenCalledOnce()
  })

  it('does not confuse an intentional local stop with natural completion', () => {
    vi.stubGlobal('AudioContext', AudioContextDouble)
    const stream = browserPorts(vi.fn()).createAudioContext().openStream(24000)
    stream.write(new Int16Array(240))
    const ended = vi.fn()
    stream.finish?.(ended)
    stream.stop()
    AudioContextDouble.current.sources[0].onended?.()
    expect(ended).not.toHaveBeenCalled()
  })
})
