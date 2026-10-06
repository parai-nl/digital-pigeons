// Van take naar het schema van site/etalage/data/opnames.json (het contract
// in README.md): een chattake wordt een Turn, een vergadertake een Reaction,
// een bron van de wire een Source. Tekst gaat er ongewijzigd doorheen; hier
// wordt alleen gekozen wat de module in beeld liet zien en in welke vorm.
import { MODULE_TO_SITE } from './config.mjs'
import { timedFrames } from './sse.mjs'

const ARTIFACT_KIND = 'pigeon_answer'
const FACILITATOR = 'facilitator'

// De vulzin die oude beurten onder elk antwoord zetten; de module toont hem
// niet (presentation.ts, isGuidanceFiller).
const GUIDANCE_FILLER = new Set(["The pigeon's answer is shown above.", 'The pigeon’s answer is shown above.', 'Het antwoord van de duif staat hierboven.'])

// chatTurn maakt van een chattake één Turn: de fasen die voorbijkwamen en de
// berichten in leesvolgorde, zoals chatStore.send ze opbouwt (kader van de
// facilitator, het blok van de duif, de bemiddeling) en zoals
// presentation.visibleMessages ze toont.
export function chatTurn(take) {
  const x = [...take.exchanges].reverse().find((e) => e.method === 'POST' && e.path === '/chat/turn' && e.status === 200 && e.chunks)
  if (!x) throw new Error('geen geslaagde beurt in de take')
  const frames = timedFrames(x.chunks)
  const phases = []
  const blocks = new Map()
  const order = []
  let framing = ''
  let mediation = ''
  let turnSources = null
  let done = null
  for (const { ms, frame } of frames) {
    switch (frame.type) {
      case 'phase':
        if (typeof frame.phase === 'string' && !phases.includes(frame.phase)) phases.push(frame.phase)
        break
      case 'text':
        if (typeof frame.delta !== 'string') break
        if (order.length) mediation += frame.delta
        else framing += frame.delta
        break
      case 'artifact': {
        const known = blocks.get(frame.id)
        if (frame.done === true) {
          if (known) known.stopReason = frame.stopReason
          break
        }
        if (known || frame.kind !== ARTIFACT_KIND) break
        const block = { id: frame.id, persona: frame.persona, kbStatus: frame.kbStatus, related: frame.related === true, text: '', deltas: [], sources: [] }
        blocks.set(frame.id, block)
        order.push(block)
        break
      }
      case 'sources':
        if (frame.id && blocks.has(frame.id)) blocks.get(frame.id).sources = frame.sources ?? []
        else if (!frame.id) turnSources = frame.sources ?? []
        break
      case 'artifact_delta': {
        const block = blocks.get(frame.id)
        if (block && typeof frame.delta === 'string' && frame.delta !== '') {
          block.text += frame.delta
          block.deltas.push([x.t + ms, frame.delta])
        }
        break
      }
      case 'done':
        done = frame
        break
      default:
        break
    }
  }
  if (!done) throw new Error('de beurt heeft geen done-frame')
  if (!order.length) throw new Error('de beurt heeft geen antwoord van de duif')

  const pigeonAnswered = order.some((b) => b.text.trim() !== '')
  const turnBroken = done.stopReason !== undefined && done.stopReason !== 'natural'
  const modulePigeon = take.item.modulePigeon
  // Bij de Kennispartner blijft de facilitator intern zolang zij antwoordt en
  // de beurt normaal eindigt (visibleMessages).
  const showFacilitator = (text) => text.trim() !== '' && !GUIDANCE_FILLER.has(text.trim()) && (modulePigeon !== 'biological' || !pigeonAnswered || turnBroken)
  const messages = []
  const notes = []
  if (showFacilitator(framing)) messages.push({ author: FACILITATOR, text: framing })
  for (const block of order) {
    const author = MODULE_TO_SITE[block.persona] ?? MODULE_TO_SITE[modulePigeon]
    if (!author) throw new Error(`onbekende duif ${block.persona}`)
    const first = block.deltas[0]?.[0] ?? 0
    const sources = mapSources(take, block.sources.length ? block.sources : turnSources ?? done.sources ?? [])
    const message = { author, text: block.text, chunks: mergeChunks(block.deltas.map(([t, d]) => [t - first, d])), kind: answerKind(block.kbStatus, modulePigeon), sources }
    if (block.related && sources.length) message.related = true
    messages.push(message)
    if (block.stopReason && block.stopReason !== 'natural') notes.push(`het antwoord eindigde met stopReason ${block.stopReason}`)
  }
  if (showFacilitator(mediation)) messages.push({ author: FACILITATOR, text: mediation })
  if (turnBroken) notes.push(`de beurt eindigde met stopReason ${done.stopReason}`)
  const turn = { take: take.id, phases, messages }
  if (notes.length) Object.defineProperty(turn, 'notes', { value: notes, enumerable: false })
  return turn
}

