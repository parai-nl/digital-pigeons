import { render, screen } from '@testing-library/svelte'
import { describe, expect, it } from 'vitest'
import AssistantMessage from '../chat/AssistantMessage.svelte'
import type { ChatMessage } from '../chat/chatStore.svelte'
import { serviceContext, testServices } from '../testing/chat'
import { scriptedFetch } from '../testing/fetch'
import { readDevMode } from './devMode'

const answer: ChatMessage = {
  key: 'a1', role: 'assistant', kind: 'pigeon_answer', pigeonId: 'biological',
  content: 'Pigeons nest on ledges.', sources: [], tools: [], partial: false, streaming: false,
  provenance: { contractVersion: 'c1', personaVersion: 'p1', promptVersion: 'i1', model: 'vertex/gemini-3.6-flash-eu', kbState: 'ready', kbVersion: 'kb1' },
}

describe('readDevMode', () => {
  it('turns on only with ?dev=1 for this page load and remembers nothing', () => {
    expect(readDevMode('')).toBe(false)
    expect(readDevMode('?dev=1')).toBe(true)
    expect(readDevMode('?dev=0')).toBe(false)
    expect(readDevMode('?pigeon=biological')).toBe(false)
    expect(readDevMode('?dev=1&pigeon=biological')).toBe(true)
  })
})

describe('build evidence behind the developer switch', () => {
  function show(dev: boolean) {
    const services = testServices(scriptedFetch([]).fetch)
    services.shell.dev = dev
    render(AssistantMessage, { props: { message: answer, personaName: 'Knowledge Partner' }, context: serviceContext(services) })
  }

  it('keeps model, behaviour contract and version hashes off the reader\'s screen', () => {
    show(false)
    expect(screen.queryByTestId('answer-version')).toBeNull()
    expect(screen.queryByText('About this answer')).toBeNull()
    expect(screen.queryByText('vertex/gemini-3.6-flash-eu')).toBeNull()
  })

  it('shows them, still folded away, once the developer switch is on', () => {
    show(true)
    const panel = screen.getByTestId('answer-version')
    expect(panel.tagName).toBe('DETAILS')
    expect(panel).not.toHaveAttribute('open')
    expect(panel).toHaveTextContent('vertex/gemini-3.6-flash-eu')
  })
})
