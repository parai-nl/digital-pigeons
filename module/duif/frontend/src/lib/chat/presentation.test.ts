import { render } from '@testing-library/svelte'
import { describe, expect, it } from 'vitest'
import { authorId, repeatedQuestionKey, visibleMessages } from './presentation'
import AssistantMessage from './AssistantMessage.svelte'
import { CONVERSATION_CASES, followups } from './cases'
import type { ChatMessage, ComparisonOrigin } from './chatStore.svelte'
import { serviceContext, SOURCE, testServices } from '../testing/chat'
import { scriptedFetch } from '../testing/fetch'

const message = (patch: Partial<ChatMessage> = {}): ChatMessage => ({
  key: 'a', role: 'assistant', kind: 'text', content: 'A bounded answer.', sources: [], tools: [],
  streaming: false, partial: false, ...patch,
})
const provenance = { contractVersion: 'v1', personaVersion: 'p', promptVersion: 'q', model: 'model', kbState: 'versioned' }

describe('conversation presentation contract', () => {
  it('hides new normal biological guidance without reattributing historical guidance', () => {
    const rows = [message({ key: 'u', role: 'user' }), message({ key: 'p', kind: 'pigeon_answer', persona: 'biological' }), message({ key: 'f', persona: 'facilitator', provenance })]
    expect(visibleMessages(rows, 'biological').map(m => m.key)).toEqual(['u', 'p'])
    expect(visibleMessages(rows.map(m => ({ ...m, provenance: undefined })), 'biological').map(m => m.key)).toEqual(['u', 'p', 'f'])
    expect(visibleMessages(rows, 'individualist')).toHaveLength(3)
  })
  it('keeps both mediating facilitator rows for the individualist and hides them for the knowledge partner', () => {
    // Rond de individualist staat de facilitator er twee
    // keer — een kader vóór haar blok en een bemiddeling erna. Rond de
    // kennispartner blijft hij intern, dus dan verdwijnen ze allebei zodra
    // zij werkelijk antwoordde.
    const rows = [
      message({ key: 'u', role: 'user' }),
      message({ key: 'kader', persona: 'facilitator', provenance, content: 'I am putting the residents’ concern to her.' }),
      message({ key: 'duif', kind: 'pigeon_answer', persona: 'individualist' }),
      message({ key: 'bemiddeling', persona: 'facilitator', provenance, content: 'She will not trade the ledge away.' }),
    ]
    expect(visibleMessages(rows, 'individualist').map(m => m.key)).toEqual(['u', 'kader', 'duif', 'bemiddeling'])
    expect(visibleMessages(rows, 'biological').map(m => m.key)).toEqual(['u', 'duif'])
  })
  it('drops the guidance filler row in both languages, whichever pigeon is chosen', () => {
    // De backend stuurt deze regel niet meer, maar oude gesprekken dragen hem
    // nog. Een rij die alleen zegt dat het antwoord hierboven staat, is geen
    // begeleiding en verdwijnt — ook bij de stakeholder, waar
    // facilitatorrijen verder gewoon zichtbaar blijven.
    const filler = (content: string) => message({ key: 'f', persona: 'facilitator', provenance, content })
    for (const content of ['The pigeon’s answer is shown above.', "The pigeon's answer is shown above.", 'Het antwoord van de duif staat hierboven.', '  Het antwoord van de duif staat hierboven.  ']) {
      const rows = [message({ key: 'u', role: 'user' }), filler(content)]
      expect(visibleMessages(rows, 'biological').map(m => m.key)).toEqual(['u'])
      expect(visibleMessages(rows, 'individualist').map(m => m.key)).toEqual(['u'])
    }
    // Echte begeleiding blijft staan, met haar eigen auteurschap.
    const real = [message({ key: 'u', role: 'user' }), filler('Which location do you mean?')]
    expect(visibleMessages(real, 'individualist').map(m => m.key)).toEqual(['u', 'f'])
  })
  it('retains necessary clarification and operational failures as guidance, never as a pigeon', () => {
    const rows = [message({ key: 'u', role: 'user' }), message({ key: 'f', persona: 'facilitator', provenance, content: 'Which location do you mean?' })]
    expect(visibleMessages(rows, 'biological')).toHaveLength(2)
    expect(authorId(rows[1])).toBe('facilitator')
    expect(visibleMessages([message({ persona: 'facilitator', partial: true, content: 'Interrupted', provenance })], 'biological')).toHaveLength(1)
    expect(authorId(message())).toBe('')
    expect(authorId(message({ pigeonId: 'biological' }))).toBe('biological')
  })
  it('marks only the row that repeats the copied question, counted from the copy', () => {
    // De kopie loopt tot en met het antwoord van de eerste duif; de rij
    // daarna is de vraag die aan de tweede duif gesteld werd.
    const origin = (ids: string[]): ComparisonOrigin => ({
      groupId: 'g', sessionId: 'origin', pigeonId: 'biological', questionId: 'q',
      originalQuestion: 'Where do pigeons nest?', contextMessageIds: ids,
    })
    const ask = (key: string, content = 'Where do pigeons nest?') => message({ key, role: 'user', kind: 'text', content })
    const copied = [ask('copied-question'), message({ key: 'copied-answer', kind: 'pigeon_answer', persona: 'biological' })]
    const ids = ['src-question', 'src-answer']

    const rows = [...copied, ask('repeated'), message({ key: 'second-answer', kind: 'pigeon_answer', persona: 'individualist' })]
    expect(repeatedQuestionKey(rows, origin(ids))).toBe('repeated')
    // Zonder vergelijking, en zolang de vraag nog niet opnieuw gesteld is,
    // verandert er niets aan de weergave.
    expect(repeatedQuestionKey(rows, undefined)).toBe('')
    expect(repeatedQuestionKey(copied, origin(ids))).toBe('')
    // Eén teken verschil is een nieuwe vraag, en zo hoort hij te lezen.
    expect(repeatedQuestionKey([...copied, ask('other', 'Where do pigeons nest? ')], origin(ids))).toBe('')
    // Een antwoord op die plek (een herstelde tak) is geen herhaalde vraag.
    expect(repeatedQuestionKey([...copied, message({ key: 'answer-first' })], origin(ids))).toBe('')

    // Ketting: het brongesprek was zelf al een vergelijking, dus dezelfde zin
    // staat al twee keer in de kopie. Geteld vanaf de kopie wijst dat nog
    // steeds de derde aan, en niet de eerste die toevallig gelijk leest.
    const chained = [
      ask('c1'), message({ key: 'c2', kind: 'pigeon_answer', persona: 'biological' }),
      ask('c3'), message({ key: 'c4', kind: 'pigeon_answer', persona: 'individualist' }),
      ask('c5'), message({ key: 'c6', kind: 'pigeon_answer', persona: 'biological' }),
    ]
    expect(repeatedQuestionKey(chained, origin(['a', 'b', 'c', 'd']))).toBe('c5')
    // Een gewone vervolgvraag verderop in hetzelfde vergelijkgesprek blijft
    // een gewone bubbel, ook als hij later dezelfde woorden gebruikt.
    expect(repeatedQuestionKey([...chained, ask('later')], origin(['a', 'b', 'c', 'd']))).toBe('c5')
  })

  it('never suggests an uncompleted, rejected, unsupported or failed follow-up', () => {
    const answer = message({ kbStatus: 'sources', sources: [SOURCE] })
    expect(followups(answer, 'Where can pigeons nest?')[0].question.en).toContain('Where can pigeons nest?')
    for (const patch of [{ partial: true }, { streaming: true }, { stopReason: 'refusal' as const }, { kbStatus: 'unavailable' as const }, { sources: [] }]) {
      expect(followups({ ...answer, ...patch }, 'Where?')).toEqual([])
    }
    const gap = followups(message({ kbStatus: 'gap' }), 'Where?')
    expect(gap).toHaveLength(1)
    expect(gap[0].id).toBe('limits')
    expect(followups(answer, '')).toEqual([])
  })
  it('keeps bounded bilingual cases with reviewable learning goals and coverage needs', () => {
    expect(new Set(CONVERSATION_CASES.map(c => c.id)).size).toBe(CONVERSATION_CASES.length)
    for (const item of CONVERSATION_CASES) {
      expect(item.question.en.length).toBeGreaterThan(20)
      expect(item.question.nl.length).toBeGreaterThan(20)
      expect(item.learningGoal).not.toBe('')
      expect(item.coverage.length).toBeGreaterThan(0)
      expect(item.gapExample).not.toBe('')
    }
  })
})

