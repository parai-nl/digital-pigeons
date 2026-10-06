import { afterEach, expect, it } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'
import { pigeonRoute, SOURCE, serviceContext, testServices } from '../testing/chat'
import { jsonResponse, scriptedFetch } from '../testing/fetch'
import ChatView from './ChatView.svelte'
import type { ChatMessage } from './chatStore.svelte'

// Vergelijken is geen scherm meer maar één gesprek: de vraag verhuist en het
// antwoord staat in de gewone chat, met één rustige herkomstregel die de duif
// noemt waar de context vandaan komt. Wat hier vastligt is het auteurschap
// (een gekopieerd antwoord houdt zijn eigen duif) en de ene weg terug.
const dispose: { dispose(): void }[] = []
afterEach(() => { cleanup(); for (const x of dispose.splice(0)) x.dispose() })
const settle = async () => { await new Promise((r) => setTimeout(r, 0)); await tick() }

const row = (patch: Partial<ChatMessage>): ChatMessage => ({
  key: 'answer', role: 'assistant', kind: 'pigeon_answer', persona: 'individualist',
  content: 'The actual comparison answer [1].', sources: [SOURCE], tools: [],
  partial: false, streaming: false, ...patch,
})

async function comparison(originalPersona = 'biological') {
  const scripted = scriptedFetch([
    pigeonRoute(),
    { method: 'GET', match: '/messages', respond: () => jsonResponse({ messages: [{ id: 'q-original', role: 'user', content: 'The repeated question?' }, { id: 'a-original', role: 'assistant', kind: 'pigeon_answer', persona: originalPersona, content: 'Original attributed answer [1].', sources: [SOURCE] }] }) },
  ])
  const s = testServices(scripted.fetch)
  dispose.push(s)
  await s.roster.load()
  await s.chat.loadPigeons()
  s.chat.selectedPigeonId = 'individualist'
  s.chat.sessionId = 'target'
  s.chat.sessions = [{ id: 'target', pigeonId: 'individualist', lang: 'en', createdAt: '2026-09-07', updatedAt: '2026-09-07', comparison: { groupId: 'g', sessionId: 'origin', pigeonId: 'biological', questionId: 'q-original', originalQuestion: 'The repeated question?', contextMessageIds: ['old-context', 'old-answer'] } }]
  return s
}

it('presents a comparison branch as one quiet origin line with a single way back', async () => {
  const s = await comparison()
  s.chat.messages = [row({ key: 'actual-question', role: 'user', content: 'The repeated question?' }), row({})]
  render(ChatView, { context: serviceContext(s) })
  await settle()
  expect(screen.getByText('Continued from your conversation with Knowledge Partner')).toBeInTheDocument()
  const back = screen.getAllByRole('button', { name: 'Back to that conversation' })
  expect(back).toHaveLength(1)
  await fireEvent.click(back[0])
  await settle()
  expect(s.chat.sessionId).toBe('origin')
})

it('keeps a comparison answer attributed to its own recorded pigeon', async () => {
  const s = await comparison()
  // Het antwoord draagt persona 'individualist'; de bovenaan gekozen duif mag
  // dat niet overschrijven, ook niet nadat er van gesprek gewisseld is.
  s.chat.messages = [row({ key: 'actual-question', role: 'user', content: 'The repeated question?', persona: undefined }), row({})]
  const { container } = render(ChatView, { context: serviceContext(s) })
  await settle()
  const answer = container.querySelector('[data-author="individualist"] .answer-author')
  expect(answer).toHaveTextContent('Stakeholder')
})

it('shows both voices of the copied conversation with their own authors', async () => {
  const s = await comparison()
  // De meegereisde context: een eigen bericht en het antwoord van de eerste
  // duif, gevolgd door de herhaalde vraag en het antwoord van de tweede duif.
  s.chat.messages = [
    row({ key: 'copied-question', role: 'user', kind: 'text', content: 'The repeated question?', persona: undefined, sources: [] }),
    row({ key: 'copied-answer', persona: 'biological', content: 'Original attributed answer [1].' }),
    row({ key: 'actual-question', role: 'user', kind: 'text', content: 'The repeated question?', persona: undefined, sources: [] }),
    row({}),
  ]
  const { container } = render(ChatView, { context: serviceContext(s) })
  await settle()
  const authors = [...container.querySelectorAll('[data-author]')].map((el) => el.getAttribute('data-author'))
  expect(authors).toEqual(['biological', 'individualist'])
  expect(container.querySelector('[data-author="biological"] .answer-author')).toHaveTextContent('Knowledge Partner')
  expect(container.querySelector('[data-author="individualist"] .answer-author')).toHaveTextContent('Stakeholder')
})
