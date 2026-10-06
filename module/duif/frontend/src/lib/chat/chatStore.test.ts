import { tick } from 'svelte'
import { describe, expect, it } from 'vitest'
import { UiLocale } from '../i18n.svelte'
import { liveStream, pigeonRoute } from '../testing/chat'
import { jsonResponse, scriptedFetch, sseOneChunk, sseResponse, type Route } from '../testing/fetch'
import { ChatStore, shortTime, type SessionRef } from './chatStore.svelte'
import { holdPartialCitation } from '../sources'

// SOURCE is de nieuwe wire-vorm: met `id` voor de kennisgraaf en een `ref`
// waarin alleen de stringvelden een betekenis hebben (`line` is er niet één).
const SOURCE = {
  number: 1,
  type: 'destillaat',
  id: 'destillaat:9f1c',
  text: 'Pigeons nest on ledges.',
  ref: { quote: 'They prefer a ledge.', paper: 'lewald1987', line: 3 },
}

// OLD_SOURCE is een rij in het oudere formaat: ander type, geen id, oud ref.
const OLD_SOURCE = { number: 1, type: 'standpunt', text: 'Pigeons nest on ledges.', ref: { file: 'x.md', doi: '10.5/y' } }

function build(routes: Route[]) {
  const scripted = scriptedFetch(routes)
  const locale = new UiLocale()
  const chat = new ChatStore({ fetch: scripted.fetch }, locale)
  return { chat, locale, calls: scripted.calls }
}

function turns(...responses: (() => Response)[]): Route {
  let i = 0
  return {
    method: 'POST',
    match: '/chat/turn',
    respond: () => {
      const make = responses[Math.min(i, responses.length - 1)]
      i += 1
      return make()
    },
  }
}

async function settle(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
  await tick()
}

async function withPigeon(routes: Route[]) {
  const ctx = build([pigeonRoute(), ...routes])
  await ctx.chat.loadPigeons()
  return ctx
}

// withAgentPigeon kiest de duif die op de agent-motor draait: dan voert de
// facilitator het woord. De pipeline-duif (biologisch) is de default van
// withPigeon.
async function withAgentPigeon(routes: Route[]) {
  const ctx = await withPigeon(routes)
  ctx.chat.selectPigeon('individualist')
  return ctx
}

describe('holdPartialCitation', () => {
  it('holds back an unterminated citation while streaming', () => {
    expect(holdPartialCitation('Pigeons nest on ledges [1')).toBe('Pigeons nest on ledges')
    expect(holdPartialCitation('Pigeons nest on ledges [')).toBe('Pigeons nest on ledges')
    expect(holdPartialCitation('Pigeons nest [1, 2')).toBe('Pigeons nest')
  })

  it('leaves a complete citation and ordinary brackets alone', () => {
    expect(holdPartialCitation('Pigeons nest on ledges [1].')).toBe('Pigeons nest on ledges [1].')
    expect(holdPartialCitation('A [note] about pigeons')).toBe('A [note] about pigeons')
  })
})

describe('shortTime', () => {
  it('renders a relative time in the ui language', () => {
    const now = Date.parse('2026-09-02T12:00:00Z')
    expect(shortTime('2026-09-02T11:55:00Z', 'en', now)).toMatch(/5/)
    expect(shortTime('2026-09-02T11:55:00Z', 'nl', now)).toMatch(/5/)
    expect(shortTime('2026-07-01T11:55:00Z', 'en', now)).toMatch(/[A-Za-z]/)
  })

  it('returns an empty string for an unusable timestamp', () => {
    expect(shortTime('', 'en')).toBe('')
  })
})

