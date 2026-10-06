import { fireEvent, render, screen, within } from '@testing-library/svelte'
import { tick } from 'svelte'
import { describe, expect, it, vi } from 'vitest'
import { COMPARISON_QUESTION, comparisonSession, liveStream, OLD_SOURCE, pigeonRoute, SOURCE, serviceContext, testServices } from '../testing/chat'
import { jsonResponse, scriptedFetch, type Route } from '../testing/fetch'
import ChatView from './ChatView.svelte'
import { type ChatMessage, type SessionRef } from './chatStore.svelte'

// Historie en "nieuw gesprek" staan in de
// navigatieregel van de schil; hun gedrag staat in
// lib/shell/ConversationActions.test.ts.

function assistant(patch: Partial<ChatMessage> = {}): ChatMessage {
  return {
    key: 'a1',
    role: 'assistant',
    kind: 'text',
    content: 'Pigeons nest on ledges [1].',
    sources: [SOURCE],
    tools: [],
    partial: false,
    streaming: false,
    ...patch,
  }
}

// pigeonAnswer is het eigen blok van de duif binnen een facilitator-beurt.
function pigeonAnswer(patch: Partial<ChatMessage> = {}): ChatMessage {
  return {
    key: 'p1',
    role: 'assistant',
    kind: 'pigeon_answer',
    persona: 'individualist',
    content: 'Coo. I like ledges.',
    sources: [],
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

function view(sessions: SessionRef[] = [], extra: Route[] = []) {
  const scripted = scriptedFetch([
    pigeonRoute(),
    ...extra,
    { method: 'GET', match: '/chat/sessions', respond: () => jsonResponse({ sessions }) },
  ])
  const services = testServices(scripted.fetch)
  const rendered = render(ChatView, { context: serviceContext(services) })
  return { ...rendered, services, chat: services.chat, locale: services.locale, calls: scripted.calls }
}

// De kennispartner heeft een kennisbank; deze route is haar stand.
function statusRoute(body: unknown): Route {
  return { method: 'GET', match: '/knowledge/status', respond: () => jsonResponse(body) }
}

describe('ChatView', () => {
  it('loads the pigeons and the sessions once on mount', async () => {
    const { calls, chat } = view()
    await settle()
    expect(calls.filter((c) => c.url.includes('/duiven'))).toHaveLength(1)
    expect(calls.filter((c) => c.url.endsWith('/chat/sessions'))).toHaveLength(1)
    // Een tweede aanroep (tabwissel) herlaadt niets.
    await chat.loadPigeons()
    await chat.loadSessions()
    expect(calls.filter((c) => c.url.includes('/duiven'))).toHaveLength(1)
  })

  it('keeps only the chat pigeons selectable and leaves the choice to the shell', async () => {
    const { chat } = view()
    await settle()
    // De duif kiest men bovenaan de schil (PigeonSelector); de chat heeft geen
    // tweede zitstok meer die hetzelfde zou beweren.
    expect(chat.pigeons.map((p) => p.id)).toEqual(['biological', 'individualist'])
    expect(screen.queryByRole('group', { name: 'Choose a pigeon' })).toBeNull()
    expect(screen.queryByRole('button', { name: /^Talk to/ })).toBeNull()
  })

  it('starts a new conversation when another pigeon is picked', async () => {
    const { chat } = view()
    await settle()
    // Het lege gesprek toont de gekozen duif zelf, als decoratie:
    // haar naam staat al in de keuzeregel en wordt hier niet herhaald.
    const portrait = () => screen.getByTestId('chat-empty-portrait').querySelector('[data-pigeon]')
    expect(portrait()).toHaveAttribute('data-pigeon', 'biological')
    expect(portrait()).toHaveAttribute('aria-hidden', 'true')
    chat.sessionId = 's-1'
    chat.messages = [assistant()]
    await tick()
    chat.selectPigeon('individualist')
    await tick()
    expect(chat.selectedPigeonId).toBe('individualist')
    expect(chat.sessionId).toBe('')
    expect(screen.getByTestId('chat-empty')).toBeInTheDocument()
    expect(portrait()).toHaveAttribute('data-pigeon', 'individualist')
  })

  it('shows the thinking indicator and no empty bubble when sources arrive before the first text', async () => {
    const live = liveStream()
    const { chat } = view([], [{ method: 'POST', match: '/chat/turn', respond: () => live.response }])
    await settle()
    void chat.send('Where do pigeons nest?')
    await settle()
    live.push({ type: 'sources', sources: [SOURCE] })
    await settle()

    expect(screen.getByTestId('assistant-message')).toHaveAttribute('aria-busy', 'true')
    expect(screen.getByText('The pigeon is thinking…')).toBeInTheDocument()
    // Nog geen tekstvlak en nog geen bronkaarten: geen lege bubbel die flitst.
    expect(screen.queryByTestId('assistant-content')).toBeNull()
    expect(screen.queryByTestId('source-count')).toBeNull()

    live.push({ type: 'text', delta: 'Pigeons nest on ledges [1].' })
    live.push({ type: 'done', sessionId: 's-1', kbStatus: 'sources', sources: [SOURCE] })
    live.close()
    await settle()
    expect(screen.getByTestId('assistant-content')).toBeInTheDocument()
    expect(screen.getByTestId('source-count')).toHaveTextContent('1 source')
    expect(screen.getByTestId('assistant-message')).toHaveAttribute('aria-busy', 'false')
  })

  it('renders the citation as a superscript reference that opens the drawer on that source', async () => {
    // De lade zelf hangt in App.svelte; de chat vraagt hem alleen te openen.
    // Wat hij dan toont staat vast in lib/sources/SourceDrawer.test.ts.
    const { chat, services } = view()
    await settle()
    chat.messages = [assistant({ key: 'a1', kbStatus: 'sources' })]
    await tick()
    const mark = screen.getByRole('button', { name: 'Source 1' })
    expect(mark).toHaveAttribute('aria-haspopup', 'dialog')
    expect(mark).toHaveAttribute('aria-expanded', 'false')
    // Een superscript direct achter het woord, niet een chip op een eigen regel.
    expect(mark.closest('sup')).not.toBeNull()
    await fireEvent.click(mark)
    await tick()
    expect(services.sources.showsSource('a1', 1)).toBe(true)
    expect(services.sources.current?.id).toBe('destillaat:9f1c')
    expect(mark).toHaveAttribute('aria-expanded', 'true')
  })

  it('shows the knowledge gap badge and the interrupted marker from history', async () => {
    const { chat } = view()
    await settle()
    chat.messages = [
      assistant({ key: 'g', content: 'I have no source for that.', sources: [], kbStatus: 'gap', partial: true }),
    ]
    await tick()
    expect(screen.getByTestId('gap-badge')).toHaveTextContent('No source found for this question.')
    expect(screen.getByTestId('partial-marker')).toHaveTextContent('(interrupted)')
  })

  // Na een kennisgat zoekt de beurt nog één keer breder, op een
  // lagere lat. Wat dat oplevert staat er wél, maar het kwam niet uit de
  // gewone zoekopdracht — en dan moet het scherm dat zeggen, anders leest "1
  // bron" als volledige dekking.
  it('marks related sources by where they came from', async () => {
    const { chat } = view()
    await settle()
    chat.messages = [
      assistant({ key: 'r', content: 'I cannot determine that. Related is [1].', kbStatus: 'sources', related: true }),
    ]
    await tick()
    expect(screen.getByTestId('related-badge')).toHaveTextContent('These sources border on your question.')
    expect(screen.getByTestId('source-count')).toHaveTextContent('1 related source')
  })

  // De brede zoekactie draait op 0,65 en de gewone bronnendrempel op 0,73, dus
  // een vraag die daar net onder viel krijgt hier bronnen die haar wél dragen:
  // de duif antwoordt dan inhoudelijk mét citaties. De
  // markering mag dat antwoord niet tegenspreken — zij zegt waar de bronnen
  // vandaan komen, niet dat er geen antwoord is.
  it('does not claim there is no source when the pigeon answers from the related ones', async () => {
    const { chat } = view()
    await settle()
    chat.messages = [
      assistant({
        key: 'rb',
        content: 'Feeding bans shift the problem rather than solving it [1].',
        kbStatus: 'sources',
        related: true,
      }),
    ]
    await tick()
    expect(screen.getByTestId('related-badge').textContent).not.toMatch(/no source/i)
    expect(screen.queryByTestId('gap-badge')).toBeNull()
  })

  // Zonder die markering blijft de bronchip zeggen wat hij altijd zei.
  it('keeps the plain source chip when the sources do cover the question', async () => {
    const { chat } = view()
    await settle()
    chat.messages = [assistant({ key: 'c', kbStatus: 'sources' })]
    await tick()
    expect(screen.queryByTestId('related-badge')).toBeNull()
    expect(screen.getByTestId('source-count')).toHaveTextContent('1 source')
  })

  // De begeleider bezit geen bronnen, dus hij mag ook niet melden dát er geen
  // bron was of dat de kennisbank plat lag: dat is een bronclaim van een
  // spreker die er geen mag doen. De bronchip hield die grens al aan;
  // de gat- en onbereikbaar-badges niet.
  it('keeps the gap and unavailable badges off the facilitator', async () => {
    const { chat } = view()
    await settle()
    for (const kbStatus of ['gap', 'unavailable'] as const) {
      chat.messages = [
        assistant({ key: 'f', persona: 'facilitator', content: 'I put the question to the pigeon.', sources: [], kbStatus }),
      ]
      await tick()
      expect(screen.queryByTestId('gap-badge')).toBeNull()
      expect(screen.queryByText('Sources are temporarily unavailable. Try again later.')).toBeNull()
    }
  })

  it('shows the budget cap as an alert banner and keeps the input usable', async () => {
    const { chat } = view()
    await settle()
    chat.error = {
      status: 402,
      reason: 'llm_budget_exceeded',
      message: { en: 'The pigeons have reached their budget.', nl: 'De duiven hebben hun budget bereikt.' },
    }
    await tick()
    expect(screen.getByRole('alert')).toHaveTextContent('The pigeons have reached their budget.')
    expect(screen.getByTestId('chat-input')).not.toBeDisabled()
  })

  it('renders a server error banner without the raw body', async () => {
    const { chat } = view([], [{ method: 'POST', match: '/chat/turn', respond: () => new Response('<html>502</html>', { status: 502 }) }])
    await settle()
    await chat.send('Hello')
    await tick()
    const banner = screen.getByTestId('chat-banner')
    expect(banner).toHaveTextContent('Something went wrong. Please try again.')
    expect(banner.textContent).not.toContain('502 Bad Gateway')
    expect(screen.getByTestId('chat-input')).not.toBeDisabled()
  })

  it('colours the character counter at the warning and the hard limit', async () => {
    const { chat } = view()
    await settle()
    const composer = screen.getByTestId('chat-composer')
    expect(composer).toHaveAttribute('data-counter-tone', 'muted')
    chat.draft = 'a'.repeat(3900)
    await tick()
    expect(composer).toHaveAttribute('data-counter-tone', 'warning')
    expect(screen.getByTestId('chat-char-counter')).toHaveTextContent('3900/4096')
    chat.draft = 'a'.repeat(4096)
    await tick()
    expect(composer).toHaveAttribute('data-counter-tone', 'destructive')
  })

  it('stops a running stream with the stop button and keeps the received text', async () => {
    const live = liveStream()
    live.response.headers.set('X-Duif-Session-ID', 'confirmed-partial')
    const { chat } = view([], [{ method: 'POST', match: '/chat/turn', respond: () => live.response }])
    await settle()
    void chat.send('Where do pigeons nest?')
    await settle()
    live.push({ type: 'text', delta: 'Half a thought' })
    await settle()
    await fireEvent.click(screen.getByTestId('chat-stop-button'))
    await settle()
    expect(chat.streaming).toBe(false)
    expect(screen.getByTestId('partial-marker')).toBeInTheDocument()
    expect(screen.getByTestId('assistant-content')).toHaveTextContent('Half a thought')
    expect(chat.error).toBeNull()
  })

  it('marks the thread busy while the history loads and holds the empty state back', async () => {
    const { chat } = view()
    await settle()
    expect(screen.getByTestId('chat-thread')).toHaveAttribute('aria-busy', 'false')
    expect(screen.getByTestId('chat-empty')).toBeInTheDocument()

    // Tussen twee gesprekken staan de berichten even leeg terwijl de
    // geschiedenis onderweg is; "stel een vraag" zou dan onwaar zijn en
    // flitsen.
    chat.loadingHistory = true
    await tick()
    expect(screen.getByTestId('chat-thread')).toHaveAttribute('aria-busy', 'true')
    expect(screen.queryByTestId('chat-empty')).toBeNull()

    chat.loadingHistory = false
    await tick()
    expect(screen.getByTestId('chat-thread')).toHaveAttribute('aria-busy', 'false')
    expect(screen.getByTestId('chat-empty')).toBeInTheDocument()
  })

  it('brings the newest message into view after the reader sends one', async () => {
    const original = Element.prototype.scrollIntoView
    const scrolled = vi.fn()
    Element.prototype.scrollIntoView = scrolled
    try {
      const { chat } = view()
      await settle()
      scrolled.mockClear()
      chat.messages = [
        {
          key: 'u1',
          role: 'user',
          kind: 'text',
          content: 'Where do pigeons nest?',
          sources: [],
          tools: [],
          partial: false,
          streaming: false,
        },
      ]
      await tick()
      expect(scrolled).toHaveBeenCalled()
    } finally {
      Element.prototype.scrollIntoView = original
    }
  })
  it('reads the knowledge status for a pigeon with a knowledge base and hangs the banner', async () => {
    const { calls } = view([], [statusRoute({ state: 'importing', counts: { destillaat: 1200 } })])
    await settle()
    expect(calls.filter((c) => c.url.includes('/knowledge/status'))).toHaveLength(1)
    expect(screen.getByTestId('knowledge-banner')).toHaveTextContent('1200 sources so far')
  })

  it('leaves the banner away for a pigeon without a knowledge base', async () => {
    const { services } = view([], [statusRoute({ state: 'importing' })])
    await settle()
    expect(screen.getByTestId('knowledge-banner')).toBeInTheDocument()
    services.chat.selectPigeon('individualist')
    await settle()
    expect(screen.queryByTestId('knowledge-banner')).toBeNull()
  })

  it('shows one quiet source line under the answer instead of a list of cards', async () => {
    const { chat, services } = view()
    await settle()
    chat.messages = [assistant({ key: 'a1', kbStatus: 'sources' })]
    await tick()
    const line = screen.getByTestId('source-count')
    expect(line).toHaveTextContent('1 source')
    // Geen kaart, geen citaat, geen bronvermelding onder het antwoord zelf.
    expect(screen.queryByTestId('source-quote')).toBeNull()
    expect(screen.queryByTestId('source-citation')).toBeNull()
    await fireEvent.click(line)
    await tick()
    expect(services.sources.showsSource('a1', 1)).toBe(true)
  })

  // ChatInput groeit op zijn eigen input-event; een concept dat via de
  // binding binnenkomt vuurt er geen, en dan blijft het veld één regel hoog met
  // een scrollbalk. De composer moet dat event dus zelf geven — precies één
  // keer, en niet bovenop het event dat de browser bij typen al stuurt.
  it('hands the input field an input event for a draft that did not come from the keyboard', async () => {
    const { chat } = view()
    await settle()
    const field = screen.getByTestId('chat-input') as HTMLTextAreaElement
    const seen: string[] = []
    field.addEventListener('input', () => seen.push(field.value))

    chat.draft = 'A case from a starter card that runs over several lines.'
    await tick()
    expect(seen).toEqual(['A case from a starter card that runs over several lines.'])

    seen.length = 0
    await fireEvent.input(field, { target: { value: 'Typed by hand' } })
    await tick()
    // Eén event: het onze mag er niet nog eens overheen.
    expect(seen).toEqual(['Typed by hand'])
    expect(chat.draft).toBe('Typed by hand')
  })

  // De vergelijkpil hangt niet aan de vervolgvragen: juist bij een afgebroken
  // antwoord of onbereikbare bronnen zwijgt `followups`, en dan moet de weg
  // naar de andere duif nog steeds openstaan — precies één keer.
  it('keeps exactly one way to the other pigeon when the answer broke off or the sources failed', async () => {
    const { chat } = view()
    await settle()
    chat.sessionId = 's-1'
    const question: ChatMessage = { key: 'q1', role: 'user', kind: 'text', content: 'Where do pigeons nest?', sources: [], tools: [], partial: false, streaming: false }

    for (const broken of [
      { partial: true },
      { kbStatus: 'unavailable' as const },
      { stopReason: 'truncated' as const },
    ]) {
      chat.messages = [question, assistant({ key: 'a1', sources: [], kbStatus: 'sources', ...broken })]
      await tick()
      // Geen vervolgvraag: dit is exact het geval waarin `followups` niets geeft.
      expect(screen.queryByRole('button', { name: /Look more closely at the evidence/ })).toBeNull()
      expect(screen.getAllByRole('button', { name: /Ask the other pigeon/ })).toHaveLength(1)
    }

    // En zolang het antwoord binnenkomt staat de pil er niet: vergelijken van
    // een half antwoord is geen vergelijking.
    chat.messages = [question, assistant({ key: 'a1', sources: [], streaming: true })]
    chat.streaming = true
    await tick()
    expect(screen.queryByRole('button', { name: /Ask the other pigeon/ })).toBeNull()
  })

  // Vervolgvragen en "Bekijk voorbeelden" vervallen zodra het gesprek
  // loopt: na het eerste antwoord bedenkt de lezer zelf wat hij vraagt. De
  // openers blijven op het lege scherm staan, en de chips bij het antwoord dat
  // de bronnen heeft blijven waar ze waren.
  it('offers no follow-up questions or examples once an answer is on screen', async () => {
    const { chat } = view()
    await settle()
    chat.selectPigeon('individualist')
    await settle()
    // Leeg gesprek: de openers staan er, want zij brengen het op gang.
    expect(screen.getByTestId('chat-empty')).toBeInTheDocument()

    chat.sessionId = 's-1'
    chat.messages = [
      { key: 'q1', role: 'user', kind: 'text', content: 'Where do pigeons nest?', sources: [], tools: [], partial: false, streaming: false },
      pigeonAnswer({ key: 'p1', content: 'On ledges [1].', sources: [SOURCE], kbStatus: 'sources' }),
      assistant({ key: 'f1', persona: 'facilitator', content: 'That is her reading of it.', sources: [] }),
    ]
    await tick()

    expect(screen.queryByTestId('chat-empty')).toBeNull()
    expect(screen.queryByRole('group', { name: 'Explore further' })).toBeNull()
    expect(screen.queryByRole('button', { name: /Look more closely at the evidence/ })).toBeNull()
    expect(screen.queryByRole('button', { name: /Explore examples/ })).toBeNull()

    const answers = screen.getAllByTestId('assistant-message')
    expect(answers.map((el) => el.dataset.persona)).toEqual(['individualist', 'facilitator'])
    expect(within(answers[0]).getByTestId('source-count')).toBeInTheDocument()
    for (const answer of answers) {
      expect(within(answer).queryByRole('button', { name: /Ask the other pigeon/ })).toBeNull()
    }
  })

  // Het gesprek heeft geen zijbanen. Wat daar ooit stond heeft een
  // eigen plek gekregen: de weg naar de andere duif hangt op de hoek van de
  // vraag die meegaat, de herkomstregel staat compact boven het gesprek, en
  // "Bewerk de laatste vraag" staat bij de foutmelding.
  it('puts the way to the other pigeon on every own question, with the question in its name', async () => {
    const { chat, locale, calls } = view([], [
      { method: 'GET', match: '/messages', respond: () => jsonResponse({ messages: [
        { id: 'server-q1', role: 'user', content: 'Where do pigeons nest?' },
        { id: 'server-q2', role: 'user', content: 'And what if those ledges disappear?' },
      ] }) },
      { method: 'POST', match: '/chat/comparisons', respond: () => jsonResponse({ session: { id: 'other', pigeonId: 'individualist', lang: 'en', createdAt: '2026-09-22', updatedAt: '2026-09-22' } }) },
    ])
    await settle()
    chat.sessionId = 's-1'
    chat.messages = [
      { key: 'q1', role: 'user', kind: 'text', content: 'Where do pigeons nest?', sources: [], tools: [], partial: false, streaming: false },
      assistant({ key: 'a1', sources: [SOURCE], kbStatus: 'sources' }),
      { key: 'q2', role: 'user', kind: 'text', content: 'And what if those ledges disappear?', sources: [], tools: [], partial: false, streaming: false },
      assistant({ key: 'a2', content: 'Then we move to what shelter is left.', sources: [] }),
    ]
    await tick()

    // Geen banen meer, op geen enkele plek in de boom.
    expect(document.body.querySelectorAll('.chat-lane, .chat-stage')).toHaveLength(0)

    // Eén knop per eigen vraag, elk met haar eigen vraag in de naam.
    const buttons = screen.getAllByTestId('ask-other-pigeon')
    expect(buttons).toHaveLength(2)
    expect(buttons[0]).toHaveAccessibleName('Ask the other pigeon “Where do pigeons nest?”')
    expect(buttons[1]).toHaveAccessibleName('Ask the other pigeon “And what if those ledges disappear?”')
    // En hij hangt bij de vraag, niet bij het antwoord.
    for (const answer of screen.getAllByTestId('assistant-message')) {
      expect(within(answer).queryByTestId('ask-other-pigeon')).toBeNull()
    }

    // De oudere vraag werkt net zo goed als de laatste: de eerste knop stuurt
    // de eerste vraag door en niet de laatste beurt.
    await fireEvent.click(buttons[0])
    await settle()
    const posted = calls.find((call) => call.url.includes('/chat/comparisons'))
    expect(posted?.body).toMatchObject({ questionId: 'server-q1', sourceSessionId: 's-1' })

    locale.choose('nl')
    await tick()
    expect(screen.getAllByTestId('ask-other-pigeon')[1]).toHaveAccessibleName('Vraag “And what if those ledges disappear?” ook aan de andere duif')
  })

  it('keeps the origin line above the conversation and the retry at the error', async () => {
    const { chat, services } = view([comparisonSession()])
    // De herkomstregel noemt de duif bij naam; die namen komen uit de schil.
    await services.roster.load()
    await settle()
    chat.selectedPigeonId = 'individualist'
    chat.sessionId = 'target'
    chat.messages = [
      { key: 'q1', role: 'user', kind: 'text', content: 'Where do pigeons nest?', sources: [], tools: [], partial: false, streaming: false },
      assistant({ key: 'a1', sources: [SOURCE], kbStatus: 'sources' }),
    ]
    await tick()

    const thread = screen.getByTestId('chat-thread')
    const line = screen.getByText('Continued from your conversation with Knowledge Partner').closest('p')
    const back = screen.getByRole('button', { name: 'Back to that conversation' })
    // De regel staat boven het gesprek, buiten de berichten, met de ene weg
    // terug erin.
    expect(line).not.toBeNull()
    expect(thread.contains(line)).toBe(false)
    expect(line?.contains(back)).toBe(true)
    expect(line!.compareDocumentPosition(thread) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

    // Zonder fout is er niets te herstellen.
    expect(screen.queryByRole('button', { name: 'Edit the last question' })).toBeNull()
    chat.error = { status: 502 }
    await tick()
    const retry = screen.getByRole('button', { name: 'Edit the last question' })
    // En met een fout staat de knop in hetzelfde blok als de melding zelf.
    expect(within(screen.getByTestId('chat-error')).getByTestId('chat-banner')).toBeInTheDocument()
    expect(screen.getByTestId('chat-error').contains(retry)).toBe(true)
    await fireEvent.click(retry)
    await tick()
    expect(chat.draft).toBe('Where do pigeons nest?')
  })

  it('gives the facilitator the sans and the pigeon the serif, with a header per speaker change', async () => {
    const { chat, services } = view()
    // De naam boven een antwoord komt uit de naamlijst van de schil; die laadt
    // App.svelte bij het mounten.
    await services.roster.load()
    await settle()
    chat.messages = [
      pigeonAnswer(),
      pigeonAnswer({ key: 'p2', content: 'And I said so twice.' }),
      assistant({ key: 'f1', persona: 'facilitator', content: 'She would rather sit high up.', sources: [] }),
      assistant({ key: 'old', content: 'An answer from before this round.', sources: [] }),
    ]
    await tick()
    // Eén renderer, vier rijen: de stem hangt aan de persona, niet aan de soort.
    const [first, second, facilitator, older] = screen.getAllByTestId('assistant-content')
    expect(first).toHaveClass('duif-voice')
    expect(second).toHaveClass('duif-voice')
    expect(facilitator).not.toHaveClass('duif-voice')
    expect(older).toHaveClass('duif-voice')
    expect(screen.getAllByTestId('assistant-message').map((el) => el.dataset.persona)).toEqual([
      'individualist',
      'individualist',
      'facilitator',
      '',
    ])
    // Twee duif-blokken achter elkaar krijgen samen één naam boven zich; de
    // facilitator en de oude duif-rij zijn allebei een sprekerwissel. De
    // zitstok draagt dezelfde namen, dus we kijken alleen in het gesprek.
    const thread = within(screen.getByTestId('chat-thread'))
    expect(thread.getAllByText('Stakeholder')).toHaveLength(1)
    expect(thread.getAllByText('Facilitator')).toHaveLength(1)
    expect(thread.getAllByText('Earlier pigeon (identity unavailable)')).toHaveLength(1)
  })

  it('keeps tool execution internal and makes guidance subordinate to the pigeon', async () => {
    const live = liveStream()
    const { chat } = view([], [{ method: 'POST', match: '/chat/turn', respond: () => live.response }])
    await settle()
    chat.selectPigeon('individualist')
    void chat.send('Where would you nest?')
    await settle()
    live.push({ type: 'tool', id: 't1', name: 'ask_pigeon' })
    await settle()
    expect(screen.queryByTestId('tool-chips')).toBeNull()

    live.push({ type: 'artifact', id: 'p1', kind: 'pigeon_answer', persona: 'individualist' })
    live.push({ type: 'artifact_delta', id: 'p1', delta: 'Coo.' })
    await settle()
    // De duif streamt zichtbaar; de denkbubbel van de facilitator zwijgt.
    expect(screen.getByTestId('assistant-content')).toHaveTextContent('Coo.')
    expect(screen.queryByText('The pigeon is thinking…')).toBeNull()

    live.push({ type: 'artifact', id: 'p1', done: true })
    live.push({ type: 'tool_result', id: 't1', name: 'ask_pigeon' })
    live.push({ type: 'text', delta: 'She likes ledges.' })
    live.push({ type: 'done', sessionId: 's-1', kbStatus: 'persona_only', stopReason: 'natural' })
    live.close()
    await settle()
    expect(screen.queryByTestId('tool-chip')).toBeNull()
    const [pigeon, facilitator] = screen.getAllByTestId('assistant-content')
    expect(pigeon).toHaveTextContent('Coo.')
    expect(facilitator).toHaveTextContent('She likes ledges.')
    expect(screen.queryByTestId('stop-badge')).toBeNull()
  })

  it('names the facilitator in her own thinking indicator', async () => {
    const live = liveStream()
    const { chat } = view([], [{ method: 'POST', match: '/chat/turn', respond: () => live.response }])
    await settle()
    chat.selectPigeon('individualist')
    void chat.send('Where would you nest?')
    await settle()
    // De facilitator wacht op de duif; "de duif denkt na" zou hier de
    // verkeerde stem zijn.
    expect(screen.getByText('The facilitator is thinking…')).toBeInTheDocument()
    expect(screen.queryByText('The pigeon is thinking…')).toBeNull()
    live.push({ type: 'text', delta: 'She likes ledges.' })
    live.push({ type: 'done', sessionId: 's-1', stopReason: 'natural' })
    live.close()
    await settle()
    expect(screen.queryByText('The facilitator is thinking…')).toBeNull()
  })

  it('keeps internal stop reasons off the screen and states the ones a reader needs', async () => {
    const { chat } = view()
    await settle()
    // Te veel stappen is machinetaal over onze motor: dat verandert niets aan
    // wat de duif zei en staat er dus niet.
    chat.messages = [
      assistant({
        key: 'f1',
        persona: 'facilitator',
        content: 'I could not reach her.',
        sources: [],
        tools: [{ id: 't1', name: 'ask_pigeon', done: true, error: 'llm_budget_exceeded' }],
        stopReason: 'max_iter',
      }),
    ]
    await tick()
    expect(screen.queryByTestId('tool-chip')).toBeNull()
    expect(screen.queryByTestId('stop-badge')).toBeNull()
    // Een geweigerd of afgekapt antwoord moet de lezer wél weten, in gewone taal.
    chat.messages = [assistant({ key: 'f2', persona: 'facilitator', content: 'No answer.', sources: [], stopReason: 'refusal' })]
    await tick()
    expect(screen.getByTestId('stop-badge')).toHaveTextContent('The pigeon did not answer this question.')
    chat.messages = [assistant({ key: 'f3', persona: 'facilitator', content: 'Half an answer.', sources: [], stopReason: 'truncated' })]
    await tick()
    expect(screen.getByTestId('stop-badge')).toHaveTextContent('This answer ran into the length limit and stops halfway.')
  })

  // Een antwoord dat de provider op de tokengrens afsloot, stopt midden in
  // een zin. Onder dát antwoord staat één korte regel die zegt waarom, in
  // beide talen — en nergens anders.
  it('states the length limit under the pigeon answer that hit it, in English and Dutch', async () => {
    const { chat, locale } = view()
    await settle()
    chat.messages = [
      pigeonAnswer({
        key: 'p-trunc',
        content: 'In the setting studied, higher numbers of humans',
        stopReason: 'truncated',
      }),
    ]
    await tick()
    // De tekst zelf blijft staan zoals hij binnenkwam.
    expect(screen.getByTestId('assistant-content')).toHaveTextContent('In the setting studied, higher numbers of humans')
    expect(screen.getByTestId('stop-badge')).toHaveTextContent('This answer ran into the length limit and stops halfway.')
    // Geen onderbroken-markering: de stroom liep netjes af.
    expect(screen.queryByTestId('partial-marker')).toBeNull()
    locale.lang = 'nl'
    await tick()
    expect(screen.getByTestId('stop-badge')).toHaveTextContent('Dit antwoord liep tegen de lengtegrens aan en stopt halverwege.')
  })

  // Sinds de duif-stream ook een content_filter-afsluiting meldt, kan er bij
  // deze reden wél tekst staan. De melding moet dan over het stoppen gaan en
  // niet beweren dat er niets te zien is.
  it('shows both the text and the filter notice when the model filter stopped a half answer', async () => {
    const { chat, locale } = view()
    await settle()
    chat.messages = [
      pigeonAnswer({
        key: 'p-filtered',
        content: 'That pavement is where I feed, and the people who',
        stopReason: 'filtered',
      }),
    ]
    await tick()
    expect(screen.getByTestId('assistant-content')).toHaveTextContent('That pavement is where I feed, and the people who')
    expect(screen.getByTestId('stop-badge')).toHaveTextContent('The model’s own filter stopped this answer.')
    expect(screen.queryByTestId('partial-marker')).toBeNull()
    locale.lang = 'nl'
    await tick()
    expect(screen.getByTestId('assistant-content')).toHaveTextContent('That pavement is where I feed, and the people who')
    expect(screen.getByTestId('stop-badge')).toHaveTextContent('Het filter van het model heeft dit antwoord gestopt.')
  })

  it('says nothing under a pigeon answer that ended normally', async () => {
    const { chat, locale } = view()
    await settle()
    chat.messages = [pigeonAnswer({ key: 'p-ok', content: 'That pavement is mine.' })]
    await tick()
    expect(screen.queryByTestId('stop-badge')).toBeNull()
    locale.lang = 'nl'
    await tick()
    expect(screen.queryByTestId('stop-badge')).toBeNull()
  })

  it('announces one sentence per finished block in a single polite status node', async () => {
    const live = liveStream()
    const { chat } = view([], [{ method: 'POST', match: '/chat/turn', respond: () => live.response }])
    await settle()
    const status = screen.getByTestId('chat-status')
    expect(status).toHaveAttribute('aria-live', 'polite')
    expect(status).toHaveTextContent('')
    chat.selectPigeon('individualist')
    void chat.send('Where would you nest?')
    await settle()
    live.push({ type: 'artifact', id: 'p1', kind: 'pigeon_answer', persona: 'individualist' })
    live.push({ type: 'artifact_delta', id: 'p1', delta: 'Coo.' })
    await settle()
    // Een streamend blok meldt niets: dat zou de hele zin per stukje voorlezen.
    expect(status).toHaveTextContent('')
    live.push({ type: 'artifact', id: 'p1', done: true })
    await settle()
    expect(status).toHaveTextContent('The pigeon has answered.')
    expect(screen.getAllByTestId('chat-status')).toHaveLength(1)

    live.push({ type: 'text', delta: 'She likes ledges.' })
    live.push({ type: 'done', sessionId: 's-1', kbStatus: 'persona_only', stopReason: 'natural' })
    live.close()
    await settle()
    expect(status).toHaveTextContent('The facilitator has answered.')
  })

  // De melding hoort bij de beurt die hem uitzond. Bleef hij staan,
  // dan zei de statusregel "heeft geantwoord" terwijl de volgende vraag nog
  // liep, of terwijl die net was afgebroken zonder antwoord.
  it('withdraws the answered announcement when the next turn starts and when a turn fails', async () => {
    const streams = [liveStream(), liveStream(), liveStream()]
    for (const s of streams) s.response.headers.set('X-Duif-Session-ID', 's-187')
    const queue = [...streams]
    const { chat } = view([], [{ method: 'POST', match: '/chat/turn', respond: () => queue.shift()!.response }])
    await settle()
    const status = screen.getByTestId('chat-status')
    chat.selectPigeon('individualist')
    const answer = (live: ReturnType<typeof liveStream>) => {
      live.push({ type: 'artifact', id: 'p1', kind: 'pigeon_answer', persona: 'individualist' })
      live.push({ type: 'artifact_delta', id: 'p1', delta: 'Coo.' })
      live.push({ type: 'artifact', id: 'p1', done: true })
    }

    const [first, second, third] = streams
    void chat.send('Where would you nest?')
    await settle()
    answer(first)
    first.push({ type: 'done', sessionId: 's-187', stopReason: 'natural' })
    first.close()
    await settle()
    expect(status).toHaveTextContent('The pigeon has answered.')

    // Een tweede beurt: zolang zij loopt is er niets af.
    void chat.send('And in winter?')
    await settle()
    second.push({ type: 'phase', phase: 'facilitator' })
    await settle()
    expect(chat.streaming).toBe(true)
    expect(status).toHaveTextContent('')
    answer(second)
    second.push({ type: 'done', sessionId: 's-187', stopReason: 'natural' })
    second.close()
    await settle()
    expect(status).toHaveTextContent('The pigeon has answered.')

    // Een beurt die op de tijdgrens stopt, ook al was haar blok al gemeld:
    // daarna zegt de regel niet meer dat er geantwoord is.
    void chat.send('And at night?')
    await settle()
    answer(third)
    await settle()
    expect(status).toHaveTextContent('The pigeon has answered.')
    third.push({ type: 'error', error: 'turn_timeout' })
    third.push({ type: 'done', sessionId: 's-187', stopReason: 'error' })
    third.close()
    await settle()
    expect(chat.error?.reason).toBe('turn_timeout')
    expect(status).toHaveTextContent('')
    expect(screen.getAllByTestId('chat-status')).toHaveLength(1)
  })

  it('never announces an answer after the reader stops the turn', async () => {
    const live = liveStream()
    live.response.headers.set('X-Duif-Session-ID', 's-stop')
    const { chat } = view([], [{ method: 'POST', match: '/chat/turn', respond: () => live.response }])
    await settle()
    chat.selectPigeon('individualist')
    void chat.send('Where would you nest?')
    await settle()
    live.push({ type: 'artifact', id: 'p1', kind: 'pigeon_answer', persona: 'individualist' })
    live.push({ type: 'artifact_delta', id: 'p1', delta: 'Coo.' })
    live.push({ type: 'artifact', id: 'p1', done: true })
    live.push({ type: 'text', delta: 'She likes' })
    await settle()
    expect(screen.getByTestId('chat-status')).toHaveTextContent('The pigeon has answered.')
    chat.abort()
    await settle()
    expect(screen.getByTestId('chat-status')).toHaveTextContent('')
  })

  // Een beurt die op de tijdgrens stopt voordat de facilitator iets
  // zei, laat geen lege kaart op zijn naam achter: alleen de vraag en de
  // foutbanner met de weg terug.
  it('leaves no empty facilitator card behind a turn that timed out before anyone spoke', async () => {
    const live = liveStream()
    live.response.headers.set('X-Duif-Session-ID', 's-185')
    const { chat, locale } = view([], [{ method: 'POST', match: '/chat/turn', respond: () => live.response }])
    await settle()
    locale.choose('nl')
    chat.selectPigeon('individualist')
    void chat.send('Wat vindt de duif van de Coolsingel?')
    await settle()
    live.push({ type: 'phase', phase: 'facilitator' })
    live.push({ type: 'tool', id: 't1', name: 'ask_pigeon' })
    await settle()
    live.push({ type: 'error', error: 'turn_timeout' })
    live.push({ type: 'done', sessionId: 's-185', stopReason: 'error' })
    live.close()
    await settle()

    expect(chat.messages.map((m) => m.role)).toEqual(['user'])
    expect(screen.queryByTestId('assistant-message')).toBeNull()
    expect(screen.queryByTestId('partial-marker')).toBeNull()
    expect(screen.getByRole('button', { name: 'Bewerk de laatste vraag' })).toBeInTheDocument()
    // De foutbanner draagt de uitleg; de stille regel eronder zou hetzelfde
    // nog eens zeggen.
    expect(screen.queryByTestId('chat-no-answer')).toBeNull()
  })

  // Het tabblad sloot terwijl de beurt liep. Het opnieuw geopende
  // gesprek zegt dat de beurt stopte en biedt dezelfde weg terug.
  it('says that a reopened conversation stopped before its last question was answered', async () => {
    const session: SessionRef = { id: 's-186', pigeonId: 'individualist', lang: 'nl', createdAt: '2026-10-05T10:00:00Z', updatedAt: '2026-10-05T10:00:00Z' }
    const history = [
      { id: 'u1', role: 'user', content: 'Waar nestel je?' },
      { id: 'a1', role: 'assistant', kind: 'pigeon_answer', persona: 'individualist', content: 'Op richels.' },
      { id: 'u2', role: 'user', content: 'En in de winter?' },
      // De kaderzin van de facilitator zoals de server hem bewaart als er
      // geen duif-antwoord achter kwam: als afgebroken rij (askPigeon).
      { id: 'f2', role: 'assistant', kind: 'text', persona: 'facilitator', content: 'Ik vraag het de Stakeholder.', partial: true },
    ]
    const { chat, locale } = view([session], [
      { method: 'GET', match: '/chat/sessions/s-186/messages', respond: () => jsonResponse({ messages: history }) },
    ])
    await settle()
    locale.choose('nl')
    await chat.openSession('s-186')
    await settle()

    const notice = screen.getByTestId('chat-no-answer')
    expect(notice).toHaveTextContent('Deze beurt is afgebroken voordat er een antwoord kwam.')
    await fireEvent.click(within(notice).getByRole('button', { name: 'Bewerk de laatste vraag' }))
    expect(chat.draft).toBe('En in de winter?')

    // Tijdens het overschakelen naar een vergelijking staat het gesprek even
    // zonder antwoord open; dat is geen afgebroken beurt.
    chat.loadingComparison = true
    await tick()
    expect(screen.queryByTestId('chat-no-answer')).toBeNull()
    chat.loadingComparison = false
    await tick()

    // Een gesprek dat wél af is, zegt niets.
    chat.messages = chat.messages.slice(0, 2)
    await tick()
    expect(screen.queryByTestId('chat-no-answer')).toBeNull()
    // Ook niet met een kaderzin vóór het antwoord: na een duif-antwoord
    // bewaart de server die als afgeronde rij.
    chat.messages = [
      ...chat.messages,
      { key: 'u4', role: 'user', kind: 'text', content: 'En in de zomer?', sources: [], tools: [], partial: false, streaming: false },
      { key: 'f4', role: 'assistant', kind: 'text', persona: 'facilitator', content: 'Ik vraag het haar.', sources: [], tools: [], partial: false, streaming: false },
      pigeonAnswer({ key: 'p4', content: 'Op het dak.' }),
    ]
    await tick()
    expect(screen.queryByTestId('chat-no-answer')).toBeNull()
    chat.messages = chat.messages.slice(0, 2)
    await tick()
    // En een vraag zonder enige rij erna evenmin stil: dat is dezelfde afkap.
    chat.messages = [...chat.messages, { key: 'u3', role: 'user', kind: 'text', content: 'En nu?', sources: [], tools: [], partial: false, streaming: false }]
    await tick()
    expect(screen.getByTestId('chat-no-answer')).toHaveTextContent('Deze beurt is afgebroken')
  })

  // Een weigering of het filter van het model zonder één woord is geen
  // afgebroken beurt: de badge zegt waarom er niets staat, en de regel eronder
  // zegt alleen dat er geen antwoord is afgerond.
  it('does not call a turn the model refused without text interrupted', async () => {
    const { chat, locale } = view()
    await settle()
    locale.choose('nl')
    chat.messages = [
      { key: 'u1', role: 'user', kind: 'text', content: 'Zeg iets kwetsends.', sources: [], tools: [], partial: false, streaming: false },
      assistant({ key: 'a1', content: '', sources: [], stopReason: 'refusal' }),
    ]
    await tick()
    const notice = screen.getByTestId('chat-no-answer')
    expect(notice).toHaveTextContent('Er is geen antwoord afgerond.')
    expect(notice).not.toHaveTextContent('afgebroken')
    expect(screen.getByTestId('stop-badge')).toBeInTheDocument()
  })

  it('reads a repeated comparison question as one handover line in both languages', async () => {
    const session = comparisonSession()
    const { chat, locale, services } = view([session])
    await services.roster.load()
    await settle()
    chat.selectedPigeonId = session.pigeonId
    chat.sessionId = session.id
    // Twee gekopieerde rijen (de vraag en het antwoord van de eerste duif),
    // dan dezelfde vraag opnieuw en het antwoord van de tweede duif.
    chat.messages = [
      { key: 'copied-question', role: 'user', kind: 'text', content: COMPARISON_QUESTION, sources: [], tools: [], partial: false, streaming: false },
      pigeonAnswer({ key: 'copied-answer', persona: 'biological', content: 'Ledges on the north side.' }),
      { key: 'repeated', role: 'user', kind: 'text', content: COMPARISON_QUESTION, sources: [], tools: [], partial: false, streaming: false },
      pigeonAnswer({ key: 'second-answer', content: 'Coo. I like ledges.' }),
    ]
    await tick()

    const thread = screen.getByTestId('chat-thread')
    expect(screen.getByTestId('repeated-question')).toHaveTextContent('The same question, now to Stakeholder')
    // De vraag staat nog één keer als bubbel: die van het brongesprek, met het
    // antwoord van de eerste duif eronder.
    expect(within(thread).getAllByText(COMPARISON_QUESTION)).toHaveLength(1)
    expect([...thread.querySelectorAll('[data-author]')].map((el) => el.getAttribute('data-author'))).toEqual(['biological', 'individualist'])

    locale.choose('nl')
    await tick()
    expect(screen.getByTestId('repeated-question')).toHaveTextContent('Dezelfde vraag, nu aan Stakeholder')

    // Een echte vervolgvraag houdt haar eigen bubbel; alleen de rij direct na
    // de kopie is de doorgegeven vraag.
    chat.messages = [...chat.messages, { key: 'follow-up', role: 'user', kind: 'text', content: COMPARISON_QUESTION, sources: [], tools: [], partial: false, streaming: false }]
    await tick()
    expect(screen.getAllByTestId('repeated-question')).toHaveLength(1)
    expect(within(thread).getAllByText(COMPARISON_QUESTION)).toHaveLength(2)
  })

  // De duifcall duurt onvoorspelbaar lang (2 s tot ruim een minuut voor
  // hetzelfde verzoek). Wat er niet mag gebeuren is dat het scherm intussen
  // stil blijft: dan leest een trage beurt als een beurt zonder antwoord.
  it('shows the pigeon waiting as soon as ask_pigeon starts, with the elapsed time once it takes long', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      const live = liveStream()
      const { chat } = view([], [{ method: 'POST', match: '/chat/turn', respond: () => live.response }])
      await settle()
      void chat.send('What does the pigeon think of the square?')
      await settle()

      // Het tool-frame is het startschot: de duif is gevraagd, haar blok komt
      // pas na het opzoeken in de kennisbank.
      live.push({ type: 'tool', id: 't1', name: 'ask_pigeon' })
      await settle()
      const waiting = screen.getByTestId('pigeon-waiting')
      expect(waiting).toHaveAttribute('aria-busy', 'true')
      expect(within(waiting).getByText('The pigeon is thinking…')).toBeInTheDocument()

      // Onder de drempel blijft de stand kaal: acht seconden wachten is gewoon
      // wachten.
      await vi.advanceTimersByTimeAsync(5_000)
      await settle()
      expect(screen.queryByTestId('answer-waiting')).toBeNull()

      // Boven de drempel telt een stille regel onder de bubbel mee. Hij staat
      // bewust náást de denkregel: die vloeit over bij elke wijziging, en een
      // tekst die elke seconde verandert zou dus elke seconde knipperen.
      await vi.advanceTimersByTimeAsync(18_000)
      await settle()
      expect(within(screen.getByTestId('pigeon-waiting')).getByTestId('answer-waiting')).toHaveTextContent('23 s')

      // Haar blok gaat open: de losse wachtbubbel verdwijnt, de teller loopt
      // door in het blok zelf — er staat immers nog geen woord.
      live.push({ type: 'artifact', id: 'a1', kind: 'pigeon_answer', persona: 'individualist' })
      await settle()
      expect(screen.queryByTestId('pigeon-waiting')).toBeNull()
      expect(screen.getByTestId('answer-waiting')).toHaveTextContent('23 s')
      expect(screen.getByText('The pigeon is thinking…')).toBeInTheDocument()

      // Het eerste woord beëindigt het wachten.
      live.push({ type: 'artifact_delta', id: 'a1', delta: 'Coo. ' })
      await settle()
      expect(screen.queryByTestId('answer-waiting')).toBeNull()
      expect(screen.queryByText(/The pigeon is thinking/)).toBeNull()
      expect(chat.pigeonWaitSince).toBe(0)

      live.push({ type: 'done', sessionId: 's-1' })
      live.close()
      await settle()
    } finally {
      vi.useRealTimers()
    }
  })

  // Een chatbeurt is vier tot vijf modelcalls achter elkaar. Zonder
  // stapregel ziet de lezer daar één "denkt na…" van een halve minuut van, en
  // weet hij niet of er nog iets komt.
  it('names every step of the turn, keeps the finished ones and clears them when the turn ends', async () => {
    const live = liveStream()
    const { chat, locale } = view([], [{ method: 'POST', match: '/chat/turn', respond: () => live.response }])
    await settle()
    chat.selectPigeon('individualist')
    void chat.send('What does the pigeon think of the square?')
    await settle()

    // De meldregel staat er al vóór de eerste stap. Een live-gebied dat samen
    // met zijn eerste inhoud in de pagina verschijnt, spreken schermlezers
    // meestal niet uit — dan zou juist de eerste stap ongehoord blijven.
    expect(screen.getByTestId('turn-step-status')).toHaveTextContent('')

    live.push({ type: 'phase', phase: 'facilitator' })
    await settle()
    const steps = () => screen.queryAllByTestId('turn-step').map((el) => el.textContent?.trim())
    expect(steps()).toEqual(['Working out the question…'])
    // De schermlezer hoort de stap die loopt, één regel voor de hele rij.
    expect(screen.getByTestId('turn-step-status')).toHaveTextContent('Now: Working out the question')

    // De stap die klaar is blijft staan, zonder beletselteken: zo is te zien
    // wat er al gebeurd is zonder dat de regel gaat schreeuwen.
    live.push({ type: 'tool', id: 't1', name: 'ask_pigeon' })
    live.push({ type: 'phase', phase: 'pigeon_grounding' })
    await settle()
    expect(steps()).toEqual(['Working out the question', 'Reading sources…'])
    expect(screen.getByTestId('turn-step-status')).toHaveTextContent('Now: Reading sources')
    // De stap staat op één plek. De wachtbubbel zei hem even mee, maar dan
    // stond dezelfde zin twee keer onder elkaar; zij houdt haar eigen algemene
    // denkregel en de teller.
    const waiting = within(screen.getByTestId('pigeon-waiting'))
    expect(waiting.queryByText('Reading sources…')).toBeNull()
    expect(waiting.getByText('The pigeon is thinking…')).toBeInTheDocument()

    // De brede zoekactie na een kennisgat is een bewuste keuze: hij kost
    // tijd, en dan hoort er te staan waaraan.
    live.push({ type: 'phase', phase: 'pigeon_related_search' })
    await settle()
    expect(steps()).toEqual(['Working out the question', 'Reading sources', 'Looking for related sources…'])

    locale.choose('nl')
    await tick()
    expect(steps()).toEqual(['Vraag formuleren', 'Bronnen lezen', 'Verwante bronnen zoeken…'])

    live.push({ type: 'phase', phase: 'pigeon' })
    live.push({ type: 'artifact', id: 'a1', kind: 'pigeon_answer', persona: 'individualist' })
    await settle()
    expect(steps().at(-1)).toBe('Antwoord schrijven…')

    live.push({ type: 'artifact_delta', id: 'a1', delta: 'Coo.' })
    live.push({ type: 'artifact', id: 'a1', done: true })
    live.push({ type: 'phase', phase: 'mediation' })
    await settle()
    expect(steps().at(-1)).toBe('Facilitator rondt af…')

    // Na de beurt verdwijnt de regel: wat overblijft is het antwoord. Chips
    // worden niet bewaard, dus na herladen zouden ze er toch niet meer staan.
    live.push({ type: 'text', delta: 'Zij zit liever hoog.' })
    live.push({ type: 'done', sessionId: 's-steps', kbStatus: 'gap', stopReason: 'natural' })
    live.close()
    await settle()
    expect(screen.queryByTestId('turn-steps')).toBeNull()
  })

  // De Stop-knop blijft tijdens het wachten bereikbaar: dat is de enige uitweg
  // uit een beurt die te lang duurt.
  it('keeps stop reachable while the pigeon is being awaited', async () => {
    const live = liveStream()
    const { chat } = view([], [{ method: 'POST', match: '/chat/turn', respond: () => live.response }])
    await settle()
    void chat.send('What does the pigeon think of the square?')
    await settle()
    live.push({ type: 'tool', id: 't1', name: 'ask_pigeon' })
    await settle()
    expect(screen.getByTestId('pigeon-waiting')).toBeInTheDocument()

    await fireEvent.click(screen.getByTestId('chat-stop-button'))
    await settle()
    expect(chat.streaming).toBe(false)
    // Geen tikkende teller onder een afgebroken beurt.
    expect(screen.queryByTestId('pigeon-waiting')).toBeNull()
    expect(chat.pigeonWaitSince).toBe(0)
  })

  it('opens a message from before the knowledge graph in the same drawer', async () => {
    const { chat, services } = view()
    await settle()
    chat.messages = [assistant({ key: 'a1', sources: [OLD_SOURCE], kbStatus: 'sources' })]
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Source 1' }))
    await tick()
    expect(services.sources.open).toBe(true)
    // Zonder id is er niets om in de kennisgraaf te openen; de lade toont die
    // knop dan niet (zie SourceDrawer.test.ts).
    expect(services.sources.current?.id).toBeUndefined()
  })
})
