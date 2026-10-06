// Wat de zaal ziet van de twee nieuwe vragen: de uitweg bij een eigen open
// vergadering en de wissel van duif tijdens de vergadering. Beide
// teksten staan in het Nederlands én in het Engels op het scherm; beide doen
// pas iets na een bevestiging.
import { fireEvent, render, screen } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import { describe, expect, it } from 'vitest'
import { SERVICES } from '../services.svelte'
import { jsonResponse, type Route, scriptedFetch } from '../testing/fetch'
import { DUIVEN_FIXTURE, meetingHarness, meetingRoutes, OPEN_SESSION_ROUTE } from '../testing/meeting'
import MeetingView from './MeetingView.svelte'

const tick = () => new Promise((resolve) => setTimeout(resolve, 0))

async function mount(routes: Route[] = [], lang?: 'en' | 'nl') {
  const scripted = scriptedFetch([...routes, ...meetingRoutes()])
  const harness = meetingHarness(scripted.fetch)
  if (lang) harness.locale.choose(lang)
  render(MeetingView, { context: new Map([[SERVICES, harness.services()]]) })
  await tick()
  flushSync()
  return { ...harness, calls: scripted.calls }
}

// Twee pratende vergaderduiven: alleen dan is een wissel er een tussen twee
// antwoordkaarten. De vaste lijst houdt de kennispartner buiten de
// vergadering; hier zit zij wel aan tafel.
const twoSpeakingPigeons: Route = {
  method: 'GET',
  match: '/v1/duiven',
  respond: () =>
    jsonResponse({
      ...DUIVEN_FIXTURE,
      duiven: DUIVEN_FIXTURE.duiven.map((d) => (d.id === 'biological' ? { ...d, meeting: true } : d)),
    }),
}

// De open-route van een wissel geeft elke sessie een eigen id.
function switchingOpenRoute(): Route {
  let n = 0
  return {
    method: 'POST',
    match: OPEN_SESSION_ROUTE,
    respond: ({ body }) => {
      n += 1
      const req = body as Record<string, unknown>
      return jsonResponse({ sessionId: 'sessie-' + n, pigeonId: req.pigeonId, lang: req.lang, listening: 'listening', voice: 'ready' }, 201)
    },
  }
}

const capacityRoute = (reason: string): Route => ({
  method: 'POST',
  match: OPEN_SESSION_ROUTE,
  respond: () => jsonResponse({ error: reason, message: { en: 'Still open elsewhere.', nl: 'Nog ergens open.' } }, 429),
})

describe('de uitweg bij een eigen open vergadering', () => {
  it('zet naast de melding een actie, en beëindigt pas na bevestiging', async () => {
    let refuse = true
    const { store, calls } = await mount([
      {
        method: 'POST',
        match: OPEN_SESSION_ROUTE,
        respond: ({ body }) => {
          if (refuse) {
            refuse = false
            return jsonResponse({ error: 'meeting_capacity_self', message: { en: 'Still open elsewhere.', nl: 'Nog ergens open.' } }, 429)
          }
          const req = body as Record<string, unknown>
          return jsonResponse({ sessionId: 'sessie-2', pigeonId: req.pigeonId, lang: req.lang, listening: 'listening', voice: 'ready' }, 201)
        },
      },
    ])

    await fireEvent.click(screen.getByTestId('seat-button'))
    await tick()
    flushSync()
    expect(screen.getByTestId('setup-notice')).toHaveTextContent('Still open elsewhere.')

    const action = screen.getByTestId('stuck-action')
    expect(action).toHaveTextContent(/End the other meeting and continue here/i)
    await fireEvent.click(action)
    flushSync()

    const dialog = screen.getByTestId('stuck-dialog')
    expect(dialog).toHaveTextContent(/End the other meeting\?/i)
    // Nee laat alles staan.
    await fireEvent.click(screen.getByTestId('stuck-dialog-cancel'))
    flushSync()
    expect(screen.queryByTestId('stuck-dialog')).toBeNull()
    expect(store.live).toBe(false)

    await fireEvent.click(screen.getByTestId('stuck-action'))
    flushSync()
    await fireEvent.click(screen.getByTestId('stuck-dialog-confirm'))
    await tick()
    await tick()
    flushSync()

    const opens = calls.filter((c) => c.url.endsWith('/meeting/sessions'))
    expect(JSON.parse(String(opens.at(-1)?.init?.body))).toMatchObject({ replaceExisting: true })
    expect(store.live).toBe(true)
  })

  it('toont de actie en de vraag ook in het Nederlands', async () => {
    await mount([capacityRoute('meeting_capacity_self')], 'nl')
    await fireEvent.click(screen.getByTestId('seat-button'))
    await tick()
    flushSync()

    expect(screen.getByTestId('stuck-action')).toHaveTextContent('Beëindig de andere vergadering en ga hier verder')
    await fireEvent.click(screen.getByTestId('stuck-action'))
    flushSync()
    expect(screen.getByTestId('stuck-dialog')).toHaveTextContent('De andere vergadering beëindigen?')
  })

  it('laat de actie weg als de tafel door anderen bezet is', async () => {
    await mount([capacityRoute('meeting_capacity')])
    await fireEvent.click(screen.getByTestId('seat-button'))
    await tick()
    flushSync()

    expect(screen.getByTestId('setup-notice')).toBeInTheDocument()
    expect(screen.queryByTestId('stuck-action')).toBeNull()
  })
})

