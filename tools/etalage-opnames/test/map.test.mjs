import assert from 'node:assert/strict'
import { test } from 'node:test'
import { answerKind, chatTurn, meetingReaction, mapSource, parseCitation, passageLang, sourceUrl } from '../lib/map.mjs'
import { timedFrames } from '../lib/sse.mjs'
import { execFileSync } from 'node:child_process'
import { REPO_DIR } from '../lib/config.mjs'
import { TOOL_CODE, codePathspecs, moduleInfo } from '../lib/server-info.mjs'
import { SOURCE, chatExchange, chatTake, meetingTake } from './fixtures.mjs'

test('een frame dat over twee brokken binnenkomt krijgt het moment van het brok waarin het af is', () => {
  const frames = timedFrames([{ ms: 1, text: 'data: {"type":"a"}\n\ndata: {"ty' }, { ms: 9, text: 'pe":"b"}\n\n: commentaar\n\ndata: kapot\n\n' }])
  assert.deepEqual(frames, [{ ms: 1, frame: { type: 'a' } }, { ms: 9, frame: { type: 'b' } }])
})

test('Stakeholder: kader, antwoord en bemiddeling in leesvolgorde, tekst ongewijzigd', () => {
  const turn = chatTurn(chatTake())
  assert.deepEqual(turn.phases, ['facilitator', 'pigeon_grounding', 'pigeon', 'mediation'])
  assert.deepEqual(turn.messages.map((m) => m.author), ['facilitator', 'stakeholder', 'facilitator'])
  const answer = turn.messages[1]
  assert.equal(answer.text, 'Eerste deel [1]. Tweede deel.')
  assert.equal(answer.kind, 'grounded')
  // De brokken zijn het tempo van de lijn: samen precies de tekst, vanaf 0.
  assert.equal(answer.chunks.map((c) => c[1]).join(''), answer.text)
  assert.equal(answer.chunks[0][0], 0)
  assert.ok(answer.chunks.every((c, i, all) => i === 0 || c[0] >= all[i - 1][0]))
  assert.equal(answer.sources.length, 1)
  assert.equal(turn.messages[0].text, 'Kaderzin.')
  assert.equal(turn.messages[2].text, 'Bemiddeling.')
})

test('Kennispartner: de facilitator blijft intern zolang zij antwoordt (visibleMessages)', () => {
  const take = chatTake({ id: 'chat-q1-nl-kennispartner-20260923T100000Z', route: 'kennispartner', exchange: chatExchange({ persona: 'biological' }) })
  const turn = chatTurn(take)
  assert.deepEqual(turn.messages.map((m) => m.author), ['kennispartner'])
})

test('een afgebroken antwoord wordt gemeld, niet verborgen', () => {
  const take = chatTake({ exchange: chatExchange({ stopReason: 'truncated' }) })
  assert.deepEqual(chatTurn(take).notes, ['het antwoord eindigde met stopReason truncated'])
})

test('verwante bronnen dragen hun markering mee', () => {
  const take = chatTake({ exchange: chatExchange({ related: true }) })
  assert.equal(chatTurn(take).messages[1].related, true)
})

test('soort antwoord: bronnen, kennisgat, standpunt zonder onderzoek', () => {
  assert.equal(answerKind('sources', 'biological'), 'grounded')
  assert.equal(answerKind('gap', 'biological'), 'gap')
  assert.equal(answerKind('gap', 'individualist'), 'standpoint')
  assert.throws(() => answerKind('unavailable', 'biological'))
})

test('bronvermelding: auteurs, jaar en titel, zonder adres of notitie in de titel', () => {
  assert.deepEqual(parseCitation(SOURCE.ref.citation), { authors: 'Doe, J., & Roe, R.', year: 2019, title: 'Pigeons in towns: A study' })
  assert.equal(parseCitation('Treiber, C. D. (2012). Clusters of cells are macrophages not neurons. https://doi.org/10.1038/nature11046').title, 'Clusters of cells are macrophages not neurons')
  assert.equal(parseCitation('Galifret, Y. (1968). [title not present in file]. Zeitschrift, 1.').title, undefined)
  assert.equal(parseCitation('Can experience reduce collisions? (2017)').title, undefined)
  assert.equal(parseCitation('DeVault, T. L. (2017). Can experience reduce collisions between birds and vehicles? Journal of Zoology, 301(1), 17–22.').title, 'Can experience reduce collisions between birds and vehicles?')
  assert.deepEqual(parseCitation('geen bronvermelding'), {})
})

