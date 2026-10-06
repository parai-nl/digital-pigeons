import { describe, expect, it } from 'vitest'
import { jsonResponse, scriptedFetch, type Route } from '../testing/fetch'
import {
  fakeGraphPorts,
  GRAPH_IDS,
  graphRoutes,
  OVERVIEW_FIXTURE,
  NODE_DETAIL,
  SUBTHEMA_CHILDREN,
} from '../testing/graph'
import { GraphStore } from './graphStore.svelte'

// Elke test bouwt zijn eigen store: de graaf leeft per mount, dus er is nooit
// gedeelde stand tussen twee tests.
function harness(extra: Route[] = []) {
  const scripted = scriptedFetch([...extra, ...graphRoutes()])
  // De graafstore kent de KnowledgeStore niet: de banner leest die zelf, en
  // `kb` uit de overview is hier de enige stand die de store bijhoudt.
  const store = new GraphStore(fakeGraphPorts(scripted.fetch))
  const graphCalls = (fragment: string) => scripted.calls.filter((c) => c.url.includes(fragment)).length
  return { store, scripted, graphCalls }
}

// deferred geeft een antwoord dat pas komt als de test het toestaat: zo is een
// dispose of een tweede klik middenin een verzoek te spelen.
function deferred(body: unknown) {
  let release = (): void => {}
  const done = new Promise<void>((resolve) => {
    release = resolve
  })
  return { release: () => release(), respond: async () => (await done, jsonResponse(body)) }
}

describe('GraphStore.load', () => {
  it('reads the structure level, its roots and its colour families', async () => {
    const { store } = harness()
    await store.load()

    expect(store.phase).toBe('ready')
    expect(store.nodes.size).toBe(3)
    expect(store.roots).toEqual([GRAPH_IDS.domein])
    // De kaart opent op het structuurniveau: de wortels staan al open, dus de
    // thema's onder hen staan er meteen. Dat kost geen verzoek — die banden
    // kwamen met de overview mee.
    expect(store.rows.map((r) => r.id)).toEqual([GRAPH_IDS.domein, GRAPH_IDS.thema])
    expect(store.revealed.has(GRAPH_IDS.domein)).toBe(true)
    expect(store.families.get(GRAPH_IDS.thema)).toBe(0)
    expect(store.totals.destillaat).toBe(2)
    expect(store.empty).toBe(false)
  })

  it('carries the import state the overview reported beside the graph', async () => {
    const { store } = harness()
    await store.load()
    expect(store.kb).toBe('ready')

    const importing = harness([
      {
        method: 'GET',
        match: '/graph/overview',
        respond: () => jsonResponse({ nodes: [], relationships: [], kb: { state: 'importing' } }),
      },
    ])
    await importing.store.load()
    expect(importing.store.kb).toBe('importing')
  })

  it('reports an empty knowledge base as empty, not as a failure', async () => {
    const { store } = harness([
      { method: 'GET', match: '/graph/overview', respond: () => jsonResponse({ nodes: [], relationships: [] }) },
    ])
    await store.load()
    expect(store.phase).toBe('ready')
    expect(store.empty).toBe(true)
    expect(store.notice).toBeNull()
  })

  it('keeps the server message when the graph is unavailable', async () => {
    const { store } = harness([
      {
        method: 'GET',
        match: '/graph/overview',
        respond: () =>
          jsonResponse({ error: 'graph_unavailable', message: { en: 'No graph', nl: 'Geen graaf' } }, 503),
      },
    ])
    await store.load()
    expect(store.phase).toBe('failed')
    expect(store.notice).toEqual({ status: 503, reason: 'graph_unavailable', message: { en: 'No graph', nl: 'Geen graaf' } })
  })

  it('says nothing more than "it went wrong" when the network drops', async () => {
    const { store } = harness([
      {
        method: 'GET',
        match: '/graph/overview',
        respond: () => {
          throw new Error('offline')
        },
      },
    ])
    await store.load()
    expect(store.phase).toBe('failed')
    expect(store.notice).toEqual({ status: 0 })
  })
})

