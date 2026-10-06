// Twee handelingen die de gebruiker uit een doodlopende straat halen: de
// uitweg uit "er is al een vergadering open" en het wisselen van duif
// terwijl de vergadering loopt. Ze delen één serverroute — een
// open-verzoek dat de plaats van je eigen sessie inneemt — en één regel: er
// gebeurt niets zonder dat de gebruiker het bevestigt.
import { describe, expect, it, vi } from 'vitest'
vi.mock('virtual:duif-media', () => ({ default: { signalSounds: null } }))
import { BLOCK_SAMPLES } from '../audio/pcm'
import { jsonResponse, type Route, scriptedFetch } from '../testing/fetch'
import { meetingHarness, meetingRoutes, OPEN_SESSION_ROUTE } from '../testing/meeting'

const tick = () => new Promise((resolve) => setTimeout(resolve, 0))

const posts = (calls: { url: string; init?: RequestInit }[], part: string, method = 'POST') =>
  calls.filter((c) => (c.init?.method ?? 'GET').toUpperCase() === method && c.url.includes(part))

const bodyOf = (call: { init?: RequestInit } | undefined) => JSON.parse(String(call?.init?.body ?? '{}'))

async function harnessWith(routes: Route[] = []) {
  const scripted = scriptedFetch([...routes, ...meetingRoutes()])
  const harness = meetingHarness(scripted.fetch)
  await harness.store.loadPigeons()
  return { ...harness, calls: scripted.calls }
}

// De open-route van een wissel: elke volgende sessie krijgt een eigen id, zodat
// een test kan zien dát er gewisseld is.
function switchingOpenRoute(): Route {
  let n = 0
  return {
    method: 'POST',
    match: OPEN_SESSION_ROUTE,
    respond: ({ body }) => {
      n += 1
      const req = body as Record<string, unknown>
      return jsonResponse(
        {
          sessionId: 'sessie-' + n,
          pigeonId: req.pigeonId,
          lang: req.lang,
          listening: 'listening',
          voice: 'ready',
          settings: { autonomous: req.autonomous, eagerness: req.eagerness, cooldownSeconds: req.cooldownSeconds },
        },
        201,
      )
    },
  }
}

describe('vastzitten door een eigen open vergadering', () => {
  const capacity = (reason: string): Route => ({
    method: 'POST',
    match: OPEN_SESSION_ROUTE,
    respond: () =>
      jsonResponse(
        { error: reason, message: { en: 'Still open elsewhere.', nl: 'Nog ergens open.' } },
        429,
      ),
  })

  it('biedt alleen een uitweg als het je eigen vergadering is', async () => {
    const { store } = await harnessWith([capacity('meeting_capacity_self')])
    await store.start()
    await tick()

    expect(store.phase).toBe('setup')
    expect(store.errorText).toBe('Still open elsewhere.')
    expect(store.stuck).toBe(true)
  })

  it('laat bij een tafel die door anderen bezet is geen knop zien', async () => {
    const { store } = await harnessWith([capacity('meeting_capacity')])
    await store.start()
    await tick()

    expect(store.errorText).toBe('Still open elsewhere.')
    expect(store.stuck).toBe(false)
    // Zonder uitweg is er ook niets te bevestigen.
    store.askReplace()
    expect(store.stuckConfirm).toBe(false)
  })

  it('sluit de andere vergadering pas na een uitdrukkelijk ja', async () => {
    let refuse = true
    const scripted = scriptedFetch([
      {
        method: 'POST',
        match: OPEN_SESSION_ROUTE,
        respond: ({ body }) => {
          if (refuse) {
            refuse = false
            return jsonResponse({ error: 'meeting_capacity_self', message: { en: 'Still open.', nl: 'Nog open.' } }, 429)
          }
          const req = body as Record<string, unknown>
          return jsonResponse({ sessionId: 'sessie-2', pigeonId: req.pigeonId, lang: req.lang, listening: 'listening', voice: 'ready' }, 201)
        },
      },
      ...meetingRoutes(),
    ])
    const { store } = meetingHarness(scripted.fetch)
    await store.loadPigeons()
    await store.start()
    await tick()
    expect(store.stuck).toBe(true)

    store.askReplace()
    expect(store.stuckConfirm).toBe(true)
    // Nee: er verandert niets, en er is niets naar de server gegaan.
    store.cancelReplace()
    expect(store.stuckConfirm).toBe(false)
    expect(posts(scripted.calls, '/meeting/sessions')).toHaveLength(1)

    store.askReplace()
    await store.confirmReplace()
    await tick()

    const opens = posts(scripted.calls, '/meeting/sessions')
    expect(opens).toHaveLength(2)
    expect(bodyOf(opens[0])).not.toHaveProperty('replaceExisting')
    expect(bodyOf(opens[1])).toMatchObject({ replaceExisting: true })
    expect(store.live).toBe(true)
    expect(store.sessionId).toBe('sessie-2')
    expect(store.errorText).toBe('')
  })
})

