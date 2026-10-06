// Componenttests van de vergaderweergave: wat de zaal daadwerkelijk ziet en
// kan aanraken. De store zelf is elders getoetst; hier gaat het om rollen,
// knoppen en de meldingen die nooit stil mogen wegvallen.
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import { describe, expect, it, vi } from 'vitest'
import { SERVICES } from '../services.svelte'
import { jsonResponse, type Route, scriptedFetch } from '../testing/fetch'
import { type FakePortsOptions, meetingHarness, meetingRoutes, readEverything, SIGNAL_EXPLANATION } from '../testing/meeting'
import MeetingView from './MeetingView.svelte'

const tick = () => new Promise((resolve) => setTimeout(resolve, 0))

async function mount(options: { routes?: Route[]; ports?: FakePortsOptions } = {}) {
  const scripted = scriptedFetch([...(options.routes ?? []), ...meetingRoutes()])
  const harness = meetingHarness(scripted.fetch, options.ports)
  render(MeetingView, { context: new Map([[SERVICES, harness.services()]]) })
  await tick()
  flushSync()
  return { ...harness, calls: scripted.calls }
}

async function mountLive(options: { pigeon?: string; routes?: Route[]; ports?: FakePortsOptions } = {}) {
  const mounted = await mount(options)
  if (options.pigeon) mounted.store.pigeonId = options.pigeon
  await mounted.store.start()
  await tick()
  flushSync()
  return mounted
}