describe('GraphStore.expand', () => {
  it('opens a structure level without asking the server again', async () => {
    const { store, graphCalls } = harness()
    await store.load()
    await store.expand(GRAPH_IDS.domein)

    expect(graphCalls('/children')).toBe(0)
    expect(store.rows.map((r) => r.id)).toContain(GRAPH_IDS.thema)
  })

  it('fetches one level deeper and remembers what came back', async () => {
    const { store, graphCalls } = harness()
    await store.load()
    // Een tak is pas te zien als het pad ernaartoe openstaat.
    await store.expand(GRAPH_IDS.domein)
    await store.expand(GRAPH_IDS.thema)
    await store.expand(GRAPH_IDS.subthema)

    expect(graphCalls('/children')).toBe(1)
    expect(store.childIds.get(GRAPH_IDS.subthema)).toEqual([GRAPH_IDS.destillaatOne, GRAPH_IDS.destillaatTwo])
    expect(store.visibleIds.has(GRAPH_IDS.destillaatOne)).toBe(true)
    // De banden van de kinderen komen mee, zodat het canvas ze kan tekenen.
    expect(store.visibleEdges.some((e) => e.type === 'belongs_to_subthema')).toBe(true)
  })

  it('lets a double click share one request', async () => {
    const gate = deferred(SUBTHEMA_CHILDREN)
    const { store, graphCalls } = harness([{ method: 'GET', match: '/children', respond: gate.respond }])
    await store.load()
    await store.expand(GRAPH_IDS.thema)

    const first = store.expand(GRAPH_IDS.subthema)
    const second = store.expand(GRAPH_IDS.subthema)
    expect(second).toBe(first)
    expect(store.loadingIds.has(GRAPH_IDS.subthema)).toBe(true)
    gate.release()
    await Promise.all([first, second])

    expect(graphCalls('/children')).toBe(1)
    expect(store.loadingIds.size).toBe(0)
  })

  it('collapses without forgetting, and reopens without a request', async () => {
    const { store, graphCalls } = harness()
    await store.load()
    await store.expand(GRAPH_IDS.domein)
    await store.expand(GRAPH_IDS.thema)
    await store.expand(GRAPH_IDS.subthema)
    store.collapse(GRAPH_IDS.subthema)
    expect(store.visibleIds.has(GRAPH_IDS.destillaatOne)).toBe(false)

    await store.toggle(GRAPH_IDS.subthema)
    expect(store.visibleIds.has(GRAPH_IDS.destillaatOne)).toBe(true)
    expect(graphCalls('/children')).toBe(1)
  })

  it('ignores an id that is not in the graph', async () => {
    const { store, graphCalls } = harness()
    await store.load()
    await store.expand('destillaat:onbekend')
    expect(graphCalls('/children')).toBe(0)
  })

  // Een tak waarvan de server nul kinderen meldt blijft dicht — ook als er nog
  // eens op geklikt wordt. Anders staat er een aria-expanded="true" boven een
  // lege group, en dat is een belofte die de boom niet waarmaakt.
  it('keeps a leaf without children closed, also on a second try', async () => {
    const { store, graphCalls } = harness([
      { method: 'GET', match: '/children', respond: () => jsonResponse({ nodes: [], relationships: [], total: 0 }) },
    ])
    await store.load()
    await store.expand(GRAPH_IDS.domein)
    await store.expand(GRAPH_IDS.thema)
    await store.expand(GRAPH_IDS.subthema)
    await store.expand(GRAPH_IDS.subthema)

    expect(store.revealed.has(GRAPH_IDS.subthema)).toBe(false)
    // En het kost geen tweede verzoek: we weten al dat er niets is.
    expect(graphCalls('/children')).toBe(1)
  })

  it('does not open an empty branch', async () => {
    const { store } = harness([
      { method: 'GET', match: '/children', respond: () => jsonResponse({ nodes: [], relationships: [], total: 0 }) },
    ])
    await store.load()
    await store.expand(GRAPH_IDS.domein)
    await store.expand(GRAPH_IDS.thema)
    await store.expand(GRAPH_IDS.subthema)
    expect(store.revealed.has(GRAPH_IDS.subthema)).toBe(false)
    expect(store.rows.find((r) => r.id === GRAPH_IDS.subthema)?.expanded).toBe(false)
  })
})

