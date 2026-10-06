// Init-script voor de uitlegvideo. Draait in de echte frontend,
// vóór de app laadt, in twee standen:
//
//  - record: de app praat met de echte server; dit script tapt elk antwoord
//    van /api/duif/v1 af (ook gestreamde SSE, per brok met zijn moment) en elk
//    bericht op het vergaderkanaal, en geeft dat door aan de opnamedriver.
//  - replay: de app praat met niemand. fetch en EventSource krijgen de
//    opgenomen antwoorden, in de opgenomen volgorde en op de tijden die de
//    driver al door de tijdkaart heeft gehaald. De tekst gaat er ongewijzigd
//    doorheen; alleen het moment verschuift.
//
// In beide standen meldt het wanneer er werkelijk geluid begint (TTS-stroom,
// het veldgeluid van de signalerende duif) en wanneer de microfoon open gaat:
// daar bouwt de geluidsspoor-stap op.
//
// Deze functie gaat als tekst de pagina in (Function.prototype.toString) en
// mag dus niets van buiten gebruiken behalve `createMatcher`, dat de driver
// ervoor zet.
export function pageRuntime(cfg, createMatcher) {
  const report = (kind, data = {}) => {
    try { void window.__uvReport(kind, { t: Date.now(), ...data }) } catch { /* driver weg */ }
  }
  const API = '/api/duif/v1/'
  const nativeFetch = window.fetch.bind(window)
  const NativeEventSource = window.EventSource
  const typesOf = (text) => [...String(text).matchAll(/"type":"([a-z_]+)"/g)].map((m) => m[1])
  const pathOf = (url) => { const u = new URL(url, location.href); return u.pathname + u.search }
  const urlOf = (input) => typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
  const methodOf = (input, init) => String(init?.method || (input instanceof Request ? input.method : 'GET')).toUpperCase()
  const NULL_BODY = new Set([101, 204, 205, 304])
  // Alleen wat het protocol draagt; cookies en autorisatie komen nooit in een take.
  const keepHeader = (name) => name === 'content-type' || name.startsWith('x-duif-')

  // --- microfoon en geluid: in beide standen gemeten --------------------------
  const devices = navigator.mediaDevices
  if (devices?.getUserMedia) {
    const gum = devices.getUserMedia.bind(devices)
    devices.getUserMedia = async (...args) => {
      const stream = await gum(...args)
      report('mic-open', { audio: stream.getAudioTracks().length })
      return stream
    }
  }
  const nativeStart = AudioBufferSourceNode.prototype.start
  AudioBufferSourceNode.prototype.start = function (when = 0, ...rest) {
    const ctx = this.context
    const lead = Math.max(0, when - ctx.currentTime)
    report('pcm-start', { at: Date.now() + lead * 1000, duration: this.buffer?.duration ?? 0, frames: this.buffer?.length ?? 0, sampleRate: this.buffer?.sampleRate ?? 0 })
    return nativeStart.call(this, when, ...rest)
  }
  const nativePlay = HTMLMediaElement.prototype.play
  // Bij afspelen staat een veldgeluid direct klaar; bij de opname moest het
  // eerst laden. cfg.mediaDelays houdt die laadtijd aan, per geluid in volgorde.
  const delays = [...(cfg.mode === 'replay' ? cfg.mediaDelays ?? [] : [])]
  HTMLMediaElement.prototype.play = function (...args) {
    const el = this
    const src = el.currentSrc || el.src
    report('media-play', { src })
    el.addEventListener('playing', () => report('media-playing', { src }), { once: true })
    el.addEventListener('ended', () => report('media-ended', { src }), { once: true })
    const wait = delays.shift() ?? 0
    if (wait > 0) return new Promise((resolve, reject) => setTimeout(() => nativePlay.apply(el, args).then(resolve, reject), wait))
    return nativePlay.apply(el, args)
  }

  // teeFetch haalt een antwoord echt op en meldt het brok voor brok aan de
  // driver: in de opname voor alles, bij afspelen alleen voor de
  // aanvullende GET's die de take nog niet heeft (cfg.supplement).
  let seq = cfg.seqStart ?? 0
  const teeFetch = async (input, init, method, path) => {
    const id = ++seq
    const body = init?.body
    const requestBody = body == null ? null : typeof body === 'string' ? body : { binary: body.byteLength ?? body.size ?? null }
    report('http-request', { id, method, path, requestBody, live: true })
    let res
    try {
      res = await nativeFetch(input, init)
    } catch (err) {
      report('http-failed', { id, error: String(err?.name || err) })
      throw err
    }
    const headers = [...res.headers].filter(([k]) => keepHeader(k))
    report('http-head', { id, status: res.status, statusText: res.statusText, headers })
    if (!res.body) { report('http-end', { id }); return res }
    const [mine, theirs] = res.body.tee()
    void (async () => {
      const reader = theirs.getReader()
      const decoder = new TextDecoder()
      try {
        for (;;) {
          const { done, value } = await reader.read()
          if (done) break
          const text = decoder.decode(value, { stream: true })
          if (text) report('http-chunk', { id, text, types: typesOf(text) })
        }
        const tail = decoder.decode()
        if (tail) report('http-chunk', { id, text: tail, types: typesOf(tail) })
        report('http-end', { id, path })
      } catch (err) {
        report('http-end', { id, path, aborted: String(err?.name || err) })
      }
    })()
    return new Response(NULL_BODY.has(res.status) ? null : mine, { status: res.status, statusText: res.statusText, headers: res.headers })
  }

  if (cfg.mode === 'record') {
    window.fetch = async (input, init) => {
      const path = pathOf(urlOf(input))
      if (!path.startsWith(API)) return nativeFetch(input, init)
      return teeFetch(input, init, methodOf(input, init), path)
    }
    let channelSeq = 0
    window.EventSource = class extends NativeEventSource {
      constructor(url, ...args) {
        super(url, ...args)
        const path = pathOf(url)
        if (!path.startsWith(API)) return
        const channel = ++channelSeq
        report('es-construct', { channel, path })
        this.addEventListener('open', () => report('es-open', { channel }))
        this.addEventListener('error', () => report('es-error', { channel, readyState: this.readyState }))
        this.addEventListener('message', (ev) => report('es-message', { channel, data: ev.data }))
        const close = this.close.bind(this)
        this.close = () => { report('es-close', { channel }); close() }
      }
    }
    return
  }

  // --- replay ------------------------------------------------------------------
  const next = createMatcher(cfg.http)
  const sleep = (ms, signal) => new Promise((resolve, reject) => {
    if (signal?.aborted) return reject(new DOMException('Aborted', 'AbortError'))
    const id = setTimeout(resolve, Math.max(0, ms))
    signal?.addEventListener('abort', () => { clearTimeout(id); reject(new DOMException('Aborted', 'AbortError')) }, { once: true })
  })
  const encoder = new TextEncoder()
  window.fetch = async (input, init) => {
    const url = urlOf(input)
    const path = pathOf(url)
    if (!path.startsWith(API)) return nativeFetch(input, init)
    const method = methodOf(input, init)
    const exchange = next(method, path)
    const start = Date.now()
    const signal = init?.signal ?? (input instanceof Request ? input.signal : undefined)
    if (!exchange && method === 'GET' && cfg.supplement && new RegExp(cfg.supplement).test(path)) {
      // Aanvullen: een leesverzoek dat de take nog niet heeft (bronnen,
      // kennisgraaf) gaat echt naar de server en wordt meegeschreven. Alleen
      // GET, alleen wat de driver uitdrukkelijk toestaat; nooit een beurt.
      return teeFetch(input, init, method, path)
    }
    if (!exchange) {
      // Een vraag die de opname niet kent krijgt nooit een verzonnen antwoord
      // en gaat ook niet naar de server: de driver breekt de opname af.
      report('replay-miss', { method, path })
      return new Response(JSON.stringify({ error: 'replay_miss' }), { status: 503, headers: { 'Content-Type': 'application/json' } })
    }
    report('http-request', { id: exchange.id, method, path })
    // Wat de app vraagt moet zijn wat er toen gevraagd werd: dezelfde vraag,
    // dezelfde duif. Een willekeurige aanvraag-id telt niet mee.
    if (exchange.requestBody != null && typeof exchange.requestBody === 'string') {
      const sent = typeof init?.body === 'string' ? init.body : null
      const norm = (text) => { try { const o = JSON.parse(text); if (o && typeof o === 'object') delete o.requestId; return JSON.stringify(o, Object.keys(o ?? {}).sort()) } catch { return text } }
      if (sent === null || norm(sent) !== norm(exchange.requestBody)) report('replay-body-mismatch', { method, path, sent, recorded: exchange.requestBody })
    }
    await sleep(exchange.headMs, signal)
    const headers = new Headers(exchange.headers)
    let body = null
    if (!NULL_BODY.has(exchange.status)) {
      if (exchange.chunks) {
        body = new ReadableStream({
          start(controller) {
            const timers = []
            const finish = () => { report('http-end', { id: exchange.id, path }); controller.close() }
            exchange.chunks.forEach((chunk, i) => {
              timers.push(setTimeout(() => {
                controller.enqueue(encoder.encode(chunk.text))
                report('http-chunk', { id: exchange.id, types: typesOf(chunk.text) })
                if (i === exchange.chunks.length - 1) finish()
              }, Math.max(0, start + chunk.ms - Date.now())))
            })
            if (!exchange.chunks.length) finish()
            signal?.addEventListener('abort', () => { timers.forEach(clearTimeout); try { controller.error(new DOMException('Aborted', 'AbortError')) } catch { /* al dicht */ } }, { once: true })
          },
        })
      } else {
        body = exchange.body ?? ''
      }
    }
    if (!exchange.chunks) report('http-end', { id: exchange.id, path })
    return new Response(body, { status: exchange.status, statusText: exchange.statusText || '', headers })
  }

  const channels = new Map()
  for (const channel of cfg.channels) {
    if (!channels.has(channel.path)) channels.set(channel.path, [])
    channels.get(channel.path).push(channel)
  }
  class ReplayEventSource extends EventTarget {
    static CONNECTING = 0
    static OPEN = 1
    static CLOSED = 2
    constructor(url, init) {
      const path = pathOf(url)
      const plan = channels.get(path)?.shift()
      // Kanalen buiten de duif-API (de gebeurtenissen van de schil) blijven
      // echt; een duifkanaal zonder opname krijgt een dicht kanaal en een
      // melding, nooit een verbinding met de server.
      if (!plan && !path.startsWith(API)) return new NativeEventSource(url, init)
      super()
      this.url = new URL(url, location.href).href
      this.withCredentials = false
      this.readyState = 0
      this.onopen = null
      this.onmessage = null
      this.onerror = null
      this.timers = []
      if (!plan) {
        this.readyState = 2
        report('replay-miss', { method: 'EVENTSOURCE', path })
        this.timers.push(setTimeout(() => this.fire('error', new Event('error')), 0))
        return
      }
      const start = Date.now()
      report('es-construct', { path })
      const at = (ms, fn) => this.timers.push(setTimeout(fn, Math.max(0, start + ms - Date.now())))
      at(plan.openMs, () => { this.readyState = 1; this.fire('open', new Event('open')) })
      plan.messages.forEach((message, i) => at(message.ms, () => {
        if (this.readyState === 2) return
        this.fire('message', new MessageEvent('message', { data: message.data, origin: location.origin }))
        report('es-message', { index: i, type: message.type })
      }))
    }
    fire(type, ev) {
      this.dispatchEvent(ev)
      const handler = this['on' + type]
      if (typeof handler === 'function') handler.call(this, ev)
    }
    close() {
      this.readyState = 2
      this.timers.forEach(clearTimeout)
      report('es-close', {})
    }
  }
  window.EventSource = ReplayEventSource
}
