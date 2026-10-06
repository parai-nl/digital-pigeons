import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'
import { describe, expect, it, vi } from 'vitest'
import AssistantMessage from '../chat/AssistantMessage.svelte'
import type { ChatMessage } from '../chat/chatStore.svelte'
import { OLD_SOURCE, SOURCE, serviceContext, testServices } from '../testing/chat'
import { jsonResponse, scriptedFetch, type Route } from '../testing/fetch'
import { parseSources } from '../sources'
import SourceDrawer from './SourceDrawer.svelte'

// Het gedrag van de bronlade zoals een lezer hem gebruikt: een verwijzing in
// de zin en één regel "n bronnen" openen hem, hij sluit met Escape, de
// sluitknop en de verduistering, en de focus komt terug waar hij vandaan kwam.
// De lade hangt in App.svelte; hier staat hij naast het antwoord dat hem
// opent, zodat dezelfde services beide voeden.
//
// De opbouw is de kern van wat de lezer ziet: één regel met het perspectief,
// dan het standpunt als hoofdelement, dan de originele woorden, en pas daarna
// — dichtgeklapt — de brongegevens met het kruimelpad.

const SECOND = {
  ...SOURCE,
  number: 2,
  id: 'destillaat:2b2b',
  text: 'Pigeons drink by suction.',
  perspective: 'The physiologist',
  ref: { paper: 'weber1994', quote: 'They keep the beak submerged.' },
}

const TRAIL = {
  node: { id: 'destillaat:9f1c', type: 'destillaat', label: { nl: 'Nestelen', en: 'Nesting' } },
  trail: [
    { id: 'domein:1', type: 'domein', label: { nl: 'Stadsecologie', en: 'Urban ecology' } },
    { id: 'thema:2', type: 'thema', label: { nl: 'Nestplekken', en: 'Nesting places' } },
    { id: 'subthema:3', type: 'subthema', label: { nl: 'Richels', en: 'Ledges' } },
  ],
}

function message(patch: Partial<ChatMessage> = {}): ChatMessage {
  return {
    key: 'a1',
    role: 'assistant',
    kind: 'pigeon_answer',
    pigeonId: 'biological',
    content: 'Pigeons nest on ledges [1] and drink differently [2].',
    sources: [SOURCE, SECOND],
    tools: [],
    partial: false,
    streaming: false,
    ...patch,
  }
}

async function settle(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
  await tick()
}

function view(patch: Partial<ChatMessage> = {}, routes: Route[] = [], lang: 'en' | 'nl' = 'en') {
  const services = testServices(scriptedFetch(routes).fetch)
  services.locale.choose(lang)
  const context = serviceContext(services)
  render(AssistantMessage, { props: { message: message(patch), personaName: 'Knowledge Partner' }, context })
  render(SourceDrawer, { context })
  return { services }
}

function nodeRoute(body: unknown, ok = true): Route {
  return { method: 'GET', match: '/graph/nodes/', respond: () => (ok ? jsonResponse(body) : new Response('nope', { status: 500 })) }
}

