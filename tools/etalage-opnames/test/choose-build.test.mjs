import assert from 'node:assert/strict'
import { test } from 'node:test'
import { compile } from '../lib/build.mjs'
import { autoChoose, choiceProblems, expectedItems, takeProblems } from '../lib/choose.mjs'
import { CHAT_ROUTES, HISTORICAL_MODELS, MEETING_PIGEONS, MODEL, endpointOrigin, modelAllowed } from '../lib/config.mjs'
import { policyProblem } from '../lib/server-info.mjs'
import { ENDPOINT, chatExchange, chatTake, meetingTake, SOURCE, VRAGEN } from './fixtures.mjs'

const FACTS = { sources: 120, claims: 4067, passages: 4225 }

// Een volledige set takes voor de fixture-vragen: vier chatroutes en drie
// vergaderduiven per taal. `stamp` maakt een tweede ronde met andere id's,
// `model` een ronde met een ander model (bijvoorbeeld een historische).
function fullSet(stamp = '100000', model = MODEL) {
  const takes = []
  for (const lang of ['nl', 'en']) {
    const direct = {}
    for (const route of CHAT_ROUTES) {
      const [from, to] = route.split('>')
      const id = `chat-q1-${lang}-${route.replace('>', '-naar-')}-20260923T${stamp}Z`
      const persona = { kennispartner: 'biological', stakeholder: 'individualist' }[to ?? from]
      const take = chatTake({ id, route, lang, model, recordedAt: `2026-09-23T${stamp.slice(0, 2)}:${stamp.slice(2, 4)}:00.000Z`, basedOn: to ? direct[from] : undefined, sourceSessionId: to ? `s-${direct[from]}` : undefined })
      if (!to) direct[from] = id
      takes.push(take)
    }
    for (const pigeon of MEETING_PIGEONS) {
      const events = pigeon === 'signalerend'
        ? [[11600, { type: 'sound', sound: { band: 'calm' } }]]
        : pigeon === 'kennispartner'
          ? [[17000, { type: 'persona' }], [17002, { type: 'text', delta: 'Bijdrage [1].' }], [17400, { type: 'tts_audio', format: 'pcm16' }], [30000, { type: 'response_done', sources: [SOURCE] }]]
          : []
      takes.push(meetingTake({ id: `meeting-s1-${lang}-${pigeon}-20260923T${stamp}Z`, pigeon, lang, model, events, voice: pigeon === 'kennispartner' ? [{ file: 'stem.mp3', format: 'pcm16', sampleRate: 24000, ms: 12000 }] : [], recordedAt: `2026-09-23T${stamp.slice(0, 2)}:${stamp.slice(2, 4)}:00.000Z` }))
    }
  }
  return takes
}
const BANDS = { calm: 'pigeon-field-coo.mp3' }

test('elk item van het contract: vier chatroutes en drie vergaderduiven per taal', () => {
  const keys = expectedItems(VRAGEN)
  assert.equal(keys.length, 2 * 4 + 2 * 3)
  assert.ok(keys.includes('chat/q1/nl/kennispartner>stakeholder'))
  assert.ok(keys.includes('meeting/s1/en/signalerend'))
})

