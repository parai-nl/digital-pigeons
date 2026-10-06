// De kennisstand is een peilketen: zolang de duif leest kijkt de store elke
// vijf seconden opnieuw. De klok gaat hier met de hand — geen faketimers, geen
// echte wachttijd — zodat elke schakel van de keten toetsbaar is: wanneer hij
// doorloopt, wanneer hij stopt en wanneer hij zwijgt.
import { describe, expect, it } from 'vitest'
import { KNOWLEDGE_POLL_MS, KnowledgeStore, parseKnowledgeStatus } from './knowledge.svelte'
import { jsonResponse, scriptedFetch, type Route } from './testing/fetch'

// settle laat de lopende beloftes van één peiling aflopen.
async function settle(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

// handClock is de schedule-poort met de hand aan de knop: hij plant niets, hij
// onthoudt het. tick() draait de geplande peiling; cancels telt hoe vaak een
// geplande peiling weer is ingetrokken.
function handClock() {
  const clock = {
    planned: [] as { fn: () => void; ms: number }[],
    cancels: 0,
    schedule(fn: () => void, ms: number): () => void {
      const entry = { fn, ms }
      clock.planned.push(entry)
      return () => {
        clock.cancels++
        const i = clock.planned.indexOf(entry)
        if (i >= 0) clock.planned.splice(i, 1)
      }
    },
    // tick draait de eerstvolgende geplande peiling en wacht hem uit.
    async tick(): Promise<void> {
      const entry = clock.planned.shift()
      if (!entry) throw new Error('er stond geen peiling gepland')
      entry.fn()
      await settle()
    },
  }
  return clock
}

// harness zet een store op een gescripte server met de handklok: `responses`
// is de rij antwoorden die de statusroute achter elkaar geeft; het laatste
// antwoord blijft staan zolang er gepeild wordt.
function harness(...responses: (() => Response)[]) {
  const clock = handClock()
  let n = 0
  const route: Route = {
    method: 'GET',
    match: '/knowledge/status',
    respond: () => {
      const make = responses[Math.min(n, responses.length - 1)]
      n++
      return make()
    },
  }
  const scripted = scriptedFetch([route])
  const store = new KnowledgeStore({ fetch: scripted.fetch, schedule: clock.schedule })
  return { store, clock, scripted, polls: () => scripted.calls.length }
}

const importing = () => jsonResponse({ state: 'importing', counts: { destillaat: 12 } })
const ready = () => jsonResponse({ state: 'ready', kbId: 'proef' })
const offline = () => {
  throw new Error('offline')
}

describe('KnowledgeStore.load', () => {
  it('plans the next poll while the pigeon is still reading', async () => {
    const { store, clock } = harness(importing)
    await store.load()

    expect(store.state).toBe('importing')
    expect(store.importing).toBe(true)
    expect(store.unreachable).toBe(false)
    expect(clock.planned).toHaveLength(1)
    expect(clock.planned[0].ms).toBe(KNOWLEDGE_POLL_MS)
  })

  it('stops the chain as soon as the import is ready', async () => {
    const { store, clock, polls } = harness(importing, ready)
    await store.load()
    await clock.tick()

    expect(store.state).toBe('ready')
    expect(store.status?.kbId).toBe('proef')
    expect(clock.planned).toHaveLength(0)
    expect(polls()).toBe(2)
  })

  it('keeps polling when one poll fails during an import', async () => {
    // Waarom: één hikje van het netwerk mocht de banner niet laten bevriezen op
    // "de duif leest nog" terwijl er niemand meer kijkt.
    const { store, clock } = harness(importing, offline, ready)
    await store.load()
    await clock.tick()

    expect(store.state).toBe('importing')
    expect(store.unreachable).toBe(true)
    expect(clock.planned).toHaveLength(1)

    await clock.tick()
    expect(store.state).toBe('ready')
    expect(store.unreachable).toBe(false)
    expect(clock.planned).toHaveLength(0)
  })

  it('treats a broken answer as a hiccup and not as a new state', async () => {
    // Een body die geen JSON is, een stand die wij niet kennen, een 503: geen
    // van drieën is een nieuwe waarheid. De laatst bekende stand blijft staan
    // en de keten loopt door tot de import echt klaar is.
    const { store, clock } = harness(
      importing,
      () => new Response('geen json', { status: 200, headers: { 'Content-Type': 'application/json' } }),
      () => jsonResponse({ state: 'onzin' }),
      () => jsonResponse({ error: 'kapot' }, 503),
      ready,
    )
    await store.load()

    for (const unreachable of [false, false, true]) {
      await clock.tick()
      expect(store.state).toBe('importing')
      expect(store.unreachable).toBe(unreachable)
      expect(clock.planned).toHaveLength(1)
    }

    await clock.tick()
    expect(store.state).toBe('ready')
    expect(store.unreachable).toBe(false)
    expect(clock.planned).toHaveLength(0)
  })

  it('does not poll when the very first attempt fails', async () => {
    // Zonder een eerdere stand weet niemand of er iets te importeren valt; dan
    // is stilstaan beter dan blijven kloppen.
    const { store, clock } = harness(offline)
    await store.load()

    expect(store.state).toBeNull()
    expect(store.unreachable).toBe(true)
    expect(clock.planned).toHaveLength(0)
  })

  it('lets a second load invalidate the poll of the first', async () => {
    const { store, clock, polls } = harness(importing)
    await store.load()
    await store.load()

    expect(clock.cancels).toBe(1)
    expect(clock.planned).toHaveLength(1)
    expect(polls()).toBe(2)
  })

  it('writes nothing when the store is disposed mid-request', async () => {
    const gate = deferred(jsonResponse({ state: 'importing' }))
    const clock = handClock()
    const scripted = scriptedFetch([{ method: 'GET', match: '/knowledge/status', respond: gate.respond }])
    const store = new KnowledgeStore({ fetch: scripted.fetch, schedule: clock.schedule })

    const loading = store.load()
    store.dispose()
    gate.release()
    await loading

    expect(store.status).toBeNull()
    expect(store.unreachable).toBe(false)
    expect(clock.planned).toHaveLength(0)
  })

  it('refuses to start a request after dispose and cancels a planned poll', async () => {
    const { store, clock, polls } = harness(importing)
    await store.load()
    expect(clock.planned).toHaveLength(1)

    store.dispose()
    expect(clock.cancels).toBe(1)
    expect(clock.planned).toHaveLength(0)

    await store.load()
    expect(polls()).toBe(1)
  })
})

// deferred houdt een antwoord vast tot de test het loslaat, zodat een dispose
// middenin een verzoek te spelen is.
function deferred(response: Response) {
  let release = (): void => {}
  const done = new Promise<void>((resolve) => {
    release = resolve
  })
  return { release: () => release(), respond: async () => (await done, response) }
}

describe('parseKnowledgeStatus', () => {
  it('keeps the known fields and drops everything else', () => {
    const status = parseKnowledgeStatus({
      state: 'ready',
      kbId: 'proef',
      counts: { destillaat: 3, quote: 'veel' },
      importedAt: '2026-01-01T00:00:00Z',
      skipped: false,
      reason: 'none',
      onbekend: 'weg hiermee',
    })

    expect(status).toEqual({
      state: 'ready',
      kbId: 'proef',
      counts: { destillaat: 3 },
      importedAt: '2026-01-01T00:00:00Z',
      skipped: false,
      reason: 'none',
    })
  })

  it('refuses a body without a state we know', () => {
    expect(parseKnowledgeStatus(null)).toBeNull()
    expect(parseKnowledgeStatus('ready')).toBeNull()
    expect(parseKnowledgeStatus({})).toBeNull()
    expect(parseKnowledgeStatus({ state: 'halfway' })).toBeNull()
  })

  it('ignores fields of the wrong shape', () => {
    expect(parseKnowledgeStatus({ state: 'failed', kbId: 7, counts: 'veel', skipped: 'ja' })).toEqual({
      state: 'failed',
    })
    expect(parseKnowledgeStatus({ state: 'importing', counts: { destillaat: Number.NaN } })).toEqual({
      state: 'importing',
      counts: {},
    })
  })
})
