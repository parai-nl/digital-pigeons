import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import FirstUseNotice from './FirstUseNotice.svelte'
import { noticeVersion } from './noticeCopy'
import { serviceContext, testServices } from '../testing/chat'
import { jsonResponse } from '../testing/fetch'
import { UiLocale } from '../i18n.svelte'
import type { SessionRef } from '../chat/chatStore.svelte'
import ConversationActions from '../shell/ConversationActions.svelte'
import { pigeonRoute } from '../testing/chat'
import { scriptedFetch } from '../testing/fetch'
import { applyEntryIntent } from './entryIntent'
afterEach(() => { cleanup(); vi.unstubAllGlobals() })

it('restores only a public pigeon/case after login and never sends URL text', async () => {
  const routes = scriptedFetch([pigeonRoute()])
  const services = testServices(routes.fetch, { locale: new UiLocale('nl') })
  await applyEntryIntent(services, '?pigeon=individualist&case=square-event&message=private-untrusted')
  expect(services.chat.selectedPigeonId).toBe('individualist')
  expect(services.chat.draft).toContain('Rotterdams plein')
  expect(services.chat.draft).not.toContain('private-untrusted')
  expect(services.chat.sessionId).toBe('')
  expect(services.meeting.phase).toBe('setup')
  services.dispose()
})

it('requires an explicit version acknowledgement without microphone or message calls', async () => {
  const fetch = vi.fn().mockResolvedValueOnce(jsonResponse({ version: noticeVersion, acknowledged: false })).mockResolvedValueOnce(jsonResponse({ version: noticeVersion, acknowledged: true }))
  vi.stubGlobal('fetch', fetch)
  const services = testServices(fetch, { locale: new UiLocale('nl') })
  render(FirstUseNotice, { context: serviceContext(services) })
  expect(await screen.findByRole('button', { name: 'Ik heb dit gelezen — verder' })).toBeInTheDocument()
  expect(fetch).toHaveBeenCalledTimes(1)
  expect(screen.getByText(/externe diensten niets bewaren/)).toBeInTheDocument()
  await fireEvent.click(screen.getByRole('button', { name: 'Ik heb dit gelezen — verder' }))
  await waitFor(() => expect(screen.queryByRole('heading')).not.toBeInTheDocument())
  expect(fetch.mock.calls[1][1].body).toBe(JSON.stringify({ version: noticeVersion }))
  expect(fetch.mock.calls.every((call) => call[0] === '/api/duif/v1/access/notice')).toBe(true)
  services.dispose()
})

it('does not accept an unknown/newer explanation or a failed acknowledgement', async () => {
  const fetch = vi.fn().mockResolvedValue(jsonResponse({ version: 'future', acknowledged: true }))
  vi.stubGlobal('fetch', fetch)
  const services = testServices(fetch)
  render(FirstUseNotice, { context: serviceContext(services) })
  expect(await screen.findByRole('alert')).toHaveTextContent('could not be checked')
  expect(screen.queryByRole('button', { name: 'I have read this — continue' })).not.toBeInTheDocument()
  services.dispose()
})

it('keeps deleted conversations and origin links out of a stale history response', async () => {
  let resolve!: (value: Response) => void
  const fetch = vi.fn((_url: unknown, init?: RequestInit) => init?.method === 'DELETE' ? Promise.resolve(new Response(null, { status: 204 })) : new Promise<Response>((r) => { resolve = r }))
  const services = testServices(fetch)
  const origin = { id: 'origin', pigeonId: 'biological', lang: 'en', title: 'Old question', updatedAt: '2026-09-07', createdAt: '2026-09-07' } as SessionRef
  const target: SessionRef = { ...origin, id: 'target', pigeonId: 'individualist', comparison: { sessionId: 'origin', groupId: 'group', pigeonId: 'biological', questionId: 'question', contextMessageIds: [], originalQuestion: 'Old question' } }
  services.chat.sessions = [origin, target]
  services.chat.sessionId = 'origin'
  services.chat.selectedPigeonId = 'biological'
  services.chat.draft = 'private unsent question'
  const flight = services.chat.loadSessions(true)
  expect(await services.chat.deleteSession('origin')).toBe(true)
  resolve(jsonResponse({ sessions: [origin, target] }))
  await flight
  expect(services.chat.sessions.map((s) => s.id)).toEqual(['target'])
  expect(services.chat.sessions[0].comparison).toBeUndefined()
  expect(services.chat.sessionId).toBe('')
  expect(services.chat.draft).toBe('')
  services.dispose()
})

it('requires explicit deletion confirmation, keeps cancel focus and reports failure without hiding history', async () => {
  // jsdom has the dialog element but no browser top-layer implementation.
  const prototype = HTMLDialogElement.prototype
  const show = Object.getOwnPropertyDescriptor(prototype, 'showModal')
  const close = Object.getOwnPropertyDescriptor(prototype, 'close')
  Object.defineProperty(prototype, 'showModal', { configurable: true, value(this: HTMLDialogElement) { this.open = true } })
  Object.defineProperty(prototype, 'close', { configurable: true, value(this: HTMLDialogElement) { this.open = false } })
  try {
  const session = { id: 'one', pigeonId: 'biological', lang: 'en', title: 'Feeding in the square', createdAt: '2026-09-07', updatedAt: '2026-09-07' }
  const routes = scriptedFetch([pigeonRoute(), { method: 'GET', match: '/chat/sessions', respond: () => jsonResponse({ sessions: [session] }) }, { method: 'DELETE', match: '/chat/sessions/one', respond: () => new Response(null, { status: 503 }) }])
  const services = testServices(routes.fetch)
  render(ConversationActions, { context: serviceContext(services) })
  await services.chat.loadPigeons()
  await services.chat.loadSessions()
  await fireEvent.click(screen.getByRole('button', { name: /^History$/ }))
  const remove = await screen.findByRole('button', { name: 'Delete: Feeding in the square' })
  await fireEvent.click(remove)
  const keep = screen.getByRole('button', { name: 'Keep conversation' })
  expect(keep).toHaveFocus()
  await fireEvent.click(keep)
  await waitFor(() => expect(remove).toHaveFocus())
  expect(services.chat.sessions).toHaveLength(1)
  await fireEvent.click(remove)
  await fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Deletion could not be confirmed')
  expect(services.chat.sessions).toHaveLength(1)
  services.dispose()
  } finally {
    if (show) Object.defineProperty(prototype, 'showModal', show); else delete (prototype as unknown as Record<string, unknown>).showModal
    if (close) Object.defineProperty(prototype, 'close', close); else delete (prototype as unknown as Record<string, unknown>).close
  }
})
