import { BroadcastChannel as NativeChannel } from 'node:worker_threads'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { onAuthEnded } from './authLifecycle'

const closes: (() => void)[] = []
afterEach(() => { for (const close of closes.splice(0)) close(); vi.unstubAllGlobals() })
function shell() { const channel = new NativeChannel('parai-auth'); closes.push(() => channel.close()); return channel }
function receive(action: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => reject(new Error('auth channel did not arrive')), 1000)
    const close = onAuthEnded(() => { clearTimeout(timeout); resolve() })
    closes.push(() => { clearTimeout(timeout); close() })
    shell().postMessage({ type: action })
  })
}
describe('normal shell auth lifecycle channel', () => {
  it.each(['logout', 'session_expired'])('receives %s from a separate native channel in this process', async (action) => {
    vi.stubGlobal('BroadcastChannel', NativeChannel)
    await receive(action)
  })
  it('ignores non-auth data and detaches idempotently without affecting the sender', async () => {
    vi.stubGlobal('BroadcastChannel', NativeChannel)
    const end = vi.fn(), close = onAuthEnded(end), sender = shell()
    closes.push(close)
    for (const data of [null, 'logout', {}, { type: 'login' }, { type: 'transcript', text: 'synthetic' }]) sender.postMessage(data)
    await new Promise(resolve => setTimeout(resolve, 20))
    expect(end).not.toHaveBeenCalled()
    close(); close(); sender.postMessage({ type: 'logout' })
    await new Promise(resolve => setTimeout(resolve, 20))
    expect(end).not.toHaveBeenCalled()
  })
  it('works standalone when the browser has no BroadcastChannel', () => {
    vi.stubGlobal('BroadcastChannel', undefined)
    const end = vi.fn(), close = onAuthEnded(end)
    close(); expect(end).not.toHaveBeenCalled()
  })
})