test('links naar een paper zijn alleen https', () => {
  assert.equal(sourceUrl({ doi: '10.1000/xyz' }), 'https://doi.org/10.1000/xyz')
  assert.equal(sourceUrl({ doi: 'https://doi.org/10.1000/xyz' }), 'https://doi.org/10.1000/xyz')
  assert.equal(sourceUrl({ citation: 'Iemand (2000). Titel. http://example.org/x' }), undefined)
  assert.equal(sourceUrl({ citation: 'Iemand (2000). Titel. https://www.jstor.org/stable/1592062.' }), 'https://www.jstor.org/stable/1592062')
})

test('een bron van de wire wordt een Source van het contract', () => {
  assert.deepEqual(mapSource(SOURCE), {
    n: 1,
    title: 'Pigeons in towns: A study',
    authors: 'Doe, J., & Roe, R.',
    year: 2019,
    url: 'https://doi.org/10.1000/xyz',
    passage: 'Pigeons are found in all towns and on the squares.',
    passageLang: 'en',
  })
  // Zonder titel is er alleen een bronsleutel; die komt nooit op de pagina.
  assert.throws(() => mapSource({ number: 2, ref: { paper: 'lim2023' } }), /bron 2 heeft geen titel .*bronsleutel lim2023/)
  assert.throws(() => mapSource({ number: 2, ref: { paper: 'galifret1967', citation: 'Galifret, Y. (1968). [title not present in file]. Zeitschrift, 1.' } }), /bronsleutel galifret1967/)
  // Een papertitel (met spaties) is wel een titel.
  assert.equal(mapSource({ number: 2, ref: { paper: 'Proximity to anthropogenic food sources' } }).title, 'Proximity to anthropogenic food sources')
  // Twee passages: de eerste in het contract, beide ernaast.
  const two = mapSource({ number: 3, ref: { citation: 'X, Y. (2020). A paper. Journal.', paper: 'x2020', passages: [{ text: 'The first passage of the paper.' }, { text: 'De tweede passage uit het rapport.' }] } })
  assert.equal(two.passage, 'The first passage of the paper.')
  assert.deepEqual(two.passages, [{ text: 'The first passage of the paper.', lang: 'en' }, { text: 'De tweede passage uit het rapport.', lang: 'nl' }])
  // Afgekeurde gronding: geen passage, en er wordt er ook geen verzonnen.
  assert.equal(mapSource({ number: 4, grounding: 'rejected', ref: { citation: 'Y, Z. (2021). Another paper. Journal.', paper: 'y2021' } }).passage, undefined)
})

test('taal van een passage', () => {
  assert.equal(passageLang('De duiven zitten op het plein en worden gevoerd door de mensen.'), 'nl')
  assert.equal(passageLang('The pigeons are on the square and they are fed by people.'), 'en')
})

test('vergadering: niets binnen het venster is stilte', () => {
  assert.deepEqual(meetingReaction(meetingTake()), { take: 'meeting-s1-nl-signalerend-20260923T100000Z', type: 'silence' })
  // Een geluid dat pas na het venster begint telt niet.
  const late = meetingTake({ events: [[34000, { type: 'sound', sound: { id: 'x', url: '/y', band: 'calm' } }]] })
  assert.equal(meetingReaction(late).type, 'silence')
})

