import { fireEvent, render, screen, within } from '@testing-library/svelte'
import { tick } from 'svelte'
import { afterEach, describe, expect, it } from 'vitest'
import type { SessionRef } from '../chat/chatStore.svelte'
import { pigeonRoute, serviceContext, testServices } from '../testing/chat'
import { jsonResponse, scriptedFetch, type Route } from '../testing/fetch'
import ConversationActions from './ConversationActions.svelte'

// De gespreksacties staan in de navigatieregel van de schil, links naast de
// tabs. Dit bestand houdt vast wat daar geldt: de
// lijst is van de gekozen duif, hij blijft staan tot je kiest, een concept gaat
// nooit ongevraagd verloren, en "nieuw gesprek" is één knop die meteen een
// nieuwe chat begint.
async function settle(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
  await tick()
}

const disposers: (() => void)[] = []
afterEach(() => { for (const dispose of disposers.splice(0)) dispose() })

async function view(sessions: SessionRef[] = [], extra: Route[] = []) {
  const scripted = scriptedFetch([
    pigeonRoute(),
    ...extra,
    { method: 'GET', match: '/chat/sessions', respond: () => jsonResponse({ sessions }) },
  ])
  const services = testServices(scripted.fetch)
  disposers.push(() => services.dispose())
  render(ConversationActions, { context: serviceContext(services) })
  // De schil laadt de naamlijst, de duiven en de gesprekken; dit onderdeel
  // toont ze alleen.
  await services.roster.load()
  await services.chat.loadPigeons()
  await services.chat.loadSessions()
  await settle()
  return { services, chat: services.chat, calls: scripted.calls }
}

