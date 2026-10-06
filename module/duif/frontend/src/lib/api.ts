// HTTP-hulpjes voor de module-API (/api/duif/v1). Twee regels:
//  1. Ruwe fouttekst bereikt de gebruiker nooit: alleen de tweetalige
//     servermelding (message{en,nl}) of een eigen i18n-tekst.
//  2. Alles is injecteerbaar (fetch als parameter) zodat tests geen globals
//     hoeven te patchen.

import type { Bilingual, Lang } from './i18n.svelte'

export const API = '/api/duif/v1'

export type Fetcher = typeof fetch

// ApiFailure is wat een mislukte aanroep oplevert: de reason-code en de
// tweetalige melding als de server die stuurde, anders alleen de status.
export interface ApiFailure {
  status: number
  reason?: string
  message?: Bilingual
}

// De talen waarin een servermelding kan binnenkomen; `satisfies` houdt de lijst
// aan `Lang` vast zonder de i18n-module echt te importeren.
const LANGS = ['en', 'nl'] as const satisfies readonly Lang[]

// bilingualFrom maakt van ongecontroleerde netwerk-JSON een Bilingual: per taal
// telt alleen een niet-lege string mee. Zonder deze wacht landt {"nl":{"x":1}}
// als melding in de UI en rendert hij als "[object Object]".
export function bilingualFrom(value: unknown): Bilingual | undefined {
  if (!value || typeof value !== 'object') return undefined
  const raw = value as Record<string, unknown>
  const out: Bilingual = {}
  for (const lang of LANGS) {
    const text = raw[lang]
    if (typeof text === 'string' && text !== '') out[lang] = text
  }
  return out.en === undefined && out.nl === undefined ? undefined : out
}

// readFailure leest een foutantwoord zonder ooit res.text() naar de UI te
// laten lekken: geen JSON (proxy-502, HTML) betekent alleen een status.
export async function readFailure(res: Response): Promise<ApiFailure> {
  const failure: ApiFailure = { status: res.status }
  try {
    const body = (await res.json()) as { error?: unknown; message?: unknown }
    if (typeof body?.error === 'string') failure.reason = body.error
    const message = bilingualFrom(body?.message)
    if (message) failure.message = message
  } catch {
    // niet-JSON body: bewust genegeerd
  }
  return failure
}

export function jsonInit(body: unknown, method = 'POST'): RequestInit {
  return { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }
}

// SSEFrame is één geparsed data-frame; wat de server stuurt is JSON per frame.
export type SSEFrame = Record<string, unknown>

// readSSE leest een fetch-body als Server-Sent Events volgens de WHATWG-spec:
// regels eindigen op LF, CRLF of CR (CRLF telt één keer), `:`-regels zijn
// commentaar, meerdere data-regels worden met LF samengevoegd, een lege regel
// verstuurt het frame, een frame zonder data wordt niet verstuurd. Een frame
// dat geen JSON is wordt overgeslagen (het protocol mag groeien; een kapot
// frame mag de stroom niet breken). Een AbortError is een normaal einde.
export async function readSSE(
  body: ReadableStream<Uint8Array> | null,
  onFrame: (frame: SSEFrame) => void,
  signal?: AbortSignal,
): Promise<void> {
  if (!body) return
  const reader = body.getReader()
  // Een abort maakt de lopende read() wakker via cancel(); zonder dit zou een
  // stroom die niets meer stuurt tot in de eeuwigheid hangen.
  const onAbort = () => {
    void reader.cancel().catch(() => {})
  }
  if (signal?.aborted) {
    onAbort()
    return
  }
  signal?.addEventListener('abort', onAbort, { once: true })
  const decoder = new TextDecoder('utf-8')
  let buffer = ''
  let data: string[] = []
  let sawCR = false

  const dispatch = () => {
    if (data.length === 0) return
    const raw = data.join('\n')
    data = []
    try {
      const parsed = JSON.parse(raw) as unknown
      if (parsed && typeof parsed === 'object') onFrame(parsed as SSEFrame)
    } catch {
      // kapot frame: overslaan
    }
  }

  const handleLine = (line: string) => {
    if (line === '') {
      dispatch()
      return
    }
    if (line.startsWith(':')) return
    const colon = line.indexOf(':')
    const field = colon < 0 ? line : line.slice(0, colon)
    let value = colon < 0 ? '' : line.slice(colon + 1)
    if (value.startsWith(' ')) value = value.slice(1)
    if (field === 'data') data.push(value)
    // event/id/retry: het duif-protocol gebruikt ze niet; genegeerd.
  }

  const consume = (chunk: string) => {
    buffer += chunk
    let start = 0
    for (let i = 0; i < buffer.length; i++) {
      const ch = buffer[i]
      if (ch === '\r') {
        handleLine(buffer.slice(start, i))
        start = i + 1
        sawCR = true
        continue
      }
      if (ch === '\n') {
        if (sawCR && i === start) {
          // de LF van een CRLF: al afgehandeld bij de CR
          start = i + 1
          sawCR = false
          continue
        }
        handleLine(buffer.slice(start, i))
        start = i + 1
      }
      sawCR = false
    }
    buffer = buffer.slice(start)
  }

  try {
    for (;;) {
      if (signal?.aborted) return
      const { done, value } = await reader.read()
      if (done) break
      if (value) consume(decoder.decode(value, { stream: true }))
    }
    consume(decoder.decode())
    if (buffer !== '') handleLine(buffer)
    dispatch()
  } catch (err) {
    if (signal?.aborted || (err instanceof DOMException && err.name === 'AbortError')) return
    throw err
  } finally {
    signal?.removeEventListener('abort', onAbort)
    try {
      await reader.cancel()
    } catch {
      // al gesloten
    }
  }
}

// shareInFlight laat gelijktijdige GET's naar dezelfde URL één antwoord delen.
// Drie stores kijken elk op hun eigen manier naar GET /v1/duiven — de naamlijst
// van de schil, de chatduiven en de vergaderduiven — terwijl dat één
// configuratie-endpoint is. Zonder dit gaat dezelfde regel drie keer over de
// lijn; met dit hulpje hoeven de stores elkaar daarvoor niet te kennen.
//
// Alleen lopende aanvragen worden gedeeld, nooit afgeronde: dit is geen cache.
// Elke roeper krijgt zijn eigen `clone()`, want een body is maar één keer
// leesbaar. Een fout deelt ook: wie meelift krijgt dezelfde afwijzing.
export function shareInFlight(fetcher: Fetcher): Fetcher {
  const running = new Map<string, Promise<Response>>()
  return (input, init) => {
    const method = (init?.method ?? 'GET').toUpperCase()
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    if (method !== 'GET' || init?.body !== undefined) return fetcher(input, init)
    const running_ = running.get(url)
    if (running_) return running_.then((res) => res.clone())
    const started = fetcher(input, init).finally(() => running.delete(url))
    running.set(url, started)
    return started.then((res) => res.clone())
  }
}
