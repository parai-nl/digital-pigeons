// Kiezen welke take per item op de etalage komt. Takes worden niet
// weggegooid (lib/take.mjs); keuze.json wijst er per item één aan, met een
// reden. Wat hier staat is
// zuivere logica (getest in test/); keuze.mjs en build.mjs doen het lezen en
// schrijven.
import { CHAT_ROUTES, HISTORICAL_MODELS, LANGS, MEETING_PIGEONS, endpointOrigin, modelAllowed } from './config.mjs'
import { chatEvidence, meetingEvidence } from './evidence.mjs'
import { chatTurn, meetingReaction } from './map.mjs'
import { itemKey } from './take.mjs'

// expectedItems: elk item dat opnames.json nodig heeft, in vaste volgorde.
export function expectedItems(vragen) {
  const keys = []
  for (const q of vragen.chat) for (const lang of LANGS) for (const route of CHAT_ROUTES) keys.push(`chat/${q.id}/${lang}/${route}`)
  for (const s of vragen.meeting) for (const lang of LANGS) for (const pigeon of MEETING_PIGEONS) keys.push(`meeting/${s.id}/${lang}/${pigeon}`)
  return keys
}

// inputText: de tekst die bij dit item hoort volgens vragen.json.
function inputText(vragen, take) {
  if (take.kind === 'chat') return vragen.chat.find((q) => q.id === take.item.question)?.text?.[take.item.lang]
  return vragen.meeting.find((s) => s.id === take.item.statement)?.text?.[take.item.lang]
}

// takeProblems zegt waarom een take niet op de etalage mag; leeg is
// bruikbaar. Alles wordt uit de ruwe opname afgeleid (lib/evidence.mjs), niet
// uit de samenvatting in take.checks. Een afgebroken antwoord (stopReason
// truncated/filtered) mag er nooit op: de tekst aanvullen of inkorten is geen
// optie. Een gesproken reactie zonder stem, een vergadering zonder
// definitief transcript of met een fout of afbreking op het kanaal, en een
// opname van een andere duif of in een andere taal dan het item evenmin.
export function takeProblems(take, vragen, { bands } = {}) {
  const out = []
  if (take.failure) out.push(`mislukt: ${take.failure}`)
  const expected = inputText(vragen, take)
  if (expected === undefined) out.push('het item staat niet (meer) in vragen.json')
  else if (take.input?.text !== expected) out.push('opgenomen met een andere tekst dan in vragen.json')
  // Het verplichte model, of een eerder model voor een take van vóór
  // de wissel (HISTORICAL_MODELS); chat en detectie altijd hetzelfde.
  const policy = take.server?.modelPolicy
  if (policy?.detectionModel !== policy?.model || !endpointOrigin(policy?.endpoint)) out.push(`modelbeleid van de server was ${JSON.stringify(policy)}`)
  else if (Object.hasOwn(HISTORICAL_MODELS, policy.model) && !modelAllowed(policy.model, take.recordedAt)) out.push(`opgenomen op ${take.recordedAt} met ${policy.model}; dat mag alleen vóór ${HISTORICAL_MODELS[policy.model]}`)
  else if (!modelAllowed(policy.model, take.recordedAt)) out.push(`modelbeleid van de server was ${JSON.stringify(policy)}`)
  if (out.length) return out
  try {
    if (take.kind === 'chat') out.push(...chatProblems(take))
    else out.push(...meetingProblems(take, bands))
  } catch (err) {
    out.push(`niet te mappen: ${err.message}`)
  }
  return out
}

function chatProblems(take) {
  const out = []
  const turn = chatTurn(take)
  if (turn.notes?.length) out.push(...turn.notes)
  const e = chatEvidence(take)
  if (e.model !== take.server.modelPolicy.model) out.push(`het done-frame meldt model ${e.model}`)
  if (!e.readBack) out.push('het gesprek is na de beurt niet teruggelezen')
  else if (!e.storedEqualsStreamed) out.push('de opgeslagen tekst wijkt af van de gestreamde')
  if (e.request.pigeonId !== take.item.modulePigeon) out.push(`de beurt ging naar ${e.request.pigeonId}, niet naar ${take.item.modulePigeon}`)
  if (e.request.message !== take.input.text) out.push('de beurt stelde een andere vraag dan de take noemt')
  if (e.request.lang !== take.item.lang) out.push(`de beurt liep in ${e.request.lang}, niet in ${take.item.lang}`)
  const authors = turn.messages.filter((m) => m.author !== 'facilitator').map((m) => m.author)
  if (authors.some((a) => a !== take.item.pigeon)) out.push(`het antwoord is van ${authors.join(', ')}, niet van ${take.item.pigeon}`)
  if (take.item.route.includes('>')) {
    if (!e.comparison) out.push('een vergelijking zonder POST /chat/comparisons')
    else if (e.comparison.pigeonId !== take.item.modulePigeon || e.comparison.session?.id !== e.request.sessionId) out.push('de vergelijking hoort niet bij deze beurt')
  }
  return out
}