describe('MeetingView', () => {
  it('offers only the pigeons that may sit at the table and seats one', async () => {
    const { store, calls } = await mount()

    expect(screen.getByTestId('seat-button')).toBeInTheDocument()
    expect(store.meetingPigeons.map((p) => p.id)).toEqual(['individualist', 'nonverbal'])

    screen.getByTestId('seat-button').click()
    await tick()
    flushSync()

    expect(calls.some((c) => c.url.endsWith('/meeting/sessions'))).toBe(true)
    expect(screen.getByTestId('live-view')).toBeInTheDocument()
  })

  // Op de opzetkaart kies je een duif, dus hoort daar haar
  // eigen kaartportret; de rustposter hoort onder het 3D-podium, waar zij de
  // renderer opvangt.
  it('shows the chosen pigeon\'s own portrait on the setup card, not a rest poster', async () => {
    const { store } = await mount()
    const portrait = () => document.querySelector('.setup-portrait [data-pigeon]')

    store.pigeonId = 'individualist'
    flushSync()
    expect(portrait()).toHaveAttribute('data-pigeon', 'individualist')

    store.pigeonId = 'nonverbal'
    flushSync()
    expect(portrait()).toHaveAttribute('data-pigeon', 'nonverbal')

    // De gestagede media staan in Git: het is haar eigen
    // kaartportret en nooit een rustposter.
    expect(document.querySelector('.setup-portrait img')?.getAttribute('src')).toMatch(/signalerend-kaart\.webp$/)
  })

  it('keeps the listen indicator in a polite status region', async () => {
    const { ports } = await mountLive()

    const indicator = screen.getByTestId('listen-indicator')
    expect(indicator).toHaveAttribute('role', 'status')
    expect(indicator).toHaveAttribute('aria-live', 'polite')
    expect(indicator).toHaveTextContent(/is listening/i)

    ports.channel().emit({ type: 'listening_heartbeat', listening: 'reconnecting' })
    flushSync()
    expect(screen.getByTestId('listen-indicator')).toHaveTextContent(/reconnect/i)
  })

  it('says the microphone is muted rather than merely off', async () => {
    const { store } = await mountLive()

    // De dempknop schrijft rechtstreeks in de store (function binding), en de
    // indicator is een afgeleide daarvan.
    await fireEvent.click(screen.getByTestId('call-control-mute'))
    flushSync()
    expect(store.muted).toBe(true)
    expect(screen.getByTestId('listen-indicator')).toHaveTextContent(/muted/i)

    await fireEvent.click(screen.getByTestId('call-control-mute'))
    flushSync()
    expect(store.muted).toBe(false)
  })

  it('gives a talking pigeon the respond button and never disables STOP', async () => {
    const { ports } = await mountLive()

    expect(screen.getByTestId('respond-button')).toBeInTheDocument()
    expect(screen.queryByTestId('intervene-button')).toBeNull()

    const stop = screen.getByTestId('stop-button')
    expect(stop).not.toBeDisabled()
    expect(stop).toHaveAttribute('aria-keyshortcuts', 'Escape')

    // Ook terwijl de duif spreekt blijft STOP bruikbaar en raakt de
    // antwoordknop alleen aria-disabled (geen focusverlies).
    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    flushSync()
    expect(screen.getByTestId('stop-button')).not.toBeDisabled()
    expect(screen.getByTestId('respond-button')).toHaveAttribute('aria-disabled', 'true')
  })

  it('gives the non-verbal pigeon its own button, following the server', async () => {
    const { ports } = await mountLive()

    // De server bepaalt welke duif aan tafel zit, niet de opzetkaart.
    ports.channel().emit({ type: 'state', sessionId: 'sessie-1', pigeonId: 'nonverbal', listening: 'listening' })
    flushSync()
    expect(screen.getByTestId('intervene-button')).toBeInTheDocument()
    expect(screen.queryByTestId('respond-button')).toBeNull()
  })

  it('stops the pigeon on Escape', async () => {
    const { store, calls } = await mountLive()

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await tick()
    expect(store.stopped).toBe(true)
    expect(calls.some((c) => c.url.includes('/stop'))).toBe(true)
  })

  it('keeps recent speech under the portrait and opens the whole session on the same spot', async () => {
    const { ports } = await mountLive()

    ports.channel().emit({ type: 'transcript', text: 'wij willen de duiven weren', speaker: '2', final: true })
    ports.channel().emit({ type: 'transcript', text: 'en dan', speaker: '2', final: false })
    flushSync()

    // Dicht: de laatste zinnen staan er, zonder log en zonder losse systeemzin
    // over tijdelijk geheugen.
    const recent = screen.getByTestId('transcript-preview')
    expect(recent).toHaveTextContent(/wij willen de duiven weren/)
    expect(recent).toHaveTextContent('en dan')
    expect(screen.queryByRole('log')).toBeNull()
    expect(screen.queryByTestId('transcript-notice')).toBeNull()

    screen.getByTestId('transcript-toggle').click()
    flushSync()

    const log = screen.getByRole('log')
    expect(log).toHaveTextContent(/Speaker 2/)
    expect(log).toHaveTextContent(/wij willen de duiven weren/)
    const partial = screen.getByTestId('transcript-partial')
    expect(partial).toHaveAttribute('aria-live', 'off')
    expect(partial).toHaveTextContent('en dan')
    // De grens van het sessiegeheugen staat klein bij het uitgeklapte verloop.
    expect(screen.getByTestId('transcript-notice')).toHaveTextContent(/temporary session memory/i)
  })

  it('keeps the expanded session log to the eighty most recent lines', async () => {
    const { ports } = await mountLive()

    // Meer regels dan het venster: wat eraf valt is weg, er bestaat geen kopie.
    for (let n = 1; n <= 95; n += 1) ports.channel().emit({ type: 'transcript', text: `Line ${n}`, speaker: '2', final: true })
    flushSync()

    screen.getByTestId('transcript-toggle').click()
    flushSync()

    const lines = within(screen.getByRole('log')).getAllByRole('listitem')
    expect(lines).toHaveLength(80)
    expect(lines[0]).toHaveTextContent('Line 16')
    expect(lines.at(-1)).toHaveTextContent('Line 95')
    expect(screen.queryByText(/Line 15$/)).toBeNull()
  })

  it('lets the opening question step aside once the room is talking', async () => {
    const { ports } = await mountLive()

    expect(screen.getByText('What would you like to discuss together?')).toBeInTheDocument()
    expect(screen.queryByTestId('stage-underway')).toBeNull()

    ports.channel().emit({ type: 'transcript', text: 'We are deciding about the square.', speaker: '2', final: true })
    flushSync()

    expect(screen.queryByText('What would you like to discuss together?')).toBeNull()
    expect(screen.getByTestId('stage-underway')).toHaveTextContent(/the conversation is under way/i)
  })

  // De vraag is een alertdialog met titel, uitleg en een
  // aftelling in m:ss; de focus staat op "Ja, ga door", en Escape is ook een
  // ja. De tweede keer kiest de zaal "Vergadering beëindigen": dat is de rode
  // knop zonder de extra vraag, ook terwijl de duif nog spreekt.
  it('raises the presence check and answers it', async () => {
    const { store, ports, calls } = await mountLive()
    expect(screen.queryByTestId('presence-dialog')).toBeNull()

    ports.channel().emit({ type: 'presence_check', message: { en: 'Nothing has been clicked for a while.', nl: '…' }, graceMs: 120_000 })
    flushSync()
    await tick()
    const dialog = screen.getByTestId('presence-dialog')
    expect(dialog).toHaveAttribute('role', 'alertdialog')
    expect(dialog).toHaveAccessibleName('Are you still there?')
    expect(dialog).toHaveAccessibleDescription(/Nothing has been clicked/)
    expect(screen.getByTestId('presence-countdown')).toHaveTextContent('The meeting stops in 2:00')
    expect(screen.getByTestId('presence-confirm')).toHaveFocus()

    ports.advance(90_000)
    flushSync()
    expect(screen.getByTestId('presence-countdown')).toHaveTextContent('0:30')
    // Escape is een ja, en geen "Stop inbreng".
    await fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' })
    await tick()
    flushSync()
    expect(screen.queryByTestId('presence-dialog')).toBeNull()
    expect(calls.filter((c) => c.url.includes('/presence'))).toHaveLength(1)
    expect(calls.some((c) => c.url.includes('/stop'))).toBe(false)

    ports.channel().emit({ type: 'persona', pigeonId: 'individualist', turnId: '1' })
    ports.channel().emit({ type: 'presence_check', message: { en: 'Still there?' }, graceMs: 120_000 })
    flushSync()
    await tick()
    screen.getByTestId('presence-end').click()
    await tick()
    flushSync()
    expect(screen.queryByTestId('end-dialog')).toBeNull()
    expect(store.live).toBe(false)
    expect(ports.mic().stopped).toBe(true)
    expect(calls.filter((c) => c.init?.method === 'DELETE' && c.init?.keepalive === true)).toHaveLength(1)
    expect(screen.getByTestId('seat-button')).toBeInTheDocument()
  })

  // AC12 — de vraag ligt boven de wisselvraag. Alle dialogen delen dezelfde
  // laag, en de later gemounte lag erbovenop: de aftelling was onzichtbaar
  // terwijl de focus op een knop zat die je niet zag. De wisselvraag gaat nu
  // dicht zodra "Ben je er nog?" verschijnt.
  it('puts the presence check above an open switch question', async () => {
    const { store, ports } = await mountLive()

    expect(store.requestPigeonSwitch('nonverbal')).toBe(true)
    flushSync()
    await tick()
    expect(screen.getByTestId('switch-dialog')).toBeInTheDocument()
    ports.channel().emit({ type: 'presence_check', message: { en: 'Still there?' }, graceMs: 120_000 })
    flushSync()
    await tick()
    expect(screen.queryByTestId('switch-dialog')).toBeNull()
    expect(screen.getByTestId('presence-dialog')).toBeInTheDocument()
    expect(screen.getByTestId('presence-confirm')).toHaveFocus()
  })

  it('names the consequence of a sound failure and keeps dropped blocks off the screen', async () => {
    const logged = vi.spyOn(console, 'warn').mockImplementation(() => {})
    try {
      const { store, ports } = await mountLive({
        routes: [{ method: 'POST', match: '/audio', respond: () => jsonResponse({ streamed: 0, dropped: 4, listening: 'listening' }, 202) }],
      })

      ports.mic().push()
      await tick()
      flushSync()
      // Weggevallen audioblokken zijn een technische toestand: de gebruiker kan
      // er niets mee. De teller blijft in de store; modulecode logt niets.
      expect(store.dropped).toBe(4)
      expect(screen.queryByTestId('meeting-notice')).toBeNull()
      expect(logged).not.toHaveBeenCalled()

      ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
      ports.channel().emit({ type: 'tts_audio', audio: 'QUJD', seq: 1 })
      ports.channel().emit({ type: 'tts_done', frames: 3 })
      await tick()
      flushSync()
      expect(store.audioGap).toBe(true)
      expect(screen.getByTestId('meeting-notice')).toHaveTextContent(/read the contribution on screen/i)
    } finally {
      logged.mockRestore()
    }
  })

  it('shows only the most urgent notice when several things are wrong at once', async () => {
    const { store } = await mountLive({ ports: { suspended: true } })

    // Geblokkeerd geluid én een audiogat: één melding, en het is de melding met
    // de knop die de gebruiker verder helpt.
    store.audioGap = true
    flushSync()
    const notices = screen.getAllByTestId('meeting-notice')
    expect(notices).toHaveLength(1)
    expect(notices[0]).toHaveAttribute('data-notice', 'sound-blocked')
    expect(screen.getByTestId('audio-enable')).toBeInTheDocument()

    store.error = { status: 503, reason: 'llm_unavailable' }
    flushSync()
    expect(screen.getAllByTestId('meeting-notice')).toHaveLength(1)
    expect(screen.getByTestId('meeting-notice')).toHaveAttribute('data-notice', 'error')
  })

  it('says the pigeon answers without a voice when the session has none', async () => {
    await mountLive({
      routes: [
        {
          method: 'POST',
          match: /\/meeting\/sessions$/,
          respond: () =>
            jsonResponse(
              { sessionId: 'sessie-1', pigeonId: 'individualist', lang: 'en', listening: 'listening', voice: 'off' },
              201,
            ),
        },
      ],
    })

    expect(screen.getByTestId('meeting-notice')).toHaveTextContent(/without voice/i)
  })

  it('keeps the voice notice away from the pigeon that never speaks', async () => {
    // De server meldt terecht voice: off — de non-verbale duif heeft geen stem.
    // Dat is haar aard, geen storing, dus er valt niets te melden.
    const { store } = await mountLive({
      routes: [
        {
          method: 'POST',
          match: /\/meeting\/sessions$/,
          respond: () =>
            jsonResponse({ sessionId: 'sessie-1', pigeonId: 'nonverbal', lang: 'en', listening: 'listening', voice: 'off' }, 201),
        },
      ],
    })

    expect(store.speaks).toBe(false)
    expect(screen.queryByTestId('meeting-notice')).toBeNull()
    expect(screen.getByTestId('intervene-button')).toBeInTheDocument()
  })

  it('offers a button when the browser holds the sound back', async () => {
    const { store } = await mountLive({ ports: { suspended: true } })

    expect(screen.getByTestId('meeting-notice')).toHaveAttribute('data-notice', 'sound-blocked')
    screen.getByTestId('audio-enable').click()
    await tick()
    expect(store.audioBlocked).toBe(true)
  })

  it('reports the reason when the server closes the session', async () => {
    const { ports } = await mountLive()

    ports.channel().emit({ type: 'session_closed', reason: 'inactivity' })
    await tick()
    flushSync()
    expect(screen.getByTestId('setup-notice')).toHaveTextContent(/nobody answered “Are you still there\?”/i)
    expect(screen.getByTestId('seat-button')).toBeInTheDocument()
  })
  it('leaves Escape to the field it belongs to', async () => {
    const { store, calls } = await mountLive()

    // In een keuzelijst sluit Escape de lijst; de duif blijft praten.
    const settings = screen.getByTestId('meeting-settings')
    await fireEvent.click(within(settings).getByRole('button', { name: /eagerness/i }))
    flushSync()
    await fireEvent.keyDown(within(settings).getByRole('textbox'), { key: 'Escape' })
    await tick()
    expect(store.stopped).toBe(false)

    // En in een tekstvlak (de invoerbalk van de chat-tab) hoort Escape ook niet
    // van ons te zijn.
    const textarea = document.createElement('textarea')
    document.body.append(textarea)
    await fireEvent.keyDown(textarea, { key: 'Escape' })
    await tick()
    expect(store.stopped).toBe(false)
    textarea.remove()

    // Staat de vergadering niet op het scherm (chat-tab actief), dan evenmin.
    store.orbVisible = false
    flushSync()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await tick()
    expect(store.stopped).toBe(false)
    expect(calls.some((c) => c.url.includes('/stop'))).toBe(false)
  })

  // De focus blijft op de stilteknop en verhuist niet mee naar de
  // grote rode knop. Die beëindigt de vergadering, en een toetsenbord dat na
  // het aanschuiven meteen een spatie krijgt hoort de duif stil te zetten, niet
  // de tafel op te breken.
  it('moves the focus to Stop contribution when the pigeon sits down and back to the seat button at the end', async () => {
    const { store, ports } = await mountLive()
    expect(document.activeElement).toBe(screen.getByTestId('stop-button'))

    ports.channel().emit({ type: 'session_closed', reason: 'ended_by_user' })
    await tick()
    flushSync()
    expect(store.live).toBe(false)
    expect(document.activeElement).toBe(screen.getByTestId('seat-button'))
  })

  // De twee stopknoppen wisselden van plaats en van naam. De grote
  // rode knop op de eerste regel sluit de vergadering; de knop die alleen de
  // duif stilzet staat op de tweede regel en heet wat hij doet.
  it('puts End meeting on the big button and Stop contribution on the quiet row', async () => {
    await mountLive()

    const primary = document.querySelector('.primary-row')!
    const secondary = document.querySelector('.secondary-row')!
    const end = screen.getByTestId('call-control-end')
    const stop = screen.getByTestId('stop-button')

    expect(primary).toContainElement(end)
    expect(end).toHaveTextContent('End meeting')
    expect(end).not.toHaveAttribute('aria-keyshortcuts')
    // De uitnodiging blijft naast de grote knop staan.
    expect(primary).toContainElement(screen.getByTestId('respond-button'))

    expect(secondary).toContainElement(stop)
    expect(stop).toHaveTextContent('Stop contribution')
    expect(stop).not.toBeDisabled()
    expect(stop).toHaveAttribute('aria-keyshortcuts', 'Escape')
    expect(secondary).toContainElement(screen.getByTestId('call-control-mute'))
  })

  // Geen uitlegregel meer over gepauzeerde bijdragen. De stand staat
  // in één woord bij de duif en de knop zegt zelf wat de volgende klik doet.
  it('replaces the pause sentence with one word at the pigeon and a resume button', async () => {
    const { store, calls, ports } = await mountLive()

    expect(screen.queryByTestId('quiet-state')).toBeNull()
    expect(screen.queryByTestId('stopped-line')).toBeNull()

    await fireEvent.click(screen.getByTestId('stop-button'))
    await tick()
    flushSync()

    expect(store.autonomyPaused).toBe(true)
    // Eén korte staat, naast de naam van de duif en los van de microfoon.
    const quiet = screen.getByTestId('quiet-state')
    expect(quiet).toHaveTextContent('Quiet')
    expect(quiet.closest('.stage-identity')).not.toBeNull()
    expect(screen.queryByTestId('stopped-line')).toBeNull()

    // Zolang de stop onderweg is blijft de knop zijn stopstand houden; pas het
    // `stopped`-bericht van de server maakt er hervatten van.
    expect(screen.getByTestId('stop-button')).toBeInTheDocument()
    ports.channel().emit({ type: 'stopped', autonomyPaused: true })
    await tick()
    flushSync()

    // Dezelfde knop draagt nu het hervatten, en is nog steeds niet uitgeschakeld.
    expect(screen.queryByTestId('stop-button')).toBeNull()
    const resume = screen.getByTestId('resume-button')
    expect(resume).toHaveTextContent('Resume contribution')
    expect(resume).not.toBeDisabled()

    await fireEvent.click(resume)
    await tick()
    flushSync()
    expect(calls.some((c) => c.url.includes('/resume'))).toBe(true)
    expect(store.autonomyPaused).toBe(false)
    expect(screen.queryByTestId('quiet-state')).toBeNull()
    expect(screen.getByTestId('stop-button')).toBeInTheDocument()
  })

  // De storingsmelding na een mislukte stop vraagt om
  // nóg een keer stoppen. Draaide de knop op dat moment al naar "Inbreng
  // hervatten", dan deed die tweede klik precies het omgekeerde en liet de duif
  // weer los. De knop blijft dus in zijn stopstand tot de server de stop kent.
  it('keeps the button on Stop contribution while the server has not confirmed the stop', async () => {
    const blocked = { method: 'POST', match: '/stop', respond: () => { throw new Error('SYNTHETIC: network blocked') } }
    const { store, calls } = await mountLive({ routes: [blocked] })

    await fireEvent.click(screen.getByTestId('stop-button'))
    await tick()
    flushSync()

    // Hier is het stil, maar de server weet van niets.
    expect(store.stopped).toBe(true)
    expect(store.stopSyncFailed).toBe(true)
    expect(screen.getByTestId('quiet-state')).toBeInTheDocument()
    expect(screen.getByTestId('meeting-notice')).toHaveTextContent(/did not confirm it/i)

    // De melding wijst naar deze knop, dus die moet ook doen wat zij zegt.
    expect(screen.queryByTestId('resume-button')).toBeNull()
    const stop = screen.getByTestId('stop-button')
    expect(stop).toHaveTextContent('Stop contribution')
    expect(stop).not.toBeDisabled()

    const stops = calls.filter((c) => c.url.includes('/stop')).length
    await fireEvent.click(stop)
    await tick()
    flushSync()

    expect(calls.filter((c) => c.url.includes('/stop')).length).toBe(stops + 1)
    expect(calls.some((c) => c.url.includes('/resume'))).toBe(false)
    expect(store.autonomyPaused).toBe(true)
    expect(screen.getByTestId('quiet-state')).toBeInTheDocument()
  })

  // De vraag bij beëindigen noemt "Stop inbreng" als
  // uitweg, en de focus zit in de dialoog. Die uitweg staat dus in de dialoog
  // zelf: hij zet de duif stil en laat de vergadering lopen.
  it('offers Stop contribution inside the end question', async () => {
    const { store, ports, calls } = await mountLive()

    ports.channel().emit({ type: 'listening_heartbeat', listening: 'listening', speaking: true })
    flushSync()
    await fireEvent.click(screen.getByTestId('call-control-end'))
    await tick()
    flushSync()

    const alternative = screen.getByTestId('end-dialog-alternative')
    expect(alternative).toHaveTextContent('Stop contribution')

    await fireEvent.click(alternative)
    await tick()
    flushSync()

    expect(store.endConfirm).toBe(false)
    expect(store.live).toBe(true)
    expect(store.stopped).toBe(true)
    expect(calls.some((c) => c.url.includes('/stop'))).toBe(true)
    expect(screen.getByTestId('quiet-state')).toBeInTheDocument()
  })

  // Escape blijft de directe stilte en beëindigt niets.
  it('keeps Escape on the silence, not on ending the meeting', async () => {
    const { store, calls } = await mountLive()

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await tick()
    flushSync()

    expect(store.stopped).toBe(true)
    expect(store.live).toBe(true)
    expect(calls.some((c) => c.url.includes('/stop'))).toBe(true)
    expect(calls.some((c) => c.init?.method === 'DELETE')).toBe(false)
  })

  // Beëindigen is één klik zolang er niets te onderbreken valt. Staat
  // de duif midden in een zin, dan is die klik waarschijnlijk bedoeld om háár
  // stil te krijgen — daar hoort één vraag bij, met de stilteknop als uitweg.
  it('ends straight away when the pigeon is quiet and asks once while it speaks', async () => {
    const { store, ports } = await mountLive()

    ports.channel().emit({ type: 'listening_heartbeat', listening: 'listening', speaking: true })
    flushSync()
    expect(store.speaking).toBe(true)

    await fireEvent.click(screen.getByTestId('call-control-end'))
    await tick()
    flushSync()
    expect(store.live).toBe(true)
    expect(screen.getByTestId('end-dialog')).toBeInTheDocument()

    await fireEvent.click(screen.getByTestId('end-dialog-cancel'))
    await tick()
    flushSync()
    expect(store.live).toBe(true)
    expect(store.endConfirm).toBe(false)

    await fireEvent.click(screen.getByTestId('call-control-end'))
    await tick()
    flushSync()
    await fireEvent.click(screen.getByTestId('end-dialog-confirm'))
    await tick()
    flushSync()
    expect(store.live).toBe(false)
  })

  it('ends without a question once the pigeon has stopped speaking', async () => {
    const { store } = await mountLive()

    expect(store.speaking).toBe(false)
    await fireEvent.click(screen.getByTestId('call-control-end'))
    await tick()
    flushSync()
    expect(screen.queryByTestId('end-dialog')).toBeNull()
    expect(store.live).toBe(false)
  })

  it('keeps arousal, topic and trigger interpretation off the signaling stage', async () => {
    const { ports } = await mountLive({ pigeon: 'nonverbal' })
    ports.channel().emit({ type: 'arousal', arousal: 0.9 })
    ports.channel().emit({ type: 'topic_hint', topic: { id: 'food', label: { en: 'Food supply' } }, score: 0.9 })
    ports.channel().emit({ type: 'intervention', fragment: 'hidden trigger', topic: { id: 'food' } })
    flushSync()
    expect(screen.queryByRole('meter')).not.toBeInTheDocument()
    expect(screen.queryByText(/Food supply/)).not.toBeInTheDocument()
    expect(screen.queryByText(/hidden trigger/)).not.toBeInTheDocument()
    expect(screen.queryByTestId('latest-answer')).not.toBeInTheDocument()
    expect(screen.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'rest')
  })

  it('writes a settings change straight into the store and lets the server echo win afterwards', async () => {
    const { store, ports, calls } = await mountLive()

    const settings = screen.getByTestId('meeting-settings')
    await fireEvent.click(within(settings).getByRole('switch'))
    flushSync()
    // De knop reageert meteen, de POST wacht op de rustpauze: geen bericht per
    // schuifstapje.
    expect(store.settings.autonomous).toBe(true)
    expect(calls.filter((c) => c.url.includes('/settings'))).toHaveLength(0)

    ports.advance(250)
    await tick()
    expect(calls.filter((c) => c.url.includes('/settings'))).toHaveLength(1)

    // Daarna is de server de baas: zijn echo zet de velden, en de velden
    // schrijven hem niet terug.
    ports.channel().emit({ type: 'settings', settings: { autonomous: false, eagerness: 'eager', cooldownSeconds: 300 } })
    flushSync()
    expect(store.settings.eagerness).toBe('eager')
    expect(screen.getByTestId('cooldown-value')).toHaveTextContent('300 s')
    ports.advance(250)
    await tick()
    expect(calls.filter((c) => c.url.includes('/settings'))).toHaveLength(1)
  })
})