describe('ChatStore', () => {
  it('loads only the pigeons that can chat and selects the first one', async () => {
    const { chat } = build([pigeonRoute()])
    await chat.loadPigeons()
    expect(chat.pigeons.map((p) => p.id)).toEqual(['biological', 'individualist'])
    expect(chat.selectedPigeonId).toBe('biological')
  })

  it('does not refetch the pigeons once they are loaded', async () => {
    const { chat, calls } = build([pigeonRoute()])
    await chat.loadPigeons()
    await chat.loadPigeons()
    expect(calls.filter((c) => c.url.includes('/duiven'))).toHaveLength(1)
  })

  it('reports an unauthorized pigeon list with the server message', async () => {
    const { chat } = build([
      {
        method: 'GET',
        match: '/duiven',
        respond: () =>
          jsonResponse({ error: 'unauthorized', message: { en: 'Login required.', nl: 'Inloggen vereist.' } }, 401),
      },
    ])
    await chat.loadPigeons()
    expect(chat.pigeons).toEqual([])
    expect(chat.errorText).toBe('Login required.')
  })

  it('lists the sessions newest first', async () => {
    const sessions: SessionRef[] = [
      { id: 'old', pigeonId: 'biological', lang: 'en', createdAt: '2026-08-01T10:00:00Z', updatedAt: '2026-08-01T10:00:00Z' },
      { id: 'new', pigeonId: 'individualist', lang: 'nl', createdAt: '2026-09-01T10:00:00Z', updatedAt: '2026-09-02T10:00:00Z' },
    ]
    const { chat } = build([{ method: 'GET', match: '/chat/sessions', respond: () => jsonResponse({ sessions }) }])
    await chat.loadSessions()
    expect(chat.sessions.map((s) => s.id)).toEqual(['new', 'old'])
  })

  it('streams sources, text and done in order and captures the session id', async () => {
    const { chat, calls } = await withPigeon([
      turns(() =>
        sseResponse([
          { type: 'sources', sources: [SOURCE] },
          { type: 'text', delta: 'Pigeons nest ' },
          { type: 'text', delta: 'on ledges [1].' },
          { type: 'done', sessionId: 's-1', kbStatus: 'sources', sources: [SOURCE] },
        ]),
      ),
    ])
    await chat.send('Where do pigeons nest?')
    const [user, reply] = chat.messages
    expect(user).toMatchObject({ role: 'user', content: 'Where do pigeons nest?' })
    expect(reply.content).toBe('Pigeons nest on ledges [1].')
    expect(reply.sources).toEqual([
      {
        number: 1,
        type: 'destillaat',
        id: 'destillaat:9f1c',
        text: 'Pigeons nest on ledges.',
        ref: { quote: 'They prefer a ledge.', paper: 'lewald1987' },
      },
    ])
    expect(reply.kbStatus).toBe('sources')
    expect(reply.streaming).toBe(false)
    expect(reply.partial).toBe(false)
    expect(chat.sessionId).toBe('s-1')
    expect(chat.streaming).toBe(false)
    expect(chat.error).toBeNull()
    const turn = calls.find((c) => c.url.includes('/chat/turn'))
    expect(turn?.body).toEqual({ sessionId: '', pigeonId: 'biological', message: 'Where do pigeons nest?', lang: 'en' })
  })

  it('renders an unreachable knowledge base as its own message, not as a gap', async () => {
    const { chat } = await withPigeon([
      turns(() => sseResponse([{ type: 'error', error: 'kb_unavailable' }, { type: 'done', sessionId: 's-2' }])),
    ])
    await chat.send('Where do pigeons nest?')
    expect(chat.error?.reason).toBe('kb_unavailable')
    // Geen servertekst meegestuurd: de eigen i18n-tekst is de terugval.
    expect(chat.errorText).toMatch(/cannot reach her knowledge base/)
    // De melding staat in de banner; een lege bubbel die de server niet
    // bewaart blijft niet staan (live == na herladen).
    expect(chat.messages.map((m) => m.role)).toEqual(['user'])
  })

  it('remembers a freshly created session for the sidebar', async () => {
    const { chat } = await withPigeon([
      turns(() => sseResponse([{ type: 'text', delta: 'Coo.' }, { type: 'done', sessionId: 's-9', kbStatus: 'persona_only' }])),
    ])
    await chat.send('Hello')
    expect(chat.sessions.map((s) => s.id)).toEqual(['s-9'])
    expect(chat.sessions[0].pigeonId).toBe('biological')
  })

  it('flags a knowledge gap answer', async () => {
    const { chat } = await withPigeon([
      turns(() => sseResponse([{ type: 'text', delta: 'I have no source for that.' }, { type: 'done', sessionId: 's-2', kbStatus: 'gap' }])),
    ])
    await chat.send('What is the budget?')
    expect(chat.messages[1].kbStatus).toBe('gap')
  })

  it('keeps the received text, shows the error and re-enables the input on an error frame', async () => {
    const { chat } = await withPigeon([
      turns(() =>
        sseResponse([
          { type: 'text', delta: 'The city says ' },
          {
            type: 'error',
            error: 'llm_unavailable',
            message: { en: 'The pigeons cannot reach their model.', nl: 'De duiven bereiken hun model niet.' },
          },
          // Beide servermotoren sturen `error` en dán `done`; zonder dat frame
          // toetst de fixture de implementatie in plaats van de wire.
          { type: 'done', sessionId: 's-err' },
        ]),
      ),
    ])
    await chat.send('Tell me more')
    expect(chat.messages[1].content).toBe('The city says ')
    expect(chat.messages[1].partial).toBe(true)
    expect(chat.messages[1].streaming).toBe(false)
    expect(chat.streaming).toBe(false)
    expect(chat.errorText).toBe('The pigeons cannot reach their model.')
  })

  it('never surfaces a non-json error body', async () => {
    const { chat } = await withPigeon([
      turns(() => new Response('<html>502 Bad Gateway</html>', { status: 502 })),
    ])
    await chat.send('Hello')
    expect(chat.error).toEqual({ status: 502 })
    expect(chat.errorText).toBe('Something went wrong. Please try again.')
    // No session acknowledgement: restore the unsent question instead of
    // showing history that a later new server session would never receive.
    expect(chat.messages).toEqual([])
    expect(chat.draft).toBe('Hello')
  })

  it('falls back to the generic text for an sse error without a message', async () => {
    const { chat } = await withPigeon([turns(() => sseResponse([{ type: 'error', error: 'kaboom' }]))])
    await chat.send('Hello')
    expect(chat.errorText).toBe('Something went wrong. Please try again.')
    expect(chat.errorText).not.toContain('kaboom')
  })

  it('reports a pre-stream 401 in the ui language', async () => {
    const { chat, locale } = await withPigeon([
      turns(() =>
        jsonResponse({ error: 'unauthorized', message: { en: 'Login required.', nl: 'Inloggen vereist.' } }, 401),
      ),
    ])
    await chat.send('Hello')
    expect(chat.errorText).toBe('Login required.')
    locale.lang = 'nl'
    expect(chat.errorText).toBe('Inloggen vereist.')
  })

  it('reports a pre-stream 404 for an unknown session', async () => {
    const { chat } = await withPigeon([
      turns(() =>
        jsonResponse(
          { error: 'session_not_found', message: { en: 'Chat session not found.', nl: 'Chatsessie niet gevonden.' } },
          404,
        ),
      ),
    ])
    chat.sessionId = 'gone'
    await chat.send('Hello')
    expect(chat.error?.status).toBe(404)
    expect(chat.errorText).toBe('Chat session not found.')
  })

  it('flags the budget cap from a pre-stream body and from an in-stream frame alike', async () => {
    const cap = {
      error: 'llm_budget_exceeded',
      message: { en: 'The pigeons have reached their budget.', nl: 'De duiven hebben hun budget bereikt.' },
    }
    const pre = await withPigeon([turns(() => jsonResponse(cap, 402))])
    await pre.chat.send('Hello')
    expect(pre.chat.capExceeded).toBe(true)
    expect(pre.chat.errorText).toBe('The pigeons have reached their budget.')

    const mid = await withPigeon([turns(() => {
      const response = sseResponse([{ type: 'text', delta: 'Coo' }, { type: 'error', ...cap }])
      response.headers.set('X-Duif-Session-ID', 'budget-session')
      return response
    })])
    await mid.chat.send('Hello')
    expect(mid.chat.capExceeded).toBe(true)
    expect(mid.chat.errorText).toBe(pre.chat.errorText)
    expect(mid.chat.messages[1].content).toBe('Coo')
  })

  it('ignores an empty or whitespace-only message', async () => {
    const { chat, calls } = await withPigeon([turns(() => sseResponse([{ type: 'done', sessionId: 's' }]))])
    await chat.send('')
    await chat.send('   \n ')
    expect(calls.filter((c) => c.url.includes('/chat/turn'))).toHaveLength(0)
    expect(chat.messages).toEqual([])
  })

  it('ignores a message longer than 4096 runes', async () => {
    const { chat, calls } = await withPigeon([turns(() => sseResponse([{ type: 'done', sessionId: 's' }]))])
    await chat.send('🐦'.repeat(4097))
    expect(calls.filter((c) => c.url.includes('/chat/turn'))).toHaveLength(0)
    await chat.send('a'.repeat(4096))
    expect(calls.filter((c) => c.url.includes('/chat/turn'))).toHaveLength(1)
  })

  it('posts exactly once when a second send arrives while streaming', async () => {
    const live = liveStream()
    const { chat, calls } = await withPigeon([turns(() => live.response)])
    const first = chat.send('One')
    await settle()
    expect(chat.streaming).toBe(true)
    await chat.send('Two')
    expect(calls.filter((c) => c.url.includes('/chat/turn'))).toHaveLength(1)
    live.push({ type: 'done', sessionId: 's-3', kbStatus: 'persona_only' })
    live.close()
    await first
    expect(chat.streaming).toBe(false)
  })

  it('closes the answer as interrupted when the user stops the stream', async () => {
    const live = liveStream()
    live.response.headers.set('X-Duif-Session-ID', 'confirmed-partial')
    const { chat } = await withPigeon([turns(() => live.response)])
    const running = chat.send('One')
    await settle()
    live.push({ type: 'text', delta: 'Half a thought' })
    await settle()
    chat.abort()
    await running
    expect(chat.streaming).toBe(false)
    expect(chat.messages[1]).toMatchObject({ content: 'Half a thought', partial: true, streaming: false })
    expect(chat.error).toBeNull()
  })

  it('writes no state and shows no error after dispose', async () => {
    const live = liveStream()
    const { chat } = await withPigeon([turns(() => live.response)])
    const running = chat.send('One')
    await settle()
    live.push({ type: 'text', delta: 'Half' })
    await settle()
    const before = chat.messages
    chat.dispose()
    chat.dispose()
    live.push({ type: 'text', delta: ' more' })
    live.push({ type: 'done', sessionId: 's-4' })
    live.close()
    await running
    expect(chat.messages).toBe(before)
    expect(chat.messages[1].content).toBe('Half')
    expect(chat.error).toBeNull()
    expect(chat.sessionId).toBe('')
  })

  it('starts a new conversation when another pigeon is picked', async () => {
    const { chat } = await withPigeon([
      turns(() => sseResponse([{ type: 'text', delta: 'Coo' }, { type: 'done', sessionId: 's-5' }])),
    ])
    await chat.send('Hello')
    expect(chat.sessionId).toBe('s-5')
    chat.selectPigeon('individualist')
    expect(chat.selectedPigeonId).toBe('individualist')
    expect(chat.sessionId).toBe('')
    expect(chat.messages).toEqual([])
  })

  it('keeps the conversation when the same pigeon is picked again', async () => {
    const { chat } = await withPigeon([
      turns(() => sseResponse([{ type: 'text', delta: 'Coo' }, { type: 'done', sessionId: 's-6' }])),
    ])
    await chat.send('Hello')
    chat.selectPigeon('biological')
    expect(chat.messages).toHaveLength(2)
    expect(chat.sessionId).toBe('s-6')
  })

  it('reloads history with citations, kb status and the interrupted flag', async () => {
    const sessions: SessionRef[] = [
      { id: 's-7', pigeonId: 'individualist', lang: 'nl', createdAt: '2026-09-01T10:00:00Z', updatedAt: '2026-09-02T10:00:00Z' },
    ]
    const { chat, locale } = await withPigeon([
      {
        method: 'GET',
        match: '/messages',
        respond: () =>
          jsonResponse({
            messages: [
              { id: 'm1', role: 'user', content: 'Where do pigeons nest?', createdAt: '2026-09-02T09:59:00Z' },
              {
                id: 'm2',
                role: 'assistant',
                content: 'Pigeons nest on ledges [1].',
                kbStatus: 'sources',
                sources: [SOURCE],
                partial: true,
                createdAt: '2026-09-02T10:00:00Z',
              },
            ],
          }),
      },
      { method: 'GET', match: '/chat/sessions', respond: () => jsonResponse({ sessions }) },
    ])
    await chat.loadSessions()
    await chat.openSession('s-7')
    expect(chat.sessionId).toBe('s-7')
    expect(chat.selectedPigeonId).toBe('individualist')
    expect(locale.lang).toBe('en') // Original content language does not change chosen UI language.
    expect(chat.conversationLang).toBe('nl')
    expect(chat.messages).toHaveLength(2)
    expect(chat.messages[1]).toMatchObject({ role: 'assistant', partial: true, kbStatus: 'sources' })
    expect(chat.messages[1].sources[0].number).toBe(1)
  })

  it('speaks as the facilitator from the first character of an agent turn', async () => {
    const live = liveStream()
    const { chat } = await withAgentPigeon([turns(() => live.response)])
    const running = chat.send('Are you an AI?')
    await settle()
    live.push({ type: 'text', delta: 'I am the facilitator.' })
    await settle()
    // Vóór `done`: de stem staat al vast, want de duif draait op de agent-motor.
    expect(chat.messages[1]).toMatchObject({
      persona: 'facilitator',
      content: 'I am the facilitator.',
      streaming: true,
    })
    live.push({ type: 'done', sessionId: 's-agent', stopReason: 'natural' })
    live.close()
    await running
    expect(chat.messages[1].persona).toBe('facilitator')
  })

  it('keeps a pipeline turn speaking as the pigeon herself', async () => {
    const live = liveStream()
    const { chat } = await withPigeon([turns(() => live.response)])
    const running = chat.send('Where do pigeons nest?')
    await settle()
    live.push({ type: 'text', delta: 'On ledges.' })
    await settle()
    expect(chat.messages[1].persona).toBeUndefined()
    live.push({ type: 'done', sessionId: 's-pipe', kbStatus: 'persona_only' })
    live.close()
    await running
    expect(chat.messages[1].persona).toBeUndefined()
  })

  // De stappen van de beurt. Ze bestaan alleen tijdens de beurt: wat
  // erna overblijft is het antwoord, niet het logboek van hoe het tot stand
  // kwam. Chips worden niet bewaard, dus zouden ze na herladen toch weg zijn.
  it('keeps the steps of the running turn in the order the server announced them', async () => {
    const live = liveStream()
    const { chat } = await withAgentPigeon([turns(() => live.response)])
    const running = chat.send('Where would you nest?')
    await settle()

    live.push({ type: 'phase', phase: 'facilitator' })
    await settle()
    expect(chat.steps).toEqual([{ phase: 'facilitator', done: false }])
    expect(chat.currentStep).toBe('facilitator')

    // De volgende stap sluit de vorige: de server meldt een stap wanneer hij
    // begint.
    live.push({ type: 'phase', phase: 'pigeon_grounding' })
    await settle()
    expect(chat.steps).toEqual([
      { phase: 'facilitator', done: true },
      { phase: 'pigeon_grounding', done: false },
    ])

    // De brede zoekactie na een kennisgat, en daarna weer het opzoeken: geen
    // tweede chip, maar dezelfde stap die opnieuw vooraan komt te staan.
    live.push({ type: 'phase', phase: 'pigeon_related_search' })
    live.push({ type: 'phase', phase: 'pigeon_grounding' })
    await settle()
    expect(chat.steps.map((s) => s.phase)).toEqual(['facilitator', 'pigeon_related_search', 'pigeon_grounding'])
    expect(chat.currentStep).toBe('pigeon_grounding')

    // Een fase die deze client niet kent verandert niets: liever een stap
    // minder dan machinetaal op het scherm.
    live.push({ type: 'phase', phase: 'gedachtenlezen' })
    await settle()
    expect(chat.currentStep).toBe('pigeon_grounding')

    live.push({ type: 'done', sessionId: 's-steps', kbStatus: 'gap', stopReason: 'natural' })
    live.close()
    await running
    expect(chat.steps).toEqual([])
    expect(chat.currentStep).toBeUndefined()
  })

  it('drops the steps when the reader stops the turn', async () => {
    const live = liveStream()
    const { chat } = await withAgentPigeon([turns(() => live.response)])
    const running = chat.send('Where would you nest?')
    await settle()
    live.push({ type: 'phase', phase: 'pigeon' })
    await settle()
    expect(chat.currentStep).toBe('pigeon')
    chat.abort()
    live.close()
    await running
    expect(chat.steps).toEqual([])
  })

  // Een eerste beurt die stopt vóór de bevestiging wordt teruggedraaid in
  // plaats van afgesloten: de vraag gaat terug het invoerveld in en het gesprek
  // is weer leeg. Die weg loopt niet langs #close, dus moet hij de stapregel
  // zelf opruimen — anders zegt de store dat er nog een stap loopt onder een
  // gesprek dat niet eens bestaat.
  it('drops the steps when a first turn ends without acknowledgement', async () => {
    const live = liveStream()
    const { chat } = await withAgentPigeon([turns(() => live.response)])
    const running = chat.send('Where would you nest?')
    await settle()
    live.push({ type: 'phase', phase: 'pigeon_grounding' })
    await settle()
    expect(chat.currentStep).toBe('pigeon_grounding')

    // Geen done-frame en geen sessiekop: de beurt is nooit bevestigd.
    live.close()
    await running
    expect(chat.error?.reason).toBe('chat_turn_unconfirmed')
    expect(chat.draft).toBe('Where would you nest?')
    expect(chat.steps).toEqual([])
    expect(chat.currentStep).toBeUndefined()
  })

  it('renders a facilitator turn with the pigeon block before the facilitator row', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'tool', id: 't1', name: 'ask_pigeon' },
          { type: 'artifact', id: 'pigeon-1', kind: 'pigeon_answer', persona: 'individualist' },
          { type: 'artifact_delta', id: 'pigeon-1', delta: 'Coo. ' },
          { type: 'artifact_delta', id: 'pigeon-1', delta: 'I like ledges.' },
          { type: 'artifact', id: 'pigeon-1', done: true },
          { type: 'tool_result', id: 't1', name: 'ask_pigeon' },
          { type: 'text', delta: 'She would rather sit high up.' },
          { type: 'done', sessionId: 's-10', kbStatus: 'persona_only', stopReason: 'natural' },
        ]),
      ),
    ])
    await chat.send('Where would you nest?')
    expect(chat.messages.map((m) => [m.role, m.kind, m.persona])).toEqual([
      ['user', 'text', undefined],
      ['assistant', 'pigeon_answer', 'individualist'],
      ['assistant', 'text', 'facilitator'],
    ])
    const [, pigeon, facilitator] = chat.messages
    expect(pigeon.content).toBe('Coo. I like ledges.')
    expect(pigeon.streaming).toBe(false)
    expect(pigeon.partial).toBe(false)
    expect(facilitator.content).toBe('She would rather sit high up.')
    expect(facilitator.tools).toMatchObject([{ id: 't1', name: 'ask_pigeon', done: true }])
    expect(facilitator.stopReason).toBeUndefined()
    expect(chat.error).toBeNull()
  })

  // De provider sluit de duif-stream soms op de tokengrens af, midden in
  // een zin. Die reden hoort bij het blok dat afbrak — de facilitator praat
  // daarna gewoon door, en zijn bemiddeling is niet afgekapt.
  it('marks the pigeon block that ran into the length limit, not the facilitator row', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'tool', id: 't1', name: 'ask_pigeon' },
          { type: 'artifact', id: 'pigeon-1', kind: 'pigeon_answer', persona: 'individualist' },
          { type: 'artifact_delta', id: 'pigeon-1', delta: 'In the setting studied, higher numbers of humans' },
          { type: 'artifact', id: 'pigeon-1', done: true, stopReason: 'truncated' },
          { type: 'tool_result', id: 't1', name: 'ask_pigeon' },
          { type: 'text', delta: 'That is the pigeon speaking for itself.' },
          { type: 'done', sessionId: 's-trunc', kbStatus: 'sources', stopReason: 'natural' },
        ]),
      ),
    ])
    await chat.send('What about the event?')
    const [, pigeon, facilitator] = chat.messages
    // De tekst blijft letterlijk wat er binnenkwam: er wordt niets aangevuld.
    expect(pigeon.content).toBe('In the setting studied, higher numbers of humans')
    expect(pigeon.stopReason).toBe('truncated')
    // Geen fout: de stroom liep netjes af, alleen het antwoord niet.
    expect(pigeon.partial).toBe(false)
    expect(facilitator.stopReason).toBeUndefined()
    expect(chat.error).toBeNull()
  })

  it('leaves a naturally closed pigeon block without a stop reason', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'artifact', id: 'pigeon-1', kind: 'pigeon_answer', persona: 'individualist' },
          { type: 'artifact_delta', id: 'pigeon-1', delta: 'That pavement is mine.' },
          { type: 'artifact', id: 'pigeon-1', done: true, stopReason: 'natural' },
          { type: 'done', sessionId: 's-ok', kbStatus: 'sources', stopReason: 'natural' },
        ]),
      ),
    ])
    await chat.send('What about the event?')
    expect(chat.messages[1].stopReason).toBeUndefined()
  })

  // De pipeline heeft geen apart duif-blok: daar draagt de done van de beurt
  // de reden, en die hoort op de rij van de duif zelf te landen.
  it('carries a length close from a pipeline done onto the pigeon row', async () => {
    const { chat } = await withPigeon([
      turns(() =>
        sseResponse([
          { type: 'text', delta: 'Higher numbers of humans on the square make the flock' },
          { type: 'done', sessionId: 's-pipe-trunc', kbStatus: 'sources', stopReason: 'truncated' },
        ]),
      ),
    ])
    await chat.send('What happens at a busy event?')
    expect(chat.messages[1].content).toBe('Higher numbers of humans on the square make the flock')
    expect(chat.messages[1].stopReason).toBe('truncated')
  })

  // Na herladen staat de melding er nog: een antwoord dat halverwege stopte
  // blijft een antwoord dat halverwege stopte.
  it('keeps a stored length close when history is reloaded', async () => {
    const sessions: SessionRef[] = [
      { id: 's-8', pigeonId: 'individualist', lang: 'en', createdAt: '2026-09-09T10:00:00Z', updatedAt: '2026-09-09T10:00:00Z' },
    ]
    const { chat } = await withPigeon([
      {
        method: 'GET',
        match: '/messages',
        respond: () =>
          jsonResponse({
            messages: [
              { id: 'm1', role: 'user', content: 'What about the event?', createdAt: '2026-09-09T09:59:00Z' },
              {
                id: 'm2',
                role: 'assistant',
                kind: 'pigeon_answer',
                persona: 'individualist',
                content: 'In the setting studied, higher numbers of humans',
                kbStatus: 'sources',
                stopReason: 'truncated',
                createdAt: '2026-09-09T10:00:00Z',
              },
              {
                id: 'm3',
                role: 'assistant',
                persona: 'facilitator',
                content: 'That is the pigeon speaking for itself.',
                stopReason: 'natural',
                createdAt: '2026-09-09T10:00:01Z',
              },
            ],
          }),
      },
      { method: 'GET', match: '/chat/sessions', respond: () => jsonResponse({ sessions }) },
    ])
    await chat.loadSessions()
    await chat.openSession('s-8')
    expect(chat.messages[1].stopReason).toBe('truncated')
    // "natural" is het normale geval en zegt de lezer niets.
    expect(chat.messages[2].stopReason).toBeUndefined()
  })

  // De facilitator bemiddelt op twee momenten. Wat de lezer
  // beleeft is de volgorde, dus dat is wat de store moet opleveren.
  it('puts the framing above the pigeon block and the mediation below it', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'tool', id: 't1', name: 'ask_pigeon' },
          { type: 'text', delta: 'I am putting the residents’ concern to her.' },
          { type: 'artifact', id: 'pigeon-1', kind: 'pigeon_answer', persona: 'individualist' },
          { type: 'artifact_delta', id: 'pigeon-1', delta: 'The ledge is mine.' },
          { type: 'artifact', id: 'pigeon-1', done: true },
          { type: 'tool_result', id: 't1', name: 'ask_pigeon' },
          { type: 'text', delta: 'She will not trade the ledge away. ' },
          { type: 'text', delta: 'That is the tension your decision has to hold.' },
          { type: 'done', sessionId: 's-mediation', kbStatus: 'persona_only', stopReason: 'natural' },
        ]),
      ),
    ])
    await chat.send('Can we net the ledge?')
    expect(chat.messages.map((m) => [m.role, m.kind, m.persona])).toEqual([
      ['user', 'text', undefined],
      ['assistant', 'text', 'facilitator'],
      ['assistant', 'pigeon_answer', 'individualist'],
      ['assistant', 'text', 'facilitator'],
    ])
    const [, framing, pigeon, mediation] = chat.messages
    expect(framing.content).toBe('I am putting the residents’ concern to her.')
    expect(framing.streaming).toBe(false)
    expect(framing.partial).toBe(false)
    expect(pigeon.content).toBe('The ledge is mine.')
    expect(mediation.content).toBe('She will not trade the ledge away. That is the tension your decision has to hold.')
    // De chips zijn de stappen van deze beurt en blijven bij één stem hangen.
    expect(mediation.tools).toMatchObject([{ id: 't1', name: 'ask_pigeon', done: true }])
    expect(framing.tools).toEqual([])
    expect(chat.error).toBeNull()
  })

  // Zonder bemiddeling blijft de afsluitende rij leeg: die verdwijnt, en de
  // chips schuiven door naar de rij waar de facilitator wél iets zei.
  it('keeps only the framing row when nothing follows the pigeon', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'tool', id: 't1', name: 'ask_pigeon' },
          { type: 'text', delta: 'I am keeping the question as asked.' },
          { type: 'artifact', id: 'pigeon-1', kind: 'pigeon_answer', persona: 'individualist' },
          { type: 'artifact_delta', id: 'pigeon-1', delta: 'The ledge is mine.' },
          { type: 'artifact', id: 'pigeon-1', done: true },
          { type: 'tool_result', id: 't1', name: 'ask_pigeon' },
          { type: 'done', sessionId: 's-framing', kbStatus: 'persona_only', stopReason: 'natural' },
        ]),
      ),
    ])
    await chat.send('Can we net the ledge?')
    expect(chat.messages.map((m) => [m.kind, m.persona])).toEqual([
      ['text', undefined],
      ['text', 'facilitator'],
      ['pigeon_answer', 'individualist'],
    ])
    expect(chat.messages[1].tools).toMatchObject([{ id: 't1', name: 'ask_pigeon', done: true }])
    expect(chat.error).toBeNull()
  })

  // Herladen moet dezelfde volgorde geven als de stroom: de server bewaart
  // drie rijen op `seq`, en de store laat ze staan zoals ze komen.
  it('reloads a mediated turn in the order the server stored it', async () => {
    const sessions: SessionRef[] = [
      { id: 's-mediation', pigeonId: 'individualist', lang: 'en', createdAt: '2026-09-09T10:00:00Z', updatedAt: '2026-09-09T10:01:00Z' },
    ]
    const { chat } = await withAgentPigeon([
      {
        method: 'GET',
        match: '/messages',
        respond: () =>
          jsonResponse({
            messages: [
              { id: 'u1', role: 'user', content: 'Can we net the ledge?' },
              { id: 'f1', role: 'assistant', kind: 'text', persona: 'facilitator', content: 'I am putting the residents’ concern to her.' },
              { id: 'p1', role: 'assistant', kind: 'pigeon_answer', persona: 'individualist', content: 'The ledge is mine.' },
              { id: 'f2', role: 'assistant', kind: 'text', persona: 'facilitator', content: 'She will not trade the ledge away.' },
            ],
          }),
      },
      { method: 'GET', match: '/chat/sessions', respond: () => jsonResponse({ sessions }) },
    ])
    await chat.loadSessions()
    await chat.openSession('s-mediation')
    expect(chat.messages.map((m) => [m.kind, m.persona])).toEqual([
      ['text', undefined],
      ['text', 'facilitator'],
      ['pigeon_answer', 'individualist'],
      ['text', 'facilitator'],
    ])
  })

  it('binds a sources frame with an artifact id to the pigeon block, with its kbStatus', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'tool', id: 't1', name: 'ask_pigeon' },
          { type: 'artifact', id: 'pigeon-1', kind: 'pigeon_answer', persona: 'biological', kbStatus: 'sources' },
          { type: 'sources', id: 'pigeon-1', sources: [SOURCE] },
          { type: 'artifact_delta', id: 'pigeon-1', delta: 'We nest on ledges [1].' },
          { type: 'artifact', id: 'pigeon-1', done: true, kbStatus: 'sources' },
          { type: 'tool_result', id: 't1', name: 'ask_pigeon' },
          { type: 'text', delta: 'That is what the research says.' },
          { type: 'done', sessionId: 's-11', kbStatus: 'sources', stopReason: 'natural' },
        ]),
      ),
    ])
    await chat.send('Where do you nest?')
    const [, pigeon, facilitator] = chat.messages
    expect(pigeon.kind).toBe('pigeon_answer')
    expect(pigeon.sources).toHaveLength(1)
    expect(pigeon.sources[0]).toMatchObject({ number: 1, id: 'destillaat:9f1c' })
    expect(pigeon.kbStatus).toBe('sources')
    expect(pigeon.content).toBe('We nest on ledges [1].')
    // De facilitator citeert niet: zijn rij draagt geen bronkaarten, en ook
    // geen kennisbankstand — die hoort bij het blok van de duif.
    expect(facilitator.sources).toEqual([])
    expect(facilitator.kbStatus).toBeUndefined()
  })

  // Het openframe zegt erbij dat deze bronnen de vraag niet dekken.
  // Zonder dat vlag leest de rij als een gewoon, gedekt antwoord.
  it('carries the related flag of the pigeon block', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'tool', id: 't1', name: 'ask_pigeon' },
          { type: 'artifact', id: 'pigeon-1', kind: 'pigeon_answer', persona: 'biological', kbStatus: 'sources', related: true },
          { type: 'sources', id: 'pigeon-1', sources: [SOURCE] },
          { type: 'artifact_delta', id: 'pigeon-1', delta: 'I cannot determine that. Related is [1].' },
          { type: 'artifact', id: 'pigeon-1', done: true, kbStatus: 'sources' },
          { type: 'tool_result', id: 't1', name: 'ask_pigeon' },
          { type: 'done', sessionId: 's-related', kbStatus: 'sources', stopReason: 'natural' },
        ]),
      ),
    ])
    await chat.send('How many pigeons live in Reykjavik?')
    const [, pigeon] = chat.messages
    expect(pigeon.related).toBe(true)
    expect(pigeon.kbStatus).toBe('sources')
  })

  it('marks a knowledge gap on the pigeon block itself', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'tool', id: 't1', name: 'ask_pigeon' },
          { type: 'artifact', id: 'pigeon-1', kind: 'pigeon_answer', persona: 'biological', kbStatus: 'gap' },
          { type: 'artifact_delta', id: 'pigeon-1', delta: 'No research on that.' },
          { type: 'artifact', id: 'pigeon-1', done: true, kbStatus: 'gap' },
          { type: 'tool_result', id: 't1', name: 'ask_pigeon' },
          { type: 'text', delta: 'The research is silent here.' },
          { type: 'done', sessionId: 's-12', kbStatus: 'gap', stopReason: 'natural' },
        ]),
      ),
    ])
    await chat.send('Do pigeons dream?')
    const [, pigeon, facilitator] = chat.messages
    expect(pigeon.kbStatus).toBe('gap')
    expect(pigeon.sources).toEqual([])
    // Het gat staat op het blok van de duif en nergens anders: de begeleider
    // mag geen bronclaim dragen, ook niet de claim dat er geen bron was.
    // De `done` van de beurt draagt de stand wél — hij gaat over de
    // beurt, niet over de spreker.
    expect(facilitator.persona).toBe('facilitator')
    expect(facilitator.kbStatus).toBeUndefined()
  })

  // De server pint zijn toolset (TOOL_NAMES), maar de store mag er niet op
  // stukgaan als de server er een stap bij zet: een onbekende naam hoort
  // ongewijzigd als chip te verschijnen. Vroeger toetste dit geval
  // `search_sources`; die stap komt niet meer op de draad.
  it('shows a tool the store does not know by its own name', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'tool', id: 's1', name: 'synthetic_future_tool' },
          { type: 'tool_result', id: 's1', name: 'synthetic_future_tool' },
          { type: 'text', delta: 'Let me ask her precisely.' },
          { type: 'done', sessionId: 's-13', kbStatus: 'persona_only', stopReason: 'natural' },
        ]),
      ),
    ])
    await chat.send('What is known?')
    const [, facilitator] = chat.messages
    expect(facilitator.tools).toMatchObject([{ id: 's1', name: 'synthetic_future_tool', done: true }])
  })

  it('keeps fixed redirect guidance in a facilitator bubble without a pigeon or sources', async () => {
    const { chat } = await withAgentPigeon([
      turns(() => sseResponse([
        { type: 'tool', id: 'r1', name: 'redirect_to_pigeon' },
        { type: 'tool_result', id: 'r1', name: 'redirect_to_pigeon' },
        { type: 'text', delta: 'This conversation is about the digital pigeons and their place in the city.' },
        { type: 'done', sessionId: 's-redirect', kbStatus: 'persona_only', stopReason: 'natural' },
      ])),
    ])
    await chat.send('A request outside the project')
    const [, facilitator] = chat.messages
    expect(chat.messages).toHaveLength(2)
    expect(facilitator.kind).toBe('text')
    expect(facilitator.persona).toBe('facilitator')
    expect(facilitator.sources).toEqual([])
    expect(facilitator.tools).toMatchObject([{ id: 'r1', name: 'redirect_to_pigeon', done: true }])
  })

  it('renders a turn without an artifact as exactly one facilitator bubble', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'tool', id: 'c1', name: 'clarify' },
          { type: 'tool_result', id: 'c1', name: 'clarify' },
          { type: 'text', delta: 'Do you mean the city or the coast?' },
          { type: 'done', sessionId: 's-11', kbStatus: 'persona_only', stopReason: 'natural' },
        ]),
      ),
    ])
    await chat.send('And there?')
    expect(chat.messages.filter((m) => m.kind === 'pigeon_answer')).toEqual([])
    expect(chat.messages).toHaveLength(2)
    expect(chat.messages[1]).toMatchObject({ persona: 'facilitator', content: 'Do you mean the city or the coast?' })
  })

  it('keeps appending an artifact delta that arrives after the close frame', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'artifact', id: 'p1', kind: 'pigeon_answer', persona: 'individualist' },
          { type: 'artifact_delta', id: 'p1', delta: 'Coo' },
          { type: 'artifact', id: 'p1', done: true },
          { type: 'artifact_delta', id: 'p1', delta: '!' },
          { type: 'text', delta: 'That was her.' },
          { type: 'done', sessionId: 's-12', stopReason: 'natural' },
        ]),
      ),
    ])
    await chat.send('Say something')
    expect(chat.messages[1].content).toBe('Coo!')
    expect(chat.messages[1].streaming).toBe(false)
  })

  it('settles every open block on done, even without a close frame', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'artifact', id: 'p1', kind: 'pigeon_answer', persona: 'individualist' },
          { type: 'artifact_delta', id: 'p1', delta: 'Half a coo' },
          { type: 'done', sessionId: 's-13', stopReason: 'natural' },
        ]),
      ),
    ])
    await chat.send('Say something')
    const pigeon = chat.messages[1]
    expect(pigeon).toMatchObject({ kind: 'pigeon_answer', content: 'Half a coo', streaming: false, partial: false })
    // Alleen duif-inhoud: de lege facilitator-rij verdwijnt zonder vals alarm.
    expect(chat.messages).toHaveLength(2)
    expect(chat.error).toBeNull()
  })

  it('marks an artifact that closed with an error as interrupted', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'artifact', id: 'p1', kind: 'pigeon_answer', persona: 'individualist' },
          { type: 'artifact_delta', id: 'p1', delta: 'Half a coo' },
          { type: 'artifact', id: 'p1', done: true, error: 'llm_budget_exceeded' },
          { type: 'tool_result', id: 't1', name: 'ask_pigeon', error: 'llm_budget_exceeded' },
          {
            type: 'error',
            error: 'llm_budget_exceeded',
            message: { en: 'The pigeons have reached their budget.', nl: 'De duiven hebben hun budget bereikt.' },
          },
          { type: 'done', sessionId: 's-14', stopReason: 'error' },
        ]),
      ),
    ])
    await chat.send('Say something')
    expect(chat.messages[1]).toMatchObject({ kind: 'pigeon_answer', partial: true, streaming: false })
    expect(chat.capExceeded).toBe(true)
    // De facilitator zei niets; zijn stopreden `error` heeft geen eigen regel,
    // dus er blijft geen lege kaart op zijn naam staan.
    expect(chat.messages.map((m) => m.kind)).toEqual(['text', 'pigeon_answer'])
  })

  it('creates a chip for a tool_result without a preceding tool frame', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'tool_result', id: 't1', name: 'ask_pigeon', error: 'llm_budget_exceeded' },
          { type: 'text', delta: 'I could not reach her.' },
          { type: 'done', sessionId: 's-14', stopReason: 'error' },
        ]),
      ),
    ])
    await chat.send('Say something')
    expect(chat.messages[1].tools).toMatchObject([{ id: 't1', name: 'ask_pigeon', done: true, error: 'llm_budget_exceeded' }])
    expect(chat.messages[1].stopReason).toBe('error')
  })

  it('puts a non-natural stop reason on the facilitator row only', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'text', delta: 'I kept asking and got nowhere.' },
          { type: 'done', sessionId: 's-15', kbStatus: 'persona_only', stopReason: 'max_iter' },
        ]),
      ),
    ])
    await chat.send('Loop please')
    expect(chat.messages[1]).toMatchObject({ persona: 'facilitator', stopReason: 'max_iter' })
  })

  it('ignores tool and artifact frames without a usable id', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'tool', name: 'ask_pigeon' },
          { type: 'artifact', kind: 'pigeon_answer', persona: 'individualist' },
          { type: 'artifact_delta', id: 'nope', delta: 'lost' },
          { type: 'text', delta: 'Only prose here.' },
          { type: 'done', sessionId: 's-16', stopReason: 'natural' },
        ]),
      ),
    ])
    await chat.send('Hello')
    expect(chat.messages).toHaveLength(2)
    expect(chat.messages[1].tools).toEqual([])
    expect(chat.messages[1].content).toBe('Only prose here.')
  })

  it('settles both open blocks when the reader stops mid artifact', async () => {
    const live = liveStream()
    live.response.headers.set('X-Duif-Session-ID', 'confirmed-partial')
    const { chat } = await withAgentPigeon([turns(() => live.response)])
    const running = chat.send('Say something')
    await settle()
    live.push({ type: 'tool', id: 't1', name: 'ask_pigeon' })
    live.push({ type: 'artifact', id: 'p1', kind: 'pigeon_answer', persona: 'individualist' })
    live.push({ type: 'artifact_delta', id: 'p1', delta: 'Half a coo' })
    await settle()
    chat.abort()
    await running
    expect(chat.streaming).toBe(false)
    // De duif hield inhoud over; de lege facilitator-rij verdwijnt en er komt
    // geen foutmelding.
    expect(chat.messages.map((m) => m.kind)).toEqual(['text', 'pigeon_answer'])
    expect(chat.messages[1]).toMatchObject({ content: 'Half a coo', partial: true, streaming: false })
    expect(chat.error).toBeNull()
  })

  it('reloads a two-voice history with kind and persona and no chips', async () => {
    const sessions: SessionRef[] = [
      { id: 's-17', pigeonId: 'individualist', lang: 'en', createdAt: '2026-09-03T10:00:00Z', updatedAt: '2026-09-03T10:00:00Z' },
    ]
    const { chat } = await withPigeon([
      {
        method: 'GET',
        match: '/messages',
        respond: () =>
          jsonResponse({
            messages: [
              { id: 'm1', role: 'user', content: 'Where would you nest?', kind: 'text', persona: '' },
              { id: 'm2', role: 'assistant', content: 'Coo.', kind: 'pigeon_answer', persona: 'individualist' },
              { id: 'm3', role: 'assistant', content: 'She likes ledges.', kind: 'text', persona: 'facilitator' },
              { id: 'm4', role: 'assistant', content: 'An older answer.', kind: 'text', persona: '' },
            ],
          }),
      },
      { method: 'GET', match: '/chat/sessions', respond: () => jsonResponse({ sessions }) },
    ])
    await chat.loadSessions()
    await chat.openSession('s-17')
    expect(chat.messages.map((m) => [m.kind, m.persona])).toEqual([
      ['text', undefined],
      ['pigeon_answer', 'individualist'],
      ['text', 'facilitator'],
      ['text', undefined],
    ])
    expect(chat.messages.every((m) => m.tools.length === 0)).toBe(true)
  })

  it('keeps an unusable bilingual message out of the error text', async () => {
    const { chat, locale } = await withPigeon([
      turns(() =>
        sseResponse([
          // Een niet-string waarde per taal: geldige JSON, geen bruikbare tekst.
          { type: 'error', error: 'llm_budget_exceeded', message: { nl: { x: 1 }, en: '' } },
          { type: 'done', sessionId: 's-bad' },
        ]),
      ),
    ])
    locale.lang = 'nl'
    await chat.send('Hello')
    expect(chat.error?.message).toBeUndefined()
    // Terugval op de eigen tekst bij de reason-code, nooit "[object Object]".
    expect(typeof chat.errorText).toBe('string')
    expect(String(chat.errorText)).not.toContain('object Object')
    expect(chat.errorText).toMatch(/budget/)
  })

  it('announces both blocks when every frame of a turn arrives in one chunk', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseOneChunk([
          { type: 'artifact', id: 'p1', kind: 'pigeon_answer', persona: 'individualist' },
          { type: 'artifact_delta', id: 'p1', delta: 'Coo.' },
          { type: 'artifact', id: 'p1', done: true },
          { type: 'text', delta: 'That was her.' },
          { type: 'done', sessionId: 's-chunk', stopReason: 'natural' },
        ]),
      ),
    ])
    await chat.send('Say something')
    expect(chat.announcements.map((a) => a.voice)).toEqual(['pigeon', 'facilitator'])
    // De volgnummers lopen op: twee keer dezelfde zin blijft twee meldingen.
    expect(chat.announcements[1].seq).toBeGreaterThan(chat.announcements[0].seq)
  })

  it('announces the pigeon again on a second turn with the same wording', async () => {
    const frames = [
      { type: 'artifact', id: 'p1', kind: 'pigeon_answer', persona: 'individualist' },
      { type: 'artifact_delta', id: 'p1', delta: 'Coo.' },
      { type: 'artifact', id: 'p1', done: true },
      { type: 'done', sessionId: 's-twice', stopReason: 'natural' },
    ]
    const { chat } = await withAgentPigeon([turns(() => sseOneChunk(frames))])
    await chat.send('Once')
    const first = chat.announcements[0].seq
    await chat.send('Twice')
    // De melding van de eerste beurt is ingetrokken toen de tweede begon;
    // de nieuwe heeft een hoger volgnummer, dus dezelfde zin wordt
    // opnieuw voorgelezen.
    expect(chat.announcements.map((a) => a.voice)).toEqual(['pigeon'])
    expect(chat.announcements[0].seq).toBe(first + 1)
  })

  it('marks an artifact that is still open at a non-natural done as interrupted', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'artifact', id: 'p1', kind: 'pigeon_answer', persona: 'individualist' },
          { type: 'artifact_delta', id: 'p1', delta: 'Half a coo' },
          { type: 'done', sessionId: 's-open', stopReason: 'error' },
        ]),
      ),
    ])
    await chat.send('Say something')
    expect(chat.messages[1]).toMatchObject({ kind: 'pigeon_answer', partial: true, streaming: false })
  })

  it('ignores an artifact of an unknown kind', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'artifact', id: 'x1', kind: 'sketch', persona: 'individualist' },
          { type: 'artifact_delta', id: 'x1', delta: 'lost' },
          { type: 'text', delta: 'Only prose here.' },
          { type: 'done', sessionId: 's-kind', stopReason: 'natural' },
        ]),
      ),
    ])
    await chat.send('Draw something')
    expect(chat.messages.map((m) => m.kind)).toEqual(['text', 'text'])
    expect(chat.messages[1].content).toBe('Only prose here.')
  })

  it('renders two artifacts in one turn as two blocks before the facilitator row', async () => {
    const { chat } = await withAgentPigeon([
      turns(() =>
        sseResponse([
          { type: 'artifact', id: 'p1', kind: 'pigeon_answer', persona: 'individualist' },
          { type: 'artifact_delta', id: 'p1', delta: 'Coo.' },
          { type: 'artifact', id: 'p1', done: true },
          { type: 'artifact', id: 'p2', kind: 'pigeon_answer', persona: 'individualist' },
          { type: 'artifact_delta', id: 'p2', delta: 'And again.' },
          { type: 'artifact', id: 'p2', done: true },
          { type: 'text', delta: 'She said it twice.' },
          { type: 'done', sessionId: 's-two', stopReason: 'natural' },
        ]),
      ),
    ])
    await chat.send('Say something')
    expect(chat.messages.map((m) => m.content)).toEqual([
      'Say something',
      'Coo.',
      'And again.',
      'She said it twice.',
    ])
  })

  it('keeps the artifact map turn-local when two turns share an artifact id', async () => {
    const frames = (delta: string, session: string) => [
      { type: 'artifact', id: 'pigeon-1', kind: 'pigeon_answer', persona: 'individualist' },
      { type: 'artifact_delta', id: 'pigeon-1', delta },
      { type: 'artifact', id: 'pigeon-1', done: true },
      { type: 'text', delta: 'Done.' },
      { type: 'done', sessionId: session, stopReason: 'natural' },
    ]
    const { chat } = await withAgentPigeon([
      turns(
        () => sseResponse(frames('First coo.', 's-t1')),
        () => sseResponse(frames('Second coo.', 's-t2')),
      ),
    ])
    await chat.send('Once')
    await chat.send('Twice')
    // De tweede beurt schrijft in haar eigen blok; het eerste blijft staan.
    expect(chat.messages.filter((m) => m.kind === 'pigeon_answer').map((m) => m.content)).toEqual([
      'First coo.',
      'Second coo.',
    ])
  })

  it('renders a stored message of an unknown kind as ordinary text', async () => {
    const sessions: SessionRef[] = [
      { id: 's-18', pigeonId: 'individualist', lang: 'en', createdAt: '2026-09-03T10:00:00Z', updatedAt: '2026-09-03T10:00:00Z' },
    ]
    const { chat } = await withPigeon([
      {
        method: 'GET',
        match: '/messages',
        respond: () =>
          jsonResponse({
            messages: [{ id: 'm1', role: 'assistant', content: 'From a later server.', kind: 'sketch', persona: '' }],
          }),
      },
      { method: 'GET', match: '/chat/sessions', respond: () => jsonResponse({ sessions }) },
    ])
    await chat.loadSessions()
    await chat.openSession('s-18')
    expect(chat.messages[0]).toMatchObject({ kind: 'text', content: 'From a later server.' })
  })

  // De markering wordt bewaard, dus wie terugleest ziet hetzelfde als
  // wie het live volgde. Zonder deze regel las een antwoord uit grenzend
  // onderzoek na herladen als een gewoon, volledig gedekt antwoord — de duif
  // benoemt die grens alleen als de bronnen haar vraag écht niet dragen.
  it('keeps the related marking of a stored answer, and leaves ordinary answers unmarked', async () => {
    const sessions: SessionRef[] = [
      { id: 's-rel', pigeonId: 'biological', lang: 'en', createdAt: '2026-01-01T10:00:00Z', updatedAt: '2026-01-01T10:00:00Z' },
    ]
    const { chat } = await withPigeon([
      {
        method: 'GET',
        match: '/messages',
        respond: () =>
          jsonResponse({
            messages: [
              { id: 'm1', role: 'assistant', kind: 'pigeon_answer', persona: 'biological', content: 'Bordering research says [1].', kbStatus: 'sources', sources: [SOURCE], related: true },
              { id: 'm2', role: 'assistant', kind: 'pigeon_answer', persona: 'biological', content: 'The research says [1].', kbStatus: 'sources', sources: [SOURCE] },
            ],
          }),
      },
      { method: 'GET', match: '/chat/sessions', respond: () => jsonResponse({ sessions }) },
    ])
    await chat.loadSessions()
    await chat.openSession('s-rel')
    expect(chat.messages[0].related).toBe(true)
    expect(chat.messages[1].related).toBeUndefined()
  })

  it('reads a stored message in the old source shape without an id', async () => {
    const sessions: SessionRef[] = [
      { id: 's-8', pigeonId: 'biological', lang: 'en', createdAt: '2026-01-01T10:00:00Z', updatedAt: '2026-01-01T10:00:00Z' },
    ]
    const { chat } = await withPigeon([
      {
        method: 'GET',
        match: '/messages',
        respond: () =>
          jsonResponse({
            messages: [{ id: 'm9', role: 'assistant', content: 'Old answer [1].', kbStatus: 'sources', sources: [OLD_SOURCE] }],
          }),
      },
      { method: 'GET', match: '/chat/sessions', respond: () => jsonResponse({ sessions }) },
    ])
    await chat.loadSessions()
    await chat.openSession('s-8')
    expect(chat.messages[0].sources).toEqual([
      { number: 1, type: 'standpunt', text: 'Pigeons nest on ledges.', ref: { file: 'x.md', doi: '10.5/y' } },
    ])
    expect(chat.messages[0].sources[0].id).toBeUndefined()
  })
})


