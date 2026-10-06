import { mount, unmount } from 'svelte'
import App from './Entry.svelte'
import { createServices, SERVICES } from './lib/services.svelte'
import { onAuthEnded } from './lib/access/authLifecycle'
import { resumeWorkspace } from './lib/shell/workspacePlace'

// Het contract met de parai-core-shell: de loader importeert
// /modules/duif/bundle.js, roept mountModule(container, context) aan en
// verwacht een synchrone { unmount }. Van de context gebruiken we alleen wat we
// nodig hebben; alles is optioneel zodat standalone dev zonder shell werkt.
export interface ModuleContext {
  mode?: 'embedded' | 'standalone'
  user?: { role?: string }
}

export interface MountResult {
  unmount: () => void
}

export function mountModule(target: HTMLElement, context: ModuleContext = {}): MountResult {
  const services = createServices(context)
  // De plek uit het adres (een herlaad door de taalwissel van de schil) en de
  // login-terugkeer uit de querystring; workspacePlace.ts weegt ze tegen
  // elkaar af.
  resumeWorkspace(services, { hash: window.location.hash, search: window.location.search })
  const app = mount(App, { target, context: new Map([[SERVICES, services]]) })
  let done = false
  let detachAuth = () => {}
  const result = {
    unmount() {
      if (done) return
      done = true
      detachAuth()
      // Eerst de sessies (microfoon, kanaal, audio) neerleggen, dan pas de DOM:
      // ook als een component tijdens het afbreken struikelt is het stil.
      try {
        services.dispose()
      } finally {
        void unmount(app)
      }
    },
  }
  // A pending or failed shell logout cannot leave a live microphone/player,
  // stale chat content or a usable Start button behind in this mount.
  detachAuth = onAuthEnded(() => result.unmount())
  return result
}

export const meta = {
  label: { en: 'Digital Pigeons', nl: 'Digitale Duiven' },
  description: {
    en: 'Three AI pigeon personas that give the city pigeon a voice.',
    nl: "Drie AI-duifpersona's die de stadsduif een stem geven.",
  },
  icon: 'bird',
}
