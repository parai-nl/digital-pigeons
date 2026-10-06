import { describe, expect, it } from 'vitest'
import { UiLocale } from '../i18n.svelte'
import { OLD_SOURCE, SOURCE } from '../testing/chat'
import { jsonResponse, scriptedFetch, type Route } from '../testing/fetch'
import { SourcePanelStore } from './sourcePanel.svelte'
import { crumbText, parseTrail, trailSteps, type Crumb } from './trail'

const SECOND = { ...SOURCE, number: 2, id: 'destillaat:2b2b', text: 'Pigeons drink by suction.' }

function nodeRoute(body: unknown, ok = true): Route {
  return { method: 'GET', match: '/graph/nodes/', respond: () => (ok ? jsonResponse(body) : new Response('nope', { status: 500 })) }
}

function store(routes: Route[] = []) {
  const scripted = scriptedFetch(routes)
  return { panel: new SourcePanelStore({ fetch: scripted.fetch }), calls: scripted.calls }
}

// settle laat de lopende ophaalactie én de daaropvolgende toekenning landen.
async function settle(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
  await new Promise((resolve) => setTimeout(resolve, 0))
}

// De vorm die de graaf werkelijk stuurt: de structuurlagen dragen hun label als
// taalobject.
const TRAIL = {
  node: { id: 'destillaat:9f1c', type: 'destillaat', label: { nl: 'Nestelen', en: 'Nesting' } },
  trail: [
    { id: 'domein:1', type: 'domein', label: { nl: 'Stadsecologie', en: 'Urban ecology' } },
    { id: 'thema:2', type: 'thema', label: { nl: 'Nestplekken', en: 'Nesting places' } },
    { id: 'subthema:3', type: 'subthema', label: { nl: 'Richels', en: 'Ledges' } },
    { id: 'destillaat:9f1c', type: 'destillaat', label: { nl: 'Nestelen', en: 'Nesting' } },
  ],
}

// De labels zoals een lezer ze in één taal ziet.
function read(trail: readonly Crumb[], lang: 'en' | 'nl' = 'en'): string[] {
  return trail.map((c) => crumbText(c, (text) => text?.[lang] ?? ''))
}

describe('SourcePanelStore', () => {
  it('opens on the source with the clicked number and keeps its owner', () => {
    const { panel } = store()
    panel.show([SOURCE, SECOND], 2, { owner: 'a1' })
    expect(panel.open).toBe(true)
    expect(panel.current?.number).toBe(2)
    expect(panel.total).toBe(2)
    expect(panel.showsSource('a1', 2)).toBe(true)
    // Een gelijk bronnummer in een ánder antwoord licht niet op.
    expect(panel.showsSource('a2', 2)).toBe(false)
    expect(panel.showsSource('a1', 1)).toBe(false)
  })

  it('falls back to the first source when the citation points at a number that was not delivered', () => {
    const { panel } = store()
    panel.show([SOURCE, SECOND], 9)
    expect(panel.current?.number).toBe(1)
  })

  it('does not open on an answer without sources', () => {
    const { panel } = store()
    panel.show([], 1)
    expect(panel.open).toBe(false)
  })

  it('steps through the sources of one answer and stops at both ends', () => {
    const { panel } = store()
    panel.show([SOURCE, SECOND], 1)
    panel.step(-1)
    expect(panel.index).toBe(0)
    panel.step(1)
    expect(panel.current?.number).toBe(2)
    panel.step(1)
    expect(panel.current?.number).toBe(2)
  })

  it('forgets the answer when it closes, so a reopen never shows a stale source', () => {
    const { panel } = store()
    panel.show([SOURCE, SECOND], 2)
    panel.hide()
    expect(panel.open).toBe(false)
    expect(panel.current).toBeNull()
    expect(panel.total).toBe(0)
  })

  it('loads the breadcrumb of the shown source and drops the source itself from it', async () => {
    const { panel, calls } = store([nodeRoute(TRAIL)])
    panel.show([SOURCE], 1)
    await settle()
    expect(read(panel.trail)).toEqual(['Urban ecology', 'Nesting places', 'Ledges'])
    expect(calls[0]?.url).toContain('/graph/nodes/destillaat%3A9f1c')
  })

  it('shows no breadcrumb and no error when the lookup fails', async () => {
    const { panel } = store([nodeRoute(null, false)])
    panel.show([SOURCE], 1)
    await settle()
    expect(panel.trail).toEqual([])
    expect(panel.open).toBe(true)
  })

  it('asks for nothing when the source has no id', async () => {
    const { panel, calls } = store([nodeRoute(TRAIL)])
    panel.show([OLD_SOURCE], 1)
    await settle()
    expect(calls).toHaveLength(0)
    expect(panel.trail).toEqual([])
  })

  it('never lets a slow answer for an earlier source overwrite the current breadcrumb', async () => {
    const bodies: Record<string, unknown> = {
      'destillaat%3A9f1c': TRAIL,
      'destillaat%3A2b2b': { ...TRAIL, trail: [{ id: 'domein:9', type: 'domein', label: { nl: 'Gedrag', en: 'Behaviour' } }] },
    }
    const scripted = scriptedFetch([
      {
        method: 'GET',
        match: '/graph/nodes/',
        respond: ({ url }) => {
          const key = Object.keys(bodies).find((id) => url.includes(id))
          return jsonResponse(key ? bodies[key] : {})
        },
      },
    ])
    const panel = new SourcePanelStore({ fetch: scripted.fetch })
    panel.show([SOURCE, SECOND], 1)
    // Doorklikken vóór het eerste antwoord landt: het spoor van bron 1 is dan
    // een antwoord uit een wereld die niet meer bestaat.
    panel.step(1)
    await settle()
    expect(read(panel.trail)).toEqual(['Behaviour'])
  })

  it('drops the breadcrumb of a closed drawer', async () => {
    const { panel } = store([nodeRoute(TRAIL)])
    panel.show([SOURCE], 1)
    panel.hide()
    await settle()
    expect(panel.trail).toEqual([])
  })
})

