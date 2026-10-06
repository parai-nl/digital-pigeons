import { test } from 'node:test'
import assert from 'node:assert/strict'
import { buildTimeMap, capGaps, mergeWindows } from './timemap.mjs'
import { createMatcher, requestPath } from './match.mjs'

test('tijdkaart: buiten vensters één op één, binnen gelijkmatig ingekort', () => {
  const map = buildTimeMap([{ from: 1000, to: 11000, duration: 2000 }])
  assert.equal(map(0), 0)
  assert.equal(map(1000), 1000)
  assert.equal(map(6000), 2000)
  assert.equal(map(11000), 3000)
  assert.equal(map(12500), 4500)
  assert.equal(map.saved, 8000)
})

test('tijdkaart: meerdere vensters tellen op en de volgorde blijft', () => {
  const map = buildTimeMap([{ from: 5000, to: 9000, duration: 1000 }, { from: 1000, to: 3000, duration: 500 }])
  assert.equal(map(3000), 1500)
  assert.equal(map(5000), 3500)
  assert.equal(map(9000), 4500)
  const times = [0, 900, 1200, 2999, 3000, 4000, 5000, 7000, 9001, 20000]
  const mapped = times.map(map)
  for (let i = 1; i < mapped.length; i++) assert.ok(mapped[i] >= mapped[i - 1], `volgorde bij ${times[i]}`)
})

test('tijdkaart: oprekken en overlappen zijn fouten', () => {
  assert.throws(() => buildTimeMap([{ from: 0, to: 1000, duration: 2000 }]))
  assert.throws(() => buildTimeMap([{ from: 0, to: 1000, duration: 500 }, { from: 900, to: 2000, duration: 500 }]))
})

test('capGaps: alleen lange pauzes binnen het deel worden ingekort', () => {
  const windows = capGaps([0, 300, 5300, 5400, 9000], 1000, { from: 0, to: 6000 })
  assert.deepEqual(windows, [{ from: 300, to: 5300, duration: 1000 }])
  const map = buildTimeMap(windows)
  assert.equal(map(5400), 1400)
})

test('mergeWindows: een later overlappend venster valt weg', () => {
  assert.deepEqual(mergeWindows([{ from: 0, to: 10, duration: 1 }], [{ from: 5, to: 20, duration: 1 }, { from: 30, to: 40, duration: 2 }]),
    [{ from: 0, to: 10, duration: 1 }, { from: 30, to: 40, duration: 2 }])
})

test('matcher: volgorde per methode en pad', () => {
  const next = createMatcher([
    { method: 'POST', path: '/v1/chat/turn', id: 1 },
    { method: 'GET', path: '/v1/duiven', id: 2 },
    { method: 'POST', path: '/v1/chat/turn', id: 3 },
  ])
  assert.equal(next('get', '/v1/duiven').id, 2)
  assert.equal(next('POST', '/v1/chat/turn').id, 1)
  assert.equal(next('POST', '/v1/chat/turn').id, 3)
})

test('matcher: een beurt wordt nooit verzonnen, een microfoonblok of GET mag herhalen', () => {
  const next = createMatcher([
    { method: 'POST', path: '/v1/chat/turn', id: 1 },
    { method: 'POST', path: '/s/1/audio', id: 2 },
    { method: 'GET', path: '/v1/duiven', id: 3 },
  ])
  next('POST', '/v1/chat/turn')
  assert.equal(next('POST', '/v1/chat/turn'), null)
  next('POST', '/s/1/audio')
  assert.equal(next('POST', '/s/1/audio').id, 2)
  next('GET', '/v1/duiven')
  assert.equal(next('GET', '/v1/duiven').id, 3)
  assert.equal(next('DELETE', '/s/1'), null)
})

test('matcher: werkt ook als losse functie in de pagina', () => {
  const inPage = new Function(`return (${createMatcher.toString()})`)()
  assert.equal(inPage([{ method: 'GET', path: '/x', id: 7 }])('GET', '/x').id, 7)
})

test('requestPath: pad en zoekdeel zonder oorsprong', () => {
  assert.equal(requestPath('http://127.0.0.1:8092/api/duif/v1/x?y=1'), '/api/duif/v1/x?y=1')
  assert.equal(requestPath('/api/duif/v1/x'), '/api/duif/v1/x')
})