describe('een andere duif aan tafel', () => {
  it('vraagt het pas en verandert niets bij nee', async () => {
    const { store, ports, calls } = await harnessWith([switchingOpenRoute()])
    await store.start()
    await tick()

    expect(store.requestPigeonSwitch('individualist')).toBe(false) // zit er al
    expect(store.requestPigeonSwitch('biological')).toBe(false) // vergadert niet
    expect(store.requestPigeonSwitch('nonverbal')).toBe(true)
    expect(store.pendingSwitch).toBe('nonverbal')

    store.cancelPigeonSwitch()
    expect(store.pendingSwitch).toBe('')
    expect(store.pigeonId).toBe('individualist')
    expect(posts(calls, '/meeting/sessions')).toHaveLength(1)
    expect(ports.mic().stopped).toBe(false)
  })

  it('doet niets zolang er geen vergadering loopt', async () => {
    const { store } = await harnessWith([switchingOpenRoute()])
    expect(store.requestPigeonSwitch('nonverbal')).toBe(false)
    expect(store.pendingSwitch).toBe('')
  })

  it('wisselt bij ja zonder de vergadering te verlaten en draagt de context over', async () => {
    const { store, ports, calls } = await harnessWith([switchingOpenRoute()])
    await store.start()
    await tick()

    const firstChannel = ports.channel()
    const mic = ports.mic()
    const ctx = ports.context()
    firstChannel.emit({ type: 'transcript', sessionId: 'sessie-1', text: 'We willen ze weren.', speaker: 'spreker-1', final: true })
    firstChannel.emit({ type: 'response_done', sessionId: 'sessie-1', turnId: '1' })
    expect(store.transcript).toHaveLength(1)

    store.requestPigeonSwitch('nonverbal')
    await store.confirmPigeonSwitch()
    await tick()

    const opens = posts(calls, '/meeting/sessions')
    expect(opens).toHaveLength(2)
    // `replaceExisting` hoort erbij: een wissel neemt de eigen plaats in, ook
    // als de sessie waarnaar hij wijst intussen vanzelf gesloten is.
    expect(bodyOf(opens[1])).toMatchObject({ pigeonId: 'nonverbal', handoverFrom: 'sessie-1', replaceExisting: true })

    // De vergadering loopt door: dezelfde microfoon, dezelfde audiocontext,
    // hetzelfde transcript op het scherm — maar een nieuw kanaal en een nieuwe
    // duif. De vorige sessie hoeft niet apart beëindigd te worden: de server
    // sluit haar met de overdracht.
    expect(store.live).toBe(true)
    expect(store.pigeonId).toBe('nonverbal')
    expect(store.sessionId).toBe('sessie-2')
    expect(mic.stopped).toBe(false)
    expect(store.localMic).toBe('open')
    expect(ctx.closed).toBe(false)
    expect(store.transcript).toHaveLength(1)
    expect(store.answers).toHaveLength(0)
    expect(firstChannel.closed).toBe(true)
    expect(ports.channel().url).toContain('/meeting/sessions/sessie-2/events')
    expect(posts(calls, '/meeting/sessions/sessie-1', 'DELETE')).toHaveLength(0)

    // En het spoor loopt door naar de nieuwe sessie.
    mic.push(new Int16Array(BLOCK_SAMPLES))
    await tick()
    await tick()
    expect(posts(calls, '/meeting/sessions/sessie-2/audio').length).toBeGreaterThan(0)
  })

  it('neemt de klaarliggende bijdrage van de vorige duif niet mee', async () => {
    const { store, ports } = await harnessWith([switchingOpenRoute()])
    await store.start()
    await tick()

    ports.channel().emit({
      type: 'candidate',
      sessionId: 'sessie-1',
      candidate: { id: 'kandidaat-1', reason: 'topic', score: 0.8, sources: 2, topic: { id: 'weren', label: { en: 'Deterrence', nl: 'Weren' } } },
    })
    expect(store.candidate?.id).toBe('kandidaat-1')

    store.requestPigeonSwitch('nonverbal')
    await store.confirmPigeonSwitch()
    await tick()

    // "De duif heeft iets over weren" was van de vorige duif. De nieuwe duif
    // heeft nog niets klaar; het blok hoort dus weg te zijn, niet over te gaan.
    expect(store.candidate).toBe(null)
    expect(store.candidateInFlight).toBe(false)
  })

  it('laat de laatste bijdrage van de vorige duif staan tot de nieuwe iets zegt', async () => {
    const { store, ports } = await harnessWith([switchingOpenRoute()])
    await store.start()
    await tick()

    const first = ports.channel()
    first.emit({ type: 'persona', sessionId: 'sessie-1', pigeonId: 'individualist', turnId: '1' })
    first.emit({ type: 'text', sessionId: 'sessie-1', turnId: '1', delta: 'Roekoe. Daar heb ik wel wat over te zeggen.' })
    first.emit({ type: 'response_done', sessionId: 'sessie-1', turnId: '1' })
    expect(store.answers).toHaveLength(1)

    store.requestPigeonSwitch('nonverbal')
    await store.confirmPigeonSwitch()
    await tick()

    // De lijst van de nieuwe sessie is leeg — haar woorden worden nooit die van
    // de nieuwe duif — maar haar laatste kaart blijft staan, met haar naam.
    expect(store.answers).toHaveLength(0)
    expect(store.carried).toMatchObject({
      pigeonId: 'individualist',
      text: 'Roekoe. Daar heb ik wel wat over te zeggen.',
      status: 'complete',
    })

    // Zodra de nieuwe duif het woord neemt, gaat de kaart van haar voorganger weg.
    ports.channel().emit({ type: 'persona', sessionId: 'sessie-2', pigeonId: 'nonverbal' })
    expect(store.carried).toBeNull()
  })

  it('zegt het als de wissel de vorige duif midden in een zin afbrak', async () => {
    const { store, ports } = await harnessWith([switchingOpenRoute()])
    await store.start()
    await tick()

    const first = ports.channel()
    first.emit({ type: 'persona', sessionId: 'sessie-1', pigeonId: 'individualist', turnId: '1' })
    first.emit({ type: 'text', sessionId: 'sessie-1', turnId: '1', delta: 'Roekoe. Ik was nog niet' })

    store.requestPigeonSwitch('nonverbal')
    await store.confirmPigeonSwitch()
    await tick()

    expect(store.carried).toMatchObject({ pigeonId: 'individualist', text: 'Roekoe. Ik was nog niet', status: 'interrupted' })
  })

  it('houdt een lege kaart weg: zonder woorden valt er niets te laten staan', async () => {
    const { store, ports } = await harnessWith([switchingOpenRoute()])
    await store.start()
    await tick()

    ports.channel().emit({ type: 'persona', sessionId: 'sessie-1', pigeonId: 'individualist' })
    store.requestPigeonSwitch('nonverbal')
    await store.confirmPigeonSwitch()
    await tick()

    expect(store.carried).toBeNull()
  })

  it('laat de vergadering staan als de wissel niet lukt', async () => {
    let first = true
    const scripted = scriptedFetch([
      {
        method: 'POST',
        match: OPEN_SESSION_ROUTE,
        respond: ({ body }) => {
          if (!first) return jsonResponse({ error: 'meeting_capacity', message: { en: 'Full.', nl: 'Vol.' } }, 429)
          first = false
          const req = body as Record<string, unknown>
          return jsonResponse({ sessionId: 'sessie-1', pigeonId: req.pigeonId, lang: req.lang, listening: 'listening', voice: 'ready' }, 201)
        },
      },
      ...meetingRoutes(),
    ])
    const { store, ports } = meetingHarness(scripted.fetch)
    await store.loadPigeons()
    await store.start()
    await tick()
    const mic = ports.mic()

    store.requestPigeonSwitch('nonverbal')
    await store.confirmPigeonSwitch()
    await tick()

    expect(store.live).toBe(true)
    expect(store.pigeonId).toBe('individualist')
    expect(store.sessionId).toBe('sessie-1')
    expect(store.errorText).toBe('Full.')
    expect(mic.stopped).toBe(false)
    // Er staat weer een kanaal op de oude sessie: het scherm is niet doof.
    expect(ports.channel().url).toContain('/meeting/sessions/sessie-1/events')
    expect(ports.channel().closed).toBe(false)
  })

  // Een wissel is een nieuwe vergadering met een nieuwe duif, dus
  // hoort ook haar eigen standaardstand voor zelfstandig meedoen mee te gaan.
  // Ging de stand van de vorige duif mee, dan zweeg de signalerende duif de
  // hele vergadering.
  // De open-verzoeken, en niets anders: een settings-POST staat op dezelfde
  // sessieroute en zou de telling anders verschuiven.
  const opens = (calls: { url: string; init?: RequestInit }[]) =>
    calls.filter((c) => (c.init?.method ?? 'GET').toUpperCase() === 'POST' && c.url.endsWith('/meeting/sessions'))

  it('geeft de nieuwe duif haar eigen stand voor zelfstandig meedoen', async () => {
    const { store, calls } = await harnessWith([switchingOpenRoute()])
    await store.start()
    await tick()

    // De stakeholder begon uit; de signalerende duif hoort aan te gaan.
    expect(bodyOf(opens(calls)[0])).toMatchObject({ pigeonId: 'individualist', autonomous: false })

    store.requestPigeonSwitch('nonverbal')
    await store.confirmPigeonSwitch()
    await tick()

    expect(bodyOf(opens(calls)[1])).toMatchObject({ pigeonId: 'nonverbal', autonomous: true })
    expect(store.settings.autonomous).toBe(true)

    // En terug: de stakeholder erft het meedoen van de signalerende duif niet.
    store.requestPigeonSwitch('individualist')
    await store.confirmPigeonSwitch()
    await tick()

    expect(bodyOf(opens(calls)[2])).toMatchObject({ pigeonId: 'individualist', autonomous: false })
    expect(store.settings.autonomous).toBe(false)
  })

  it('laat een stand die de bezoeker zelf koos ongemoeid bij een wissel', async () => {
    const { store, calls, ports } = await harnessWith([switchingOpenRoute()])
    await store.start()
    await tick()

    // Uitdrukkelijk aangezet bij de stakeholder: dat is een keuze over deze
    // vergadering, geen standaard van een duif, dus hij reist mee.
    store.updateSettings({ autonomous: true })
    ports.advance(1000)
    await tick()

    store.requestPigeonSwitch('nonverbal')
    await store.confirmPigeonSwitch()
    await tick()
    expect(bodyOf(opens(calls)[1])).toMatchObject({ pigeonId: 'nonverbal', autonomous: true })

    // En uitdrukkelijk uit blijft uit, ook bij de duif die standaard aan staat.
    store.updateSettings({ autonomous: false })
    ports.advance(1000)
    await tick()
    store.requestPigeonSwitch('individualist')
    await store.confirmPigeonSwitch()
    await tick()
    expect(bodyOf(opens(calls)[2])).toMatchObject({ pigeonId: 'individualist', autonomous: false })
  })
})

describe('het tabblad dat wegvalt', () => {
  it('beëindigt de sessie met keepalive, zodat een wegnavigerende pagina hem niet afkapt', async () => {
    const { store, calls } = await harnessWith([switchingOpenRoute()])
    await store.start()
    await tick()

    globalThis.dispatchEvent(new Event('pagehide'))
    const del = posts(calls, '/meeting/sessions/sessie-1', 'DELETE')
    expect(del).toHaveLength(1)
    expect(del[0]?.init?.keepalive).toBe(true)
  })

  it('gebruikt keepalive ook bij een gewoon einde en bij unmount', async () => {
    const { store, calls } = await harnessWith([switchingOpenRoute()])
    await store.start()
    await tick()
    store.end()
    await tick()

    const del = posts(calls, '/meeting/sessions/sessie-1', 'DELETE')
    expect(del).toHaveLength(1)
    expect(del[0]?.init?.keepalive).toBe(true)
  })
})