describe('separate conversations and explicit comparison', () => {
  it('returns to each pigeon conversation and draft without transferring context', async () => {
    const { chat, calls } = await withPigeon([
      turns(() => sseResponse([{ type: 'text', delta: 'Biological answer' }, { type: 'done', sessionId: 'bio' }])),
    ])
    await chat.send('Original question')
    chat.draft = 'Unsent bio follow-up'
    chat.selectPigeon('individualist')
    expect(chat.messages).toEqual([])
    expect(chat.draft).toBe('')
    chat.draft = 'Unsent individual question'
    chat.selectPigeon('biological')
    expect(chat.sessionId).toBe('bio')
    expect(chat.messages[1].content).toBe('Biological answer')
    expect(chat.messages[1].pigeonId).toBe('biological')
    expect(chat.draft).toBe('Unsent bio follow-up')
    chat.selectPigeon('individualist')
    expect(chat.draft).toBe('Unsent individual question')
    expect(calls.filter((c) => c.init?.method === 'POST')).toHaveLength(1)
  })

  it('keeps a deliberately new conversation empty after another pigeon is visited', async () => {
    const { chat } = await withPigeon([turns(() => sseResponse([{ type: 'text', delta: 'Old' }, { type: 'done', sessionId: 'old' }]))])
    await chat.send('Old question')
    chat.newConversation()
    chat.selectPigeon('individualist')
    chat.selectPigeon('biological')
    expect(chat.sessionId).toBe('')
    expect(chat.messages).toEqual([])
  })

  it('does not let a delayed history response replace another pigeon conversation', async () => {
    let resolve!: (r: Response) => void
    const { chat } = await withPigeon([{ method: 'GET', match: '/slow/messages', respond: () => new Promise((r) => { resolve = r }) }])
    chat.sessions = [{ id: 'slow', pigeonId: 'individualist', lang: 'nl', createdAt: '', updatedAt: '' }]
    chat.selectPigeon('individualist')
    expect(chat.loadingHistory).toBe(true)
    chat.selectPigeon('biological')
    chat.draft = 'Still mine'
    resolve(jsonResponse({ messages: [{ id: 'foreign', role: 'assistant', content: 'Other voice' }] }))
    await settle()
    expect(chat.selectedPigeonId).toBe('biological')
    expect(chat.messages).toEqual([])
    expect(chat.draft).toBe('Still mine')
    expect(chat.loadingHistory).toBe(false)
  })

  it('switches to the other pigeon in one click, carries the answered turn and asks straight away', async () => {
    const origin = { id: 'origin', pigeonId: 'biological', lang: 'nl', createdAt: '', updatedAt: '' }
    const comparison = { groupId: 'q', sessionId: 'origin', pigeonId: 'biological', questionId: 'q', originalQuestion: 'Wat betekent een nestplek?', contextMessageIds: ['u', 'a', 'q', 'a2'] }
    const target = { id: 'target', pigeonId: 'individualist', lang: 'nl', createdAt: '', updatedAt: '', comparison }
    const rows = [
      { id: 'u', role: 'user', content: 'We bespreken een plein.' },
      { id: 'a', role: 'assistant', persona: 'biological', kind: 'pigeon_answer', content: 'Een eerder antwoord [1].' },
      { id: 'q', role: 'user', content: comparison.originalQuestion },
      { id: 'a2', role: 'assistant', persona: 'biological', kind: 'pigeon_answer', content: 'Het antwoord op die vraag [1].' },
    ]
    const copied = rows.map((row, i) => ({ ...row, id: `copied-${i}` }))
    const { chat, calls } = await withPigeon([
      { method: 'GET', match: '/origin/messages', respond: () => jsonResponse({ messages: rows }) },
      { method: 'GET', match: '/target/messages', respond: () => jsonResponse({ messages: copied }) },
      { method: 'POST', match: '/chat/comparisons', respond: () => jsonResponse({ session: target }, 201) },
      turns(() => sseResponse([{ type: 'text', delta: 'My view' }, { type: 'done', sessionId: 'target' }])),
    ])
    chat.sessions = [origin]
    await chat.openSession('origin')
    await chat.beginComparison('q')
    // Geen tussenscherm en geen selectie: alleen de vraag en de andere duif.
    const copy = calls.find((c) => c.url.endsWith('/chat/comparisons'))
    expect(copy?.body).toEqual({ requestId: expect.any(String), sourceSessionId: 'origin', questionId: 'q', pigeonId: 'individualist' })
    const turn = calls.find((c) => c.url.endsWith('/chat/turn'))
    expect(turn?.body).toMatchObject({ sessionId: 'target', pigeonId: 'individualist', message: comparison.originalQuestion, lang: 'nl' })
    expect(chat.sessionId).toBe('target')
    expect(chat.selectedPigeonId).toBe('individualist')
    expect(chat.loadingComparison).toBe(false)
    // De hele beantwoorde beurt reisde mee, en beide antwoorden blijven van de
    // eerste duif; daarachter staat de nieuwe beurt van de tweede duif.
    expect(chat.messages.slice(0, 4).map((m) => [m.role, m.persona])).toEqual([
      ['user', undefined], ['assistant', 'biological'], ['user', undefined], ['assistant', 'biological'],
    ])
    expect(chat.messages.slice(4).map((m) => m.role)).toEqual(['user', 'assistant'])
    expect(chat.activeComparison).toEqual(comparison)
    chat.selectPigeon('biological')
    expect(chat.sessionId).toBe('origin')
    expect(chat.messages.map((m) => m.content)).toEqual(rows.map((m) => m.content))
  })

  it('starts one comparison however often the reader clicks', async () => {
    const origin = { id: 'origin', pigeonId: 'biological', lang: 'en', createdAt: '', updatedAt: '' }
    const comparison = { groupId: 'q', sessionId: 'origin', pigeonId: 'biological', questionId: 'q', originalQuestion: 'One question', contextMessageIds: ['q'] }
    const target = { id: 'target', pigeonId: 'individualist', lang: 'en', createdAt: '', updatedAt: '', comparison }
    const { chat, calls } = await withPigeon([
      { method: 'GET', match: '/origin/messages', respond: () => jsonResponse({ messages: [{ id: 'q', role: 'user', content: 'One question' }] }) },
      { method: 'GET', match: '/target/messages', respond: () => jsonResponse({ messages: [{ id: 'copied-q', role: 'user', content: 'One question' }] }) },
      { method: 'POST', match: '/chat/comparisons', respond: () => jsonResponse({ session: target }, 201) },
      turns(() => sseResponse([{ type: 'text', delta: 'My view' }, { type: 'done', sessionId: 'target' }])),
    ])
    chat.sessions = [origin]
    await chat.openSession('origin')
    // Drie keer klikken vóór het eerste antwoord terug is: de tweede en derde
    // klik mogen geen tweede vertakking en geen tweede modelbeurt maken.
    await Promise.all([chat.beginComparison('q'), chat.beginComparison('q'), chat.beginComparison('q')])
    expect(calls.filter((c) => c.url.endsWith('/chat/comparisons'))).toHaveLength(1)
    expect(calls.filter((c) => c.url.endsWith('/chat/turn'))).toHaveLength(1)
    expect(chat.sessionId).toBe('target')
    expect(chat.error).toBeNull()
  })

  it('refuses a question the stored conversation no longer has, without copying anything', async () => {
    const { chat, calls } = await withPigeon([{ method: 'GET', match: '/origin/messages', respond: () => jsonResponse({ messages: [{ id: 'q', role: 'user', content: 'A rewritten question' }] }) }])
    chat.sessions = [{ id: 'origin', pigeonId: 'biological', lang: 'en', createdAt: '', updatedAt: '' }]
    await chat.openSession('origin')
    chat.messages = [{ ...chat.messages[0], content: 'The question the reader clicked' }]
    await chat.beginComparison('q')
    expect(chat.error?.status).toBe(409)
    expect(chat.sessionId).toBe('origin')
    expect(calls.filter((c) => c.init?.method === 'POST')).toHaveLength(0)
  })

  it('keeps sources for an unknown artifact off the facilitator message', async () => {
    const { chat } = await withAgentPigeon([turns(() => sseResponse([
      { type: 'text', delta: 'Facilitator' },
      { type: 'sources', id: 'missing-artifact', sources: [SOURCE] },
      { type: 'done', sessionId: 's', stopReason: 'natural' },
    ]))])
    await chat.send('Question')
    expect(chat.messages.find((m) => m.persona === 'facilitator')?.sources).toEqual([])
  })
})

