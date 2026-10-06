// Gedrag van het takeformaat en het afspelen: wat in git staat,
// speelt ongewijzigd en in dezelfde volgorde af.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp, readdir, readFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { buildPlan, externalizeAudio, loadTake, messageType, sanitizeTake, saveTake, TAKE_FORMAT, timeWindows } from './take.mjs'
import { buildTimeMap } from './timemap.mjs'

const root = path.resolve(import.meta.dirname, '..')
const config = JSON.parse(await readFile(path.join(root, 'video.config.json'), 'utf8'))
const takeDirs = (await readdir(path.join(root, 'takes'))).map((d) => path.join(root, 'takes', d))

for (const dir of takeDirs) {
  test(`afspelen van ${path.basename(dir)}: zelfde tekst, zelfde volgorde, alleen korter`, async () => {
    const take = await loadTake(dir)
    const meeting = take.scene.startsWith('meeting')
    const rules = meeting ? config.timeMap.meeting : config.timeMap.chat
    const map = buildTimeMap(timeWindows(take, rules))
    const plan = buildPlan(take, map)
    const recorded = take.exchanges.filter((x) => !x.failed && x.status)
    assert.deepEqual(plan.http.map((x) => x.id), recorded.map((x) => x.id), 'volgorde van de verzoeken')
    plan.http.forEach((x, i) => {
      const r = recorded[i]
      assert.equal(x.method + x.path, r.method + r.path)
      if (r.chunks) {
        assert.equal(x.chunks.map((c) => c.text).join(''), r.chunks.map((c) => c.text).join(''), `tekst van ${r.path}`)
        x.chunks.forEach((c, k) => {
          if (k) assert.ok(c.ms >= x.chunks[k - 1].ms, 'brokken blijven op volgorde')
          assert.ok(c.ms <= r.chunks[k].ms + 1, 'nooit later dan in de opname')
        })
      } else {
        assert.equal(x.body, r.body)
      }
    })
    plan.channels.forEach((c, i) => {
      const r = take.channels[i]
      assert.deepEqual(c.messages.map((m) => m.data), r.messages.map((m) => m.data), 'kanaalberichten letterlijk')
      c.messages.forEach((m, k) => { if (k) assert.ok(m.ms >= c.messages[k - 1].ms) })
      // Een vergadering speelt op ware snelheid tot de knop "Let the pigeon
      // respond": geen bericht vroeger of later. Daarna mag alleen het wachten
      // op haar eerste woord korter; nooit later dan opgenomen.
      const click = take.actions.find((a) => a.name === 'respond-click')
      if (meeting) c.messages.forEach((m, k) => {
        const at = r.t + r.messages[k].ms
        if (!click || at <= click.t) assert.equal(m.ms, r.messages[k].ms)
        else assert.ok(m.ms <= r.messages[k].ms)
      })
    })
    if (meeting) {
      const click = take.actions.find((a) => a.name === 'respond-click')
      for (const w of map.windows) assert.ok(click && w.from >= click.t, 'alleen ingekort ná de knop')
    }
  })
}

test('takes in git bevatten geen cookies of autorisatie', async () => {
  for (const dir of takeDirs) {
    const take = JSON.parse(await readFile(path.join(dir, 'take.json'), 'utf8'))
    for (const x of take.exchanges) {
      for (const [name] of x.headers ?? []) assert.ok(!/cookie|authorization/i.test(name), `${path.basename(dir)}: kop ${name}`)
    }
    assert.ok(!JSON.stringify(take).includes('dev@localhost'), `${path.basename(dir)}: ontwikkelaarsaccount`)
  }
})

test('stem: uit de berichten naar wav en terug, byte voor byte', async () => {
  const pcm = Buffer.from(Array.from({ length: 4800 }, (_, i) => (i * 37) % 256))
  const frames = [pcm.subarray(0, 1000), pcm.subarray(1000, 3001), pcm.subarray(3001)]
  const original = frames.map((f, seq) => JSON.stringify({ type: 'tts_audio', turnId: '3', seq, format: 'pcm16', sampleRate: 24000, audio: f.toString('base64') }))
  const take = { format: TAKE_FORMAT, exchanges: [], actions: [], probes: [], channels: [{ channel: 1, path: '/x', t: 0, messages: [{ ms: 1, data: '{"type":"persona","turnId":"3"}' }, ...original.map((data, i) => ({ ms: 10 + i, data }))] }] }
  const audio = externalizeAudio(take, 'test')
  assert.equal(audio.length, 1)
  assert.ok(!JSON.stringify(take).includes(frames[1].toString('base64')), 'geen base64 meer in take.json')
  const dir = await mkdtemp(path.join(tmpdir(), 'uitlegvideo-'))
  await saveTake(dir, take, audio)
  const back = await loadTake(dir)
  const data = back.channels[0].messages.slice(1).map((m) => JSON.parse(m.data))
  data.forEach((ev, i) => assert.deepEqual(ev, JSON.parse(original[i])))
  assert.equal(back.channels[0].messages[0].data, '{"type":"persona","turnId":"3"}')
})