function meetingProblems(take, bands) {
  const out = []
  const reaction = meetingReaction(take, { bands })
  const e = meetingEvidence(take)
  if (!e.opened) out.push('de vergadering is niet geopend')
  if (e.pigeonId !== take.item.modulePigeon || e.requestedPigeonId !== take.item.modulePigeon) out.push(`aan tafel zat ${e.pigeonId}, niet ${take.item.modulePigeon}`)
  if (e.lang !== take.item.lang) out.push(`de vergadering liep in ${e.lang}, niet in ${take.item.lang}`)
  if (e.autonomous !== true) out.push('zelfstandig meedoen stond niet aan')
  if (!e.finalTranscripts.length) out.push('de spraakherkenning gaf geen definitief transcript')
  if (e.errors.length) out.push(`fout op het vergaderkanaal: ${e.errors.join(', ')}`)
  if (e.stopped) out.push('de duif is op het vergaderkanaal afgebroken (stopped)')
  if (reaction.type === 'speech' && (!e.ttsFrames || !take.voice?.length || !(take.voice[0].ms > 0))) out.push('gesproken reactie zonder stem')
  return out
}

// autoChoose vult keuze.json aan: een item zonder (geldige) keuze krijgt de
// nieuwste bruikbare take. Een handmatige keuze blijft staan. Een vergelijking
// moet voortbouwen op precies de gekozen take van het brongesprek; anders
// klopt het antwoord van de eerste duif op de pagina niet met wat de tweede
// duif meekreeg.
export function autoChoose(keuze, takes, vragen, options = {}) {
  const items = { ...(keuze.items ?? {}) }
  const byKey = new Map()
  for (const take of takes) {
    const key = itemKey(take)
    if (!byKey.has(key)) byKey.set(key, [])
    byKey.get(key).push(take)
  }
  const ids = new Set(takes.map((t) => t.id))
  const changes = []
  const keys = expectedItems(vragen)
  // Eerst de directe gesprekken en de vergadering, dan de vergelijkingen.
  const ordered = [...keys.filter((k) => !k.includes('>')), ...keys.filter((k) => k.includes('>'))]
  for (const key of ordered) {
    if (items[key] && ids.has(items[key].take) && !items[key].auto) continue
    const all = (byKey.get(key) ?? []).slice().sort((a, b) => b.recordedAt.localeCompare(a.recordedAt))
    const usable = all.filter((t) => takeProblems(t, vragen, options).length === 0)
    let pool = usable
    if (key.includes('>')) {
      const [, question, lang, route] = key.split('/')
      const source = items[`chat/${question}/${lang}/${route.split('>')[0]}`]?.take
      pool = usable.filter((t) => t.item.basedOn === source)
    }
    const chosen = pool[0]
    if (!chosen) {
      if (items[key]?.auto) delete items[key]
      continue
    }
    const reason = all.length === 1
      ? 'automatisch: de enige bewaarde take van dit item'
      : `automatisch: de nieuwste bruikbare van ${all.length} bewaarde takes; niet gekozen: ${all.filter((t) => t !== chosen).map((t) => `${t.id} (${takeProblems(t, vragen, options)[0] ?? 'ook bruikbaar, maar ouder'})`).join('; ')}`
    if (items[key]?.take !== chosen.id) changes.push(key)
    items[key] = { take: chosen.id, reason, auto: true }
  }
  return { keuze: { ...keuze, items: sortItems(items, keys) }, changes }
}

function sortItems(items, keys) {
  const out = {}
  for (const key of keys) if (items[key]) out[key] = items[key]
  for (const key of Object.keys(items)) if (!(key in out)) out[key] = items[key]
  return out
}

// choiceProblems: alles wat de bouw tegenhoudt. Leeg betekent dat
// opnames.json volledig en consistent gebouwd kan worden.
export function choiceProblems(keuze, takesById, vragen, options = {}) {
  const out = []
  const items = keuze.items ?? {}
  for (const key of expectedItems(vragen)) {
    const choice = items[key]
    if (!choice) { out.push(`${key}: geen take gekozen`); continue }
    const take = takesById.get(choice.take)
    if (!take) { out.push(`${key}: take ${choice.take} bestaat niet`); continue }
    if (itemKey(take) !== key) { out.push(`${key}: take ${take.id} hoort bij ${itemKey(take)}`); continue }
    for (const p of takeProblems(take, vragen, options)) out.push(`${key}: ${p}`)
    if (!choice.reason) out.push(`${key}: geen reden bij de keuze`)
    if (take.kind === 'chat' && take.item.route.includes('>')) {
      const source = items[`chat/${take.item.question}/${take.item.lang}/${take.item.from}`]?.take
      if (take.item.basedOn !== source) out.push(`${key}: bouwt voort op ${take.item.basedOn}, maar voor het brongesprek is ${source} gekozen`)
      // En volgens de opname zelf: de vergelijking kopieerde uit precies het
      // gesprek dat die brontake aanmaakte.
      const sourceTake = takesById.get(source)
      const copiedFrom = chatEvidence(take).comparison?.sourceSessionId
      if (sourceTake && copiedFrom !== chatEvidence(sourceTake).sessionId) out.push(`${key}: kopieerde uit gesprek ${copiedFrom}, niet uit het gesprek van ${source}`)
    }
  }
  return out
}