test('automatisch kiezen neemt de nieuwste bruikbare take, en een vergelijking volgt haar brongesprek', () => {
  const takes = [...fullSet('100000'), ...fullSet('110000')]
  // De nieuwste vergelijking van de Stakeholder is afgebroken: niet bruikbaar.
  const broken = takes.find((t) => t.id === 'chat-q1-nl-kennispartner-naar-stakeholder-20260923T110000Z')
  Object.assign(broken, chatTake({ id: broken.id, route: broken.item.route, basedOn: broken.item.basedOn, sourceSessionId: 's-chat-q1-nl-kennispartner-20260923T100000Z', exchange: chatExchange({ persona: 'individualist', stopReason: 'truncated' }) }))
  const { keuze } = autoChoose({ items: {} }, takes, VRAGEN, { bands: BANDS })
  assert.equal(keuze.items['chat/q1/nl/kennispartner'].take, 'chat-q1-nl-kennispartner-20260923T110000Z')
  // De enige bruikbare vergelijking bouwt voort op het oudere brongesprek; die
  // mag niet naast het nieuwere brongesprek staan.
  assert.equal(keuze.items['chat/q1/nl/kennispartner>stakeholder'], undefined)
  assert.equal(keuze.items['chat/q1/nl/stakeholder>kennispartner'].take, 'chat-q1-nl-stakeholder-naar-kennispartner-20260923T110000Z')
  const byId = new Map(takes.map((t) => [t.id, t]))
  assert.deepEqual(choiceProblems(keuze, byId, VRAGEN, { bands: BANDS }), ['chat/q1/nl/kennispartner>stakeholder: geen take gekozen'])
})

test('een handmatige keuze blijft staan; een tegenstrijdige vergelijking wordt gemeld', () => {
  const takes = [...fullSet('100000'), ...fullSet('110000')]
  const manual = { items: { 'chat/q1/nl/kennispartner>stakeholder': { take: 'chat-q1-nl-kennispartner-naar-stakeholder-20260923T100000Z', reason: 'mooiste formulering' } } }
  const { keuze } = autoChoose(manual, takes, VRAGEN, { bands: BANDS })
  assert.equal(keuze.items['chat/q1/nl/kennispartner>stakeholder'].take, 'chat-q1-nl-kennispartner-naar-stakeholder-20260923T100000Z')
  assert.equal(keuze.items['chat/q1/nl/kennispartner>stakeholder'].auto, undefined)
  const problems = choiceProblems(keuze, new Map(takes.map((t) => [t.id, t])), VRAGEN, { bands: BANDS })
  // Twee keer hetzelfde probleem: volgens de take (basedOn) en volgens de
  // opname zelf (uit welk gesprek de vergelijking kopieerde).
  assert.equal(problems.length, 2)
  assert.match(problems[0], /bouwt voort op chat-q1-nl-kennispartner-20260923T100000Z, maar voor het brongesprek is chat-q1-nl-kennispartner-20260923T110000Z gekozen/)
  assert.match(problems[1], /kopieerde uit gesprek s-chat-q1-nl-kennispartner-20260923T100000Z, niet uit het gesprek van chat-q1-nl-kennispartner-20260923T110000Z/)
})

test('een take met een andere vraag, een ander model of een mislukking komt er niet op', () => {
  const take = chatTake()
  assert.deepEqual(takeProblems(take, VRAGEN), [])
  assert.match(takeProblems({ ...take, input: { text: 'Oude plaatshouder?' } }, VRAGEN)[0], /andere tekst/)
  assert.match(takeProblems({ ...take, server: { ...take.server, modelPolicy: { model: 'x', detectionModel: 'x', endpoint: 'y' } } }, VRAGEN)[0], /modelbeleid/)
  // Een endpoint dat geen HTTPS-oorsprong is (zoals de offline sentinel), ook.
  for (const endpoint of ['synthetic-loopback', 'http://elders.example', undefined]) assert.match(takeProblems({ ...take, server: { ...take.server, modelPolicy: { ...take.server.modelPolicy, endpoint } } }, VRAGEN)[0], /modelbeleid/)
  assert.match(takeProblems({ ...take, failure: 'foutframe' }, VRAGEN)[0], /mislukt/)
})

// De wissel van vertex_ai/gemini-3.5-flash naar vertex/gemini-3.6-flash-eu
// kwam op 24 september 2026 op main. Opnemen gaat alleen met
// het huidige model; de takes van 23 september blijven eerlijke herkomst van
// het model van de module van toen.
const OLD = Object.keys(HISTORICAL_MODELS)[0]

