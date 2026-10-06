import { describe, expect, it } from 'vitest'
import { OVERVIEW_FIXTURE, SUBTHEMA_CHILDREN } from '../testing/graph'
import { canExpand, isNodeType, parseChildren, parseNode, parseNodeDetail, parseOverview, relocalize } from './types'

describe('parseNode', () => {
  it('reads a node and keeps only the text metadata', () => {
    const node = parseNode({
      id: 'destillaat:1',
      type: 'destillaat',
      label: 'Voeren',
      metadata: { text: 'Voeren houdt de kolonie groot', score: 0.82, leeg: '', diep: { a: 1 } },
    })
    expect(node).toEqual({
      id: 'destillaat:1',
      type: 'destillaat',
      label: 'Voeren',
      metadata: { text: 'Voeren houdt de kolonie groot' },
      translations: {},
      sourceLanguage: null,
      translated: false,
      // Deze knoop draagt wél een naam; het typewoord blijft ongebruikt.
      unnamed: false,
    })
  })

  it('picks the reader language from a bilingual field and keeps the original', () => {
    const wire = {
      id: 'destillaat:1',
      type: 'destillaat',
      label: { nl: 'Voeren', en: 'Feeding' },
      metadata: { text: { nl: 'Voeren houdt de kolonie groot', en: 'Feeding keeps the colony large' }, scope: 'stad' },
      sourceLanguage: 'nl',
    }
    const dutch = parseNode(wire, 'nl')
    expect(dutch?.label).toBe('Voeren')
    expect(dutch?.metadata.text).toBe('Voeren houdt de kolonie groot')
    // De brontaal is Nederlands, dus een Nederlandse lezer ziet het origineel.
    expect(dutch?.translated).toBe(false)

    const english = parseNode(wire, 'en')
    expect(english?.label).toBe('Feeding')
    expect(english?.metadata.text).toBe('Feeding keeps the colony large')
    expect(english?.translated).toBe(true)
    // De opsommingswaarde blijft staan zoals hij binnenkwam.
    expect(english?.metadata.scope).toBe('stad')
  })

  it('falls back to the other language instead of showing an empty screen', () => {
    const node = parseNode(
      { id: 'thema:1', type: 'thema', label: { nl: 'Overlast' }, metadata: { beschrijving: { nl: 'Alleen Nederlands' } }, sourceLanguage: 'nl' },
      'en',
    )
    expect(node?.label).toBe('Overlast')
    expect(node?.metadata.beschrijving).toBe('Alleen Nederlands')
    // Er is niets vertaald, dus er valt ook niets te melden.
    expect(node?.translated).toBe(false)
  })

  it('leaves an untranslated quote a plain string', () => {
    const node = parseNode({ id: 'quote:1', type: 'quote', label: 'The original passage.', metadata: { text: 'The original passage.' }, sourceLanguage: 'nl' }, 'nl')
    expect(node?.label).toBe('The original passage.')
    expect(node?.translations).toEqual({})
    expect(node?.translated).toBe(false)
  })

  it('relocalizes a loaded node without a second request', () => {
    const wire = {
      id: 'thema:1',
      type: 'thema',
      label: { nl: 'Overlast', en: 'Nuisance' },
      metadata: { beschrijving: { nl: 'Wat bewoners melden', en: 'What residents report' } },
      sourceLanguage: 'nl',
    }
    const node = parseNode(wire, 'nl')
    expect(node).not.toBeNull()
    const english = relocalize(node!, 'en')
    expect(english.label).toBe('Nuisance')
    expect(english.metadata.beschrijving).toBe('What residents report')
    expect(english.translated).toBe(true)
    // De oorspronkelijke knoop blijft staan zoals hij was.
    expect(node?.label).toBe('Overlast')
  })

  it('refuses a node without id or with a type it does not know', () => {
    expect(parseNode({ type: 'thema', label: 'x' })).toBeNull()
    expect(parseNode({ id: 'gek:1', type: 'gek', label: 'x' })).toBeNull()
    expect(parseNode(null)).toBeNull()
    expect(parseNode('thema:1')).toBeNull()
  })

  // Een ontbrekend label gaf vroeger de knoop-id als kaartnaam. Een
  // rij-id zegt de lezer niets; het typewoord wel.
  it('names the kind of node when the label is missing or is a pipeline key', () => {
    expect(parseNode({ id: 'thema:1', type: 'thema' })?.label).toBe('Theme')
    expect(parseNode({ id: 'thema:1', type: 'thema' }, 'nl')?.label).toBe('Thema')
    expect(parseNode({ id: 'thema:1', type: 'thema', label: 'wonen_en_leefomgeving' })?.label).toBe('Theme')
    expect(parseNode({ id: 'thema:1', type: 'thema', label: 'destillaat:0be06f8c-9f08' })?.label).toBe('Theme')
  })

  // En andersom: een echte naam blijft staan, ook zonder spatie of hoofdletter.
  // `lewald1987` is de sleutel waaronder een paper in de kennisbank staat.
  it('keeps a written name, a title and a bibliography key', () => {
    expect(parseNode({ id: 'interview:1', type: 'interview', label: 'lewald1987' })?.label).toBe('lewald1987')
    expect(parseNode({ id: 'thema:1', type: 'thema', label: 'Stad en dier' })?.label).toBe('Stad en dier')
    expect(parseNode({ id: 'thema:1', type: 'thema', label: 'Stressrespons' })?.label).toBe('Stressrespons')
  })

  // Een taalwissel mag de sleutel niet alsnog binnenlaten: de andere taal van
  // een tweetalig veld kan er net zo goed een dragen.
  it('keeps the guard when the reader switches language', () => {
    const node = parseNode({ id: 'thema:1', type: 'thema', label: { en: 'Housing', nl: 'wonen_en_leefomgeving' } })!
    expect(node.label).toBe('Housing')
    expect(relocalize(node, 'nl').label).toBe('Thema')
  })
})