describe('parseTrail', () => {
  it('keeps only the three structure layers, in the order the server sent them', () => {
    expect(read(parseTrail(TRAIL))).toEqual(['Urban ecology', 'Nesting places', 'Ledges'])
    expect(read(parseTrail(TRAIL), 'nl')).toEqual(['Stadsecologie', 'Nestplekken', 'Richels'])
  })

  // Een eentalig corpus mag een kale string sturen; die staat er in elke taal.
  it('also reads a label that is plain text', () => {
    const trail = parseTrail({ trail: [{ id: 'thema:2', type: 'thema', label: ' Nesting places ' }] })
    expect(read(trail, 'nl')).toEqual(['Nesting places'])
  })

  // De graaf toont geen perspectieven meer, dus het kruimelpad ook
  // niet — ook niet als een server het nog meestuurt.
  it('leaves out a perspective a server still sends', () => {
    expect(parseTrail({
      ...TRAIL,
      trail: [...TRAIL.trail, { id: 'perspective:4', type: 'perspective', label: { en: 'The observer' } }],
      perspective: { id: 'perspective:4', type: 'perspective', label: { en: 'The observer' } },
    }).map((c) => c.id)).toEqual(['domein:1', 'thema:2', 'subthema:3'])
  })

  it('ignores a trail shape it does not know', () => {
    expect(parseTrail({ trail: 'domein:1' })).toEqual([])
    expect(parseTrail({ trail: [42, null, 'thema:2'] })).toEqual([])
    expect(parseTrail({ trail: [{ id: 'x', type: 'quote', label: 'A quote' }] })).toEqual([])
    expect(parseTrail({ trail: [{ id: 'x', type: 'thema' }] })).toEqual([])
    expect(parseTrail({ trail: [{ id: 'x', type: 'thema', label: '  ' }] })).toEqual([])
    expect(parseTrail({ trail: [{ id: 'x', type: 'thema', label: { nl: ' ', en: '' } }] })).toEqual([])
    expect(parseTrail({ trail: [{ type: 'thema', label: { en: 'No id' } }] })).toEqual([])
    expect(parseTrail(null)).toEqual([])
    expect(parseTrail([TRAIL])).toEqual([])
  })

  it('names a repeated step once', () => {
    expect(read(parseTrail({
      trail: [
        { id: 'thema:2', type: 'thema', label: { en: 'Nesting places' } },
        { id: 'thema:2', type: 'thema', label: { en: 'Nesting places' } },
      ],
    }))).toEqual(['Nesting places'])
  })
})

