// De plek van de lezer in het adresfragment. Wat hier vastligt:
// alleen gevalideerde aanwijzingen gaan erin, een verzonnen id komt er niet
// uit, het adres krijgt geen nieuwe stap, en een herlaad zet de lezer terug
// waar hij was — maar opent nooit een gesprek dat niet van hem is.
import { afterEach, describe, expect, it, vi } from 'vitest'
import { jsonResponse, scriptedFetch, type Route } from '../testing/fetch'
import { GRAPH_IDS, graphRoutes } from '../testing/graph'
import { pigeonRoute, testServices } from '../testing/chat'
import type { SessionRef } from '../chat/chatStore.svelte'
import { CONVERSATION_CASES } from '../chat/cases'
import { currentPlace, readWorkspacePlace, rememberWorkspacePlace, resumeWorkspace, workspaceHash, type WorkspacePlace } from './workspacePlace'

const HOME: WorkspacePlace = { tab: 'chat', mode: 'chat', help: false, node: null, pigeon: null, session: null }
const OWN = '0123456789abcdef0123456789abcdef'
const FOREIGN = 'fedcba9876543210fedcba9876543210'

async function settle(): Promise<void> {
  for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0))
}

afterEach(() => {
  vi.restoreAllMocks()
  globalThis.history.replaceState(null, '', '/')
})

describe('workspacePlace in het adres', () => {
  it('houdt het adres kaal op het startscherm', () => {
    expect(workspaceHash('', HOME)).toBe('')
    expect(readWorkspacePlace('')).toEqual(HOME)
  })

  it('schrijft en leest de graaf met zijn knoop en de weergave eronder, en laat vreemde sleutels staan', () => {
    const place: WorkspacePlace = { ...HOME, tab: 'graph', mode: 'meeting', node: GRAPH_IDS.destillaatOne }
    const hash = workspaceHash('#anders=1&view=meeting', place)
    expect(hash).toContain('anders=1')
    expect(readWorkspacePlace(hash)).toEqual(place)
    expect(readWorkspacePlace(workspaceHash(hash, HOME))).toEqual(HOME)
    expect(workspaceHash(hash, HOME)).toBe('#anders=1')
  })

  it('laat niets door wat niet de vorm van een id heeft, in geen van beide richtingen', () => {
    const forged = '#view=admin&mode=x&help=yes&node=' + encodeURIComponent('Wat ik de duif vroeg') + '&pigeon=evil&chat=' + encodeURIComponent('mijn vraag')
    expect(readWorkspacePlace(forged)).toEqual(HOME)
    // Een knoop hoort alleen bij de graaf; op de chat valt hij weg.
    expect(readWorkspacePlace('#node=' + encodeURIComponent(GRAPH_IDS.thema)).node).toBeNull()
    const written = workspaceHash('', { ...HOME, node: 'vrije tekst', session: 'een concept', pigeon: 'iemand' } as WorkspacePlace)
    expect(written).toBe('')
  })

  it('vervangt de huidige stap met de stand van de schil, en alleen als er iets veranderde', () => {
    globalThis.history.replaceState({ 'sveltekit:history': 7 }, '', '/duif?pigeon=biological')
    const replace = vi.spyOn(globalThis.history, 'replaceState')
    const push = vi.spyOn(globalThis.history, 'pushState')
    rememberWorkspacePlace({ ...HOME, tab: 'meeting' })
    expect(replace).toHaveBeenCalledExactlyOnceWith({ 'sveltekit:history': 7 }, '', '/duif?pigeon=biological#view=meeting')
    rememberWorkspacePlace({ ...HOME, tab: 'meeting' })
    expect(replace).toHaveBeenCalledOnce()
    rememberWorkspacePlace(HOME)
    expect(globalThis.location.href).toMatch(/\/duif\?pigeon=biological$/)
    expect(push).not.toHaveBeenCalled()
  })

  it('werkt door als de browser het vervangen weigert', () => {
    vi.spyOn(globalThis.history, 'replaceState').mockImplementation(() => { throw new Error('throttled') })
    expect(() => rememberWorkspacePlace({ ...HOME, tab: 'graph' })).not.toThrow()
  })
})