test('vergadering: signaal met het afgespeelde veldgeluid en het gebaar', () => {
  const take = meetingTake({
    events: [[11500, { type: 'intervention' }], [11600, { type: 'sound', sound: { id: 'coo', url: '/sounds/coo', band: 'calm' } }]],
    probes: [{ kind: 'gesture', t: 1300, clip: 'Peck' }, { kind: 'media-play', t: 11610, src: 'http://127.0.0.1:8094/modules/duif/media/pigeon-field-coo.mp3' }, { kind: 'gesture', t: 11605, clip: 'WingWave' }],
  })
  assert.deepEqual(meetingReaction(take), { take: take.id, type: 'signal', delayMs: 3600, sound: 'etalage/media/geluid/koer/pigeon-field-coo.mp3', gesture: 'ruffle' })
  // Zonder gemeld bestand: de band via de catalogus van de module.
  const bare = meetingTake({ events: [[11600, { type: 'sound', sound: { band: 'alarm' } }]] })
  assert.equal(meetingReaction(bare, { bands: { alarm: 'pigeon-field-loft-chorus.mp3' } }).sound, 'etalage/media/geluid/koer/pigeon-field-loft-chorus.mp3')
})

test('vergadering: spraak met tekst, bronnen, stem en gebaar', () => {
  const take = meetingTake({
    id: 'meeting-s1-nl-kennispartner-20260923T100000Z',
    pigeon: 'kennispartner',
    events: [[17000, { type: 'persona' }], [17001, { type: 'sources', sources: [SOURCE] }], [17002, { type: 'text', delta: 'Een bijdrage [1].' }], [17400, { type: 'tts_audio', format: 'pcm16' }], [30000, { type: 'response_done', sources: [SOURCE] }]],
    probes: [{ kind: 'gesture', t: 17003, clip: 'Peck' }],
    voice: [{ file: 'stem.mp3', format: 'pcm16', sampleRate: 24000, ms: 12000 }],
  })
  const r = meetingReaction(take)
  assert.equal(r.type, 'speech')
  assert.equal(r.delayMs, 9000)
  assert.equal(r.text, 'Een bijdrage [1].')
  assert.equal(r.sources[0].n, 1)
  assert.equal(r.audio, `etalage/media/geluid/stem/${take.id}.mp3`)
  assert.equal(r.audioMs, 12000)
  assert.equal(r.gesture, 'peck')
})

test('vergadering: een bijdrage die niet afkwam is geen reactie', () => {
  const take = meetingTake({ pigeon: 'stakeholder', events: [[17000, { type: 'persona' }], [17002, { type: 'text', delta: 'Half' }], [18000, { type: 'stopped' }]] })
  assert.throws(() => meetingReaction(take), /niet afgerond/)
})

test('de bouwstand in een take draagt geen lokaal pad', () => {
  const build = { commit: 'a', moduleTree: 'b', dirty: false, paraiCore: { path: '/pad/naar/parai-core', commit: 'c', dirty: false }, tags: ['skipauth'], builtAt: 't', binary: 'module/bin/duif-etalage' }
  assert.deepEqual(moduleInfo(build, 'h'), { commit: 'a', moduleTree: 'b', dirty: false, paraiCore: { commit: 'c', dirty: false }, tags: ['skipauth'], builtAt: 't', binary: 'module/bin/duif-etalage', bundleSha256: 'h' })
})

test('de pathspecs van het gereedschap: een map krijgt alles eronder, een bestand blijft een bestand', () => {
  assert.deepEqual(codePathspecs(['a/*.mjs', 'a/lib/', 'a/query.sql']), [':(glob)a/*.mjs', ':(glob)a/lib/**', ':(glob)a/query.sql'])
  // Tegen de echte repository: de pathspecs matchen de scripts, lib/ en elke
  // getrackte .sql van het gereedschap, en git klaagt niet (een /** achter
  // een bestand matcht nooit).
  const git = (...args) => execFileSync('git', ['-C', REPO_DIR, ...args], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).split('\n')
  const out = git('ls-files', '--', ...TOOL_CODE)
  const queries = git('ls-files', '--', 'tools/etalage-opnames/').filter((f) => /^tools\/etalage-opnames\/[^/]+\.sql$/.test(f))
  for (const file of ['tools/etalage-opnames/build.mjs', 'tools/etalage-opnames/lib/map.mjs', ...queries]) assert.ok(out.includes(file), file)
  assert.ok(!out.some((f) => f.includes('/takes/')), 'takes tellen niet als code')
})
