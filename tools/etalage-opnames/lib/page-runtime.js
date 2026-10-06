// Init-script voor de vergaderopname. Draait in de echte frontend vóór de app
// laadt en luistert alleen mee: elk verzoek aan /api/duif/v1 (met het antwoord,
// ook gestreamd), elk bericht op het vergaderkanaal, wanneer de microfoon open
// gaat, wanneer er werkelijk geluid begint en welk gebaar de 3D-duif maakt.
// Het verandert niets aan wat de app doet of ontvangt.
//
// Overgenomen en ingekort uit de opnamestand van de uitlegvideo
// (tools/uitlegvideo/replay/page-runtime.js); het gebaar komt erbij.
//
// De functie gaat als tekst de pagina in (Function.prototype.toString) en mag
// dus niets van buiten gebruiken.
export function pageRuntime() {
  const report = (kind, data = {}) => {
    try { void window.__etReport(kind, { t: Date.now(), ...data }) } catch { /* driver weg */ }
  }
  const API = '/api/duif/v1/'
  const nativeFetch = window.fetch.bind(window)
  const NativeEventSource = window.EventSource
  const pathOf = (url) => { const u = new URL(url, location.href); return u.pathname + u.search }
  const urlOf = (input) => typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
  const methodOf = (input, init) => String(init?.method || (input instanceof Request ? input.method : 'GET')).toUpperCase()
  const NULL_BODY = new Set([101, 204, 205, 304])
  // Alleen wat het protocol draagt; cookies en autorisatie komen nooit in een take.
  const keepHeader = (name) => name === 'content-type' || name.startsWith('x-duif-')

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
    const lead = Math.max(0, when - this.context.currentTime)
    report('pcm-start', { at: Date.now() + lead * 1000, duration: this.buffer?.duration ?? 0, sampleRate: this.buffer?.sampleRate ?? 0 })
    return nativeStart.call(this, when, ...rest)
  }
  const nativePlay = HTMLMediaElement.prototype.play
  HTMLMediaElement.prototype.play = function (...args) {
    const el = this
    const src = el.currentSrc || el.src
    report('media-play', { src })
    el.addEventListener('playing', () => report('media-playing', { src }), { once: true })
    el.addEventListener('ended', () => report('media-ended', { src }), { once: true })
    return nativePlay.apply(el, args)
  }

  // Het gebaar. De frontend kiest het zelf, per reactie, tussen de twee
  // gebakken clips van het model (Peck en WingWave; gestureSequence.ts); het
  // staat in geen enkel bericht. Three.js zet bij elke nieuwe AnimationAction
  // `this._clip`: via die ene toewijzing krijgt dit script de klasse te pakken
  // en meldt het elke play() met de naam van de clip. De eerste melding na een
  // reactie is het gebaar dat de duif maakte.
  Object.defineProperty(Object.prototype, '_clip', {
    configurable: true,
    set(value) {
      Object.defineProperty(this, '_clip', { value, writable: true, configurable: true, enumerable: true })
      const proto = Object.getPrototypeOf(this)
      if (!value || typeof value.name !== 'string' || !Array.isArray(value.tracks) || typeof proto?.play !== 'function' || proto.__etalageGesture) return
      const play = proto.play
      proto.play = function (...args) {
        report('gesture', { clip: this._clip?.name ?? '' })
        return play.apply(this, args)
      }
      Object.defineProperty(proto, '__etalageGesture', { value: true })
    },
  })

  let seq = 0
  window.fetch = async (input, init) => {
    const path = pathOf(urlOf(input))
    if (!path.startsWith(API)) return nativeFetch(input, init)
    const id = ++seq
    const method = methodOf(input, init)
    const body = init?.body
    const requestBody = body == null ? null : typeof body === 'string' ? body : { binary: body.byteLength ?? body.size ?? null }
    report('http-request', { id, method, path, requestBody })
    let res
    try {
      res = await nativeFetch(input, init)
    } catch (err) {
      report('http-failed', { id, error: String(err?.name || err) })
      throw err
    }
    report('http-head', { id, status: res.status, headers: [...res.headers].filter(([k]) => keepHeader(k)) })
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
          if (text) report('http-chunk', { id, text })
        }
        const tail = decoder.decode()
        if (tail) report('http-chunk', { id, text: tail })
        report('http-end', { id })
      } catch (err) {
        report('http-end', { id, aborted: String(err?.name || err) })
      }
    })()
    return new Response(NULL_BODY.has(res.status) ? null : mine, { status: res.status, statusText: res.statusText, headers: res.headers })
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
}