test('opnemen weigert elk model behalve het huidige; kiezen en bouwen aanvaarden een historisch model', () => {
  assert.equal(MODEL, 'vertex/gemini-3.6-flash-eu')
  assert.equal(policyProblem({ model: MODEL, detectionModel: MODEL, endpoint: ENDPOINT }, ENDPOINT), null)
  assert.match(policyProblem({ model: MODEL, detectionModel: MODEL, endpoint: 'https://other.invalid' }, ENDPOINT), /endpoint is/)
  assert.match(policyProblem({ model: MODEL, detectionModel: MODEL, endpoint: ENDPOINT }, null), /PARAI_LLM_BASE_URL/)
  assert.equal(endpointOrigin('https://llm.example.invalid:443/v1/'), 'https://llm.example.invalid')
  for (const raw of ['http://llm.example.invalid', 'https://user:x@llm.example.invalid', 'https://llm.example.invalid?k=1', '', undefined]) assert.equal(endpointOrigin(raw), null)
  for (const old of Object.keys(HISTORICAL_MODELS)) {
    assert.match(policyProblem({ model: old, detectionModel: old, endpoint: ENDPOINT }, ENDPOINT), /chatmodel is vertex_ai\/gemini-3\.5-flash/)
    assert.match(policyProblem({ model: MODEL, detectionModel: old, endpoint: ENDPOINT }, ENDPOINT), /detectiemodel/)
  }
  const old = OLD
  const takes = fullSet('100000', old)
  assert.deepEqual(takeProblems(takes[0], VRAGEN), [])
  const { keuze } = autoChoose({ items: {} }, takes, VRAGEN, { bands: BANDS })
  const { opnames } = compile({ vragen: VRAGEN, keuze, takesById: new Map(takes.map((t) => [t.id, { ...t, dir: '/nergens' }])), facts: FACTS, bands: BANDS })
  // Het model in opnames.json is dat van de takes, niet dat van de huidige regel.
  assert.equal(opnames.recording.model, old)
})

test('een eerder model geldt alleen voor takes van vóór de wissel', () => {
  assert.equal(modelAllowed(OLD, '2026-09-23T14:46:55.746Z'), true)
  assert.equal(modelAllowed(OLD, '2026-09-24T23:59:59.000Z'), true)
  assert.equal(modelAllowed(OLD, '2026-09-25T00:00:00.000Z'), false)
  assert.equal(modelAllowed(OLD, undefined), false)
  assert.equal(modelAllowed(MODEL, '2027-01-01T00:00:00.000Z'), true)
  assert.equal(modelAllowed('toString', '2026-09-23T10:00:00.000Z'), false)
  // Een nieuwe take met het oude model: kiezen en bouwen weigeren.
  const late = '2026-09-26T10:00:00.000Z'
  const chat = chatTake({ model: OLD, recordedAt: late })
  assert.deepEqual(takeProblems(chat, VRAGEN), [`opgenomen op ${late} met ${OLD}; dat mag alleen vóór ${HISTORICAL_MODELS[OLD]}`])
  const meeting = meetingTake({ model: OLD, recordedAt: late })
  assert.match(takeProblems(meeting, VRAGEN)[0], /dat mag alleen vóór/)
  const takes = fullSet('100000', OLD).map((t) => ({ ...t, recordedAt: late }))
  const keuze = { items: {} }
  for (const t of takes) {
    const key = t.kind === 'chat' ? `chat/q1/${t.item.lang}/${t.item.route}` : `meeting/s1/${t.item.lang}/${t.item.pigeon}`
    keuze.items[key] = { take: t.id, reason: 'test' }
  }
  assert.throws(() => compile({ vragen: VRAGEN, keuze, takesById: new Map(takes.map((t) => [t.id, { ...t, dir: '/nergens' }])), facts: FACTS, bands: BANDS }), /dat mag alleen vóór/)
})