describe('conversation recovery: drafts, resets and late responses', () => {
  const origin: SessionRef = { id: 'origin', pigeonId: 'biological', lang: 'en', createdAt: '', updatedAt: '' }
  const question = { id: 'q', role: 'user', content: 'Original case question' }
  const target: SessionRef = { id: 'target', pigeonId: 'individualist', lang: 'en', createdAt: '', updatedAt: '', title: question.content,
    comparison: { groupId: 'q', sessionId: 'origin', pigeonId: 'biological', questionId: 'q', originalQuestion: question.content, contextMessageIds: [] } }
  const originRoute: Route = { method: 'GET', match: '/origin/messages', respond: () => jsonResponse({ messages: [question] }) }

  it('preserves unsent drafts for two sessions of the same pigeon', async () => {
    const { chat } = await withPigeon([{ method: 'GET', match: '/messages', respond: () => jsonResponse({ messages: [] }) }])
    chat.sessions = ['a', 'b'].map((id) => ({ ...origin, id }))
    await chat.openSession('a')
    chat.draft = 'Unsent original case details'
    await chat.openSession('b')
    chat.draft = 'Another unfinished question'
    await chat.openSession('a')
    expect(chat.draft).toBe('Unsent original case details')
    await chat.openSession('b')
    expect(chat.draft).toBe('Another unfinished question')
  })

  it('keeps both pigeons empty after a deliberate reset of the visible chats', async () => {
    const { chat } = await withPigeon([turns(() => sseResponse([{ type: 'done', sessionId: 'old-bio' }]))])
    await chat.send('Old group context')
    chat.draft = 'Old group draft'
    chat.selectPigeon('individualist')
    chat.draft = 'Old individual draft'
    for (const pigeon of chat.pigeons) { chat.selectPigeon(pigeon.id); chat.newConversation() }
    for (const pigeon of chat.pigeons) {
      chat.selectPigeon(pigeon.id)
      expect(chat.messages).toEqual([])
      expect(chat.draft).toBe('')
      expect(chat.sessionId).toBe('')
    }
  })

  it('ignores a late failed history body after the active pigeon changed', async () => {
    let release!: (value: unknown) => void
    const body = new Promise((resolve) => { release = resolve })
    const { chat } = await withPigeon([{ method: 'GET', match: '/slow/messages', respond: () => ({ ok: false, status: 403, json: () => body }) as unknown as Response }])
    chat.sessions = [{ ...origin, id: 'slow', pigeonId: 'individualist' }]
    const loading = chat.openSession('slow')
    await settle()
    chat.selectPigeon('biological')
    release({ error: 'old-session-denied', message: { en: 'The other session could not load.' } })
    await loading
    expect(chat.error).toBeNull()
    expect(chat.selectedPigeonId).toBe('biological')
  })

  for (const stage of ['lookup', 'copy']) {
    it(`ignores a late failed comparison ${stage} body after the reader moved on`, async () => {
      let release!: (value: unknown) => void
      const body = new Promise((resolve) => { release = resolve })
      const failed = () => ({ ok: false, status: 403, json: () => body }) as unknown as Response
      let reads = 0
      const { chat } = await withPigeon([
        { method: 'GET', match: '/origin/messages', respond: () => ++reads > 1 && stage === 'lookup' ? failed() : jsonResponse({ messages: [question] }) },
        { method: 'POST', match: '/chat/comparisons', respond: failed },
      ])
      chat.sessions = [origin]
      await chat.openSession(origin.id)
      const loading = chat.beginComparison('q')
      await settle()
      chat.selectPigeon('individualist')
      release({ error: 'old-comparison-denied', message: { en: 'Old comparison error.' } })
      await loading
      expect(chat.error).toBeNull()
      expect(chat.loadingComparison).toBe(false)
      expect(chat.selectedPigeonId).toBe('individualist')
    })
  }

  it('restores a first question as draft when stopped before acknowledgement and ignores late headers', async () => {
    let release!: (response: Response) => void
    const response = new Promise<Response>((resolve) => { release = resolve })
    const { chat } = await withPigeon([{ method: 'POST', match: '/chat/turn', respond: () => response }])
    const running = chat.send('Original first question')
    await settle()
    chat.selectPigeon('individualist')
    chat.selectPigeon('biological')
    expect(chat.messages).toEqual([])
    expect(chat.sessionId).toBe('')
    expect(chat.draft).toBe('Original first question')
    expect(chat.error?.reason).toBe('chat_turn_unconfirmed')
    const late = sseResponse([{ type: 'done', sessionId: 'late' }])
    late.headers.set('X-Duif-Session-ID', 'late')
    release(late)
    await running
    expect(chat.sessionId).toBe('')
    expect(chat.messages).toEqual([])
    expect(chat.draft).toBe('Original first question')
  })

  it('resumes the acknowledged first session after switching away mid-stream', async () => {
    const live = liveStream()
    live.response.headers.set('X-Duif-Session-ID', 'first-session')
    let turnsMade = 0
    const { chat, calls } = await withPigeon([turns(() => ++turnsMade === 1 ? live.response : sseResponse([{ type: 'done', sessionId: 'first-session' }]))])
    const running = chat.send('Original context')
    await settle()
    expect(chat.sessionId).toBe('first-session') // Before any SSE frame.
    live.push({ type: 'text', delta: 'Partial first answer' })
    await settle()
    chat.selectPigeon('individualist')
    chat.selectPigeon('biological')
    await running
    expect(chat.messages[1]).toMatchObject({ content: 'Partial first answer', partial: true })
    await chat.send('What about that case?')
    expect(calls.filter((call) => call.url.endsWith('/chat/turn'))[1].body).toMatchObject({ sessionId: 'first-session', message: 'What about that case?' })
  })

  it('retries a lost comparison creation response with the same request ID and asks once', async () => {
    let creates = 0
    const { chat, calls } = await withPigeon([
      originRoute,
      { method: 'POST', match: '/chat/comparisons', respond: () => { if (++creates === 1) throw new Error('response lost after commit'); return jsonResponse({ session: target }, 201) } },
      { method: 'GET', match: '/target/messages', respond: () => jsonResponse({ messages: [] }) },
      turns(() => sseResponse([{ type: 'done', sessionId: 'target' }])),
    ])
    chat.sessions = [origin]
    await chat.openSession(origin.id)
    await chat.beginComparison('q')
    expect(chat.error).not.toBeNull()
    await chat.beginComparison('q')
    const creations = calls.filter((call) => call.url.endsWith('/chat/comparisons')).map((call) => call.body as { requestId: string })
    expect(creations).toHaveLength(2)
    expect(creations[0].requestId).toMatch(/^[a-f0-9]{32}$/)
    expect(creations[1].requestId).toBe(creations[0].requestId)
    expect(calls.filter((call) => call.url.endsWith('/chat/turn'))).toHaveLength(1)
    expect(chat.sessionId).toBe('target')
  })

  it('recovers a stored compare question after a lost turn response without asking the model twice', async () => {
    let targetReads = 0
    const { chat, calls } = await withPigeon([
      originRoute,
      { method: 'POST', match: '/chat/comparisons', respond: () => jsonResponse({ session: target }, 201) },
      { method: 'GET', match: '/target/messages', respond: () => jsonResponse({ messages: ++targetReads === 1 ? [] : [question, { id: 'answer', role: 'assistant', content: 'Already answered' }] }) },
      { method: 'POST', match: '/chat/turn', respond: () => { throw new Error('response lost after question and answer committed') } },
    ])
    chat.sessions = [origin]
    await chat.openSession(origin.id)
    await chat.beginComparison('q')
    expect(chat.error).not.toBeNull()
    chat.selectPigeon('biological')
    await chat.beginComparison('q')
    const creations = calls.filter((call) => call.url.endsWith('/chat/comparisons')).map((call) => call.body as { requestId: string })
    expect(creations[1].requestId).toBe(creations[0].requestId)
    expect(calls.filter((call) => call.url.endsWith('/chat/turn'))).toHaveLength(1)
    expect(chat.messages[1].content).toBe('Already answered')
    expect(chat.draft).toBe('')
  })

  it('keeps a target committed after the reader went back discoverable and resumable without an automatic turn', async () => {
    let release!: (response: Response) => void
    const response = new Promise<Response>((resolve) => { release = resolve })
    const { chat, calls } = await withPigeon([
      originRoute,
      { method: 'POST', match: '/chat/comparisons', respond: () => response },
      { method: 'GET', match: '/target/messages', respond: () => jsonResponse({ messages: [] }) },
    ])
    chat.sessions = [origin]
    await chat.openSession(origin.id)
    const creating = chat.beginComparison('q')
    await settle()
    await chat.openSession(origin.id)
    release(jsonResponse({ session: target }, 201))
    await creating
    expect(chat.sessionId).toBe('origin')
    expect(chat.sessions.find((session) => session.id === 'target')).toEqual(target)
    await chat.openSession('target')
    expect(chat.draft).toBe(question.content)
    expect(calls.filter((call) => call.url.endsWith('/chat/turn'))).toHaveLength(0)
  })
})