test('sanitizeTake: alleen gesprekken uit de take zelf blijven in de lijst', () => {
  const take = { exchanges: [
    { method: 'GET', path: '/api/duif/v1/chat/sessions', headers: [], body: JSON.stringify({ sessions: [{ id: 'own' }, { id: 'foreign', title: 'proef' }] }) },
    { method: 'POST', path: '/api/duif/v1/chat/turn', headers: [['x-duif-session-id', 'own']], chunks: [] },
  ] }
  assert.equal(sanitizeTake(take), 1)
  assert.deepEqual(JSON.parse(take.exchanges[0].body).sessions.map((s) => s.id), ['own'])
})

test('vergadering: de stille wacht tussen haar tekst en haar stem is hooguit voiceWaitMs; tekst en stem blijven gelijk', async () => {
  const rules = config.timeMap.meeting
  assert.ok(rules.voiceWaitMs > 0)
  const take = await loadTake(path.join(root, 'takes', config.takes['meeting-individualist-respond']))
  const withVoice = buildPlan(take, buildTimeMap(timeWindows(take, rules)))
  const without = buildPlan(take, buildTimeMap(timeWindows(take, { respondWaitMs: rules.respondWaitMs })))
  const typed = (plan) => plan.channels[0].messages.map((m) => ({ ...m, type: messageType(m) }))
  const a = typed(withVoice)
  const b = typed(without)
  const done = a.findIndex((m) => m.type === 'text_done')
  const voice = a.findIndex((m) => m.type === 'tts_audio')
  assert.ok(done >= 0 && voice > done, 'eerst haar tekst, dan haar stem')
  // In de opname zat er ruim vier seconden tussen; nu hooguit voiceWaitMs.
  assert.ok(b[voice].ms - b[done].ms > 4000)
  assert.ok(a[voice].ms - a[done].ms <= rules.voiceWaitMs + 1, `${a[voice].ms - a[done].ms} ms stil`)
  // Alles tot en met haar tekst staat op hetzelfde moment als zonder deze regel.
  for (let k = 0; k <= done; k++) assert.equal(a[k].ms, b[k].ms, `${a[k].type} op hetzelfde moment`)
  // De stem zelf: dezelfde berichten, in hetzelfde tempo, alleen eerder.
  const shift = b[voice].ms - a[voice].ms
  for (let k = voice; k < a.length; k++) {
    assert.equal(a[k].data, b[k].data)
    assert.ok(Math.abs(b[k].ms - a[k].ms - shift) <= 1, `${a[k].type} schuift gelijk op`)
  }
})

test('chat: het shot van de andere duif eindigt vóór de afsluitende facilitatortekst, na haar hele antwoord', async () => {
  // storyboard.json laat het shot "other" eindigen op chunk:text#2: het tweede
  // streambrok met facilitatortekst. In de gekozen chattake is dat de
  // afsluiting van de tweede beurt, en die komt pas na het laatste woord van
  // de Stakeholder.
  const board = JSON.parse(await readFile(path.join(root, 'storyboard.json'), 'utf8'))
  assert.equal(board.shots.find((s) => s.id === 'other').to, 'chunk:text#2-0.1')
  const take = await loadTake(path.join(root, 'takes', config.takes.chat))
  const turns = take.exchanges.filter((x) => x.path.endsWith('/chat/turn') && x.chunks)
  const chunks = turns.flatMap((x) => x.chunks.map((c) => ({ t: x.t + c.ms, turn: turns.indexOf(x), text: c.text })))
  const texts = chunks.filter((c) => c.text.includes('"type":"text"'))
  assert.equal(texts[1]?.turn, 1, 'de tweede facilitatortekst hoort bij de tweede beurt')
  const lastWord = chunks.filter((c) => c.turn === 1 && c.text.includes('"type":"artifact_delta"')).at(-1)
  assert.ok(lastWord.t < texts[1].t, 'haar antwoord is af vóór de afsluiting')
})