describe('SourceDrawer', () => {
  it('leaves no source list under the answer, only the citations and one quiet line', () => {
    view()
    expect(screen.queryByTestId('source-drawer')).toBeNull()
    expect(screen.getByTestId('source-count')).toHaveTextContent('2 sources')
    expect(screen.getByRole('button', { name: 'Source 1' })).toBeInTheDocument()
    expect(screen.queryByTestId('source-quote')).toBeNull()
  })

  it('opens the drawer on the cited source and shows its passage and summary', async () => {
    view()
    await fireEvent.click(screen.getByRole('button', { name: 'Source 2' }))
    await tick()
    expect(screen.getByTestId('source-position')).toHaveTextContent('Source 2 of 2')
    expect(screen.queryByTestId('source-title')).toBeNull()
    expect(screen.getByTestId('source-quote')).toHaveTextContent('They keep the beak submerged.')
    expect(screen.getByTestId('source-summary')).toHaveTextContent('Pigeons drink by suction.')
    expect(screen.getByTestId('source-reading')).toHaveTextContent('read the original passage below to check it')
    expect(screen.getByRole('button', { name: 'Source 2' })).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('button', { name: 'Source 1' })).toHaveAttribute('aria-expanded', 'false')
  })

  it('names the perspective on one labelled line and gives the standpoint the heading weight', async () => {
    view({ sources: [{ ...SOURCE, perspective: 'Scientific interest', ref: { ...SOURCE.ref, citation: 'Lewald, J. (1987). Sound localisation. Journal, 4(2).' } }] })
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    const line = screen.getByTestId('source-perspective')
    expect(line).toHaveTextContent('Perspective')
    expect(line).toHaveTextContent('Scientific interest')
    expect(line.textContent).not.toContain('Lewald')
    expect(screen.getByTestId('source-drawer').querySelector('h2')).toBeNull()
    const finding = screen.getByTestId('source-finding')
    expect(finding).toHaveTextContent('The standpoint drawn from this source')
    expect(line.compareDocumentPosition(finding) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  // De tweetalige kennisbank (duiven/3). De samenvatting volgt de taal van de
  // lezer; het citaat eronder is altijd het origineel en verandert nooit mee.
  const BILINGUAL = {
    ...SOURCE,
    text: 'Duiven nestelen op richels.',
    perspective: 'De kritische wetenschapper',
    sourceLanguage: 'nl' as const,
    localized: {
      text: { nl: 'Duiven nestelen op richels.', en: 'Pigeons nest on ledges.' },
      perspective: { nl: 'De kritische wetenschapper', en: 'The critical scientist' },
    },
  }

  // De wire draagt naast de samenvatting en het perspectief ook een
  // thema, tweetalig. De bronkaart heeft dat nooit laten zien — waar dit
  // standpunt hangt staat in het kruimelpad onder "Over deze bron" — en dat
  // blijft zo, in allebei de talen.
  it('never puts the knowledge base theme on the card, in either language', async () => {
    for (const [lang, absent] of [['en', 'Urban ecology'], ['nl', 'Stadsecologie']] as const) {
      // Door de lezer van de wire heen, want dáár werd het thema gelezen.
      const parsed = parseSources([{
        ...BILINGUAL,
        theme: 'Stadsecologie',
        localized: { ...BILINGUAL.localized, theme: { nl: 'Stadsecologie', en: 'Urban ecology' } },
      }])
      view({ sources: parsed }, [], lang)
      await fireEvent.click(screen.getByTestId('source-count'))
      await tick()
      const card = screen.getByTestId('source-drawer').textContent ?? ''
      expect(card, lang).not.toContain(absent)
      // Wat er wél staat: het perspectief in de taal van de lezer.
      expect(screen.getByTestId('source-perspective')).toHaveTextContent(lang === 'nl' ? 'De kritische wetenschapper' : 'The critical scientist')
      cleanup()
    }
  })

  it('shows the English reader the translation and says the quotation is the original', async () => {
    view({ sources: [BILINGUAL] }, [], 'en')
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.getByTestId('source-summary')).toHaveTextContent('Pigeons nest on ledges.')
    expect(screen.getByTestId('source-perspective')).toHaveTextContent('The critical scientist')
    expect(screen.getByTestId('source-translated')).toHaveTextContent('Translated summary; the quotation is the original.')
    // Het citaat is niet vertaald en staat er onveranderd.
    expect(screen.getByTestId('source-quote')).toHaveTextContent('They prefer a ledge on the north side.')
  })

  it('gives the Dutch reader the original without a translation label', async () => {
    view({ sources: [BILINGUAL] }, [], 'nl')
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.getByTestId('source-summary')).toHaveTextContent('Duiven nestelen op richels.')
    expect(screen.queryByTestId('source-translated')).toBeNull()
  })

  it('keeps a saved answer from before the translation in its own language', async () => {
    view({ sources: [{ ...SOURCE, text: 'Duiven nestelen op richels.' }] }, [], 'en')
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.getByTestId('source-summary')).toHaveTextContent('Duiven nestelen op richels.')
    expect(screen.queryByTestId('source-translated')).toBeNull()
  })

  it('says out loud that the standpoint is a summary and where to check it', async () => {
    view({}, [], 'nl')
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.getByTestId('source-reading')).toHaveTextContent('Zo is deze bron samengevat; lees de oorspronkelijke passage hieronder om dat te controleren.')
  })

  it('reads from the standpoint down to the passage, with the source details folded away', async () => {
    view({}, [nodeRoute(TRAIL)])
    await fireEvent.click(screen.getByTestId('source-count'))
    await settle()
    const finding = screen.getByTestId('source-finding')
    const original = screen.getByTestId('source-original')
    const about = screen.getByTestId('source-about')
    expect(finding.compareDocumentPosition(original) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(original.compareDocumentPosition(about) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(about).not.toHaveAttribute('open')
    expect(about).toContainElement(screen.getByTestId('source-trail'))
    expect(screen.getByTestId('source-trail')).not.toBeVisible()
  })

  it('shows the reference, the doi and the breadcrumb once the details are opened', async () => {
    view({ sources: [{ ...SOURCE, ref: { ...SOURCE.ref, citation: 'Lewald, J. (1987). Sound localisation.', doi: '10.1234/synthetic' } }] }, [nodeRoute(TRAIL)])
    await fireEvent.click(screen.getByTestId('source-count'))
    await settle()
    const about = screen.getByTestId('source-about')
    await fireEvent.click(about.querySelector('summary') as HTMLElement)
    await tick()
    expect(about).toHaveAttribute('open')
    expect(screen.getByTestId('source-citation')).toBeVisible()
    expect(screen.getByTestId('source-doi')).toBeVisible()
    expect(screen.getByTestId('source-trail')).toBeVisible()
  })

  it('does not promise an original passage that the source does not carry', async () => {
    view({ sources: [{ ...SOURCE, ref: { paper: 'lewald1987' } }] }, [], 'nl')
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.queryByTestId('source-quote')).toBeNull()
    expect(screen.getByTestId('source-reading')).toHaveTextContent('de oorspronkelijke passage staat er hier niet bij')
  })

  it('opens the first source from the "n sources" line', async () => {
    view()
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.getByTestId('source-position')).toHaveTextContent('Source 1 of 2')
    expect(screen.getByTestId('source-summary')).toHaveTextContent('Pigeons nest on ledges.')
  })

  it('says "1 source" for a single source and reads the count in Dutch', async () => {
    view({ sources: [SOURCE] }, [], 'nl')
    expect(screen.getByTestId('source-count')).toHaveTextContent('1 bron')
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.getByTestId('source-position')).toHaveTextContent('Bron 1 van 1')
  })

  it('steps to the next and previous source of the same answer', async () => {
    view()
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.getByTestId('source-previous')).toBeDisabled()
    await fireEvent.click(screen.getByTestId('source-next'))
    await tick()
    expect(screen.getByTestId('source-quote')).toHaveTextContent('They keep the beak submerged.')
    expect(screen.getByTestId('source-next')).toBeDisabled()
    await fireEvent.click(screen.getByTestId('source-previous'))
    await tick()
    expect(screen.getByTestId('source-quote')).toHaveTextContent('They prefer a ledge on the north side.')
  })

  // Het plafond staat op twintig bronnen. De lade toont er één
  // tegelijk, dus twintig is voor haar niet anders dan twee — behalve dat de
  // teller, de positieregel en de tweecijferige citatieknoppen moeten kloppen.
  it('carries a full set of twenty sources through the chip, the position line and the citations', async () => {
    const many = Array.from({ length: 20 }, (_, i) => ({
      ...SOURCE,
      number: i + 1,
      id: `destillaat:${i + 1}`,
      text: `Standpoint ${i + 1}.`,
      ref: { paper: 'synthetic-study', quote: `Passage ${i + 1}.` },
    }))
    view({ sources: many, content: 'Pigeons nest on ledges [1] and also higher up [20].' })
    expect(screen.getByTestId('source-count')).toHaveTextContent('20 sources')
    await fireEvent.click(screen.getByRole('button', { name: 'Source 20' }))
    await tick()
    expect(screen.getByTestId('source-position')).toHaveTextContent('Source 20 of 20')
    expect(screen.getByTestId('source-quote')).toHaveTextContent('Passage 20.')
    expect(screen.getByTestId('source-next')).toBeDisabled()
    await fireEvent.click(screen.getByTestId('source-previous'))
    await tick()
    expect(screen.getByTestId('source-position')).toHaveTextContent('Source 19 of 20')
  })

  it('changes standpoint and perspective together when you step to the next source', async () => {
    view({ sources: [{ ...SOURCE, perspective: 'The city ecologist' }, SECOND] })
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.getByTestId('source-perspective')).toHaveTextContent('The city ecologist')
    expect(screen.getByTestId('source-summary')).toHaveTextContent('Pigeons nest on ledges.')
    await fireEvent.click(screen.getByTestId('source-next'))
    await tick()
    expect(screen.getByTestId('source-perspective')).toHaveTextContent('The physiologist')
    expect(screen.getByTestId('source-perspective')).not.toHaveTextContent('The city ecologist')
    expect(screen.getByTestId('source-summary')).toHaveTextContent('Pigeons drink by suction.')
  })

  it('moves focus into the drawer and returns it to the citation on Escape', async () => {
    view()
    const cite = screen.getByRole('button', { name: 'Source 1' })
    cite.focus()
    await fireEvent.click(cite)
    await settle()
    expect(document.activeElement).toBe(screen.getByTestId('source-close'))
    await fireEvent.keyDown(window, { key: 'Escape' })
    await settle()
    expect(screen.queryByTestId('source-drawer')).toBeNull()
    expect(document.activeElement).toBe(cite)
  })

  it('returns focus to the line that opened it when the close button closes it', async () => {
    view()
    const line = screen.getByTestId('source-count')
    line.focus()
    await fireEvent.click(line)
    await settle()
    await fireEvent.click(screen.getByTestId('source-close'))
    await settle()
    expect(screen.queryByTestId('source-drawer')).toBeNull()
    expect(document.activeElement).toBe(line)
  })

  it('closes on a click on the backdrop', async () => {
    view()
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    await fireEvent.click(screen.getByTestId('source-backdrop'))
    await tick()
    expect(screen.queryByTestId('source-drawer')).toBeNull()
  })

  // De verduistering ligt over het antwoord heen. Wat er onder de klik
  // ligt bepaalt de browser met elementsFromPoint; jsdom kent dat niet, dus de
  // test zegt het hier zelf: eerst de verduistering, dan wat eronder staat.
  function underBackdrop(below: () => Element) {
    const original = Object.getOwnPropertyDescriptor(document, 'elementsFromPoint')
    Object.defineProperty(document, 'elementsFromPoint', {
      configurable: true,
      value: () => [screen.getByTestId('source-backdrop'), below(), document.body],
    })
    return () => {
      if (original) Object.defineProperty(document, 'elementsFromPoint', original)
      else delete (document as { elementsFromPoint?: unknown }).elementsFromPoint
    }
  }

  it('jumps to another cited source when its citation is clicked through the backdrop', async () => {
    view()
    const first = screen.getByRole('button', { name: 'Source 1' })
    const second = screen.getByRole('button', { name: 'Source 2' })
    first.focus()
    await fireEvent.click(first)
    await settle()
    expect(screen.getByTestId('source-position')).toHaveTextContent('Source 1 of 2')
    const restore = underBackdrop(() => second)
    try {
      await fireEvent.click(screen.getByTestId('source-backdrop'))
      await settle()
    } finally {
      restore()
    }
    expect(screen.getByTestId('source-drawer')).toBeInTheDocument()
    expect(screen.getByTestId('source-position')).toHaveTextContent('Source 2 of 2')
    expect(screen.getByTestId('source-quote')).toHaveTextContent('They keep the beak submerged.')
    expect(second).toHaveAttribute('aria-expanded', 'true')
    expect(first).toHaveAttribute('aria-expanded', 'false')
    // De focus blijft in de lade, en keert bij sluiten terug naar de verwijzing
    // die het laatst gekozen werd.
    expect(document.activeElement).toBe(screen.getByTestId('source-close'))
    await fireEvent.keyDown(window, { key: 'Escape' })
    await settle()
    expect(screen.queryByTestId('source-drawer')).toBeNull()
    expect(document.activeElement).toBe(second)
  })

  it('returns focus to the citation a keyboard user moved on to while the drawer was open', async () => {
    view()
    const first = screen.getByRole('button', { name: 'Source 1' })
    const second = screen.getByRole('button', { name: 'Source 2' })
    first.focus()
    await fireEvent.click(first)
    await settle()
    second.focus()
    await fireEvent.click(second)
    await settle()
    expect(screen.getByTestId('source-position')).toHaveTextContent('Source 2 of 2')
    await fireEvent.keyDown(window, { key: 'Escape' })
    await settle()
    expect(document.activeElement).toBe(second)
  })

  it('still closes on a backdrop click that lands on anything but a citation', async () => {
    view()
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    const restore = underBackdrop(() => screen.getByTestId('source-count'))
    try {
      await fireEvent.click(screen.getByTestId('source-backdrop'))
      await tick()
    } finally {
      restore()
    }
    expect(screen.queryByTestId('source-drawer')).toBeNull()
  })

  // Scrollen boven de verduistering schuift het gesprek eronder, met
  // de lade open. jsdom rekent geen opmaak uit; de lijst krijgt hier zelf een
  // hoogte en een scrollBy die meeschrijft.
  function conversationBelow() {
    const list = document.createElement('div')
    list.dataset.testid = 'message-list-scroll'
    list.style.overflowY = 'auto'
    Object.defineProperty(list, 'scrollHeight', { value: 4000 })
    Object.defineProperty(list, 'clientHeight', { value: 800 })
    const scrollBy = vi.fn()
    list.scrollBy = scrollBy as unknown as HTMLElement['scrollBy']
    const line = document.createElement('p')
    list.append(line)
    document.body.append(list)
    return { line, scrollBy, remove: () => list.remove() }
  }

  it('scrolls the conversation under the backdrop with the wheel and keeps the drawer open', async () => {
    view()
    await fireEvent.click(screen.getByRole('button', { name: 'Source 1' }))
    await settle()
    const below = conversationBelow()
    const restore = underBackdrop(() => below.line)
    try {
      const backdrop = screen.getByTestId('source-backdrop')
      // fireEvent geeft false terug als de standaardactie is tegengehouden:
      // de pagina zelf schuift niet nog eens mee.
      expect(await fireEvent.wheel(backdrop, { deltaY: -240, clientX: 200, clientY: 300 })).toBe(false)
      expect(below.scrollBy).toHaveBeenLastCalledWith({ left: 0, top: -240 })
      // Een wiel dat in regels telt, schuift ook een zichtbaar stuk.
      await fireEvent.wheel(backdrop, { deltaY: 3, deltaMode: 1, clientX: 200, clientY: 300 })
      expect(below.scrollBy).toHaveBeenLastCalledWith({ left: 0, top: 48 })
      // Shift met wiel is opzij, ook als de browser de beweging op deltaY laat.
      await fireEvent.wheel(backdrop, { deltaY: 100, shiftKey: true, clientX: 200, clientY: 300 })
      expect(below.scrollBy).toHaveBeenLastCalledWith({ left: 100, top: 0 })
      // Ctrl met wiel is zoomen en blijft van de browser.
      below.scrollBy.mockClear()
      expect(await fireEvent.wheel(backdrop, { deltaY: 100, ctrlKey: true, clientX: 200, clientY: 300 })).toBe(true)
      expect(below.scrollBy).not.toHaveBeenCalled()
    } finally {
      restore()
      below.remove()
    }
    expect(screen.getByTestId('source-position')).toHaveTextContent('Source 1 of 2')
    expect(document.activeElement).toBe(screen.getByTestId('source-close'))
  })

  it('moves the conversation along with a finger dragged over the backdrop', async () => {
    view()
    await fireEvent.click(screen.getByRole('button', { name: 'Source 1' }))
    await settle()
    const below = conversationBelow()
    const restore = underBackdrop(() => below.line)
    try {
      const backdrop = screen.getByTestId('source-backdrop')
      const at = (clientY: number) => ({ touches: [{ identifier: 7, clientX: 120, clientY }] })
      await fireEvent.touchStart(backdrop, at(300))
      await fireEvent.touchMove(backdrop, at(380))
      // Vinger omlaag: het gesprek gaat terug naar eerdere berichten.
      expect(below.scrollBy).toHaveBeenLastCalledWith({ left: 0, top: -80 })
      await fireEvent.touchMove(backdrop, at(350))
      expect(below.scrollBy).toHaveBeenLastCalledWith({ left: 0, top: 30 })
      await fireEvent.touchCancel(backdrop)
      below.scrollBy.mockClear()
      // Twee vingers is knijpen, geen schuiven.
      await fireEvent.touchStart(backdrop, { touches: [{ identifier: 1, clientX: 100, clientY: 300 }, { identifier: 2, clientX: 200, clientY: 300 }] })
      await fireEvent.touchMove(backdrop, { touches: [{ identifier: 1, clientX: 100, clientY: 380 }, { identifier: 2, clientX: 200, clientY: 220 }] })
      expect(below.scrollBy).not.toHaveBeenCalled()
    } finally {
      restore()
      below.remove()
    }
    expect(screen.getByTestId('source-drawer')).toBeInTheDocument()
  })

  it('scrolls the conversation also when nothing scrollable lies right under the pointer', async () => {
    view()
    await fireEvent.click(screen.getByRole('button', { name: 'Source 1' }))
    await settle()
    const below = conversationBelow()
    // Bijvoorbeeld de kop of het invoerveld: die scrollen zelf niet.
    const header = document.createElement('header')
    document.body.append(header)
    const restore = underBackdrop(() => header)
    try {
      await fireEvent.wheel(screen.getByTestId('source-backdrop'), { deltaY: -240, clientX: 200, clientY: 40 })
      expect(below.scrollBy).toHaveBeenLastCalledWith({ left: 0, top: -240 })
    } finally {
      restore()
      below.remove()
      header.remove()
    }
  })

  // Wie met de lade open verder gescrold heeft, staat waar hij wil staan. Bij
  // sluiten krijgt de verwijzing de focus terug, maar zonder de lijst terug te
  // trekken naar waar de lezer begon.
  it('returns focus without scrolling back once the reader scrolled under the backdrop', async () => {
    view()
    const first = screen.getByRole('button', { name: 'Source 1' })
    const focus = vi.spyOn(first, 'focus')
    for (const scrolled of [true, false]) {
      first.focus()
      await fireEvent.click(first)
      await settle()
      const below = conversationBelow()
      const restore = underBackdrop(() => below.line)
      try {
        if (scrolled) await fireEvent.wheel(screen.getByTestId('source-backdrop'), { deltaY: 600, clientX: 200, clientY: 300 })
        focus.mockClear()
        await fireEvent.keyDown(window, { key: 'Escape' })
        await settle()
      } finally {
        restore()
        below.remove()
      }
      expect(document.activeElement, String(scrolled)).toBe(first)
      expect(focus, String(scrolled)).toHaveBeenLastCalledWith({ preventScroll: scrolled })
    }
  })

  // Een tik op een lijst die nog uitloopt, zet hem stil. Die tik is geen klik
  // naast de verwijzing: de lade blijft open. Een tik daarna sluit wel.
  it('keeps the drawer open on the tap that stops a glide', async () => {
    vi.stubGlobal('requestAnimationFrame', vi.fn(() => 1))
    vi.stubGlobal('cancelAnimationFrame', vi.fn())
    view()
    await fireEvent.click(screen.getByRole('button', { name: 'Source 1' }))
    await settle()
    const below = conversationBelow()
    const restore = underBackdrop(() => below.line)
    try {
      const backdrop = screen.getByTestId('source-backdrop')
      const at = (clientY: number) => ({ touches: [{ identifier: 3, clientX: 120, clientY }] })
      await fireEvent.touchStart(backdrop, at(300))
      await fireEvent.touchMove(backdrop, at(380))
      await fireEvent.touchEnd(backdrop, { touches: [] })
      expect(requestAnimationFrame).toHaveBeenCalled()
      // De tik die de uitloop stilzet.
      await fireEvent.touchStart(backdrop, at(380))
      await fireEvent.touchEnd(backdrop, { touches: [] })
      await fireEvent.click(backdrop)
      await settle()
      expect(screen.getByTestId('source-position')).toHaveTextContent('Source 1 of 2')
      // Een gewone tik daarna.
      await fireEvent.touchStart(backdrop, at(380))
      await fireEvent.touchEnd(backdrop, { touches: [] })
      await fireEvent.click(backdrop)
      await settle()
    } finally {
      restore()
      below.remove()
      vi.unstubAllGlobals()
    }
    expect(screen.queryByTestId('source-drawer')).toBeNull()
  })

  it('shows the breadcrumb after a successful lookup, without any internal id', async () => {
    view({}, [nodeRoute(TRAIL)])
    await fireEvent.click(screen.getByTestId('source-count'))
    await settle()
    const trail = screen.getByTestId('source-trail')
    expect(trail).toHaveTextContent('Urban ecology')
    expect(trail).toHaveTextContent('Nesting places')
    expect(trail).toHaveTextContent('Ledges')
    expect(trail.textContent).not.toContain('destillaat')
    expect(trail.textContent).not.toContain('domein:1')
  })

  // Het kruimelpad is kennisbanktekst: het volgt de taal van de lezer, zoals de
  // samenvatting erboven.
  it('shows the breadcrumb in the language of the reader', async () => {
    view({}, [nodeRoute(TRAIL)], 'nl')
    await fireEvent.click(screen.getByTestId('source-count'))
    await settle()
    const trail = screen.getByTestId('source-trail')
    expect(trail).toHaveTextContent('Nestplekken')
    expect(trail).toHaveTextContent('Richels')
    expect(trail).not.toHaveTextContent('Nesting places')
  })

  // Een subthema met de titel van zijn thema staat er één keer. Dat
  // hangt af van de taal op het scherm, dus een taalwissel rekent het opnieuw.
  it('names a subtheme with the title of its theme once, and recounts after a language switch', async () => {
    const sameInDutch = {
      ...TRAIL,
      trail: [
        { id: 'domein:1', type: 'domein', label: { nl: 'Gedrag en zintuigen', en: 'Behaviour and senses' } },
        { id: 'thema:2', type: 'thema', label: { nl: 'Gehoor en geluid bij vogels', en: 'Hearing and sound in birds' } },
        { id: 'subthema:3', type: 'subthema', label: { nl: 'Gehoor en geluid bij vogels', en: 'Hearing in pigeons' } },
      ],
    }
    const { services } = view({}, [nodeRoute(sameInDutch)], 'nl')
    await fireEvent.click(screen.getByTestId('source-count'))
    await settle()
    const steps = () => [...screen.getByTestId('source-trail').querySelectorAll('.trail-step')].map((el) => el.textContent)
    expect(steps()).toEqual(['Gedrag en zintuigen', 'Gehoor en geluid bij vogels'])
    expect(screen.getByTestId('source-trail').textContent).toBe('Gedrag en zintuigen›Gehoor en geluid bij vogels')

    services.locale.choose('en')
    await tick()
    expect(steps()).toEqual(['Behaviour and senses', 'Hearing and sound in birds', 'Hearing in pigeons'])

    services.locale.choose('nl')
    await tick()
    expect(steps()).toEqual(['Gedrag en zintuigen', 'Gehoor en geluid bij vogels'])
  })

  it('stays silent when the breadcrumb lookup fails', async () => {
    view({}, [nodeRoute(null, false)])
    await fireEvent.click(screen.getByTestId('source-count'))
    await settle()
    expect(screen.queryByTestId('source-trail')).toBeNull()
    expect(screen.getByTestId('source-drawer')).toBeInTheDocument()
    expect(screen.getByTestId('source-summary')).toHaveTextContent('Pigeons nest on ledges.')
  })

  it('ignores a trail shape it does not know instead of printing it', async () => {
    view({}, [nodeRoute({ trail: ['domein:1', { label: 'No type' }, { type: 'quote', label: 'A quote' }] })])
    await fireEvent.click(screen.getByTestId('source-count'))
    await settle()
    expect(screen.queryByTestId('source-trail')).toBeNull()
  })

  it('shows a doi as text and never as an address', async () => {
    view({ sources: [{ ...SOURCE, ref: { ...SOURCE.ref, doi: '10.1234/synthetic' } }] })
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.getByTestId('source-doi')).toHaveTextContent('10.1234/synthetic')
    expect(screen.getByTestId('source-drawer').querySelector('a[href]')).toBeNull()
  })

  it('never turns a doi-shaped-but-not-doi value into an address', async () => {
    view({ sources: [{ ...SOURCE, ref: { ...SOURCE.ref, doi: 'javascript:synthetic-doi' } }] })
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.getByTestId('source-doi')).toHaveTextContent('javascript:synthetic-doi')
    expect(screen.queryByTestId('source-doi-link')).toBeNull()
  })

  it('states the grounding note in plain language, with the standpoint it qualifies', async () => {
    view({ sources: [{ ...SOURCE, grounding: 'unverified' }] })
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    const unverified = screen.getByTestId('source-unverified')
    expect(unverified).toHaveTextContent('not been checked against the original text')
    expect(screen.getByTestId('source-finding')).toContainElement(unverified)
    expect(screen.getByTestId('source-about')).not.toContainElement(unverified)
  })

  // Een afgekeurde gronding laat de bron staan en haalt haar passages
  // eraf. De kaart moet dat sterker zeggen dan "niet vergeleken" — hier ís
  // vergeleken, en de woorden waren niet terug te vinden.
  it('says that a rejected source lost its passages, not merely that it was unchecked', async () => {
    view({ sources: [{ ...SOURCE, grounding: 'rejected', ref: { ...SOURCE.ref, quote: undefined, passages: undefined } }] })
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    const rejected = screen.getByTestId('source-rejected')
    expect(rejected).toHaveTextContent('could not be traced back to the paper')
    expect(screen.getByTestId('source-finding')).toContainElement(rejected)
    expect(screen.queryByTestId('source-unverified')).toBeNull()
    // En niet twee keer hetzelfde: de leesregel zou hier "de oorspronkelijke
    // passage staat er niet bij" melden, wat de regel hierboven al zegt én
    // uitlegt. Eén grijze regel, niet twee.
    expect(screen.queryByTestId('source-reading')).toBeNull()
  })

  it('keeps the reading note in Dutch too when the passages were rejected', async () => {
    view({ sources: [{ ...SOURCE, grounding: 'rejected', ref: { ...SOURCE.ref, quote: undefined, passages: undefined } }] }, [], 'nl')
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.getByTestId('source-rejected')).toHaveTextContent('waren niet in de paper terug te vinden')
    expect(screen.queryByTestId('source-reading')).toBeNull()
  })

  // Een afgekeurde gronding hoort haar passages kwijt te zijn, maar als de
  // server er toch een meestuurt blijft de leesregel staan: die wijst dan naar
  // een passage die er wél is, en dat is iets anders dan de afkeuring.
  it('keeps the reading note when a rejected source still carries a passage', async () => {
    view({ sources: [{ ...SOURCE, grounding: 'rejected' }] })
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.getByTestId('source-rejected')).toBeVisible()
    expect(screen.getByTestId('source-reading')).toHaveTextContent('read the original passage below to check it')
  })

  it('names a source that carries only a citation key, and keeps the passage label honest', async () => {
    view({ sources: [{ ...SOURCE, ref: { paper: 'lim2023', quote: 'They roost close to food.' } }] })
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.getByTestId('source-key')).toHaveTextContent('Source key: lim2023')
    expect(screen.queryByTestId('source-paper')).toBeNull()
    expect(screen.getByTestId('source-original')).toHaveTextContent('The original words')
    expect(screen.getByTestId('source-original')).not.toHaveTextContent('in the paper')
  })

  it('calls a real title a paper and does not repeat it as a key', async () => {
    view({ sources: [{ ...SOURCE, ref: { paper: 'Proximity to food sources', quote: 'They roost close to food.' } }] })
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.getByTestId('source-paper')).toHaveTextContent('From Proximity to food sources')
    expect(screen.queryByTestId('source-key')).toBeNull()
    expect(screen.getByTestId('source-original')).toHaveTextContent('The original words in the paper')
  })

  it('prints the doi once: not again when the reference already carries it', async () => {
    view({ sources: [{ ...SOURCE, ref: { citation: 'Lewald, J. (1987). Sound localisation. https://doi.org/10.1234/synthetic', doi: '10.1234/synthetic' } }] })
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.queryByTestId('source-doi')).toBeNull()
    expect(screen.getByTestId('source-citation')).toHaveTextContent('10.1234/synthetic')
  })

  it('warns about an incomplete bibliography in one sentence, not two', async () => {
    view({ sources: [{ ...SOURCE, ref: { ...SOURCE.ref, citation: 'Lewald, J. (1987). Sound localisation.', citationMissing: 'volume, issue' } }] })
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.getByTestId('source-bibliography-missing')).toHaveTextContent('Bibliographic details to check: volume, issue. This is not an assessment of the research quality.')
    expect(screen.queryByTestId('source-bibliography-note')).toBeNull()
  })

  it('opens the source in the knowledge graph and closes the drawer', async () => {
    const { services } = view()
    const focussed = vi.fn()
    services.shell.onOpenInGraph = focussed
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    await fireEvent.click(screen.getByTestId('source-graph-button'))
    await tick()
    expect(focussed).toHaveBeenCalledWith('destillaat:9f1c')
    expect(services.shell.tab).toBe('graph')
    expect(screen.queryByTestId('source-drawer')).toBeNull()
  })

  it('shows no graph button for a message from before the knowledge graph', async () => {
    view({ sources: [OLD_SOURCE], content: 'Pigeons nest on ledges [1].' })
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.getByTestId('source-drawer')).toBeInTheDocument()
    expect(screen.queryByTestId('source-graph-button')).toBeNull()
  })
})