// answerKind: de soort antwoord zoals de module hem markeert
// (AssistantMessage.svelte). Bronnen gevonden → grounded. Geen bron: bij de
// Kennispartner een kennisgat, bij de Stakeholder "Standpunt zonder
// onderzoek".
export function answerKind(kbStatus, modulePigeon) {
  if (kbStatus === 'sources') return 'grounded'
  if (kbStatus === 'gap' || kbStatus === 'persona_only') return modulePigeon === 'individualist' ? 'standpoint' : 'gap'
  throw new Error(`kennisbankstand ${kbStatus ?? '(geen)'} is geen bruikbaar antwoord`)
}

// mergeChunks voegt delta's die in hetzelfde netwerkbrok binnenkwamen samen:
// dat is het tempo waarin de lezer de tekst zag groeien.
export function mergeChunks(deltas) {
  const out = []
  for (const [ms, text] of deltas) {
    const last = out.at(-1)
    if (last && last[0] === ms) last[1] += text
    else out.push([ms, text])
  }
  return out
}

// mapSource zet een bron van de wire (lib/sources.ts in de module) om naar de
// Source van de etalage: nummer, titel, auteurs, jaar, link, passage.
export function mapSource(src) {
  const ref = src.ref ?? {}
  const cited = parseCitation(ref.citation ?? '')
  const paper = typeof ref.paper === 'string' && /\s/.test(ref.paper.trim()) ? ref.paper.trim() : ''
  const out = { n: src.number }
  const title = cited.title || paper
  if (title) out.title = title
  if (cited.authors) out.authors = cited.authors
  if (cited.year) out.year = cited.year
  const url = sourceUrl(ref)
  if (url) out.url = url
  // De lade toont alle passages (hoogstens acht), anders het citaat. Het
  // contract heeft één passage: de eerste. Heeft de bron er meer, dan staan ze
  // er buiten het contract ook allemaal bij, zodat de pagina ze kan tonen.
  // Een bron met afgekeurde gronding heeft geen passage en krijgt er
  // ook geen.
  const passages = (ref.passages ?? []).slice(0, 8).map((p) => p?.text).filter((t) => typeof t === 'string' && t.trim() !== '')
  if (!passages.length && typeof ref.quote === 'string' && ref.quote.trim()) passages.push(ref.quote)
  if (passages.length) {
    out.passage = passages[0]
    out.passageLang = passageLang(passages[0])
    if (passages.length > 1) out.passages = passages.map((text) => ({ text, lang: passageLang(text) }))
  }
  // Geen titel uit de bronvermelding en geen papertitel: dan is er alleen een
  // bronsleutel (zoals "galifret1967"), en een machinesleutel hoort niet op
  // de publieke pagina. De bouw weigert; los het op in de kennisbank of kies
  // een andere take.
  if (!out.title) throw new Error(`bron ${src.number} heeft geen titel in de bronvermelding (bronsleutel ${String(ref.paper ?? '').trim() || 'onbekend'}); een machinesleutel komt niet op de pagina`)
  return out
}

