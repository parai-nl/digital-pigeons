import { render, screen } from '@testing-library/svelte'
import { describe, expect, it } from 'vitest'
import { serviceContext, testServices, SOURCE } from '../testing/chat'
import { scriptedFetch } from '../testing/fetch'
import MeetingAnswer from './MeetingAnswer.svelte'
import type { AnswerLine } from './meetingStore.svelte'

// Ook aan de vergadertafel is zichtbaar dat er bronnen afvielen.
// Dezelfde regels als in de chat: gewone taal voor de zaal, redencodes alleen
// achter de ontwikkelaarschakelaar, en niets zolang de bijdrage nog streamt.

function view(patch: Partial<AnswerLine>, lang: 'en' | 'nl' = 'en', dev = false) {
  const services = testServices(scriptedFetch([]).fetch)
  services.locale.choose(lang)
  services.shell.dev = dev
  render(MeetingAnswer, {
    props: {
      locale: services.locale,
      answer: {
        id: 1, pigeonId: 'biological', text: 'Synthetic answer [1].', note: '',
        status: 'complete', kbStatus: 'sources', sources: [SOURCE], startedAt: 0, ...patch,
      },
    },
    context: serviceContext(services),
  })
}

describe('de uitvalmelding aan de vergadertafel', () => {
  it('noemt in gewone taal dat er bronnen afvielen', () => {
    view({ dropped: { count: 2, reasons: { evidence_unreadable: 2 } } })
    const line = screen.getByTestId('answer-dropped')
    expect(line.textContent).toContain('2 sources that were found did not make it into this contribution.')
    expect(line.textContent).not.toContain('evidence_unreadable')
  })

  it('spreekt Nederlands wanneer de vergadering dat doet', () => {
    view({ dropped: { count: 1, reasons: { other_knowledge_base: 1 } } }, 'nl')
    expect(screen.getByTestId('answer-dropped').textContent)
      .toContain('Eén gevonden bron viel af en staat niet bij deze bijdrage.')
  })

  it('toont de redenen pas in devMode, in de taal van de vergadering', () => {
    view({ dropped: { count: 3, reasons: { no_text: 1, partial_search: 2 } } }, 'nl', true)
    const line = screen.getByTestId('answer-dropped').textContent ?? ''
    expect(line).toContain('geen leesbare tekst (1)')
    expect(line).toContain('een deel van de zoekopdracht faalde (2)')
  })

  // Een code die de server niet (meer) stuurt telt wel mee, maar krijgt geen
  // eigen regel: `grounding_rejected` stuurt de gate niet meer.
  it('telt een onbekende redencode mee zonder hem te tonen', () => {
    view({ dropped: { count: 3, reasons: { no_text: 1, grounding_rejected: 2 } } }, 'nl', true)
    const line = screen.getByTestId('answer-dropped').textContent ?? ''
    expect(line).toContain('3 gevonden bronnen vielen af en staan niet bij deze bijdrage.')
    expect(line).toContain('geen leesbare tekst (1)')
    expect(line).not.toContain('grounding_rejected')
  })

  it('zwijgt zonder uitval en zolang de bijdrage nog streamt', () => {
    view({})
    expect(screen.queryByTestId('answer-dropped')).toBeNull()
    view({ status: 'streaming', text: '', dropped: { count: 2, reasons: { no_text: 2 } } })
    expect(screen.queryByTestId('answer-dropped')).toBeNull()
  })

  it('staat er ook bij een kennisgat: juist dan is de uitval het nieuws', () => {
    view({ kbStatus: 'gap', sources: [], dropped: { count: 2, reasons: { other_knowledge_base: 2 } } })
    expect(screen.getByTestId('answer-dropped').textContent)
      .toContain('2 sources that were found did not make it into this contribution.')
  })
})
