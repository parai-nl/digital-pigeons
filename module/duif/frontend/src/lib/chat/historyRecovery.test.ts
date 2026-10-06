import { describe, expect, it } from 'vitest'
import { UiLocale } from '../i18n.svelte'
import { pigeonRoute } from '../testing/chat'
import { jsonResponse, scriptedFetch, sseResponse } from '../testing/fetch'
import { ChatStore, type SessionRef } from './chatStore.svelte'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}
const saved: SessionRef = { id: 'recovered', pigeonId: 'biological', lang: 'en', title: 'Earlier question', createdAt: '2026-09-07T10:00:00Z', updatedAt: '2026-09-07T10:00:00Z' }

describe('history recovery', () => {
  it('coalesces mount loads and refreshes a previously empty list without changing the current draft or opening a session', async () => {
    const initial = deferred<Response>()
    let reads = 0
    const routes = scriptedFetch([{ match: '/chat/sessions', respond: () => ++reads === 1 ? initial.promise : jsonResponse({ sessions: [saved] }) }])
    const chat = new ChatStore({ fetch: routes.fetch }, new UiLocale())
    const first = chat.loadSessions()
    const second = chat.loadSessions()
    expect(reads).toBe(1)
    initial.resolve(jsonResponse({ sessions: [] }))
    await Promise.all([first, second])
    chat.draft = 'My recovered unsent question'
    await chat.loadSessions()
    expect(reads).toBe(1)
    await chat.loadSessions(true)
    expect(chat.sessions).toEqual([saved])
    expect(chat.sessionId).toBe('')
    expect(chat.draft).toBe('My recovered unsent question')
  })

  it('does not erase a newly confirmed conversation when an older list snapshot arrives', async () => {
    const snapshot = deferred<Response>()
    const routes = scriptedFetch([pigeonRoute(),
      { match: '/chat/sessions', respond: () => snapshot.promise },
      { method: 'POST', match: '/chat/turn', respond: () => sseResponse([{ type: 'text', delta: 'An answer.' }, { type: 'done', sessionId: 'new-session' }]) },
    ])
    const chat = new ChatStore({ fetch: routes.fetch }, new UiLocale())
    await chat.loadPigeons()
    const loading = chat.loadSessions()
    await chat.send('A new question')
    expect(chat.sessionId).toBe('new-session')
    snapshot.resolve(jsonResponse({ sessions: [saved] }))
    await loading
    expect(chat.sessions.map((s) => s.id)).toEqual(['new-session', 'recovered'])
    expect(chat.sessions[0].title).toBe('A new question')
  })

  it('isolates refresh failure from the active turn and permits recovery', async () => {
    let fail = true
    const routes = scriptedFetch([{ match: '/chat/sessions', respond: () => fail ? jsonResponse({ error: 'unavailable' }, 503) : jsonResponse({ sessions: [saved] }) }])
    const chat = new ChatStore({ fetch: routes.fetch }, new UiLocale())
    chat.sessions = [saved]
    chat.error = { status: 429, reason: 'llm_budget_exceeded' }
    await chat.loadSessions(true)
    expect(chat.sessionsError?.status).toBe(503)
    expect(chat.error.reason).toBe('llm_budget_exceeded')
    expect(chat.sessions).toEqual([saved])
    fail = false
    await chat.loadSessions(true)
    expect(chat.sessionsError).toBeNull()
  })

  it('ignores a failure body that finishes after disposal', async () => {
    const body = deferred<unknown>()
    const response = new Response('', { status: 503 })
    response.json = () => body.promise
    const chat = new ChatStore({ fetch: async () => response }, new UiLocale())
    const loading = chat.loadSessions()
    await Promise.resolve()
    chat.dispose()
    body.resolve({ error: 'late', message: { en: 'Too late', nl: 'Te laat' } })
    await loading
    expect(chat.sessionsError).toBeNull()
    expect(chat.error).toBeNull()
  })
})