describe('ConversationActions', () => {
  it('offers exactly history and a new conversation, and shows the empty state', async () => {
    await view()
    const row = screen.getByRole('group', { name: 'Conversations' })
    expect(within(row).getAllByRole('button').map((button) => (button.textContent ?? '').replace(/\s+/g, ' ').trim()))
      .toEqual(['History', 'New conversation'])
    expect(screen.queryByTestId('session-list')).toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: /^History/ }))
    await settle()
    expect(screen.getByTestId('sessions-empty')).toHaveTextContent('No conversations yet. Ask your first question.')
  })

  it('keeps history scoped to the selected pigeon and marks the open one', async () => {
    const sessions: SessionRef[] = [
      { id: 's-a', pigeonId: 'individualist', lang: 'en', createdAt: '2026-09-01T10:00:00Z', updatedAt: '2026-09-02T10:00:00Z' },
      { id: 's-b', pigeonId: 'biological', lang: 'en', createdAt: '2026-08-01T10:00:00Z', updatedAt: '2026-08-01T10:00:00Z' },
    ]
    const { chat } = await view(sessions, [
      { method: 'GET', match: '/messages', respond: () => jsonResponse({ messages: [] }) },
    ])
    await fireEvent.click(screen.getByRole('button', { name: /^History/ }))
    const rows = screen.getAllByRole('button', { name: /Open the conversation with/ })
    expect(rows).toHaveLength(1)
    expect(rows[0]).toHaveTextContent('Knowledge Partner')
    expect(rows[0]).not.toHaveAttribute('aria-current')
    await fireEvent.click(rows[0])
    await settle()
    expect(chat.sessionId).toBe('s-b')
    expect(screen.queryByTestId('session-list')).toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: /^History/ }))
    expect(screen.getAllByRole('button', { name: /Open the conversation with/ })[0]).toHaveAttribute('aria-current', 'true')
  })

  it('keeps the history list open until a conversation is chosen and does not refetch on opening', async () => {
    const sessions: SessionRef[] = [
      { id: 's-a', pigeonId: 'biological', lang: 'en', title: 'Where do pigeons nest?', createdAt: '2026-09-01T10:00:00Z', updatedAt: '2026-09-02T10:00:00Z' },
      { id: 's-b', pigeonId: 'biological', lang: 'en', title: 'What do pigeons eat?', createdAt: '2026-08-01T10:00:00Z', updatedAt: '2026-08-01T10:00:00Z' },
    ]
    const { chat, calls } = await view(sessions, [{ method: 'GET', match: '/messages', respond: () => jsonResponse({ messages: [] }) }])
    const before = calls.filter((call) => call.url.endsWith('/chat/sessions')).length
    await fireEvent.click(screen.getByRole('button', { name: /^History/ }))
    await settle()
    // Openen haalt de lijst niet opnieuw op: dat verving de rijen onder de
    // aanwijzer terwijl de lezer aan het klikken was.
    expect(calls.filter((call) => call.url.endsWith('/chat/sessions'))).toHaveLength(before)
    expect(screen.getByTestId('session-list')).toBeInTheDocument()
    // De lijst blijft staan zolang er niets gekozen is, ook na een vernieuwing.
    await fireEvent.click(screen.getByRole('button', { name: 'Refresh history' }))
    await settle()
    expect(screen.getByTestId('session-list')).toBeInTheDocument()
    // Eén klik op een gesprek opent dat gesprek en sluit de lijst.
    const rows = screen.getAllByRole('button', { name: /Open the conversation with/ })
    expect(rows).toHaveLength(2)
    await fireEvent.click(rows[1])
    await settle()
    expect(chat.sessionId).toBe('s-b')
    expect(screen.queryByTestId('session-list')).toBeNull()
  })

  it('asks before a new conversation throws an unsent draft away', async () => {
    const prototype = HTMLDialogElement.prototype
    const show = Object.getOwnPropertyDescriptor(prototype, 'showModal')
    const close = Object.getOwnPropertyDescriptor(prototype, 'close')
    Object.defineProperty(prototype, 'showModal', { configurable: true, value(this: HTMLDialogElement) { this.open = true } })
    Object.defineProperty(prototype, 'close', { configurable: true, value(this: HTMLDialogElement) { this.open = false } })
    try {
      const { chat } = await view()
      const row = screen.getByRole('group', { name: 'Conversations' })
      chat.draft = 'A question I have not sent yet'
      await fireEvent.click(within(row).getByRole('button', { name: 'New conversation' }))
      await settle()
      expect(chat.draft).toBe('A question I have not sent yet')
      await fireEvent.click(screen.getByRole('button', { name: 'Keep this conversation' }))
      await settle()
      expect(chat.draft).toBe('A question I have not sent yet')

      await fireEvent.click(within(row).getByRole('button', { name: 'New conversation' }))
      await settle()
      const dialog = screen.getByText('Start a clean conversation?').closest('dialog')
      expect(dialog).not.toBeNull()
      await fireEvent.click(within(dialog as HTMLElement).getByRole('button', { name: 'New conversation' }))
      await settle()
      expect(chat.draft).toBe('')
    } finally {
      if (show) Object.defineProperty(prototype, 'showModal', show); else delete (prototype as unknown as Record<string, unknown>).showModal
      if (close) Object.defineProperty(prototype, 'close', close); else delete (prototype as unknown as Record<string, unknown>).close
    }
  })

  // De keuze tussen chatten en vergaderen staat in de tabs bovenin;
  // hier hoeft zij niet nog een keer gesteld te worden. Eén klik begint dus
  // meteen een nieuwe chat, en de knop is een gewone knop: geen menuknop, geen
  // menu, ook niet met het toetsenbord.
  it('begint met één klik een nieuwe chat en draagt geen menu', async () => {
    const { chat } = await view()
    const button = screen.getByRole('button', { name: 'New conversation' })
    expect(button).not.toHaveAttribute('aria-haspopup')
    expect(button).not.toHaveAttribute('aria-expanded')

    await fireEvent.keyDown(button, { key: 'ArrowDown' })
    await settle()
    expect(screen.queryByRole('menu')).toBeNull()

    chat.sessionId = 'een-lopend-gesprek'
    await fireEvent.click(button)
    await settle()
    expect(screen.queryByRole('menu')).toBeNull()
    expect(chat.sessionId).toBe('')
  })
})