describe('Release meeting stage', () => {
  it('shows one persistent latest answer with its own sources and files earlier ones in the session log', async () => {
    const { ports } = await mountLive()
    ports.channel().emit({ type: 'transcript', text: 'wat doen we met de duiven', speaker: '2', final: true })
    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'text', delta: 'First contribution.' })
    ports.channel().emit({ type: 'response_done', kbStatus: 'sources', sources: [{ number: 1, type: 'destillaat', text: 'First supporting source' }] })
    flushSync()
    expect(screen.getByTestId('latest-answer')).toHaveTextContent('First contribution.')
    ports.channel().emit({ type: 'transcript', text: 'en daarna', speaker: '2', final: true })
    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    ports.channel().emit({ type: 'text', delta: 'Second contribution.' })
    ports.channel().emit({ type: 'response_done', kbStatus: 'sources', sources: [{ number: 1, type: 'destillaat', text: 'Second supporting source' }] })
    flushSync()
    const current = screen.getByTestId('latest-answer')
    expect(current).toHaveTextContent('Second contribution.')
    expect(current).not.toHaveTextContent('First contribution.')
    expect(within(current).getByTestId('source-count')).toHaveTextContent('1 source')
    // Dicht staat het verleden niet in de weg; het zit in hetzelfde gebied als
    // het transcript, achter het pijltje.
    expect(screen.queryByText('First contribution.')).toBeNull()
    screen.getByTestId('transcript-toggle').click()
    flushSync()
    const log = screen.getByRole('log')
    expect(log).toHaveTextContent('First contribution.')
    expect(log).not.toHaveTextContent('Second contribution.')
    // De bijdrage staat op haar plek in het gesprek: na de zin waar zij op volgde.
    const order = log.textContent ?? ''
    expect(order.indexOf('wat doen we met de duiven')).toBeLessThan(order.indexOf('First contribution.'))
    expect(order.indexOf('First contribution.')).toBeLessThan(order.indexOf('en daarna'))
    expect(screen.getByTestId('stop-button')).toBeEnabled()
  })

  it('grows the answer while the deltas arrive instead of appearing at the end', async () => {
    const { ports } = await mountLive()
    ports.channel().emit({ type: 'persona', pigeonId: 'individualist' })
    flushSync()
    // Voordat er één woord binnen is staat de ballon er al: de duif is bezig.
    expect(screen.getByTestId('latest-answer')).toHaveTextContent('One moment…')
    ports.channel().emit({ type: 'text', delta: 'Pigeons nest ' })
    flushSync()
    expect(screen.getByTestId('latest-answer')).toHaveTextContent('Pigeons nest')
    expect(screen.getByTestId('latest-answer')).toHaveTextContent(/preparing a contribution/i)
    ports.channel().emit({ type: 'text', delta: 'on ledges.' })
    flushSync()
    expect(screen.getByTestId('latest-answer')).toHaveTextContent('Pigeons nest on ledges.')
    ports.channel().emit({ type: 'response_done', kbStatus: 'sources', sources: [] })
    flushSync()
    expect(screen.getByTestId('latest-answer')).toHaveTextContent('Pigeons nest on ledges.')
    expect(screen.getByTestId('latest-answer')).not.toHaveTextContent(/preparing a contribution/i)
  })

  it('exposes microphone checking as an explicit local action and leaves advanced settings closed', async () => {
    const { store, ports, calls } = await mount()
    expect(ports.mics).toHaveLength(0)
    expect(screen.getByText('Advanced meeting settings').closest('details')).not.toHaveAttribute('open')
    await fireEvent.click(screen.getByRole('button', { name: 'Check microphone & browser audio' }))
    expect(store.deviceCheck).toBe('checking')
    expect(ports.mics).toHaveLength(1)
    expect(calls.some((call) => call.url.endsWith('/audio'))).toBe(false)
    ports.advance(3000)
    flushSync()
    expect(screen.getByTestId('device-check-status')).toHaveTextContent('No sound reached the microphone')
    expect(ports.mic().stopped).toBe(true)
  })

  // De drie duiven delen in de vergadering hetzelfde rustbeeld, dus de
  // kleurcodering van het hoofdmenu is het enige dat de overgang draagt. Het
  // scherm zegt in data-pigeon welke duif het is; de stijl hangt daaraan.
  it('carries the identity of the seated pigeon on the meeting screen', async () => {
    const { store, ports } = await mount()

    const view = screen.getByTestId('meeting-view')
    expect(view).toHaveAttribute('data-pigeon', 'individualist')

    store.pigeonId = 'nonverbal'
    flushSync()
    expect(screen.getByTestId('meeting-view')).toHaveAttribute('data-pigeon', 'nonverbal')

    // Ook tijdens de vergadering, en dan volgt hij de server: die bepaalt welke
    // duif aan tafel zit.
    store.pigeonId = 'individualist'
    await store.start()
    await tick()
    flushSync()
    expect(screen.getByTestId('meeting-view')).toHaveAttribute('data-pigeon', 'individualist')
    ports.channel().emit({ type: 'state', sessionId: 'sessie-1', pigeonId: 'nonverbal', listening: 'listening' })
    flushSync()
    expect(screen.getByTestId('meeting-view')).toHaveAttribute('data-pigeon', 'nonverbal')
  })

  // Het verloop hoort in de linkerkolom onder de duif, niet als brede band
  // onder duif én antwoord. Het raster doet dat met namen, dus de DOM-volgorde
  // mag duif, antwoord, verloop zijn — precies de volgorde die een smal scherm
  // nodig heeft (verloop ónder het antwoord, niet ertussen).
  it('puts the session log in the pigeon column, after the answer in the DOM', async () => {
    await mountLive()

    const scene = document.querySelector('.stage-scene')
    expect(scene).not.toBeNull()
    const areas = [...scene!.children].map((el) => {
      if (el.classList.contains('pigeon-stage')) return 'pigeon'
      if (el.classList.contains('stage-answer')) return 'answer'
      if (el.classList.contains('stage-transcript')) return 'transcript'
      return el.className
    })
    expect(areas).toEqual(['pigeon', 'answer', 'transcript'])
    // Het verloop zit in het raster, niet als losse band eronder.
    expect(screen.getByTestId('session-transcript').closest('.stage-scene')).toBe(scene)
  })

  it('keeps the session log under the signalling pigeon, which has no answer column', async () => {
    await mountLive({ pigeon: 'nonverbal' })

    const scene = document.querySelector('.stage-scene')
    expect(scene?.classList.contains('signaling')).toBe(true)
    expect(document.querySelector('.stage-answer')).toBeNull()
    expect(screen.getByTestId('session-transcript').closest('.stage-scene')).toBe(scene)
  })

  // Één bedieningsset, nu met een icoon vóór elk opschrift. Het icoon is
  // aria-hidden — de tekst blijft het label — maar bij de microfoon draagt het
  // wel de stand, want die moet je kunnen zien zonder te lezen.
  it('gives every control in the single set an icon before its label', async () => {
    await mountLive()

    const iconOf = (testid: string) => screen.getByTestId(testid).querySelector('.btn-icon')
    for (const [testid, icon] of [
      ['stop-button', 'stop'],
      ['respond-button', 'respond'],
      ['call-control-mute', 'microphone-live'],
      ['call-control-end', 'end'],
    ] as const) {
      const mark = iconOf(testid)
      expect(mark, testid).not.toBeNull()
      expect(mark).toHaveAttribute('data-icon', icon)
      expect(mark).toHaveAttribute('aria-hidden', 'true')
      expect(mark!.querySelector('svg'), testid).not.toBeNull()
      // Het icoon staat vóór de tekst, en de tekst blijft er staan.
      expect(screen.getByTestId(testid).firstElementChild).toBe(mark)
      expect(screen.getByTestId(testid).textContent?.trim()).not.toBe('')
    }
  })

  it('shows the microphone state on the mute control, muted versus live', async () => {
    const { store } = await mountLive()

    const button = () => screen.getByTestId('call-control-mute')
    expect(button()).toHaveAttribute('data-mic', 'live')
    expect(button().querySelector('.btn-icon')).toHaveAttribute('data-icon', 'microphone-live')
    expect(button()).toHaveAttribute('aria-pressed', 'false')

    await fireEvent.click(button())
    flushSync()
    expect(store.muted).toBe(true)
    expect(button()).toHaveAttribute('data-mic', 'muted')
    expect(button().querySelector('.btn-icon')).toHaveAttribute('data-icon', 'microphone-muted')
    expect(button()).toHaveAttribute('aria-pressed', 'true')
    // Het opschrift blijft de handeling noemen, niet de stand.
    expect(button()).toHaveTextContent(/unmute microphone/i)
  })

  // Een geweigerde of gesloten microfoon is geen open microfoon: de knop is dan
  // uit, en het teken mag niet blijven zeggen dat er geluisterd wordt.
  it('shows a closed or refused microphone as off, not as live', async () => {
    const { store } = await mountLive()

    const button = () => screen.getByTestId('call-control-mute')
    expect(button()).toHaveAttribute('data-mic', 'live')

    for (const state of ['refused', 'closed'] as const) {
      store.localMic = state
      flushSync()
      expect(button(), state).toHaveAttribute('data-mic', 'off')
      expect(button().querySelector('.btn-icon'), state).toHaveAttribute('data-icon', 'microphone-off')
      expect(button(), state).toBeDisabled()
      // Het teken is er wel: een lege knop laat de stand niet zien.
      expect(button().querySelector('.btn-icon svg'), state).not.toBeNull()
    }

    // Zodra de microfoon weer open is, staat de stand er ook weer.
    store.localMic = 'open'
    flushSync()
    expect(button()).toHaveAttribute('data-mic', 'live')
    expect(button()).toBeEnabled()
  })

  // Een stop die de server niet bereikte is een storing in de bediening, geen
  // afgekapt geluid: de balk zegt dat ook.
  it('names a Stop that never reached the server as such in the banner', async () => {
    const { store } = await mountLive({
      routes: [{ method: 'POST', match: '/stop', respond: () => Promise.reject(new Error('offline')) }],
    })

    await store.stop()
    await tick()
    flushSync()

    const notice = screen.getByTestId('meeting-notice')
    expect(notice).toHaveAttribute('data-notice', 'stop-sync')
    expect(notice).toHaveTextContent(/did not confirm it/i)
    expect(notice).not.toHaveTextContent(/sound/i)
  })

  it('gives the signalling pigeon its own icon on the invitation', async () => {
    await mountLive({ pigeon: 'nonverbal' })

    const invite = screen.getByTestId('intervene-button')
    expect(invite.querySelector('.btn-icon')).toHaveAttribute('data-icon', 'intervene')
    expect(invite.querySelector('.btn-icon svg')).not.toBeNull()
  })

  it('shows a neutral visible signal and an operational sound failure without a text answer', async () => {
    const { ports } = await mountLive({ pigeon: 'nonverbal' })
    ports.channel().emit({ type: 'sound', eventId: 'event-1', sound: { id: 'clip', url: '/api/duif/v1/sounds/clip' } })
    flushSync()
    expect(screen.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'active')
    expect(screen.getByText('A signal is being shown.')).toBeInTheDocument()
    ports.soundPlayers.at(-1)!.fail()
    flushSync()
    expect(screen.getByTestId('meeting-notice')).toHaveTextContent('sound could not play')
    expect(screen.queryByTestId('latest-answer')).not.toBeInTheDocument()
    ports.advance(1500)
    flushSync()
    expect(screen.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'rest')
  })

  // Per arousal-band klinkt er een andere opname. Het
  // verschil mag hoorbaar zijn; het mag niet verklaard worden. Deze test speelt
  // alle drie de banden af in beide talen en leest daarna alles wat de zaal kan
  // zien of voorgelezen krijgt — inclusief aria-teksten, titels en alt-teksten.
  it('names no band, emotion or theme anywhere the participant can read or hear it', async () => {
    for (const lang of ['en', 'nl'] as const) {
      const { ports, locale } = await mountLive({ pigeon: 'nonverbal' })
      locale.choose(lang)
      flushSync()
      ports.channel().emit({ type: 'topic_hint', topic: { id: 'food', label: { en: 'Food supply', nl: 'Voedselaanbod' } }, score: 0.9 })
      ports.channel().emit({ type: 'intervention', fragment: 'hidden trigger', topic: { id: 'food' } })
      const spoken: string[] = []
      for (const band of ['calm', 'insistent', 'alarm']) {
        ports.channel().emit({ type: 'sound', eventId: `event-${band}-${lang}`, sound: { id: 'clip', url: '/api/duif/v1/sounds/clip', band }, durationMs: 1200 })
        flushSync()
        // Met het native pakket geïnstalleerd staat hier een andere opname dan
        // bij de vorige band; zonder dat pakket het geluid van de server zelf.
        expect(ports.soundPlayers.at(-1)!.url).toBeTruthy()
        readEverything(spoken) // terwijl het signaal loopt
        ports.soundPlayers.at(-1)!.end()
        flushSync()
        readEverything(spoken) // en erna
      }
      // Controle dat de scan iets leest: `listening|luistert` hoort hier wél in
      // te zitten, anders bewijst deze test niets.
      expect(spoken.some((text) => /\b(listening|luistert)\b/i.test(text))).toBe(true)
      const offending = spoken.filter((text) => SIGNAL_EXPLANATION.test(text))
      expect(offending, `${lang}: de deelnemersweergave legt het signaal uit`).toEqual([])
      cleanup()
    }
  })
})

