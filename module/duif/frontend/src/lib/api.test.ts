import { describe, expect, it } from 'vitest'
import { readFailure, readSSE, shareInFlight, type SSEFrame } from './api'

function stream(chunks: string[]): ReadableStream<Uint8Array> {
  const enc = new TextEncoder()
  return new ReadableStream({
    start(controller) {
      for (const c of chunks) controller.enqueue(enc.encode(c))
      controller.close()
    },
  })
}

async function collect(chunks: string[], signal?: AbortSignal): Promise<SSEFrame[]> {
  const frames: SSEFrame[] = []
  await readSSE(stream(chunks), (f) => frames.push(f), signal)
  return frames
}

describe('readSSE', () => {
  it('parses LF-separated frames, also when split across chunks', async () => {
    const frames = await collect(['data: {"type":"text","delta":"He"}\n\ndata: {"ty', 'pe":"done"}\n\n'])
    expect(frames).toEqual([{ type: 'text', delta: 'He' }, { type: 'done' }])
  })

  it('accepts CRLF and bare CR line endings', async () => {
    const frames = await collect(['data: {"a":1}\r\n\r\n', 'data: {"b":2}\r\r'])
    expect(frames).toEqual([{ a: 1 }, { b: 2 }])
  })

  it('handles a CRLF split between chunks as one line ending', async () => {
    const frames = await collect(['data: {"a":1}\r', '\n\r\n'])
    expect(frames).toEqual([{ a: 1 }])
  })

  it('ignores comment lines and unknown fields, joins multiple data lines', async () => {
    const frames = await collect([': heartbeat\nevent: x\ndata: {"a":\ndata: 1}\n\n'])
    expect(frames).toEqual([{ a: 1 }])
  })

  it('skips a broken frame without breaking the stream', async () => {
    const frames = await collect(['data: {not json\n\ndata: {"ok":true}\n\n'])
    expect(frames).toEqual([{ ok: true }])
  })

  it('flushes a final frame without trailing blank line', async () => {
    const frames = await collect(['data: {"tail":true}'])
    expect(frames).toEqual([{ tail: true }])
  })

  it('does not dispatch a frame without data', async () => {
    const frames = await collect(['\n\n: only comment\n\n'])
    expect(frames).toEqual([])
  })

  it('treats an abort as a quiet end', async () => {
    const ac = new AbortController()
    const rs = new ReadableStream<Uint8Array>({
      pull() {
        return new Promise(() => {}) // hangt tot abort
      },
    })
    const p = readSSE(rs, () => {}, ac.signal)
    ac.abort()
    await expect(p).resolves.toBeUndefined()
  })

  it('handles a null body', async () => {
    await expect(readSSE(null, () => {})).resolves.toBeUndefined()
  })
})

describe('readFailure', () => {
  it('extracts reason and bilingual message from a JSON body', async () => {
    const res = new Response(JSON.stringify({ error: 'llm_budget_exceeded', message: { en: 'E', nl: 'N' } }), {
      status: 402,
    })
    expect(await readFailure(res)).toEqual({ status: 402, reason: 'llm_budget_exceeded', message: { en: 'E', nl: 'N' } })
  })

  it('never surfaces a non-JSON body', async () => {
    const res = new Response('<html>502 Bad Gateway</html>', { status: 502 })
    expect(await readFailure(res)).toEqual({ status: 502 })
  })
})

describe('shareInFlight', () => {
  it('geeft gelijktijdige lezers van dezelfde URL één aanvraag en ieder een eigen body', async () => {
    let calls = 0
    let release!: () => void
    const gate = new Promise<void>((resolve) => { release = resolve })
    const shared = shareInFlight(async () => {
      calls += 1
      await gate
      return new Response(JSON.stringify({ duiven: [] }), { headers: { 'Content-Type': 'application/json' } })
    })
    const both = Promise.all([shared('/api/duif/v1/duiven'), shared('/api/duif/v1/duiven')])
    release()
    const [first, second] = await both
    expect(calls).toBe(1)
    await expect(first.json()).resolves.toEqual({ duiven: [] })
    await expect(second.json()).resolves.toEqual({ duiven: [] })
  })

  it('deelt niets na afloop, en laat een andere URL, een POST of een body met rust', async () => {
    const seen: string[] = []
    const shared = shareInFlight(async (input, init) => {
      seen.push(((init?.method ?? 'GET') + ' ' + String(input)))
      return new Response('{}', { headers: { 'Content-Type': 'application/json' } })
    })
    await shared('/a')
    await shared('/a')
    await Promise.all([shared('/b'), shared('/a', { method: 'POST', body: '{}' })])
    expect(seen).toEqual(['GET /a', 'GET /a', 'GET /b', 'POST /a'])
  })

  it('deelt ook de afwijzing, zodat een meelifter geen tweede poging veroorzaakt', async () => {
    let calls = 0
    const shared = shareInFlight(async () => { calls += 1; throw new Error('offline') })
    const results = await Promise.allSettled([shared('/a'), shared('/a')])
    expect(calls).toBe(1)
    expect(results.map((r) => r.status)).toEqual(['rejected', 'rejected'])
  })
})