test('een take met een onbekend model, gemengde modellen of een ander detectiemodel komt er niet op', () => {
  const old = OLD
  const take = chatTake({ model: 'other/unknown-model' })
  assert.match(takeProblems(take, VRAGEN)[0], /modelbeleid/)
  const mixed = chatTake({ model: old })
  mixed.server = { ...mixed.server, modelPolicy: { ...mixed.server.modelPolicy, detectionModel: MODEL } }
  assert.match(takeProblems(mixed, VRAGEN)[0], /modelbeleid/)
  // Het done-frame moet het model noemen dat de server meldde.
  const otherFrame = chatTake({ model: old, exchange: chatExchange({ model: MODEL }) })
  assert.deepEqual(takeProblems(otherFrame, VRAGEN), [`het done-frame meldt model ${MODEL}`])
  // Een set waarin de ene helft met het oude en de andere met het nieuwe model
  // is opgenomen, geeft geen eenduidige herkomst: de bouw weigert.
  const oldSet = fullSet('100000', old)
  const newSet = fullSet('110000', MODEL)
  const keuze = { items: {} }
  for (const t of [...oldSet.filter((x) => x.item.lang === 'nl'), ...newSet.filter((x) => x.item.lang === 'en')]) {
    const key = t.kind === 'chat' ? `chat/q1/${t.item.lang}/${t.item.route}` : `meeting/s1/${t.item.lang}/${t.item.pigeon}`
    keuze.items[key] = { take: t.id, reason: 'test' }
  }
  const byId = new Map([...oldSet, ...newSet].map((t) => [t.id, { ...t, dir: '/nergens' }]))
  assert.throws(() => compile({ vragen: VRAGEN, keuze, takesById: byId, facts: FACTS, bands: BANDS }), /melden modellen/)
})

// Gemuteerde takes: de samenvatting in de take zegt dat alles klopt, de ruwe
// opname niet. Kiezen gaat op de ruwe opname.
test('chat: model en opslag komen uit het done-frame en de teruglezing, niet uit de samenvatting', () => {
  const otherModel = chatTake({ exchange: chatExchange({ model: 'openai/iets-anders' }) })
  assert.equal(otherModel.provenance.model, MODEL)
  assert.deepEqual(takeProblems(otherModel, VRAGEN), ['het done-frame meldt model openai/iets-anders'])
  const otherStored = chatTake({ stored: ['Een andere tekst dan gestreamd.'] })
  assert.equal(otherStored.checks.storedEqualsStreamed, true)
  assert.deepEqual(takeProblems(otherStored, VRAGEN), ['de opgeslagen tekst wijkt af van de gestreamde'])
  const noReadBack = chatTake()
  noReadBack.exchanges = noReadBack.exchanges.filter((x) => x.method !== 'GET')
  assert.deepEqual(takeProblems(noReadBack, VRAGEN), ['het gesprek is na de beurt niet teruggelezen'])
  const otherPigeon = chatTake()
  otherPigeon.exchanges[0].requestBody = { ...otherPigeon.exchanges[0].requestBody, pigeonId: 'biological' }
  assert.match(takeProblems(otherPigeon, VRAGEN)[0], /ging naar biological, niet naar individualist/)
})