describe('facilitator card', () => {
  // De kaart van de facilitator hangt in CSS aan het vak dát de tekst draagt
  // (`div:has(> [data-testid='assistant-content'])`) en niet aan een plaats in
  // de kolom van MessageBubble. jsdom past geen stijlen toe, maar lost de
  // selector wél op: deze test bewaakt dat hij precies één vak aanwijst en dat
  // dat het vak met de tekst is. Krijgt MessageBubble ooit een kop of een
  // persona-blok, dan valt dat hier om in plaats van stil in beeld.
  const CARD = "[data-testid='message-bubble'] > div > div:has(> [data-testid='assistant-content'])"
  it('binds to the box that carries the text, not to a position in the column', () => {
    const services = testServices(scriptedFetch([]).fetch)
    const guidance = message({
      key: 'bemiddeling', persona: 'facilitator', kind: 'text',
      content: 'Between the square and the pigeon sits a choice about space.',
    })
    const { container } = render(AssistantMessage, {
      props: { message: guidance, personaName: 'Facilitator' },
      context: serviceContext(services),
    })
    const matched = container.querySelectorAll(CARD)
    expect(matched).toHaveLength(1)
    expect(matched[0].querySelector("[data-testid='assistant-content']")).not.toBeNull()
    expect(matched[0].textContent).toContain('Between the square and the pigeon sits a choice about space.')
  })
})