test('naamsvermelding: Engels uit de gestructureerde velden, gegroepeerd, met terugval', async () => {
  const { creditLines, englishCreator } = await import('./credits.mjs')
  assert.deepEqual(englishCreator('Digitale Duiven; fototextuur: Luc Viatour / Lucnix.be'), { text: 'Digitale Duiven, photo texture by Luc Viatour / Lucnix.be', translated: true })
  assert.equal(englishCreator('Iemand; bewerking: X').translated, false)
  const a = { creator: 'Digitale Duiven; fototextuur: Luc Viatour / Lucnix.be', license: 'CC BY-SA 3.0', source: 'https://commons.wikimedia.org/wiki/File:Columba_livia_Luc_Viatour.jpg' }
  const manifest = {
    native: { model: { attribution: a }, poster: { attribution: a } },
    visual: [{ kind: 'portrait', attribution: a }, { kind: 'portrait', attribution: a }, { kind: 'portrait', attribution: { creator: 'X', license: 'CC0 1.0' } }],
  }
  const warnings = []
  const lines = creditLines(manifest, (m) => warnings.push(m))
  assert.deepEqual(lines.map((l) => l.text), ['3D pigeon and portraits: Digitale Duiven, photo texture by Luc Viatour / Lucnix.be · CC BY-SA 3.0'])
  assert.equal(lines[0].source, a.source)
  assert.deepEqual(warnings, [])
  const raw = creditLines({ native: { model: { attribution: { creator: 'Onbekend; bewerking: Y', license: 'CC BY 4.0' } } } }, (m) => warnings.push(m))
  assert.equal(raw[0].text, '3D pigeon: Onbekend; bewerking: Y · CC BY 4.0')
  assert.equal(warnings.length, 1)
})

test('stem: één ondertitel per zin, grenzen op de langste pauzes', async () => {
  const { spokenCues } = await import('./edl.mjs')
  const text = 'You complain about droppings [16]. Why must we be cleared? Killing us is cruel.'
  const cues = spokenCues(text, 1000, 12000, [[4280, 5020], [6000, 6100], [8270, 8870]])
  assert.deepEqual(cues.map((c) => c.text), ['You complain about droppings [16].', 'Why must we be cleared?', 'Killing us is cruel.'])
  assert.deepEqual(cues.map((c) => [c.from, c.to]), [[1000, 5280], [6020, 9270], [9870, 13000]])
  const fallback = spokenCues('One. Two two.', 0, 900, [])
  assert.deepEqual(fallback.map((c) => [c.from, c.to]), [[0, 300], [300, 900]])
})

test('stem: aansluitend ingeplande stukjes zijn één uiting', async () => {
  const { pcmRuns } = await import('./edl.mjs')
  const capture = { audio: [
    { kind: 'pcm-start', at: 1000, duration: 0.5 }, { kind: 'pcm-start', at: 1500, duration: 0.5 },
    { kind: 'pcm-start', at: 5000, duration: 1 }, { kind: 'mic-open', t: 0 },
  ] }
  assert.deepEqual(pcmRuns(capture), [{ start: 1000, end: 2000 }, { start: 5000, end: 6000 }])
})

test('formaat: venster maal schaal is de videomaat; alleen het standaardformaat heeft geen achtervoegsel', async () => {
  const { resolveFormat } = await import('./format.mjs')
  const config = { defaultFormat: '16x9', formats: {
    '16x9': { viewport: { width: 1280, height: 720 }, scale: 1.5, out: { width: 1920, height: 1080 } },
    '4x3': { viewport: { width: 1152, height: 864 }, scale: 1.25, out: { width: 1440, height: 1080 } },
    fout: { viewport: { width: 1000, height: 800 }, scale: 1.5, out: { width: 1440, height: 1080 } },
  } }
  assert.equal(resolveFormat(config).suffix, '')
  assert.equal(resolveFormat(config, '4x3').suffix, '-4x3')
  assert.throws(() => resolveFormat(config, 'fout'))
  assert.throws(() => resolveFormat(config, 'onbekend'))
})