test('vergadering: stem, transcript, kanaal, duif en taal worden gecontroleerd', () => {
  const speech = (extra = {}) => meetingTake({
    id: 'meeting-s1-nl-kennispartner-20260923T100000Z',
    pigeon: 'kennispartner',
    events: [[17000, { type: 'persona' }], [17002, { type: 'text', delta: 'Bijdrage [1].' }], [17400, { type: 'tts_audio', format: 'pcm16' }], [30000, { type: 'response_done', sources: [SOURCE] }]],
    voice: [{ file: 'stem.mp3', format: 'pcm16', sampleRate: 24000, ms: 12000 }],
    ...extra,
  })
  assert.deepEqual(takeProblems(speech(), VRAGEN), [])
  // Tekst zonder stem: zoals een tts_start_failed.
  const mute = speech({ voice: [], events: [[17000, { type: 'persona' }], [17002, { type: 'text', delta: 'Bijdrage.' }], [30000, { type: 'response_done' }]] })
  assert.deepEqual(takeProblems(mute, VRAGEN), ['gesproken reactie zonder stem'])
  assert.deepEqual(takeProblems(speech({ transcript: false }), VRAGEN), ['de spraakherkenning gaf geen definitief transcript'])
  const failed = speech({ events: [[12000, { type: 'error', reason: 'tts_start_failed' }], [17000, { type: 'persona' }], [17002, { type: 'text', delta: 'Bijdrage.' }], [17400, { type: 'tts_audio' }], [30000, { type: 'response_done' }]] })
  assert.deepEqual(takeProblems(failed, VRAGEN), ['fout op het vergaderkanaal: tts_start_failed'])
  const stopped = speech({ events: [[16000, { type: 'stopped' }], [17000, { type: 'persona' }], [17002, { type: 'text', delta: 'Bijdrage.' }], [17400, { type: 'tts_audio' }], [30000, { type: 'response_done' }]] })
  assert.deepEqual(takeProblems(stopped, VRAGEN), ['de duif is op het vergaderkanaal afgebroken (stopped)'])
  // De samenvatting zegt kennispartner; de server opende de vergadering met een andere duif.
  assert.match(takeProblems(speech({ opened: { pigeonId: 'individualist' } }), VRAGEN)[0], /aan tafel zat individualist, niet biological/)
  assert.match(takeProblems(speech({ opened: { lang: 'en' } }), VRAGEN)[0], /liep in en, niet in nl/)
  assert.match(takeProblems(speech({ opened: { autonomous: false } }), VRAGEN)[0], /zelfstandig meedoen/)
})

test('een vergelijking die uit een ander gesprek kopieerde dan het gekozen brongesprek valt op', () => {
  const direct = chatTake({ id: 'chat-q1-nl-kennispartner-20260923T100000Z', route: 'kennispartner', exchange: chatExchange({ persona: 'biological', sessionId: 's-bron' }), sessionId: 's-bron' })
  const comparison = chatTake({ id: 'chat-q1-nl-kennispartner-naar-stakeholder-20260923T100000Z', route: 'kennispartner>stakeholder', basedOn: direct.id, sourceSessionId: 's-ander' })
  const keuze = { items: { 'chat/q1/nl/kennispartner': { take: direct.id, reason: 'x' }, 'chat/q1/nl/kennispartner>stakeholder': { take: comparison.id, reason: 'x' } } }
  const problems = choiceProblems(keuze, new Map([direct, comparison].map((t) => [t.id, t])), VRAGEN).filter((p) => p.startsWith('chat/q1/nl/kennispartner>'))
  assert.deepEqual(problems, [`chat/q1/nl/kennispartner>stakeholder: kopieerde uit gesprek s-ander, niet uit het gesprek van ${direct.id}`])
})