describe('GraphStore.more', () => {
  it('loads the next page and stops asking when everything is in', async () => {
    let page = 0
    const { store } = harness([
      {
        method: 'GET',
        match: '/children',
        respond: () => {
          page++
          return page === 1
            ? jsonResponse({
                nodes: [SUBTHEMA_CHILDREN.nodes[0]],
                relationships: [],
                truncated: true,
                total: 2,
                nextOffset: 1,
              })
            : jsonResponse({ nodes: [SUBTHEMA_CHILDREN.nodes[1]], relationships: [], total: 2 })
        },
      },
    ])
    await store.load()
    await store.expand(GRAPH_IDS.subthema)
    expect(store.nextOffsets.get(GRAPH_IDS.subthema)).toBe(1)

    await store.more(GRAPH_IDS.subthema)
    expect(store.childIds.get(GRAPH_IDS.subthema)).toHaveLength(2)
    expect(store.nextOffsets.has(GRAPH_IDS.subthema)).toBe(false)

    // Zonder volgende pagina doet "meer laden" niets meer.
    await store.more(GRAPH_IDS.subthema)
    expect(page).toBe(2)
  })

  it('counts a child that comes back twice only once', async () => {
    let page = 0
    const { store } = harness([
      {
        method: 'GET',
        match: '/children',
        respond: () => {
          page++
          return page === 1
            ? jsonResponse({ nodes: [SUBTHEMA_CHILDREN.nodes[0]], relationships: [], truncated: true, total: 2, nextOffset: 1 })
            : // De tweede pagina overlapt met de eerste: een server die
              // tussendoor herordent doet dat zomaar.
              jsonResponse({ nodes: SUBTHEMA_CHILDREN.nodes, relationships: [], total: 2 })
        },
      },
    ])
    await store.load()
    await store.expand(GRAPH_IDS.subthema)
    await store.more(GRAPH_IDS.subthema)

    expect(store.childIds.get(GRAPH_IDS.subthema)).toEqual([GRAPH_IDS.destillaatOne, GRAPH_IDS.destillaatTwo])
  })
})

describe('GraphStore.focus', () => {
  it('opens the trail to a distillate and selects it', async () => {
    const { store } = harness()
    await store.load()
    await store.focus(GRAPH_IDS.destillaatOne)

    expect(store.selected).toBe(GRAPH_IDS.destillaatOne)
    expect(store.trail.map((n) => n.id)).toEqual([
      GRAPH_IDS.domein,
      GRAPH_IDS.thema,
      GRAPH_IDS.subthema,
      GRAPH_IDS.destillaatOne,
    ])
    // Het standpunt zelf blijft dicht: er werd om dít punt gevraagd.
    expect(store.revealed.has(GRAPH_IDS.destillaatOne)).toBe(false)
  })

  it('waits for the overview when it arrives first, and the last question wins', async () => {
    const gate = deferred(OVERVIEW_FIXTURE)
    const { store } = harness([{ method: 'GET', match: '/graph/overview', respond: gate.respond }])

    const loading = store.load()
    await store.focus(GRAPH_IDS.subthema)
    await store.focus(GRAPH_IDS.destillaatOne)
    gate.release()
    await loading

    expect(store.selected).toBe(GRAPH_IDS.destillaatOne)
  })

  it('loads the graph itself when nothing was loaded yet', async () => {
    const { store } = harness()
    await store.focus(GRAPH_IDS.destillaatOne)
    expect(store.phase).toBe('ready')
    expect(store.selected).toBe(GRAPH_IDS.destillaatOne)
  })

  // Een focus en een uitklap zijn twee verschillende navigaties. Hingen ze aan
  // dezelfde teller, dan liet de focus de uitklap stil sterven: geen standpunt
  // op het scherm, een leeg kruimelpad, en een verouderde belofte in #inflight.
  it('passes over a running expand without killing it', async () => {
    const gate = deferred(SUBTHEMA_CHILDREN)
    const { store, graphCalls } = harness([{ method: 'GET', match: '/children', respond: gate.respond }])
    await store.load()
    await store.expand(GRAPH_IDS.thema)

    const opening = store.expand(GRAPH_IDS.subthema)
    const focusing = store.focus(GRAPH_IDS.destillaatOne)
    gate.release()
    await Promise.all([opening, focusing])

    // Eén verzoek: de uitklap die al liep is niet stilgevallen om daarna door
    // de focus opnieuw gedaan te worden.
    expect(graphCalls('/children')).toBe(1)
    expect(store.revealed.has(GRAPH_IDS.subthema)).toBe(true)
    expect(store.childIds.get(GRAPH_IDS.subthema)).toHaveLength(2)
    expect(store.selected).toBe(GRAPH_IDS.destillaatOne)
    expect(store.trail.map((n) => n.id)).toEqual([
      GRAPH_IDS.domein,
      GRAPH_IDS.thema,
      GRAPH_IDS.subthema,
      GRAPH_IDS.destillaatOne,
    ])
  })

  it('lets go of a wish that was waiting on a load that failed', async () => {
    let broken = true
    const { store } = harness([
      {
        method: 'GET',
        match: '/graph/overview',
        respond: () =>
          broken
            ? jsonResponse({ error: 'graph_unavailable', message: { en: 'No graph', nl: 'Geen graaf' } }, 503)
            : jsonResponse(OVERVIEW_FIXTURE),
      },
    ])
    await store.focus(GRAPH_IDS.destillaatOne)
    expect(store.phase).toBe('failed')

    broken = false
    await store.load()
    // De vraag van daarnet springt niet alsnog weg zodra het weer lukt.
    expect(store.phase).toBe('ready')
    expect(store.selected).toBeNull()
  })

  it('drops a queued wish as soon as the reader points at something themselves', async () => {
    const gate = deferred(OVERVIEW_FIXTURE)
    const { store } = harness([{ method: 'GET', match: '/graph/overview', respond: gate.respond }])

    const loading = store.load()
    await store.focus(GRAPH_IDS.destillaatOne)
    store.select(null)
    gate.release()
    await loading

    expect(store.phase).toBe('ready')
    expect(store.selected).toBeNull()
  })

  it('only announces a selection that came from outside', async () => {
    const { store } = harness()
    await store.load()
    store.select(GRAPH_IDS.thema)
    // Een gewone klik verplaatst de aandacht niet: de boom hoort de focus niet
    // uit de handen van de gebruiker te trekken.
    expect(store.focusRequest).toBe(0)

    await store.focus(GRAPH_IDS.destillaatOne)
    expect(store.focusRequest).toBe(1)
  })

  it('turns an unknown id into a plain notice, not an empty screen', async () => {
    const { store } = harness()
    await store.load()
    await store.focus('destillaat:onbekend')

    expect(store.notice?.reason).toBe('node_not_found')
    expect(store.notice?.message?.nl).toContain('kennisbank')
    expect(store.selected).toBeNull()
    expect(store.phase).toBe('ready')
  })
})