describe('de wisseldialoog tijdens de vergadering', () => {
  it('noemt beide duiven bij naam en wisselt pas na ja', async () => {
    const { store, calls } = await mount()
    await fireEvent.click(screen.getByTestId('seat-button'))
    await tick()
    flushSync()

    store.requestPigeonSwitch('nonverbal')
    flushSync()
    const dialog = screen.getByTestId('switch-dialog')
    expect(dialog).toHaveTextContent(/Seat a different pigeon at the table\?/i)
    expect(dialog).toHaveTextContent(/Signaling Pigeon/i)
    expect(dialog).toHaveTextContent(/Stakeholder/i)

    await fireEvent.click(screen.getByTestId('switch-dialog-cancel'))
    flushSync()
    expect(screen.queryByTestId('switch-dialog')).toBeNull()
    expect(store.pigeonId).toBe('individualist')

    store.requestPigeonSwitch('nonverbal')
    flushSync()
    await fireEvent.click(screen.getByTestId('switch-dialog-confirm'))
    await tick()
    await tick()
    flushSync()

    const opens = calls.filter((c) => c.url.endsWith('/meeting/sessions'))
    expect(JSON.parse(String(opens.at(-1)?.init?.body))).toMatchObject({ pigeonId: 'nonverbal', handoverFrom: 'sessie-1' })
    expect(store.pigeonId).toBe('nonverbal')
    expect(store.live).toBe(true)
  })

  // De vorige duif verdwijnt niet met de wissel: haar laatste bijdrage blijft
  // op het scherm staan, onder haar eigen naam, tot de nieuwe iets zegt.
  async function switchWithAContribution(lang?: 'en' | 'nl') {
    const harness = await mount([twoSpeakingPigeons, switchingOpenRoute()], lang)
    await fireEvent.click(screen.getByTestId('seat-button'))
    await tick()
    flushSync()
    expect(harness.store.pigeonId).toBe('biological')

    const first = harness.ports.channel()
    first.emit({ type: 'persona', sessionId: 'sessie-1', pigeonId: 'biological', turnId: '1' })
    first.emit({ type: 'text', sessionId: 'sessie-1', turnId: '1', delta: 'Roekoe. Ik woon hier ook.' })
    first.emit({ type: 'response_done', sessionId: 'sessie-1', turnId: '1' })
    flushSync()

    harness.store.requestPigeonSwitch('individualist')
    flushSync()
    await fireEvent.click(screen.getByTestId('switch-dialog-confirm'))
    await tick()
    await tick()
    flushSync()
    expect(harness.store.pigeonId).toBe('individualist')
    return harness
  }

  it('laat de laatste bijdrage van de vorige duif staan, met haar naam erbij', async () => {
    const { ports } = await switchWithAContribution()

    const carried = screen.getByTestId('carried-answer')
    expect(carried).toHaveTextContent('Roekoe. Ik woon hier ook.')
    expect(carried).toHaveTextContent(/Knowledge Partner/)
    expect(carried).toHaveTextContent(/The last contribution from Knowledge Partner, before Stakeholder took her place at the table\./)

    // Zodra de nieuwe duif het woord neemt, staat haar kaart er alleen voor.
    ports.channel().emit({ type: 'persona', sessionId: 'sessie-2', pigeonId: 'individualist' })
    flushSync()
    expect(screen.queryByTestId('carried-answer')).toBeNull()
    expect(screen.getByTestId('latest-answer')).toHaveTextContent(/Stakeholder/)
  })

  it('zegt datzelfde in het Nederlands', async () => {
    await switchWithAContribution('nl')

    const carried = screen.getByTestId('carried-answer')
    expect(carried).toHaveTextContent('Roekoe. Ik woon hier ook.')
    expect(carried).toHaveTextContent('De laatste bijdrage van Kennispartner, voordat Stakeholder aan tafel ging.')
  })

  it('stelt dezelfde vraag in het Nederlands', async () => {
    const { store } = await mount([], 'nl')
    await fireEvent.click(screen.getByTestId('seat-button'))
    await tick()
    flushSync()

    store.requestPigeonSwitch('nonverbal')
    flushSync()
    expect(screen.getByTestId('switch-dialog')).toHaveTextContent('Wil je een andere duif aan tafel zetten?')
  })
})