test('de bouw levert precies de vorm van het contract', () => {
  const takes = fullSet()
  const { keuze } = autoChoose({ items: {} }, takes, VRAGEN, { bands: BANDS })
  const { opnames, voices, sounds, warnings } = compile({ vragen: VRAGEN, keuze, takesById: new Map(takes.map((t) => [t.id, { ...t, dir: '/nergens' }])), facts: FACTS, bands: BANDS })
  assert.deepEqual(warnings, [])
  assert.equal(opnames.schema, 1)
  const r = opnames.recording
  assert.equal(r.model, MODEL)
  // Het endpoint en de bouwgegevens van de server staan in de takes, niet op
  // de publieke pagina.
  for (const key of ['endpoint', 'moduleTree', 'paraiCore', 'database']) assert.equal(key in r, false, key)
  assert.match(r.moduleCommit, /^[0-9a-f]{40}$/)
  assert.equal(r.knowledgeBase, 'Duiven')
  assert.equal(r.placeholder, true)
  assert.ok(r.recordedFrom <= r.recordedTo)
  assert.deepEqual(opnames.facts, FACTS)
  const q = opnames.chat.questions[0]
  assert.deepEqual(q.text, VRAGEN.chat[0].text)
  for (const lang of ['nl', 'en']) {
    assert.deepEqual(Object.keys(q.turns[lang]), CHAT_ROUTES)
    for (const turn of Object.values(q.turns[lang])) {
      assert.match(turn.take, /^chat-/)
      for (const m of turn.messages) {
        assert.ok(['facilitator', 'kennispartner', 'stakeholder'].includes(m.author))
        if (m.author !== 'facilitator') {
          assert.ok(['grounded', 'gap', 'standpoint'].includes(m.kind))
          assert.equal(m.chunks.map((c) => c[1]).join(''), m.text)
          for (const s of m.sources) assert.ok(Number.isInteger(s.n) && s.title && (!s.url || s.url.startsWith('https://')))
        }
      }
    }
  }
  const s = opnames.meeting.statements[0]
  assert.deepEqual(s.speaker, VRAGEN.meeting[0].speaker)
  assert.deepEqual(Object.keys(s.reactions.nl), MEETING_PIGEONS)
  assert.equal(s.reactions.nl.signalerend.type, 'signal')
  assert.equal(s.reactions.nl.signalerend.sound, 'etalage/media/geluid/koer/pigeon-field-coo.mp3')
  assert.equal(s.reactions.nl.kennispartner.type, 'speech')
  assert.equal(s.reactions.nl.stakeholder.type, 'silence')
  assert.equal(voices.length, 2)
  assert.deepEqual(sounds, ['etalage/media/geluid/koer/pigeon-field-coo.mp3'])
})

test('de bouw weigert zolang een item geen bruikbare take heeft', () => {
  const takes = fullSet().filter((t) => t.id !== 'meeting-s1-en-stakeholder-20260923T100000Z')
  const { keuze } = autoChoose({ items: {} }, takes, VRAGEN, { bands: BANDS })
  assert.throws(() => compile({ vragen: VRAGEN, keuze, takesById: new Map(takes.map((t) => [t.id, t])), facts: FACTS, bands: BANDS }), /meeting\/s1\/en\/stakeholder: geen take gekozen/)
})

test('de bouw weigert een bron zonder titel en noemt take en bronsleutel', () => {
  const takes = fullSet()
  const keyOnly = { number: 1, type: 'destillaat', text: 'x', ref: { paper: 'galifret1967', passages: [{ text: 'Een passage.' }] } }
  const bad = takes.findIndex((t) => t.id === 'chat-q1-nl-stakeholder-20260923T100000Z')
  takes[bad] = chatTake({ id: takes[bad].id, route: 'stakeholder', exchange: chatExchange({ persona: 'individualist', sources: [keyOnly] }), stored: ['Eerste deel [1]. Tweede deel.'] })
  const { keuze } = autoChoose({ items: {} }, takes, VRAGEN, { bands: BANDS })
  keuze.items['chat/q1/nl/stakeholder'] = { take: takes[bad].id, reason: 'vastgezet voor de test' }
  assert.throws(() => compile({ vragen: VRAGEN, keuze, takesById: new Map(takes.map((t) => [t.id, t])), facts: FACTS, bands: BANDS }), /chat-q1-nl-stakeholder-20260923T100000Z: bron 1 heeft geen titel .*bronsleutel galifret1967/)
})

test('de gekozen takes delen één HTTPS-endpoint; welk, staat in de takes', async () => {
  const { recording } = await import('../lib/build.mjs')
  const take = (endpoint) => ({ recordedAt: '2026-10-01T00:00:00Z', server: { modelPolicy: { model: MODEL, detectionModel: MODEL, endpoint } } })
  assert.throws(() => recording([take('https://a.example.invalid'), take('https://b.example.invalid')], VRAGEN), /endpoints/)
  assert.throws(() => recording([take('synthetic-loopback')], VRAGEN), /endpoints/)
})