// mapSources: de bronnen van één take; een fout noemt de take.
function mapSources(take, sources) {
  try {
    return sources.map(mapSource)
  } catch (err) {
    throw new Error(`${take.id}: ${err.message}`)
  }
}

// sourceUrl: alleen https. Een doi wordt een doi.org-link; anders een
// https-adres dat letterlijk in de bronvermelding staat.
export function sourceUrl(ref) {
  const doi = typeof ref.doi === 'string' ? ref.doi.trim().replace(/^https?:\/\/(dx\.)?doi\.org\//i, '') : ''
  if (/^10\.\d{4,9}\/\S+$/.test(doi)) return `https://doi.org/${doi}`
  const found = typeof ref.citation === 'string' ? ref.citation.match(/https:\/\/[^\s<>"]+[^\s<>".,;)]/) : null
  return found ? found[0] : undefined
}

// parseCitation leest auteurs, jaar en titel uit een APA-achtige
// bronvermelding: "Achternaam, A., & Ander, B. (2017). Titel. Tijdschrift, …".
// Lukt dat niet zeker, dan blijven de velden leeg in plaats van gegokt.
export function parseCitation(citation) {
  const text = String(citation).replace(/\s+/g, ' ').trim()
  const m = text.match(/^(.+?)\s*\((\d{4})[a-z]?(?:,[^)]*)?\)\.?\s*(.+)$/)
  if (!m) return {}
  const [, authorsPart, year, tail] = m
  const authors = authorsPart.replace(/[,\s]+$/, '').trim()
  // Een adres achter de titel ("… neurons. https://doi.org/…") hoort er niet bij.
  const rest = tail.replace(/\s*https?:\/\/\S+.*$/, '')
  // De titel loopt tot de eerste zinsgrens die niet binnen een afkorting valt:
  // ". ", "? " of "! " gevolgd door een hoofdletter of cijfer.
  let title = ''
  const re = /([.?!])\s+(?=[A-Z0-9À-Ý])/g
  let hit
  while ((hit = re.exec(rest))) {
    const candidate = rest.slice(0, hit.index + (hit[1] === '.' ? 0 : 1))
    const lastWord = candidate.split(' ').at(-1) ?? ''
    // Een losse hoofdletter of een bekende afkorting is geen einde van de titel.
    if (hit[1] === '.' && (/^[A-Z]$/.test(lastWord) || /^(e\.g|i\.e|vs|St|Dr|No|Vol|U\.S|U\.K)$/i.test(lastWord))) continue
    title = candidate
    break
  }
  if (!title) title = rest.replace(/\.$/, '')
  title = title.trim()
  // "[title not present in file]" en dergelijke zijn een notitie, geen titel.
  if (/^\[.*\]$/.test(title)) title = ''
  return { authors, year: Number(year), ...(title ? { title } : {}) }
}

