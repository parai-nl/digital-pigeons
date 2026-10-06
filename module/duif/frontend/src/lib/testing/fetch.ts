// scriptedFetch: een fetch-fake op basis van route-matchers. Gedeeld door de
// chat- en vergadertests; geen globals, geen mocks op window.
import type { Fetcher } from '../api'

export type Route = {
  method?: string
  match: string | RegExp
  respond: (req: { url: string; init?: RequestInit; body: unknown }) => Response | Promise<Response>
}

export function jsonResponse(body: unknown, status = 200, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json', ...headers },
  })
}

export function sseResponse(frames: unknown[], status = 200): Response {
  const enc = new TextEncoder()
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const f of frames) controller.enqueue(enc.encode(`data: ${JSON.stringify(f)}\n\n`))
      controller.close()
    },
  })
  return new Response(stream, { status, headers: { 'Content-Type': 'text/event-stream' } })
}

// sseOneChunk levert alle frames in één netwerkchunk. Dat is het geval waarin
// de client alle regels achter elkaar verwerkt zonder dat de weergave ertussen
// aan bod komt: een kort antwoord komt zo binnen.
export function sseOneChunk(frames: unknown[], status = 200): Response {
  const enc = new TextEncoder()
  const body = frames.map((f) => `data: ${JSON.stringify(f)}\n\n`).join('')
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(enc.encode(body))
      controller.close()
    },
  })
  return new Response(stream, { status, headers: { 'Content-Type': 'text/event-stream' } })
}

export interface ScriptedFetch {
  fetch: Fetcher
  calls: { url: string; init?: RequestInit; body: unknown }[]
}

export function scriptedFetch(routes: Route[]): ScriptedFetch {
  const calls: ScriptedFetch['calls'] = []
  const fetch: Fetcher = async (input, init) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    const method = (init?.method ?? 'GET').toUpperCase()
    let body: unknown = undefined
    if (typeof init?.body === 'string') {
      try {
        body = JSON.parse(init.body)
      } catch {
        body = init.body
      }
    } else if (init?.body) {
      body = init.body
    }
    const call = { url, init, body }
    calls.push(call)
    for (const r of routes) {
      if (r.method && r.method.toUpperCase() !== method) continue
      const hit = typeof r.match === 'string' ? url.includes(r.match) : r.match.test(url)
      if (hit) return r.respond(call)
    }
    return new Response('not scripted: ' + method + ' ' + url, { status: 599 })
  }
  return { fetch, calls }
}
