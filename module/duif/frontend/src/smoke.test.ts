import { mount, tick, unmount } from 'svelte'
import { describe, expect, it, vi } from 'vitest'
import { BroadcastChannel as NativeChannel } from 'node:worker_threads'
import { waitFor } from '@testing-library/svelte'
import App from './App.svelte'
import { meta, mountModule } from './index'
import { createServices, SERVICES } from './lib/services.svelte'

describe('mountModule', () => {
  it('mounts into the target and unmounts idempotently', () => {
    const target = document.createElement('div')
    document.body.append(target)
    const handle = mountModule(target, { mode: 'standalone' })
    expect(target.querySelector('[data-testid="duif-root"]')).not.toBeNull()
    handle.unmount()
    handle.unmount()
    target.remove()
  })

  it('hides the orb stream while the chat tab is active', async () => {
    const target = document.createElement('div')
    document.body.append(target)
    const services = createServices({ mode: 'standalone' })
    const app = mount(App, { target, context: new Map([[SERVICES, services]]) })
    await tick()
    // De chat-tab is de starttab: de VoiceOrb krijgt dan geen stream.
    expect(services.meeting.orbVisible).toBe(false)
    services.dispose()
    await unmount(app)
    target.remove()
  })

  it('unmounts its local UI on shell logout before a later shell teardown', async () => {
    vi.stubGlobal('BroadcastChannel', NativeChannel)
    const target = document.createElement('div'); document.body.append(target)
    const handle = mountModule(target, { mode: 'embedded' })
    const sender = new NativeChannel('parai-auth')
    try {
      expect(target.querySelector('[data-testid="duif-root"]')).not.toBeNull()
      sender.postMessage({ type: 'logout' })
      await waitFor(() => expect(target.querySelector('[data-testid="duif-root"]')).toBeNull())
      handle.unmount(); handle.unmount()
    } finally { sender.close(); handle.unmount(); target.remove(); vi.unstubAllGlobals() }
  })

  // De testomgeving zelf. Draait jsdom op een opaque origin (`about:blank`),
  // dan gooit elke localStorage-aanraking een SecurityError. Zo'n worp vliegt
  // door de opruiming van een afterEach heen: de gerenderde DOM blijft staan en
  // de volgende test vindt alles dubbel. Dat is geen fout in de suite die
  // struikelt, dus faalt hij hier — met de oorzaak in de melding.
  //
  // Vitest geeft jsdom zelf al een gewone origin, dus "niet opaque" bewijst
  // niets: die assertie bleef staan toen de configregel weg was. Wat we
  // werkelijk willen is dat de origin uit onze eigen vite.config.ts komt, en
  // dat controleert dit door hem letterlijk te noemen. Verandert een default
  // van vitest, dan valt deze test om in plaats van de suite die er toevallig
  // als eerste last van krijgt.
  it('runs on the origin we configured, so storage works', () => {
    expect(location.origin).toBe('http://localhost')
    expect(() => {
      localStorage.setItem('duif-origin-probe', '1')
      localStorage.clear()
    }).not.toThrow()
  })

  it('exposes bilingual meta for the shell', () => {
    expect(meta.label.en).toBe('Digital Pigeons')
    expect(meta.label.nl).toBe('Digitale Duiven')
    expect(meta.icon).toBe('bird')
  })
})
