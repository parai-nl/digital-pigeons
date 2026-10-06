// De leeskant van een beurt die tegen de tijdgrens aanliep.
//
// De server kapte zo'n beurt zwijgend af: geen `error`, geen `done`, de stroom
// sloot gewoon. Hij stuurt nu een `error` met de reden `turn_timeout` en daarna
// `done`. Deze tests dekken wat de lezer daarvan merkt, én de stiltes die
// overblijven als de verbinding zelf wegvalt — dan is er geen server meer om
// iets uit te leggen, en hoort de melding van de client te komen.
import { describe, expect, it } from 'vitest'
import { UiLocale } from '../i18n.svelte'
import { liveStream, pigeonRoute } from '../testing/chat'
import { scriptedFetch, type Route } from '../testing/fetch'
import { ChatStore } from './chatStore.svelte'
import { tick } from 'svelte'

const TIMEOUT_NL = 'Dit antwoord duurde te lang en is gestopt. De knop werkt nog — probeer het opnieuw, het liefst met een kortere of concretere vraag.'

function turns(respond: () => Response): Route {
  return { method: 'POST', match: '/chat/turn', respond }
}

async function settle(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0))
  await tick()
}

// De sessie-header hoort erbij: de echte server bevestigt de sessie vóór het
// eerste frame, en zonder haar zou de client de beurt als "niet bevestigd"
// terugdraaien in plaats van hem af te ronden.
function confirmedStream() {
  const live = liveStream()
  live.response.headers.set('X-Duif-Session-ID', 's-151')
  return live
}

async function chatWith(live: { response: Response }, pigeon?: string) {
  const scripted = scriptedFetch([pigeonRoute(), turns(() => live.response)])
  const locale = new UiLocale()
  locale.choose('nl')
  const chat = new ChatStore({ fetch: scripted.fetch }, locale)
  await chat.loadPigeons()
  if (pigeon) chat.selectPigeon(pigeon)
  return { chat, locale }
}

describe('een beurt die de tijdgrens van de server haalt', () => {
  it('toont de reden van de server en geeft de invoer weer vrij', async () => {
    const live = confirmedStream()
    const { chat } = await chatWith(live)
    const running = chat.send('Wat vindt de duif van de Coolsingel?')
    await settle()
    live.push({
      type: 'error',
      error: 'turn_timeout',
      message: { nl: TIMEOUT_NL, en: 'This answer took too long and was stopped.' },
    })
    live.push({ type: 'done', sessionId: 's-151', stopReason: 'error' })
    live.close()
    await running

    expect(chat.error?.reason).toBe('turn_timeout')
    expect(chat.errorText).toBe(TIMEOUT_NL)
    // De banner is geen waarschuwing maar een storing, en de invoer is vrij:
    // opnieuw proberen is precies wat de tekst vraagt.
    expect(chat.capExceeded).toBe(false)
    expect(chat.streaming).toBe(false)
  })

  it('leest zijn eigen catalogustekst als alleen de code overkomt', async () => {
    const live = confirmedStream()
    const { chat } = await chatWith(live)
    const running = chat.send('Wat vindt de duif van de Coolsingel?')
    await settle()
    live.push({ type: 'error', error: 'turn_timeout' })
    live.push({ type: 'done', sessionId: 's-151', stopReason: 'error' })
    live.close()
    await running

    expect(chat.errorText).toBe(TIMEOUT_NL)
  })

  it('houdt het halve duif-blok staan en markeert het als afgebroken', async () => {
    const live = confirmedStream()
    const { chat } = await chatWith(live, 'individualist')
    const running = chat.send('Wat vindt de duif van de Coolsingel?')
    await settle()
    live.push({ type: 'tool', id: 't1', name: 'ask_pigeon' })
    live.push({ type: 'artifact', id: 'p1', kind: 'pigeon_answer', persona: 'individualist' })
    live.push({ type: 'artifact_delta', id: 'p1', delta: 'De Coolsingel is' })
    await settle()
    live.push({ type: 'error', error: 'turn_timeout', message: { nl: TIMEOUT_NL, en: 'Too long.' } })
    live.push({ type: 'done', sessionId: 's-151', stopReason: 'error' })
    live.close()
    await running

    const answer = chat.messages.find((m) => m.kind === 'pigeon_answer')
    expect(answer).toMatchObject({ content: 'De Coolsingel is', partial: true, streaming: false })
    expect(chat.errorText).toBe(TIMEOUT_NL)
    // De wachtstand van de duif is weg: de teller hoort niet door te tikken
    // onder een beurt die al gestopt is.
    expect(chat.pigeonWaitOpen).toBe(false)
  })
})

