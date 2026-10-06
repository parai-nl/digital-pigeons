import { render, screen } from '@testing-library/svelte'
import { describe, expect, it } from 'vitest'
import { serviceContext, testServices, SOURCE } from '../testing/chat'
import { scriptedFetch } from '../testing/fetch'
import MeetingAnswer from './MeetingAnswer.svelte'
import type { AnswerLine } from './meetingStore.svelte'

// Het begrensde standpunt zonder bron aan de vergadertafel: dezelfde
// markering, dezelfde grenzen als in de chat.

function view(patch: Partial<AnswerLine>, lang: 'en' | 'nl' = 'en') {
  const services = testServices(scriptedFetch([]).fetch)
  services.locale.choose(lang)
  render(MeetingAnswer, {
    props: {
      locale: services.locale,
      answer: {
        id: 1, pigeonId: 'individualist', text: 'Die brede stoep is van mij.', note: '',
        status: 'complete', kbStatus: 'gap', sources: [], startedAt: 0, ...patch,
      },
    },
    context: serviceContext(services),
  })
}

describe('standpunt zonder onderzoek, aan de vergadertafel', () => {
  it('markeert een bijdrage van de Stakeholder zonder bron', () => {
    view({})
    expect(screen.getByTestId('stance-marker')).toHaveTextContent('Position without research')
  })

  it('spreekt Nederlands wanneer de vergadering dat doet', () => {
    view({}, 'nl')
    expect(screen.getByTestId('stance-marker')).toHaveTextContent('Standpunt zonder onderzoek')
  })

  it('blijft weg zodra er wél bronnen zijn', () => {
    view({ kbStatus: 'sources', sources: [SOURCE] })
    expect(screen.queryByTestId('stance-marker')).toBeNull()
  })

  it('geldt niet voor de kennispartner', () => {
    view({ pigeonId: 'biological' })
    expect(screen.queryByTestId('stance-marker')).toBeNull()
  })

  it('wacht tot de bijdrage af is', () => {
    view({ status: 'streaming', text: '' })
    expect(screen.queryByTestId('stance-marker')).toBeNull()
  })
})