describe('parseOverview', () => {
  it('reads the structure level with its totals', () => {
    const overview = parseOverview(OVERVIEW_FIXTURE)
    expect(overview.nodes).toHaveLength(3)
    expect(overview.edges).toHaveLength(2)
    expect(overview.edges[0]).toEqual({
      source: OVERVIEW_FIXTURE.nodes[0].id,
      target: OVERVIEW_FIXTURE.nodes[1].id,
      type: 'contains_thema',
    })
    expect(overview.totals.destillaat).toBe(2)
  })

  // De overview draagt de importstand mee, zodat een agent niet apart hoeft te
  // pollen om te weten waarom de graaf leeg is.
  it('reads the import state the server put beside the graph', () => {
    expect(parseOverview({ nodes: [], relationships: [], kb: { state: 'importing' } }).kb).toBe('importing')
    expect(parseOverview({ nodes: [], relationships: [], kb: { state: 'incomplete' } }).kb).toBe('incomplete')
    // Een stand die we niet kennen is geen stand, geen gok.
    expect(parseOverview({ nodes: [], kb: { state: 'halverwege' } }).kb).toBeNull()
    expect(parseOverview({ nodes: [], kb: 'ready' }).kb).toBeNull()
  })

  it('survives an empty knowledge base and a broken body', () => {
    expect(parseOverview({ nodes: [], relationships: [] })).toEqual({ nodes: [], edges: [], totals: {}, childCounts: new Map(), kb: null })
    expect(parseOverview(null)).toEqual({ nodes: [], edges: [], totals: {}, childCounts: new Map(), kb: null })
    expect(parseOverview({ nodes: 'nee', totals: { a: 'veel' }, childCounts: { 'thema:1': 'veel' } })).toEqual({
      nodes: [],
      edges: [],
      totals: {},
      childCounts: new Map(),
      kb: null,
    })
  })

  it('drops an edge that misses an end', () => {
    const overview = parseOverview({
      nodes: [],
      relationships: [{ source_id: 'a', relationship_type: 'contains' }, { source_id: 'a', target_id: 'b' }],
    })
    expect(overview.edges).toEqual([])
  })
})

