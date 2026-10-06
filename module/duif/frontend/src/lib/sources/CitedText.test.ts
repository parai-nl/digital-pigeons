import { render, screen } from '@testing-library/svelte'
import { describe, expect, it } from 'vitest'
import AssistantMessage from '../chat/AssistantMessage.svelte'
import type { ChatMessage } from '../chat/chatStore.svelte'
import MeetingAnswer from '../meeting/MeetingAnswer.svelte'
import type { AnswerLine } from '../meeting/meetingStore.svelte'
import { serviceContext, SOURCE, testServices } from '../testing/chat'
import { scriptedFetch } from '../testing/fetch'

// De citatiemarkering is een eigen inline-vak in de zin, en tussen dat vak en
// het leesteken erna ziet de browser een afbreekkans: een punt of komma zakte
// daardoor los naar de volgende regel. Markering en leesteken horen in één
// wikkel die niet afbreekt. Chat en vergadering renderen allebei door
// CitedText heen, dus geldt de fix voor allebei — en wordt hij hier voor
// allebei vastgelegd.

const SECOND = { ...SOURCE, number: 2, text: 'Second supporting source' }

function marks(): HTMLElement[] {
  return screen.getAllByTestId('cite-hold')
}

function meeting(text: string, sources = [SOURCE]) {
  const services = testServices(scriptedFetch([]).fetch)
  const answer: AnswerLine = {
    id: 1, pigeonId: 'biological', text, note: '', status: 'complete', kbStatus: 'sources', sources, startedAt: 0,
  }
  render(MeetingAnswer, {
    props: { locale: services.locale, answer },
    context: serviceContext(services),
  })
}

function chat(content: string, sources = [SOURCE]) {
  const services = testServices(scriptedFetch([]).fetch)
  const message: ChatMessage = {
    key: 'a1', role: 'assistant', kind: 'pigeon_answer', pigeonId: 'biological',
    content, sources, tools: [], partial: false, streaming: false,
  }
  render(AssistantMessage, {
    props: { message, personaName: 'Knowledge Partner' },
    context: serviceContext(services),
  })
}

describe('CitedText keeps punctuation with its citation mark', () => {
  it('wraps the mark and the sentence-closing period in one unbreakable run', () => {
    meeting('Pigeons nest on ledges [1]. And they stay.')
    const [hold] = marks()
    expect(hold.textContent).toBe('1.')
    // De punt staat niet meer los in de lopende tekst: hij zit in de wikkel.
    expect(screen.getByTestId('answer-text').textContent).toBe('Pigeons nest on ledges1. And they stay.')
  })

  it('does the same in a chat answer', () => {
    chat('Pigeons nest on ledges [1], mostly.')
    const [hold] = marks()
    expect(hold.textContent).toBe('1,')
    expect(screen.getByTestId('assistant-content').textContent).toBe('Pigeons nest on ledges1, mostly.')
  })

  it('holds two adjacent marks and the punctuation after them together', () => {
    meeting('Both agree [1,2]. Nothing else.', [SOURCE, SECOND])
    const [hold] = marks()
    expect(hold.textContent).toBe('12.')
    expect(hold.querySelectorAll('button')).toHaveLength(2)
  })

  it('leaves a mark that is followed by a word alone', () => {
    meeting('As noted [1] the ledge matters.')
    const [hold] = marks()
    expect(hold.textContent).toBe('1')
    expect(screen.getByTestId('answer-text').textContent).toBe('As noted1 the ledge matters.')
  })

  it('keeps every mark in its own run when a sentence carries two of them', () => {
    meeting('One [1]. Two [2]!', [SOURCE, SECOND])
    expect(marks().map((hold) => hold.textContent)).toEqual(['1.', '2!'])
  })
})