describe('GraphStore.select', () => {
  it('only selects what is really there', async () => {
    const { store } = harness()
    await store.load()
    store.select('thema:verzonnen')
    expect(store.selected).toBeNull()
    store.select(GRAPH_IDS.thema)
    expect(store.selected).toBe(GRAPH_IDS.thema)
    store.select(null)
    expect(store.selected).toBeNull()
  })
})

describe('GraphStore.query', () => {
  it('counts only what is on screen', async () => {
    const { store } = harness()
    await store.load()
    await store.expand(GRAPH_IDS.domein)
    store.query = 'overlast'
    expect([...store.matches]).toEqual([GRAPH_IDS.thema])
    expect(store.searching).toBe(true)
    store.query = ''
    expect(store.matches.size).toBe(0)
    expect(store.searching).toBe(false)
  })
})

describe('GraphStore.invalidate', () => {
  it('marks what is on screen as stale without throwing it away', async () => {
    const { store } = harness()
    await store.load()
    await store.expand(GRAPH_IDS.domein)

    store.invalidate()
    // Volgende keer dat de tab opengaat wordt er opnieuw geladen; tot dan blijft
    // staan wat er staat.
    expect(store.phase).toBe('idle')
    expect(store.revealed.has(GRAPH_IDS.domein)).toBe(true)
    expect(store.rows.map((r) => r.id)).toContain(GRAPH_IDS.thema)
  })

  it('says nothing about a store that is already gone', async () => {
    const { store } = harness()
    await store.load()
    store.dispose()
    store.invalidate()
    expect(store.phase).toBe('ready')
  })
})

describe('GraphStore.dispose', () => {
  it('writes nothing more once the module is taken down mid-request', async () => {
    const gate = deferred(SUBTHEMA_CHILDREN)
    const { store } = harness([{ method: 'GET', match: '/children', respond: gate.respond }])
    await store.load()
    await store.expand(GRAPH_IDS.thema)

    const pending = store.expand(GRAPH_IDS.subthema)
    store.dispose()
    gate.release()
    await pending

    expect(store.revealed.has(GRAPH_IDS.subthema)).toBe(false)
    expect(store.nodes.has(GRAPH_IDS.destillaatOne)).toBe(false)
    expect(store.disposed).toBe(true)
  })

  it('is idempotent and refuses new work', async () => {
    const { store, graphCalls } = harness()
    store.dispose()
    store.dispose()
    await store.load()
    await store.focus(GRAPH_IDS.destillaatOne)
    expect(graphCalls('/graph/')).toBe(0)
    expect(store.phase).toBe('idle')
  })
})


