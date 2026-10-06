import { applyEntryIntent } from '../access/entryIntent'
import { isTab, type Services, type Tab } from '../services.svelte'
import { PIGEONS } from './pigeons'

// De plek van de lezer in het adres. Een taalwissel in de
// parai-core-schil herlaadt de pagina: de module begint dan van voren af aan, op het startscherm van de
// chat. Wat de module in het geheugen had, overleeft dat niet; het adres wel.
//
// Daarom schrijft dit bestand, en alleen dit bestand, de plek in het fragment
// van het adres (`#view=graph&node=…`), door de huidige stap in de
// geschiedenis te vervangen. Het fragment en niet de querystring: een fragment
// gaat nooit naar de server en staat dus niet in toegangslogs of in een
// Referer. Vervangen en nooit een stap toevoegen: de terugknop van de browser
// blijft van de schil.
//
// Wat erin mag is een vaste, gevalideerde set aanwijzingen — geen inhoud. Een
// weergave, de duif, een knoop-id uit de kennisbank en het id van een eigen
// gesprek; nooit een vraag, een concept, een antwoord of iets van een
// vergadering. Een lopende vergadering overleeft de herlaad niet (haar inhoud
// is vluchtig); na de herlaad staat de lezer op het vergaderscherm, klaar om
// opnieuw te beginnen. meeting_frontend_test.go laat precies deze ene
// aanroep toe en verder geen enkele history- of opslag-API.

export interface WorkspacePlace {
  tab: Tab
  // mode is de weergave onder de graaf: daar gaat "terug" naartoe.
  mode: 'chat' | 'meeting'
  help: boolean
  node: string | null
  // pigeon is de gekozen duif, alleen als die afwijkt van de eerste; de
  // signalerende duif ('nonverbal') heeft geen chat en staat hier ook in.
  pigeon: string | null
  // session is het id van het open gesprek. Het wordt alleen teruggezet als
  // het in de eigen gesprekslijst van deze gebruiker staat.
  session: string | null
}

const KEYS = ['view', 'mode', 'help', 'node', 'pigeon', 'chat'] as const
// Dezelfde vormen als de server toetst: tabel + uuid (graph.go) en 128 bits
// hex (repo.go). Wat daarbuiten valt, raakt nooit een route.
const NODE_ID = /^[a-z_]+:[0-9a-f-]{36}$/
const SESSION_ID = /^[0-9a-f]{32}$/
const PIGEON_IDS: readonly string[] = PIGEONS.map((pigeon) => pigeon.id)

function fragment(hash: string): URLSearchParams {
  return new URLSearchParams(hash.startsWith('#') ? hash.slice(1) : hash)
}

// normalize laat van een plek alleen over wat past: een onbekende weergave
// wordt de chat, een verzonnen id wordt niets. Lezen en schrijven gaan er
// allebei doorheen, zodat er niets het adres in of uit komt dat niet past.
function normalize(place: { [K in keyof WorkspacePlace]: unknown }): WorkspacePlace {
  const tab = isTab(place.tab) ? place.tab : 'chat'
  const { node, pigeon, session } = place
  return {
    tab,
    mode: place.mode === 'meeting' ? 'meeting' : 'chat',
    help: place.help === true,
    node: tab === 'graph' && typeof node === 'string' && NODE_ID.test(node) ? node : null,
    pigeon: typeof pigeon === 'string' && PIGEON_IDS.includes(pigeon) ? pigeon : null,
    session: typeof session === 'string' && SESSION_ID.test(session) ? session : null,
  }
}

export function readWorkspacePlace(hash: string): WorkspacePlace {
  const params = fragment(hash)
  return normalize({
    tab: params.get('view'),
    mode: params.get('mode'),
    help: params.get('help') === '1',
    node: params.get('node'),
    pigeon: params.get('pigeon'),
    session: params.get('chat'),
  })
}

// workspaceHash schrijft de plek in het fragment. Op het startscherm blijft
// het leeg; wat er verder in het fragment stond, blijft staan.
export function workspaceHash(hash: string, place: WorkspacePlace): string {
  const params = fragment(hash)
  for (const key of KEYS) params.delete(key)
  const { tab, mode, help, node, pigeon, session } = normalize(place)
  if (tab !== 'chat') params.set('view', tab)
  if (tab === 'graph' && mode !== 'chat') params.set('mode', mode)
  if (help) params.set('help', '1')
  if (node) params.set('node', node)
  if (pigeon) params.set('pigeon', pigeon)
  if (session) params.set('chat', session)
  const out = params.toString()
  return out ? '#' + out : ''
}

// currentPlace leest de plek uit de services: wat de lezer nu ziet, plus
// wat er na een herlaad nog onderweg is (shell.heldPlace). Dat laatste telt
// alleen zolang de lezer zelf niets anders koos: geen eigen duif, geen
// gesprek, geen concept, geen eigen knoop.
export function currentPlace({ shell, graph, chat }: Pick<Services, 'shell' | 'graph' | 'chat'>): WorkspacePlace {
  const held = shell.heldPlace
  const chosen = shell.signaling ? 'nonverbal' : chat.selectedPigeonId
  const fresh = !shell.signaling && !chat.sessionId && !chat.draft && (!chosen || chosen === chat.pigeons[0]?.id)
  const pigeon = fresh ? (held.pigeon ?? chosen) : chosen
  return {
    tab: shell.tab,
    mode: shell.previousTab,
    help: shell.help,
    node: shell.tab === 'graph' ? (graph.selected ?? held.node) : null,
    pigeon: pigeon && pigeon !== chat.pigeons[0]?.id ? pigeon : null,
    session: chat.sessionId || (fresh ? held.session : null),
  }
}

