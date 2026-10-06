// Het afspeelscript in een echte Chromium: wat de opname niet kent,
// krijgt geen verzonnen antwoord en gaat ook niet naar het netwerk.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { chromium } from 'playwright'
import { pageRuntime } from '../replay/page-runtime.js'
import { createMatcher } from './match.mjs'

const ORIGIN = 'http://uitlegvideo.test'

async function page(cfg) {
  const browser = await chromium.launch()
  const p = await browser.newPage()
  const reports = []
  const network = []
  await p.exposeFunction('__uvReport', (kind, data) => { reports.push({ kind, ...data }) })
  await p.addInitScript({ content: `(${pageRuntime.toString()})(${JSON.stringify(cfg)}, ${createMatcher.toString()});` })
  await p.route(`${ORIGIN}/**`, (route) => {
    const url = new URL(route.request().url())
    if (url.pathname === '/') return route.fulfill({ status: 200, contentType: 'text/html', body: '<!doctype html><title>t</title>' })
    network.push(url.pathname)
    return route.fulfill({ status: 500, body: 'network' })
  })
  await p.goto(ORIGIN + '/')
  return { browser, p, reports, network }
}

test('afspelen: een onbekend duifkanaal is meteen dicht, meldt zich en maakt geen verbinding', async () => {
  const { browser, p, reports, network } = await page({ mode: 'replay', http: [], channels: [] })
  try {
    const state = await p.evaluate(() => new Promise((resolve) => {
      const es = new EventSource('/api/duif/v1/meeting/sessions/unknown/events')
      es.onerror = () => resolve({ readyState: es.readyState, closed: EventSource.CLOSED })
    }))
    assert.equal(state.readyState, state.closed)
    await p.waitForTimeout(100)
    assert.ok(reports.some((r) => r.kind === 'replay-miss' && r.method === 'EVENTSOURCE'))
    assert.deepEqual(network, [])
  } finally { await browser.close() }
})

test('afspelen: een onbekend verzoek krijgt 503 en een melding, nooit het netwerk; een bekend het opgenomen antwoord', async () => {
  const http = [{ id: 1, method: 'POST', path: '/api/duif/v1/chat/turn', status: 200, headers: [['content-type', 'text/event-stream']], headMs: 0, chunks: [{ ms: 5, text: 'data: {"type":"done"}\n\n' }] }]
  const { browser, p, reports, network } = await page({ mode: 'replay', http, channels: [] })
  try {
    const result = await p.evaluate(async () => {
      const known = await fetch('/api/duif/v1/chat/turn', { method: 'POST', body: '{}' })
      const again = await fetch('/api/duif/v1/chat/turn', { method: 'POST', body: '{}' })
      return { known: await known.text(), again: again.status }
    })
    assert.equal(result.known, 'data: {"type":"done"}\n\n')
    assert.equal(result.again, 503)
    assert.ok(reports.some((r) => r.kind === 'replay-miss' && r.path === '/api/duif/v1/chat/turn'))
    assert.deepEqual(network, [])
  } finally { await browser.close() }
})

test('afspelen: een beurt met een andere vraag wordt gemeld; een andere aanvraag-id niet', async () => {
  const turn = { id: 1, method: 'POST', path: '/api/duif/v1/chat/turn', status: 200, headers: [], headMs: 0, body: 'ok', requestBody: JSON.stringify({ message: 'Pigeons?', requestId: 'a' }) }
  const { browser, p, reports } = await page({ mode: 'replay', http: [turn, { ...turn, id: 2 }], channels: [] })
  try {
    await p.evaluate(async () => {
      await fetch('/api/duif/v1/chat/turn', { method: 'POST', body: JSON.stringify({ requestId: 'b', message: 'Pigeons?' }) })
      await fetch('/api/duif/v1/chat/turn', { method: 'POST', body: JSON.stringify({ message: 'Something else', requestId: 'a' }) })
    })
    const mismatches = reports.filter((r) => r.kind === 'replay-body-mismatch')
    assert.equal(mismatches.length, 1)
    assert.match(mismatches[0].sent, /Something else/)
  } finally { await browser.close() }
})

test('aanvullen: alleen een toegestane GET gaat echt naar de server en wordt meegeschreven', async () => {
  const { browser, p, reports, network } = await page({ mode: 'replay', http: [], channels: [], supplement: '^/api/duif/v1/graph/' })
  try {
    const status = await p.evaluate(async () => {
      const graph = await fetch('/api/duif/v1/graph/overview')
      const turn = await fetch('/api/duif/v1/chat/turn', { method: 'POST', body: '{}' })
      return [graph.status, turn.status]
    })
    assert.deepEqual(status, [500, 503])
    assert.deepEqual(network, ['/api/duif/v1/graph/overview'])
    assert.ok(reports.some((r) => r.kind === 'http-request' && r.live && r.path === '/api/duif/v1/graph/overview'))
    assert.ok(reports.some((r) => r.kind === 'replay-miss' && r.path === '/api/duif/v1/chat/turn'))
  } finally { await browser.close() }
})
