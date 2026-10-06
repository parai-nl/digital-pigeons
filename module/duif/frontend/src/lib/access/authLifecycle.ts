// The shipping shell announces logout/session expiry on this same-origin
// channel at the start of logout. A separate instance also receives messages
// from the shell in this document; it does not wait for the logout HTTP reply.
// No private shell implementation, fetch override or DOM selector is needed.
export function onAuthEnded(end: () => void): () => void {
  if (typeof globalThis.BroadcastChannel !== 'function') return () => {}
  const channel = new BroadcastChannel('parai-auth')
  let active = true
  const close = () => {
    if (!active) return
    active = false
    channel.onmessage = null
    channel.close()
  }
  channel.onmessage = (event: MessageEvent<unknown>) => {
    const data = event.data
    if (!active || !data || typeof data !== 'object' || !('type' in data)) return
    if (data.type !== 'logout' && data.type !== 'session_expired') return
    close()
    end()
  }
  return close
}