// rememberWorkspacePlace zet de plek in het adres, zonder nieuwe stap in de
// geschiedenis en zonder de stand van de schil (SvelteKit bewaart zijn eigen
// navigatie-index in history.state) aan te raken. Weigert de browser het,
// dan is de plek alleen niet bewaard; de module werkt gewoon door.
export function rememberWorkspacePlace(place: WorkspacePlace): void {
  const location = globalThis.location
  if (!location || !globalThis.history) return
  const hash = workspaceHash(location.hash, place)
  if (hash === location.hash || (hash === '' && !location.href.includes('#'))) return
  const url = location.pathname + location.search + hash
  try { globalThis.history.replaceState(globalThis.history.state, '', url) } catch { /* plek niet bewaard */ }
}

// resumeWorkspace is wat mountModule bij het opstarten doet: de plek uit het
// adres terugzetten, en de login-terugkeer (`?pigeon=&case=`, entryIntent.ts)
// laten lopen zolang die nog over dezelfde stand gaat. Na het inloggen met een
// voorbeeldvraag schrijft de module meteen `#pigeon=…` in het adres; een
// taalwissel daarna hoort de voorbereide vraag (nu in de andere taal) niet
// kwijt te raken. Draagt het adres een gesprek of een andere duif, dan is de
// lezer verder gegaan en wint de plek.
export function resumeWorkspace(services: Services, { hash, search }: { hash: string; search: string }): void {
  const place = restoreWorkspacePlace(services, hash)
  const entryPigeon = new URLSearchParams(search).get('pigeon')
  const entryStillCurrent = !place || (!place.session && (!place.pigeon || place.pigeon === entryPigeon))
  const conversation = place && carriesConversation(place) ? restoreConversation(services, place) : null
  if (!entryStillCurrent) return
  // Na elkaar, niet naast elkaar: allebei vragen ze de duivenlijst, en een
  // tweede loadPigeons() tijdens de eerste wacht daar niet op. Zonder herstel
  // gaat de login-terugkeer meteen, vóór de app zelf de lijst vraagt.
  if (conversation) void conversation.then(() => applyEntryIntent(services, search))
  else void applyEntryIntent(services, search)
}

function carriesConversation(place: WorkspacePlace): boolean {
  return place.session !== null || (place.pigeon !== null && place.pigeon !== 'nonverbal')
}

// restoreWorkspacePlace zet de weergave uit het adres meteen terug, begint de
// graafknoop te laden en houdt knoop, duif en gesprek vast tot ze er zijn.
// Geeft de gelezen plek, of null als het adres er geen droeg.
export function restoreWorkspacePlace(services: Services, hash: string): WorkspacePlace | null {
  const place = readWorkspacePlace(hash)
  if (workspaceHash('', place) === '') return null
  const { shell, graph } = services
  shell.tab = place.tab
  shell.previousTab = place.mode
  shell.help = place.help
  shell.signaling = place.pigeon === 'nonverbal'
  shell.heldPlace = { node: place.node, pigeon: place.pigeon === 'nonverbal' ? null : place.pigeon, session: place.session }
  const node = place.node
  if (node) {
    void graph.focus(node).then(() => {
      // Losgelaten zodra er een antwoord is: de knoop staat er, de lezer koos
      // zelf een andere, of de server zegt dat hij er niet is. Een
      // netwerkfout of een server die even niet kan, laat hem staan.
      const status = graph.notice?.status ?? 0
      if (graph.selected !== null || graph.notice?.reason === 'node_not_found' || (status >= 400 && status < 500)) {
        shell.heldPlace = { ...shell.heldPlace, node: null }
      }
    })
  }
  return place
}

async function restoreConversation({ chat, shell }: Services, place: WorkspacePlace): Promise<void> {
  await Promise.all([chat.loadPigeons(), place.session ? chat.loadSessions() : undefined])
  if (chat.disposed) return
  // Kwam een lijst niet binnen, dan blijven duif en gesprek in het adres
  // staan: de volgende herlaad probeert het opnieuw.
  if (!chat.pigeonsLoaded || (place.session && !chat.sessionsLoaded)) return
  shell.heldPlace = { ...shell.heldPlace, pigeon: null, session: null }
  const untouched = !shell.signaling && !chat.sessionId && !chat.draft && chat.selectedPigeonId === chat.pigeons[0]?.id
  if (!untouched) return
  const session = place.session ? chat.sessions.find((item) => item.id === place.session) : undefined
  if (session && chat.pigeons.some((pigeon) => pigeon.id === session.pigeonId)) {
    await chat.openSession(session.id)
    return
  }
  // Een duif zonder gesprek: haar startscherm, net als vóór de herlaad. Niet
  // via selectPigeon — die hervat haar laatste gesprek, en dat stond er niet.
  if (place.pigeon && chat.pigeons.some((pigeon) => pigeon.id === place.pigeon)) chat.selectedPigeonId = place.pigeon
}