describe('Graph navigation races', () => {
  it('does not overwrite a manual selection with a late external focus', async () => {
    const gate = deferred(NODE_DETAIL)
    const { store } = harness([{ method: 'GET', match: /\/nodes\/[^/]+$/, respond: gate.respond }])
    await store.load()
    const focused = store.focus(GRAPH_IDS.destillaatOne)
    store.select(GRAPH_IDS.thema)
    gate.release()
    await focused
    expect(store.selected).toBe(GRAPH_IDS.thema)
    expect(store.focusRequest).toBe(0)
  })

  it('does not merge children from a previous overview into a reloaded collection', async () => {
    const gate = deferred(SUBTHEMA_CHILDREN)
    const { store } = harness([{ method: 'GET', match: '/children', respond: gate.respond }])
    await store.load()
    const opening = store.expand(GRAPH_IDS.subthema)
    await store.load()
    gate.release()
    await opening
    expect(store.nodes.has(GRAPH_IDS.destillaatOne)).toBe(false)
    expect(store.childIds.has(GRAPH_IDS.subthema)).toBe(false)
    expect(store.loadingIds.size).toBe(0)
  })

  it('enriches an existing reference with full metadata on explicit focus', async () => {
    const { store } = harness()
    await store.load()
    store.nodes = new Map(store.nodes).set(GRAPH_IDS.destillaatOne, { id: GRAPH_IDS.destillaatOne, type: 'destillaat', label: 'Reference title', metadata: {} })
    await store.focus(GRAPH_IDS.destillaatOne)
    expect(store.selectedNode?.metadata.text).toBe(NODE_DETAIL.node.metadata.text)
  })
})


it('rejects a detail response for a different node instead of silently using it as a fallback', async () => {
  const { store } = harness([{ method: 'GET', match: /\/nodes\/[^/]+$/, respond: () => jsonResponse(NODE_DETAIL) }])
  await store.load()
  store.select(GRAPH_IDS.domein)
  await store.focus(GRAPH_IDS.quote)
  expect(store.selected).toBeNull()
  expect(store.notice?.reason).toBe('node_not_found')
})

// De tweetalige kennisbank (duiven/3). De store kiest de taal bij het lezen van
// de wire, en een taalwissel her-kiest wat al binnen is zonder tweede verzoek.
describe('bilingual knowledge content', () => {
  const OVERVIEW = {
    nodes: [
      {
        id: GRAPH_IDS.domein,
        type: 'domein',
        label: { nl: 'Stadsnatuur', en: 'Urban nature' },
        metadata: { beschrijving: { nl: 'Mensen en dieren delen de straat.', en: 'People and animals share the street.' } },
        sourceLanguage: 'nl',
      },
    ],
    relationships: [],
    totals: { domein: 1 },
    childCounts: {},
    kb: { state: 'ready' },
  }

  it('reads the wire in the language the store was set to', async () => {
    const { store } = harness([{ method: 'GET', match: '/graph/overview', respond: () => jsonResponse(OVERVIEW) }])
    store.setLang('en')
    await store.load()
    const node = store.nodes.get(GRAPH_IDS.domein)
    expect(node?.label).toBe('Urban nature')
    expect(node?.metadata.beschrijving).toBe('People and animals share the street.')
    expect(node?.translated).toBe(true)
  })

  it('switches language without a second request and keeps the branch open', async () => {
    const { store, graphCalls } = harness([{ method: 'GET', match: '/graph/overview', respond: () => jsonResponse(OVERVIEW) }])
    store.setLang('nl')
    await store.load()
    expect(store.nodes.get(GRAPH_IDS.domein)?.label).toBe('Stadsnatuur')
    const before = graphCalls('/graph/')
    store.setLang('en')
    expect(store.nodes.get(GRAPH_IDS.domein)?.label).toBe('Urban nature')
    expect(store.nodes.get(GRAPH_IDS.domein)?.translated).toBe(true)
    expect(graphCalls('/graph/')).toBe(before)
    // Terug naar de brontaal: geen vertaling meer om te melden.
    store.setLang('nl')
    expect(store.nodes.get(GRAPH_IDS.domein)?.translated).toBe(false)
  })
})
