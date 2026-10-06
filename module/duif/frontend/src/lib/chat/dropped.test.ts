import { render, screen } from '@testing-library/svelte'
import { describe, expect, it } from 'vitest'
import { serviceContext, testServices } from '../testing/chat'
import { scriptedFetch } from '../testing/fetch'
import AssistantMessage from './AssistantMessage.svelte'
import { FACILITATOR, type ChatMessage } from './chatStore.svelte'

// De stille drops worden zichtbaar. Wat hier vastligt: de zin is
// gewone taal zonder foutcode, hij staat er alleen als er werkelijk
// iets wegviel, de facilitator doet geen uitspraak over bronnen, en de
// redencodes blijven achter de ontwikkelaarschakelaar.

function message(patch: Partial<ChatMessage> = {}): ChatMessage {
  return {
    key: 'a1', role: 'assistant', kind: 'pigeon_answer', pigeonId: 'biological',
    content: 'Pigeons nest on ledges.', sources: [], tools: [], partial: false, streaming: false, ...patch,
  }
}

function show(patch: Partial<ChatMessage> = {}, dev = false) {
  const services = testServices(scriptedFetch([]).fetch)
  services.shell.dev = dev
  return render(AssistantMessage, {
    props: { message: message(patch), personaName: 'The Biological Pigeon' },
    context: serviceContext(services),
  })
}

describe('de uitvalmelding in de chat', () => {
  it('noemt in gewone taal dat er bronnen afvielen', () => {
    show({ dropped: { count: 2, reasons: { evidence_unreadable: 2 } } })
    const line = screen.getByTestId('answer-dropped')
    expect(line.textContent).toContain('2 sources that were found did not make it into this answer.')
    // Geen techniek op het scherm zolang de schakelaar uit staat.
    expect(line.textContent).not.toContain('evidence_unreadable')
    expect(line.textContent).not.toContain('could not be read')
  })

  it('telt enkelvoud apart', () => {
    show({ dropped: { count: 1, reasons: { other_knowledge_base: 1 } } })
    expect(screen.getByTestId('answer-dropped').textContent)
      .toContain('One source that was found did not make it into this answer.')
  })

  it('toont de redenen pas in devMode', () => {
    show({ dropped: { count: 3, reasons: { no_text: 1, partial_search: 2 } } }, true)
    const line = screen.getByTestId('answer-dropped').textContent ?? ''
    expect(line).toContain('no readable text (1)')
    expect(line).toContain('part of the search failed (2)')
  })

  // De gate laat nog vier redenen vallen. Een code die de server niet meer
  // stuurt — `no_original_passages` en `grounding_rejected` van een oudere
  // server, of iets nieuws uit een latere server — loopt langs
  // hetzelfde pad als elke onbekende code: geteld in de neutrale zin, zonder
  // eigen regel.
  it('laat een onbekende redencode weg in plaats van hem te tonen', () => {
    show({ dropped: { count: 3, reasons: { no_text: 1, iets_nieuws: 1, grounding_rejected: 1 } } }, true)
    const line = screen.getByTestId('answer-dropped').textContent ?? ''
    expect(line).toContain('3 sources that were found did not make it into this answer.')
    expect(line).toContain('no readable text (1)')
    expect(line).not.toContain('iets_nieuws')
    expect(line).not.toContain('grounding_rejected')
  })

  it('zwijgt zonder uitval', () => {
    show()
    expect(screen.queryByTestId('answer-dropped')).toBeNull()
  })

  it('zwijgt bij de facilitator: hij bezit geen bronnen', () => {
    show({ persona: FACILITATOR, dropped: { count: 2, reasons: { no_text: 2 } } })
    expect(screen.queryByTestId('answer-dropped')).toBeNull()
  })

  it('wacht tot het antwoord er staat', () => {
    show({ streaming: true, dropped: { count: 2, reasons: { no_text: 2 } } })
    expect(screen.queryByTestId('answer-dropped')).toBeNull()
  })
})