describe('trailSteps', () => {
  // De echte taalkeuze van de lade, met haar terugval op de andere taal.
  const shown = (trail: readonly Crumb[], lang: 'en' | 'nl') => trailSteps(trail, new UiLocale(lang).pickContent).map((step) => step.text)

  // Zo staat het in de v3-kennisbank: het subthema draagt de titel van zijn
  // thema, in beide talen.
  it('names a subtheme with the title of its theme once, in either language', () => {
    const trail = parseTrail({
      trail: [
        { id: 'domein:1', type: 'domein', label: { nl: 'Gedrag en zintuigen', en: 'Behaviour and senses' } },
        { id: 'thema:2', type: 'thema', label: { nl: 'Gehoor en geluid bij vogels', en: 'Hearing and sound in birds' } },
        { id: 'subthema:3', type: 'subthema', label: { nl: 'Gehoor en geluid bij vogels', en: 'Hearing and sound in birds' } },
      ],
    })
    expect(shown(trail, 'nl')).toEqual(['Gedrag en zintuigen', 'Gehoor en geluid bij vogels'])
    expect(shown(trail, 'en')).toEqual(['Behaviour and senses', 'Hearing and sound in birds'])
  })

  // Gelijk is gelijk in de getoonde taal: wat in het Nederlands samenvalt, kan
  // in het Engels twee titels zijn. Dan staan ze er in het Engels allebei.
  it('compares in the language on screen, so the two languages may differ', () => {
    const trail = parseTrail({
      trail: [
        { id: 'thema:2', type: 'thema', label: { nl: 'Nestplekken', en: 'Nesting places' } },
        { id: 'subthema:3', type: 'subthema', label: { nl: 'Nestplekken', en: 'Nest sites on buildings' } },
      ],
    })
    expect(shown(trail, 'nl')).toEqual(['Nestplekken'])
    expect(shown(trail, 'en')).toEqual(['Nesting places', 'Nest sites on buildings'])
  })

  it('treats a different case or spacing as the same title, but not a different word', () => {
    const trail = parseTrail({
      trail: [
        { id: 'domein:1', type: 'domein', label: { en: 'Urban ecology' } },
        { id: 'thema:2', type: 'thema', label: { en: 'Nesting places' } },
        { id: 'subthema:3', type: 'subthema', label: { en: 'nesting  Places' } },
      ],
    })
    expect(shown(trail, 'en')).toEqual(['Urban ecology', 'Nesting places'])
    const differs = parseTrail({
      trail: [
        { id: 'thema:2', type: 'thema', label: { en: 'Nesting places' } },
        { id: 'subthema:3', type: 'subthema', label: { en: 'Nesting place' } },
      ],
    })
    expect(shown(differs, 'en')).toEqual(['Nesting places', 'Nesting place'])
  })

  // Dezelfde ï kan als één teken (U+00EF) of als i plus trema (U+0308) in de
  // kennisbank staan. Op het scherm is dat dezelfde titel.
  it('treats a composed and a decomposed accent as the same title', () => {
    const trail = parseTrail({
      trail: [
        { id: 'thema:2', type: 'thema', label: { nl: 'Reïntroductie van roofvogels', en: 'Reintroduction of raptors' } },
        { id: 'subthema:3', type: 'subthema', label: { nl: 'Reïntroductie van roofvogels', en: 'Reintroduction of raptors' } },
      ],
    })
    expect(shown(trail, 'nl')).toEqual(['Reïntroductie van roofvogels'])
  })

  // Alleen het niveau direct erboven telt: een verderop herhaald woord is een
  // andere plek in de graaf en blijft staan.
  it('only folds a step into the one right above it', () => {
    const trail = parseTrail({
      trail: [
        { id: 'domein:1', type: 'domein', label: { en: 'Pigeons' } },
        { id: 'thema:2', type: 'thema', label: { en: 'Feeding' } },
        { id: 'subthema:3', type: 'subthema', label: { en: 'Pigeons' } },
      ],
    })
    expect(shown(trail, 'en')).toEqual(['Pigeons', 'Feeding', 'Pigeons'])
  })
})