test('storyboard: elk anker is leesbaar en verwijst naar een moment dat een scène markeert', async () => {
  const { readFile } = await import('node:fs/promises')
  const path = (await import('node:path')).default
  const root = path.resolve(import.meta.dirname, '..')
  const board = JSON.parse(await readFile(path.join(root, 'storyboard.json'), 'utf8'))
  const scenes = await readFile(path.join(root, 'lib/scenes.mjs'), 'utf8')
  const marks = new Set([...scenes.matchAll(/ctl\.mark\('([a-z0-9-]+)'/g)].map((m) => m[1]))
  const re = /^([a-zA-Z]+)(?::([a-zA-Z0-9_-]+))?(?:#(\d+))?([+-]\d+(?:\.\d+)?)?$/
  const anchors = [...board.shots.flatMap((s) => [s.from, s.to].filter(Boolean)), board.poster.at]
  for (const a of anchors) {
    const m = re.exec(a)
    assert.ok(m, `anker ${a}`)
    assert.ok(['mark', 'mic', 'audio', 'pcmEnd', 'pcmStart', 'event', 'chunk', 'start'].includes(m[1]), `soort ${a}`)
    if (m[1] === 'mark') assert.ok(marks.has(m[2]), `moment ${m[2]} komt niet voor in lib/scenes.mjs`)
  }
})

test('model: opnemen alleen met de huidige alias; een take houdt zijn eigen herkomst', async () => {
  const { RECORD_MODEL, recordPolicyError, takeModelAllowed } = await import('./model.mjs')
  const configured = 'https://llm.example.invalid/v1'
  const now = { model: RECORD_MODEL, detectionModel: RECORD_MODEL, endpoint: 'https://llm.example.invalid' }
  assert.equal(recordPolicyError(now, configured), null)
  assert.ok(recordPolicyError(now, undefined))
  assert.ok(recordPolicyError(now, 'http://llm.example.invalid'))
  // Een eerdere alias is voor een nieuwe opname een ander model, ook als alleen
  // de detectie er nog op staat.
  assert.ok(recordPolicyError({ ...now, model: 'vertex_ai/gemini-3.5-flash', detectionModel: 'vertex_ai/gemini-3.5-flash' }, configured))
  assert.ok(recordPolicyError({ ...now, detectionModel: 'vertex_ai/gemini-3.5-flash' }, configured))
  assert.ok(recordPolicyError({ ...now, endpoint: 'https://example.org' }, configured))
  assert.ok(recordPolicyError(undefined, configured))
  // De herkomst staat in de take zelf; afspelen vraagt geen bepaald model.
  // Elke take in git heeft een geldige herkomst (takeModelAllowed), en de takes
  // van 23 september zijn opgenomen met vertex_ai/gemini-3.5-flash, de alias
  // die toen gold. Een nieuwe take met het huidige model is altijd geldig.
  const { readdir, readFile } = await import('node:fs/promises')
  const path = (await import('node:path')).default
  const takes = path.resolve(import.meta.dirname, '..', 'takes')
  for (const dir of await readdir(takes)) {
    const take = JSON.parse(await readFile(path.join(takes, dir, 'take.json'), 'utf8'))
    if (take.supplementFor) continue
    const models = [take.app?.modelPolicy?.model, take.app?.modelPolicy?.detectionModel, ...(take.provenance ?? []).map((p) => p.model)]
    for (const model of models) assert.ok(takeModelAllowed(model, take.recordedAt), `${dir}: ${model} is geen geldige herkomst voor ${take.recordedAt}`)
    if (dir.includes('-20260923T')) for (const model of models) assert.equal(model, 'vertex_ai/gemini-3.5-flash', `${dir}: herkomst van de takes van 23 september`)
  }
})

test('herkomst: een take met het huidige model is altijd geldig, 3.5 alleen tot de wissel', async () => {
  const { RECORD_MODEL, takeModelAllowed } = await import('./model.mjs')
  // Een nieuwe opname van vandaag, met het verplichte model.
  assert.equal(takeModelAllowed(RECORD_MODEL, '2026-09-24T18:00:00.000Z'), true)
  assert.equal(takeModelAllowed(RECORD_MODEL, '2027-01-01T00:00:00.000Z'), true)
  assert.equal(takeModelAllowed('vertex_ai/gemini-3.5-flash', '2026-09-23T11:53:43.000Z'), true)
  assert.equal(takeModelAllowed('vertex_ai/gemini-3.5-flash', '2026-09-24T23:59:59.000Z'), true)
  assert.equal(takeModelAllowed('vertex_ai/gemini-3.5-flash', '2026-09-25T00:00:00.000Z'), false)
  assert.equal(takeModelAllowed('vertex_ai/gemini-3.5-flash', 'onbekend'), false)
  assert.equal(takeModelAllowed('other/unknown-model', '2026-09-23T11:53:43.000Z'), false)
  assert.equal(takeModelAllowed(undefined, '2026-09-24T18:00:00.000Z'), false)
})

test('ondertitels: alleen de duif, nooit de naamsvermelding', async () => {
  const { captionsVtt } = await import('./captions.mjs')
  const out = captionsVtt([
    { speaker: 'Stakeholder', text: 'Second.', start: 70.1, end: 72.6 },
    { speaker: 'Signaling Pigeon', text: '[pigeon sound]', start: 47.54, end: 52.276 },
  ])
  assert.equal(out, 'WEBVTT\n\n1\n00:00:47.540 --> 00:00:52.276\n<v Signaling Pigeon>[pigeon sound]\n\n2\n00:01:10.100 --> 00:01:12.600\n<v Stakeholder>Second.\n')
  assert.throws(() => captionsVtt([{ text: '3D pigeon: … CC BY-SA 3.0', start: 80, end: 81 }]), /zonder spreker/)
})

test('naamsvermelding: v4 met een model per duif, en de catalogus in de repo', async () => {
  const { creditLines, modelAttribution } = await import('./credits.mjs')
  const a = { creator: 'Digitale Duiven; fototextuur: Luc Viatour / Lucnix.be', license: 'CC BY-SA 3.0', source: 'https://commons.wikimedia.org/wiki/File:Columba_livia_Luc_Viatour.jpg', licenseUrl: 'https://creativecommons.org/licenses/by-sa/3.0/' }
  const own = { ...a, source: 'https://example.org/stakeholder' }
  const manifest = {
    native: { pigeons: { biological: { model: { attribution: a }, poster: { attribution: a } }, individualist: { model: { attribution: own }, poster: { attribution: own } } } },
    visual: [{ kind: 'portrait', attribution: a }],
  }
  // De vergelijking met de app gaat per duif: de Stakeholder toont haar eigen model.
  assert.equal(modelAttribution(manifest, 'individualist'), own)
  assert.equal(modelAttribution(manifest, 'biological'), a)
  assert.equal(modelAttribution(manifest, 'nonverbal'), null)
  assert.deepEqual(creditLines(manifest, () => {}).map((l) => [l.text, l.source]), [
    ['3D pigeon and portraits: Digitale Duiven, photo texture by Luc Viatour / Lucnix.be · CC BY-SA 3.0', a.source],
    ['3D pigeon: Digitale Duiven, photo texture by Luc Viatour / Lucnix.be · CC BY-SA 3.0', own.source],
  ])
  // De catalogus die video.config.json noemt levert voor elke duif een model
  // met licentie, en samen één regel voor de video.
  const { readFile } = await import('node:fs/promises')
  const path = (await import('node:path')).default
  const root = path.resolve(import.meta.dirname, '..')
  const config = JSON.parse(await readFile(path.join(root, 'video.config.json'), 'utf8'))
  const real = JSON.parse(await readFile(path.resolve(root, '..', '..', config.credits.manifest), 'utf8'))
  for (const pigeon of ['biological', 'individualist', 'nonverbal']) assert.ok(modelAttribution(real, pigeon)?.licenseUrl, pigeon)
  assert.equal(creditLines(real, () => {}).length, 1)
})

test('ondertitels in git (help en etalage): geen naamsvermelding, alleen de duif', async () => {
  // De ondertitels die in git staan (help en etalage) dragen geen vermelding
  // of adres: die ligt in beeld al ingebrand en staat in de voet van de site.
  const { existsSync, readFileSync } = await import('node:fs')
  const path = (await import('node:path')).default
  const repo = path.resolve(import.meta.dirname, '../../..')
  for (const rel of ['module/duif/frontend/public/media/uitlegvideo-en.vtt', 'site/etalage/media/video/uitleg.en.vtt']) {
    const file = path.join(repo, rel)
    if (!existsSync(file)) continue
    const body = readFileSync(file, 'utf8')
    assert.doesNotMatch(body, /creativecommons|https?:\/\/|CC BY/, rel)
    assert.match(body, /<v (Stakeholder|Signaling Pigeon)>/, rel)
  }
})