describe('workspacePlace na een herlaad', () => {
  const sessions: SessionRef[] = [
    { id: OWN, pigeonId: 'individualist', lang: 'nl', createdAt: '2026-10-06', updatedAt: '2026-10-06' },
  ]
  function services(extra: Route[] = []) {
    const scripted = scriptedFetch([
      ...extra,
      pigeonRoute(),
      { method: 'GET', match: '/chat/sessions/' + OWN + '/messages', respond: () => jsonResponse({ messages: [{ id: 'm1', role: 'user', content: 'Waar broeden duiven?' }] }) },
      { method: 'GET', match: '/chat/sessions', respond: () => jsonResponse({ sessions }) },
      ...graphRoutes(),
    ])
    return { scripted, services: testServices(scripted.fetch) }
  }

  const resume = (s: ReturnType<typeof services>['services'], place: WorkspacePlace, search = '') => resumeWorkspace(s, { hash: workspaceHash('', place), search })

  it('doet niets zonder plek en zonder login-terugkeer', () => {
    const { services: s, scripted } = services()
    resumeWorkspace(s, { hash: '', search: '' })
    expect(s.shell.tab).toBe('chat')
    expect(scripted.calls).toEqual([])
  })

  it('opent de graaf weer bij dezelfde knoop, met de weergave eronder', async () => {
    const { services: s } = services()
    resume(s, { ...HOME, tab: 'graph', mode: 'meeting', node: GRAPH_IDS.destillaatOne })
    expect(s.shell.tab).toBe('graph')
    expect(s.shell.previousTab).toBe('meeting')
    await settle()
    expect(s.graph.selected).toBe(GRAPH_IDS.destillaatOne)
    s.dispose()
  })

  it('hervat het eigen gesprek met de duif die erbij hoort', async () => {
    const { services: s } = services()
    resume(s, { ...HOME, pigeon: 'individualist', session: OWN })
    await settle()
    expect(s.chat.sessionId).toBe(OWN)
    expect(s.chat.selectedPigeonId).toBe('individualist')
    expect(s.chat.messages.map((m) => m.content)).toEqual(['Waar broeden duiven?'])
    s.dispose()
  })

  it('opent geen gesprek dat niet in de eigen lijst staat, en hervat ook niet stilletjes het laatste', async () => {
    const { services: s, scripted } = services()
    resume(s, { ...HOME, pigeon: 'individualist', session: FOREIGN })
    await settle()
    expect(scripted.calls.some((call) => call.url.includes('/messages'))).toBe(false)
    expect(s.chat.sessionId).toBe('')
    // De duif staat er weer, op haar startscherm zoals vóór de herlaad.
    expect(s.chat.selectedPigeonId).toBe('individualist')
    s.dispose()
  })

  it('wint niet van wat de lezer intussen zelf koos', async () => {
    const { services: s, scripted } = services()
    resume(s, { ...HOME, session: OWN })
    s.chat.draft = 'Al aan het typen'
    await settle()
    expect(scripted.calls.some((call) => call.url.includes('/messages'))).toBe(false)
    expect(s.chat.draft).toBe('Al aan het typen')
    s.dispose()
  })

  it('zet de signalerende duif en het vergaderscherm terug zonder iets te starten', async () => {
    const { services: s, scripted } = services()
    resume(s, { ...HOME, tab: 'meeting', help: true, pigeon: 'nonverbal' })
    expect(s.shell.tab).toBe('meeting')
    expect(s.shell.help).toBe(true)
    expect(s.shell.signaling).toBe(true)
    await settle()
    expect(s.meeting.phase).toBe('setup')
    expect(scripted.calls.some((call) => call.url.includes('/meeting/'))).toBe(false)
    s.dispose()
  })

  // Na het inloggen met een voorbeeldvraag staat er
  // meteen `#pigeon=…` in het adres. Een taalwissel daarna mag die voorbereide
  // vraag niet wissen; zij komt terug in de nieuwe taal.
  it('laat de voorbereide vraag van de login-terugkeer staan na een herlaad', async () => {
    const { services: s } = services()
    s.locale.lang = 'nl'
    resume(s, { ...HOME, pigeon: 'individualist' }, '?pigeon=individualist&case=square-event')
    await settle()
    expect(s.chat.selectedPigeonId).toBe('individualist')
    expect(s.chat.draft).toBe(CONVERSATION_CASES.find((item) => item.id === 'square-event')?.question.nl)
  })

  it('laat de login-terugkeer los zodra de lezer verder ging: een gesprek of een andere duif', async () => {
    const withSession = services()
    resume(withSession.services, { ...HOME, pigeon: 'individualist', session: OWN }, '?pigeon=individualist&case=square-event')
    await settle()
    expect(withSession.services.chat.sessionId).toBe(OWN)
    expect(withSession.services.chat.draft).toBe('')

    const otherPigeon = services()
    resume(otherPigeon.services, { ...HOME, pigeon: 'individualist' }, '?pigeon=biological&case=square-event')
    await settle()
    expect(otherPigeon.services.chat.selectedPigeonId).toBe('individualist')
    expect(otherPigeon.services.chat.draft).toBe('')
  })

  // Het adres wordt al bij het mounten bijgehouden.
  // Wat nog onderweg is, hoort er dan niet uit te vallen — ook niet als de
  // lijst traag is of mislukt — maar wel zodra de lezer zelf iets kiest.
  it('houdt gesprek en duif in het adres vast terwijl de gesprekslijst traag is of mislukt', async () => {
    let fail: () => void = () => {}
    const { services: s } = services([
      { method: 'GET', match: /\/chat\/sessions$/, respond: () => new Promise<Response>((resolve) => { fail = () => resolve(jsonResponse({}, 503)) }) },
    ])
    resume(s, { ...HOME, pigeon: 'individualist', session: OWN })
    expect(currentPlace(s)).toMatchObject({ pigeon: 'individualist', session: OWN })
    await settle()
    expect(currentPlace(s)).toMatchObject({ pigeon: 'individualist', session: OWN })
    fail()
    await settle()
    expect(s.chat.sessionsLoaded).toBe(false)
    expect(currentPlace(s)).toMatchObject({ pigeon: 'individualist', session: OWN })

    s.chat.draft = 'Een eigen vraag'
    expect(currentPlace(s)).toMatchObject({ pigeon: null, session: null })
    s.dispose()
  })

  it('houdt de graafknoop vast als de kaart even niet laadt, en laat hem los als de knoop er niet is', async () => {
    const down = services([{ method: 'GET', match: '/graph/overview', respond: () => jsonResponse({}, 503) }])
    resume(down.services, { ...HOME, tab: 'graph', node: GRAPH_IDS.destillaatOne })
    expect(currentPlace(down.services).node).toBe(GRAPH_IDS.destillaatOne)
    await settle()
    expect(down.services.graph.selected).toBeNull()
    expect(currentPlace(down.services).node).toBe(GRAPH_IDS.destillaatOne)
    down.services.dispose()

    const gone = services([{ method: 'GET', match: /\/graph\/nodes\/[^/]+$/, respond: () => jsonResponse({ error: 'node_not_found' }, 404) }])
    resume(gone.services, { ...HOME, tab: 'graph', node: GRAPH_IDS.destillaatTwo })
    await settle()
    expect(currentPlace(gone.services).node).toBeNull()
    gone.services.dispose()
  })
})
