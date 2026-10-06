import { render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'
import { describe, expect, it } from 'vitest'
import { UiLocale } from '../i18n.svelte'
import { pigeonRoute } from '../testing/chat'
import { jsonResponse, scriptedFetch } from '../testing/fetch'
import PigeonSelector from './PigeonSelector.svelte'
import { RosterStore } from './roster.svelte'

// De duifnaam heeft één bron: personas.yaml via GET /v1/duiven.
// Wat hier vastligt is dat de schil die naam nooit zelf verzint — ook niet
// zolang de server nog niet geantwoord heeft, en ook niet als hij het niet doet.

const settle = async () => { await new Promise((r) => setTimeout(r, 0)); await tick() }

function roster(locale = new UiLocale()) {
  return new RosterStore(scriptedFetch([pigeonRoute()]).fetch, locale)
}

describe('de naamlijst van de schil', () => {
  it('zegt eerlijk dat de naam nog onderweg is en toont daarna de naam uit de configuratie', async () => {
    const store = roster()
    expect(store.pending).toBe(true)
    expect(store.name('biological')).toBe('Loading the pigeon’s name…')
    await store.load()
    expect(store.pending).toBe(false)
    expect(store.name('biological')).toBe('Knowledge Partner')
    expect(store.name('individualist')).toBe('Stakeholder')
  })

  it('volgt de taal van de interface', async () => {
    const locale = new UiLocale('nl')
    const store = roster(locale)
    expect(store.name('biological')).toBe('De naam van de duif wordt geladen…')
    await store.load()
    expect(store.name('biological')).toBe('Kennispartner')
    expect(store.name('individualist')).toBe('Stakeholder')
    locale.choose('en')
    expect(store.name('biological')).toBe('Knowledge Partner')
  })

  it('valt bij een onbereikbare server niet terug op een oude naam', async () => {
    const store = new RosterStore(async () => { throw new Error('offline') }, new UiLocale())
    await store.load()
    expect(store.status).toBe('failed')
    expect(store.name('biological')).toBe('Pigeon name unavailable')
    expect(store.name('individualist')).toBe('Pigeon name unavailable')
  })

  it('noemt een duif die niet in de lijst staat een eerdere duif, geen laadstand', async () => {
    const store = roster()
    await store.load()
    expect(store.name('een-oude-duif')).toBe('Earlier pigeon (identity unavailable)')
  })

  it('haalt de lijst één keer op', async () => {
    const scripted = scriptedFetch([pigeonRoute()])
    const store = new RosterStore(scripted.fetch, new UiLocale())
    await Promise.all([store.load(), store.load()])
    await store.load()
    expect(scripted.calls.filter((c) => c.url.includes('/duiven'))).toHaveLength(1)
  })

  it('laat een naam zonder tekst in de gekozen taal niet als lege kop staan', async () => {
    const scripted = scriptedFetch([
      { method: 'GET', match: '/duiven', respond: () => jsonResponse({ duiven: [{ id: 'biological', name: {}, chat: true, meeting: true }] }) },
    ])
    const store = new RosterStore(scripted.fetch, new UiLocale())
    await store.load()
    expect(store.name('biological')).toBe('Earlier pigeon (identity unavailable)')
  })
})

describe('de duifkiezer', () => {
  it('draagt de laadstand en daarna dezelfde naam in beide talen, met een rol die de naam niet herhaalt', async () => {
    const locale = new UiLocale()
    const store = roster(locale)
    render(PigeonSelector, { selected: 'biological', locale, roster: store, onselect: () => {} })
    expect(screen.getAllByText('Loading the pigeon’s name…')).toHaveLength(3)
    expect(screen.getByText('Translates available research')).toBeInTheDocument()

    void store.load()
    await settle()
    expect(screen.getByText('Knowledge Partner')).toBeInTheDocument()
    expect(screen.getByText('Stakeholder')).toBeInTheDocument()
    expect(screen.getByText('Signaling Pigeon')).toBeInTheDocument()

    locale.choose('nl')
    await tick()
    expect(screen.getByText('Kennispartner')).toBeInTheDocument()
    expect(screen.getByText('Stakeholder')).toBeInTheDocument()
    expect(screen.getByText('Signalerende duif')).toBeInTheDocument()
    expect(screen.getByText('Vertaalt beschikbaar onderzoek')).toBeInTheDocument()
    expect(screen.getByText('Brengt het belang van de duif in')).toBeInTheDocument()
  })
})