describe('een beurt die de tijdgrens haalt voordat er iemand sprak', () => {
  // De `done` draagt de stopreden `error`. Die heeft geen melding op
  // het scherm, dus de facilitator-rij heeft niets te tonen en verdwijnt; de
  // chips van de beurt gaan met haar mee. Voorheen bleef er een lege kaart op
  // zijn naam staan met "(afgebroken)" eronder.
  it('laat alleen de vraag en de foutmelding achter', async () => {
    const live = confirmedStream()
    const { chat } = await chatWith(live, 'individualist')
    const running = chat.send('Wat vindt de duif van de Coolsingel?')
    await settle()
    live.push({ type: 'phase', phase: 'facilitator' })
    live.push({ type: 'tool', id: 't1', name: 'ask_pigeon' })
    live.push({ type: 'artifact', id: 'p1', kind: 'pigeon_answer', persona: 'individualist' })
    await settle()
    live.push({ type: 'error', error: 'turn_timeout', message: { nl: TIMEOUT_NL, en: 'Too long.' } })
    live.push({ type: 'done', sessionId: 's-151', stopReason: 'error' })
    live.close()
    await running

    expect(chat.messages.map((m) => m.role)).toEqual(['user'])
    expect(chat.errorText).toBe(TIMEOUT_NL)
    expect(chat.announcements).toEqual([])
  })

  it('houdt een rij die wel een gemelde stopreden draagt', async () => {
    const live = confirmedStream()
    const { chat } = await chatWith(live, 'individualist')
    const running = chat.send('Wat vindt de duif van de Coolsingel?')
    await settle()
    live.push({ type: 'done', sessionId: 's-151', stopReason: 'refusal' })
    live.close()
    await running

    // Een weigering zonder tekst is wél iets om te zien: de badge zegt het.
    expect(chat.messages.map((m) => [m.role, m.stopReason])).toEqual([['user', undefined], ['assistant', 'refusal']])
  })
})

describe('een stroom die zonder done sluit', () => {
  it('markeert de tekst die er stond als afgebroken en meldt de stilte', async () => {
    const live = confirmedStream()
    const { chat } = await chatWith(live)
    const running = chat.send('Wat vindt de duif van de Coolsingel?')
    await settle()
    live.push({ type: 'text', delta: 'De Coolsingel is' })
    await settle()
    live.close() // stil afgekapt: geen error, geen done
    await running

    expect(chat.messages[1]).toMatchObject({ content: 'De Coolsingel is', partial: true, streaming: false })
    // Zonder deze melding bleef de banner hier leeg: de lezer hield een half
    // antwoord over dat niets over zichzelf zei.
    expect(chat.error).toEqual({ status: 0 })
    expect(chat.errorText).toBe('Er ging iets mis. Probeer het opnieuw.')
    expect(chat.streaming).toBe(false)
  })

  it('meldt de stilte ook wanneer er nog geen woord binnen was', async () => {
    const live = confirmedStream()
    const { chat } = await chatWith(live)
    const running = chat.send('Wat vindt de duif van de Coolsingel?')
    await settle()
    live.close()
    await running

    expect(chat.error).toEqual({ status: 0 })
    expect(chat.streaming).toBe(false)
    // De lege antwoordrij verdwijnt; de vraag blijft staan.
    expect(chat.messages.map((m) => m.role)).toEqual(['user'])
  })

  it('meldt de stilte ook wanneer alleen de duif aan het woord was', async () => {
    const live = confirmedStream()
    const { chat } = await chatWith(live, 'individualist')
    const running = chat.send('Wat vindt de duif van de Coolsingel?')
    await settle()
    live.push({ type: 'artifact', id: 'p1', kind: 'pigeon_answer', persona: 'individualist' })
    live.push({ type: 'artifact_delta', id: 'p1', delta: 'De Coolsingel is' })
    await settle()
    live.close()
    await running

    const answer = chat.messages.find((m) => m.kind === 'pigeon_answer')
    expect(answer).toMatchObject({ content: 'De Coolsingel is', partial: true, streaming: false })
    expect(chat.error).toEqual({ status: 0 })
  })

  it('blijft zwijgen wanneer de lezer zelf stopt', async () => {
    const live = confirmedStream()
    const { chat } = await chatWith(live)
    const running = chat.send('Wat vindt de duif van de Coolsingel?')
    await settle()
    live.push({ type: 'text', delta: 'De Coolsingel is' })
    await settle()
    chat.abort()
    await running

    // Wie zelf op stop drukt krijgt geen foutmelding: de stilte is dan zijn
    // eigen keuze en niet die van de server.
    expect(chat.error).toBeNull()
    expect(chat.messages[1]).toMatchObject({ partial: true, streaming: false })
  })
})