// Zelfstandig meedoen is een standaard per duif. De signalerende
// duif heeft geen woorden: staat de schakelaar uit, dan gebeurt er aan tafel
// niets en lijkt zij stuk. Bij haar staat hij dus aan én in het zicht; bij de
// twee pratende duiven uit en onder "Geavanceerd", want hun bijdrage
// onderbreekt het gesprek.
describe('Autonomy default per pigeon', () => {
  const advanced = () => document.querySelector('details.advanced')
  const switchOf = (scope: HTMLElement) => within(scope).getByRole('switch')

  it('starts the signalling pigeon with autonomy on, on the setup card itself', async () => {
    const { store } = await mount()

    store.pigeonId = 'nonverbal'
    flushSync()

    expect(store.settings.autonomous).toBe(true)
    const field = screen.getByTestId('autonomy-field')
    expect(switchOf(field)).toBeChecked()
    // In het zicht betekent: buiten het dichtgeklapte paneel.
    expect(advanced()?.contains(field)).toBe(false)
    expect(within(screen.getByTestId('settings-fields')).queryByRole('switch')).toBeNull()
    expect(field).toHaveTextContent(/standaard aan|on by default/i)
  })

  it('starts the speaking pigeons with autonomy off, where the switch already was', async () => {
    const { store } = await mount()

    store.pigeonId = 'individualist'
    flushSync()

    expect(store.settings.autonomous).toBe(false)
    const field = screen.getByTestId('autonomy-field')
    expect(switchOf(field)).not.toBeChecked()
    expect(advanced()?.contains(field)).toBe(true)
    expect(field).toHaveTextContent(/standaard uit|off by default/i)
  })

  it('carries the pigeon default into the session the server opens', async () => {
    const { store, calls } = await mount()

    store.pigeonId = 'nonverbal'
    flushSync()
    await store.start()
    await tick()
    flushSync()

    const opened = calls.find((c) => c.url.endsWith('/meeting/sessions'))
    expect((opened?.body as { autonomous?: boolean }).autonomous).toBe(true)
    expect(store.settings.autonomous).toBe(true)
  })

  it('keeps a deliberate choice when the visitor looks at another pigeon', async () => {
    const { store } = await mount()

    store.pigeonId = 'nonverbal'
    flushSync()
    // Uitdrukkelijk uitgezet: dan hoort een blik op een andere duif die keuze
    // niet terug te draaien.
    store.updateSettings({ autonomous: false })
    store.pigeonId = 'individualist'
    store.pigeonId = 'nonverbal'
    flushSync()

    expect(store.settings.autonomous).toBe(false)
    expect(switchOf(screen.getByTestId('autonomy-field'))).not.toBeChecked()
  })

  // Alleen de schakelaar zelf bevriest zijn standaard. Gretigheid en
  // cooldown horen daar los van te staan; deden ze dat niet, dan stond de
  // signalerende duif na één draai aan de gretigheid op uit terwijl de hint
  // eronder nog "standaard aan" zei.
  it('keeps the pigeon default after another field on the card was changed', async () => {
    const { store } = await mount()

    store.pigeonId = 'individualist'
    flushSync()
    store.updateSettings({ eagerness: 'eager' })
    store.updateSettings({ cooldownSeconds: 120 })
    flushSync()

    store.pigeonId = 'nonverbal'
    flushSync()

    expect(store.settings.eagerness).toBe('eager')
    expect(store.settings.cooldownSeconds).toBe(120)
    expect(store.settings.autonomous).toBe(true)
    expect(switchOf(screen.getByTestId('autonomy-field'))).toBeChecked()
  })

  // Met autonomie aan hoort de
  // deelnemer te zien dát het geluid tegengehouden wordt, anders lijkt een
  // geblokkeerde browser op een duif die niets doet.
  it('says the browser is holding the sound back while she listens on her own', async () => {
    const { store } = await mountLive({ pigeon: 'nonverbal', ports: { suspended: true } })

    expect(store.settings.autonomous).toBe(true)
    expect(store.speaks).toBe(false)
    const notice = screen.getByTestId('meeting-notice')
    expect(notice).toHaveAttribute('data-notice', 'sound-blocked')
    // De tekst van de signalerende duif, niet die van een gesproken bijdrage:
    // het zichtbare signaal blijft staan.
    expect(notice).toHaveTextContent(/visual signal|zichtbare signaal/i)
    expect(screen.getByTestId('audio-enable')).toBeInTheDocument()
    // En zij is zichtbaar aan het luisteren, zonder dat de gebruiker iets doet.
    expect(screen.getByTestId('listen-indicator')).toHaveAttribute('data-state', 'listening')
  })
})
