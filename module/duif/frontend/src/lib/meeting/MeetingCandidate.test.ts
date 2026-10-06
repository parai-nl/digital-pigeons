// Het kandidaatblok: wat de zaal ziet zodra de duif iets klaar heeft
// liggen. De bijdrage zelf staat er nooit in — die hoor je pas als ze klinkt.
import { fireEvent, render, screen } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import { describe, expect, it } from 'vitest'
import { SERVICES } from '../services.svelte'
import { scriptedFetch } from '../testing/fetch'
import { meetingHarness, meetingRoutes } from '../testing/meeting'
import MeetingCandidate from './MeetingCandidate.svelte'
import type { MeetingCandidate as Candidate } from './protocol'

const tick = () => new Promise((resolve) => setTimeout(resolve, 0))

async function mount(candidate: Candidate | null, lang: 'en' | 'nl' = 'nl') {
  const scripted = scriptedFetch(meetingRoutes())
  const harness = meetingHarness(scripted.fetch)
  const services = harness.services()
  services.locale.choose(lang)
  harness.store.candidate = candidate
  render(MeetingCandidate, {
    props: { store: harness.store, locale: services.locale },
    context: new Map([[SERVICES, services]]),
  })
  await tick()
  flushSync()
  return { ...harness, calls: scripted.calls }
}

const held: Candidate = {
  id: 'c1',
  topic: { id: 'urban-ecology', label: { en: 'Urban ecology', nl: 'Stadsecologie' } },
  score: 0.7,
  reason: 'topic',
  addressed: false,
  sources: 2,
}

describe('MeetingCandidate', () => {
  it('says what the pigeon has something about, and never what she would say', async () => {
    await mount(held)
    const block = screen.getByTestId('meeting-candidate')
    expect(block).toHaveTextContent('De duif heeft iets te zeggen over dit thema: Stadsecologie')
    // Het label is een titel met hoofdletter; het staat als eigen
    // label achter de zin, niet midden in de zin.
    expect(screen.getByTestId('candidate-topic')).toHaveTextContent(/^Stadsecologie$/)
    // Zij wacht niet meer op een stilte, dus dat belooft het blok ook niet.
    expect(block).toHaveTextContent('Ze breekt zelf in; deze knop is de snelweg.')
    expect(block).not.toHaveTextContent('stilte')
    expect(screen.getByTestId('candidate-sources')).toHaveTextContent('2 bronnen')
    expect(screen.getByTestId('candidate-speak')).toBeInTheDocument()
  })

  it('counts one source as one source', async () => {
    await mount({ ...held, sources: 1 })
    expect(screen.getByTestId('candidate-sources')).toHaveTextContent('1 bron')
    expect(screen.getByTestId('candidate-sources')).not.toHaveTextContent('1 bronnen')
  })

  it('names being addressed instead of a theme, and says the answer waits for the sentence', async () => {
    await mount({ ...held, reason: 'addressed', addressed: true }, 'en')
    const block = screen.getByTestId('meeting-candidate')
    expect(block).toHaveTextContent('The pigeon has an answer for you')
    // De aanspreekroute is de enige die nog wacht, en alleen op het zinseinde.
    expect(block).toHaveTextContent('It answers as soon as the sentence is finished.')
    expect(block).not.toHaveTextContent('pause in the conversation')
  })

  it('promises no pause in either language when the pigeon simply breaks in', async () => {
    await mount(held, 'en')
    const block = screen.getByTestId('meeting-candidate')
    expect(block).toHaveTextContent('It breaks in by itself; this button is the shortcut.')
    expect(block).not.toHaveTextContent('waiting for a pause')
  })

  it('gives the signaling pigeon no theme and no meaning', async () => {
    await mount({ id: 'c2', topic: null, score: 0, reason: 'signal', addressed: false, sources: 0 })
    const block = screen.getByTestId('meeting-candidate')
    expect(block).toHaveTextContent('De duif wil een geluid geven')
    expect(block).not.toHaveTextContent('Stadsecologie')
    expect(screen.queryByTestId('candidate-sources')).toBeNull()
  })

  it('falls back to a general line without a theme', async () => {
    await mount({ ...held, topic: null })
    expect(screen.getByTestId('meeting-candidate')).toHaveTextContent('De duif heeft hier iets over te zeggen')
  })

  it('shows nothing without a candidate, and nothing while the pigeon speaks', async () => {
    const first = await mount(null)
    expect(screen.queryByTestId('meeting-candidate')).toBeNull()
    first.store.candidate = held
    first.store.speaking = true
    await tick()
    flushSync()
    expect(screen.queryByTestId('meeting-candidate')).toBeNull()
  })

  it('lets it be heard on one click', async () => {
    const { store, calls } = await mount(held)
    store.sessionId = 'sess-1'
    await fireEvent.click(screen.getByTestId('candidate-speak'))
    await tick()
    expect(calls.some((c) => c.url.endsWith('/candidate/speak'))).toBe(true)
  })
})