describe('parseChildren', () => {
  it('reads a page of children with its total', () => {
    const page = parseChildren(SUBTHEMA_CHILDREN)
    expect(page.nodes).toHaveLength(2)
    expect(page.total).toBe(2)
    expect(page.truncated).toBe(false)
    expect(page.nextOffset).toBeNull()
  })

  it('reads a truncated page and its next offset', () => {
    const page = parseChildren({ nodes: [], relationships: [], truncated: true, total: 900, nextOffset: 150 })
    expect(page).toMatchObject({ truncated: true, total: 900, nextOffset: 150 })
  })

  it('refuses an offset that does not move forward', () => {
    expect(parseChildren({ nodes: [], nextOffset: 0 }).nextOffset).toBeNull()
    expect(parseChildren({ nodes: [], nextOffset: -5 }).nextOffset).toBeNull()
    expect(parseChildren({ nodes: [], nextOffset: 'meer' }).nextOffset).toBeNull()
  })
})

describe('parseNodeDetail', () => {
  it('reads the node with the trail from root to parent', () => {
    const detail = parseNodeDetail({
      node: { id: 'destillaat:1', type: 'destillaat', label: 'Voeren' },
      trail: [
        { id: 'domein:1', type: 'domein', label: 'Stad' },
        { id: 'destillaat:1', type: 'destillaat', label: 'Voeren' },
      ],
    })
    // De knoop zelf hoort niet in zijn eigen kruimelpad.
    expect(detail?.trail.map((n) => n.id)).toEqual(['domein:1'])
  })

  it('is nothing without a readable node', () => {
    expect(parseNodeDetail({ trail: [] })).toBeNull()
    expect(parseNodeDetail(null)).toBeNull()
  })
})

describe('node types', () => {
  it('knows its six kinds and that a quote is a leaf', () => {
    expect(isNodeType('subthema')).toBe(true)
    expect(isNodeType('perspectief')).toBe(false)
    expect(canExpand('quote')).toBe(false)
    expect(canExpand('destillaat')).toBe(true)
  })

  // De graaf toont geen perspectieven en geen redactionele persona's.
  // Stuurt een server ze toch mee, dan zijn het onbekende soorten: ze vallen
  // weg in plaats van als knoop zonder vorm of kleur op de kaart te landen.
  it('drops perspectives and editorial personas a server still sends', () => {
    const overview = parseOverview({
      nodes: [
        { id: 'thema:1', type: 'thema', label: 'Overlast' },
        { id: 'perspective:1', type: 'perspective', label: 'De criticus' },
        { id: 'persona:1', type: 'persona', label: 'Rie' },
      ],
      relationships: [{ source_id: 'thema:1', target_id: 'perspective:1', relationship_type: 'contains_perspective' }],
    })
    expect(overview.nodes.map((n) => n.id)).toEqual(['thema:1'])
  })
})


it('allows bibliographic text but excludes private properties and invalid confidence', () => {
  const node = parseNode({ id: 'interview:synthetic', type: 'interview', label: 'Synthetic title', metadata: { citation: 'Synthetic citation', doi: '10.1/synthetic', citationMissing: 'Pages', citationConfidence: 'medium', storage_key: 'PRIVATE_CANARY', content_hash: 'PRIVATE_CANARY', bron_bestand: 'PRIVATE_CANARY', citatie_bron: 'PRIVATE_CANARY' } })
  expect(node?.metadata).toEqual({ citation: 'Synthetic citation', doi: '10.1/synthetic', citationMissing: 'Pages', citationConfidence: 'medium' })
  expect(JSON.stringify(node)).not.toContain('PRIVATE_CANARY')
  expect(parseNode({ id: 'interview:x', type: 'interview', metadata: { citation: {}, citationConfidence: '97%', citationMissing: true } })?.metadata).toEqual({})
})


it('accepts only code-owned quote grounding strings, without converting legacy/raw flags', () => {
  for (const grounding of ['rejected', 'corpus_grounded']) expect(parseNode({ id: 'quote:synthetic', type: 'quote', metadata: { grounding } })?.metadata.grounding).toBe(grounding)
  for (const grounding of [false, true, 'false', 'expert_verified', undefined]) expect(parseNode({ id: 'quote:synthetic', type: 'quote', metadata: { grounded: false, grounding } })?.metadata).toEqual({})
})
