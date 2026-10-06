// De module-API aanspreken zoals de frontend dat doet, en elk verzoek met zijn
// antwoord vastleggen. Gestreamde antwoorden (SSE) worden per brok bewaard
// met het moment ten opzichte van het verzoek. Alleen content-type en de
// x-duif-*-koppen gaan de take in; cookies en autorisatie nooit.
//
// Idee en vorm van de uitlegvideo-opname (tools/uitlegvideo/lib/take.mjs), hier
// zonder browser.

export const API = '/api/duif/v1'
const keepHeader = (name) => name === 'content-type' || name.startsWith('x-duif-')

// createLimiter houdt de dure verzoeken onder de grens van de server (twaalf
// per minuut per account, access_policy.go) met een eigen, lagere grens.
export function createLimiter(perMinute = 10) {
  const recent = []
  return {
    async slot() {
      for (;;) {
        const now = Date.now()
        while (recent.length && now - recent[0] >= 60000) recent.shift()
        if (recent.length < perMinute) { recent.push(now); return }
        await sleep(60000 - (now - recent[0]) + 50)
      }
    },
  }
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

// createRecorder maakt de HTTP-kant van één take. `t0` is het begin van de
// take; alle tijden in de take zijn milliseconden daarvanaf.
export function createRecorder(base, { limiter, t0 = Date.now(), log = () => {} } = {}) {
  const exchanges = []
  let seq = 0

  async function send(method, path, body, { costly = false, stream = false, timeoutMs = 240000 } = {}) {
    for (let attempt = 1; ; attempt++) {
      if (costly) await limiter?.slot()
      const x = { id: ++seq, method, path, requestBody: body === undefined ? null : body, t: Date.now() - t0 }
      exchanges.push(x)
      const init = { method, signal: AbortSignal.timeout(timeoutMs), headers: {} }
      if (body !== undefined) {
        init.headers['Content-Type'] = 'application/json'
        init.body = JSON.stringify(body)
      }
      const res = await fetch(base + API + path, init)
      x.headMs = Date.now() - t0 - x.t
      x.status = res.status
      x.headers = [...res.headers].filter(([k]) => keepHeader(k))
      const sse = (res.headers.get('content-type') ?? '').includes('text/event-stream')
      if (sse && stream) {
        x.chunks = []
        const decoder = new TextDecoder()
        for await (const part of res.body) {
          const text = decoder.decode(part, { stream: true })
          if (text) x.chunks.push({ ms: Date.now() - t0 - x.t, text })
        }
        const tail = decoder.decode()
        if (tail) x.chunks.push({ ms: Date.now() - t0 - x.t, text: tail })
      } else {
        x.body = await res.text()
      }
      x.endMs = Date.now() - t0 - x.t
      // Een 429 van de toegangsgrens gebeurt vóór elke modelaanroep: wachten
      // en opnieuw is dan veilig. Beide pogingen blijven in de take staan.
      if (res.status === 429 && attempt < 4) {
        const wait = Number(res.headers.get('retry-after') || 60) * 1000
        log(`  429 op ${method} ${path}; ${Math.round(wait / 1000)} s wachten`)
        x.retried = true
        await sleep(wait)
        continue
      }
      return x
    }
  }

  return {
    exchanges,
    json: async (method, path, body, options) => {
      const x = await send(method, path, body, options)
      let parsed = null
      try { parsed = x.body ? JSON.parse(x.body) : null } catch { /* geen JSON */ }
      return { x, status: x.status, body: parsed }
    },
    stream: (method, path, body, options) => send(method, path, body, { ...options, stream: true }),
  }
}