// passageLang: de taal van een passage. De kennisbank draagt die niet per
// citaat (source_language gaat over de bundel), dus het wordt geteld: veel
// voorkomende Nederlandse tegen Engelse woorden. Bij twijfel Engels, de taal
// van vrijwel alle papers in het corpus.
const NL = new Set(['de', 'het', 'een', 'en', 'van', 'is', 'dat', 'die', 'niet', 'op', 'te', 'zijn', 'met', 'voor', 'er', 'ook', 'als', 'bij', 'worden', 'wordt', 'door', 'naar', 'maar', 'om', 'aan', 'deze', 'duiven', 'nog', 'kan', 'uit', 'meer', 'dan', 'geen', 'wel', 'hun'])
const EN = new Set(['the', 'a', 'an', 'and', 'of', 'is', 'that', 'which', 'not', 'on', 'to', 'are', 'with', 'for', 'there', 'also', 'as', 'at', 'be', 'by', 'from', 'but', 'in', 'this', 'these', 'pigeons', 'was', 'were', 'can', 'more', 'than', 'no', 'their', 'have', 'has'])
export function passageLang(text) {
  let nl = 0
  let en = 0
  for (const word of String(text).toLowerCase().match(/[a-zà-ÿ']+/g) ?? []) {
    if (NL.has(word)) nl++
    if (EN.has(word)) en++
  }
  return nl > en ? 'nl' : 'en'
}

// De twee gebakken clips van het model en hun namen in de frontend
// (pigeonMotion.ts): Peck → peck, WingWave → ruffle.
export const GESTURES = { Peck: 'peck', WingWave: 'ruffle' }
export const KOER_DIR = 'etalage/media/geluid/koer'
export const STEM_DIR = 'etalage/media/geluid/stem'

// meetingReaction maakt van een vergadertake één Reaction: wat de duif deed
// binnen het venster na de uitspraak. Spraak (tekst, bronnen, stem), signaal
// (veldgeluid) of stilte; bij spraak en signaal het gebaar dat de 3D-duif
// maakte. `bands` is de koppeling band → bestand uit de mediacatalogus van de
// module, voor als de pagina geen afgespeeld bestand meldde.
export function meetingReaction(take, { bands = {} } = {}) {
  const { utteranceEndMs, windowEndMs } = take.timeline ?? {}
  if (!Number.isFinite(utteranceEndMs) || !Number.isFinite(windowEndMs)) throw new Error('de take heeft geen tijdlijn van de uitspraak')
  const messages = (take.channels ?? []).flatMap((c) => c.messages.map((m) => ({ t: m.t, ev: m.tts ?? JSON.parse(m.data) }))).sort((a, b) => a.t - b.t)
  const start = messages.find((m) => m.ev.type === 'persona' || m.ev.type === 'sound')
  if (!start || start.t > windowEndMs) return { take: take.id, type: 'silence' }
  const delayMs = Math.max(0, start.t - utteranceEndMs)
  const clip = (take.probes ?? []).find((p) => p.kind === 'gesture' && p.t >= start.t - 250)?.clip
  const gesture = clip ? GESTURES[clip] : undefined
  if (clip && !gesture) throw new Error(`onbekende clip ${clip}`)

  if (start.ev.type === 'sound') {
    const played = (take.probes ?? []).find((p) => p.kind === 'media-play' && p.t >= start.t - 250)?.src
    const file = played ? decodeURIComponent(new URL(played, 'http://x').pathname.split('/').pop()) : bands[start.ev.sound?.band]
    if (!file) throw new Error('niet vast te stellen welk geluid de module afspeelde')
    return { take: take.id, type: 'signal', delayMs, sound: `${KOER_DIR}/${file}`, ...(gesture ? { gesture } : {}) }
  }

  let text = ''
  let sources = null
  let end = null
  for (const m of messages) {
    if (m.t < start.t) continue
    if (m.ev.type === 'text' && typeof m.ev.delta === 'string') text += m.ev.delta
    if (m.ev.type === 'sources' && Array.isArray(m.ev.sources)) sources = m.ev.sources
    if (m.ev.type === 'response_done') { end = m; if (Array.isArray(m.ev.sources)) sources = m.ev.sources; break }
    if (m.ev.type === 'stopped' || m.ev.type === 'error') { end = m; break }
  }
  if (end?.ev.type !== 'response_done') throw new Error(`de bijdrage is niet afgerond (${end?.ev.type ?? 'geen einde'})`)
  if (!text.trim()) throw new Error('de bijdrage heeft geen tekst')
  const reaction = { take: take.id, type: 'speech', delayMs, text, sources: mapSources(take, sources ?? []) }
  const voice = (take.voice ?? [])[0]
  if (voice) {
    reaction.audio = `${STEM_DIR}/${take.id}.mp3`
    reaction.audioMs = voice.ms
  }
  if (gesture) reaction.gesture = gesture
  return reaction
}
