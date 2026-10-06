import { render, screen } from '@testing-library/svelte'
import { describe, expect, it } from 'vitest'
import { serviceContext, testServices, SOURCE } from '../testing/chat'
import { scriptedFetch } from '../testing/fetch'
import AssistantMessage from './AssistantMessage.svelte'
import { FACILITATOR, type ChatMessage } from './chatStore.svelte'

// De Stakeholder mag zonder bron een begrensd standpunt
// innemen. Dat blijft zo; wat verandert is dat de lezer het ziet. Het label is
// gewone taal, het staat in de chiprij bij de brondekking, en het komt
// nooit bij de kennispartner of de facilitator te staan.

function message(patch: Partial<ChatMessage> = {}): ChatMessage {
  return {
    key: 'a1', role: 'assistant', kind: 'pigeon_answer', pigeonId: 'individualist',
    persona: 'individualist', content: 'Die brede stoep is van mij.',
    sources: [], tools: [], partial: false, streaming: false, ...patch,
  }
}

function show(patch: Partial<ChatMessage> = {}, lang: 'en' | 'nl' = 'en') {
  const services = testServices(scriptedFetch([]).fetch)
  services.locale.choose(lang)
  return render(AssistantMessage, {
    props: { message: message(patch), personaName: 'The Stakeholder' },
    context: serviceContext(services),
  })
}

describe('standpunt zonder onderzoek, in de chat', () => {
  it('markeert een antwoord van de Stakeholder zonder bron', () => {
    show({ kbStatus: 'gap' })
    expect(screen.getByTestId('stance-marker')).toHaveTextContent('Position without research')
    // De markering vervangt de algemene gat-badge: die zegt alleen dat er niets
    // gevonden is, niet wat er dan wél staat.
    expect(screen.queryByTestId('gap-badge')).toBeNull()
  })

  it('spreekt Nederlands wanneer het gesprek dat doet', () => {
    show({ kbStatus: 'gap' }, 'nl')
    expect(screen.getByTestId('stance-marker')).toHaveTextContent('Standpunt zonder onderzoek')
  })

  it('blijft weg zodra er wél bronnen zijn', () => {
    show({ kbStatus: 'sources', sources: [SOURCE] })
    expect(screen.queryByTestId('stance-marker')).toBeNull()
  })

  it('geldt niet voor de kennispartner: die krijgt de gewone gat-badge', () => {
    show({ pigeonId: 'biological', persona: 'biological', kbStatus: 'gap' })
    expect(screen.queryByTestId('stance-marker')).toBeNull()
    expect(screen.getByTestId('gap-badge')).toHaveTextContent('No source found for this question.')
  })

  it('geldt niet voor de facilitator: hij bezit geen bronnen', () => {
    show({ persona: FACILITATOR, kbStatus: 'gap' })
    expect(screen.queryByTestId('stance-marker')).toBeNull()
  })

  it('wacht tot het antwoord er staat', () => {
    show({ kbStatus: 'gap', streaming: true, content: 'Die brede' })
    expect(screen.queryByTestId('stance-marker')).toBeNull()
  })

  it('staat niet bij een onbereikbare kennisbank: dat is een storing, geen standpunt', () => {
    show({ kbStatus: 'unavailable' })
    expect(screen.queryByTestId('stance-marker')).toBeNull()
  })
})
