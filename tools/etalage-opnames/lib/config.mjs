// Vaste afspraken van de opnamestroom (contract: README.md). Alles
// wat meer dan één script nodig heeft staat hier, zodat opnemen, kiezen en
// bouwen dezelfde namen en paden gebruiken.
import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync } from 'node:fs'
import path from 'node:path'
import { parseEnv } from 'node:util'

export const TOOL_DIR = path.resolve(import.meta.dirname, '..')
export const REPO_DIR = path.resolve(TOOL_DIR, '../..')
export const MODULE_DIR = path.join(REPO_DIR, 'module')
export const TAKES_DIR = path.join(TOOL_DIR, 'takes')
export const SITE_DIR = path.join(REPO_DIR, 'site')

// Elke modelaanroep van de duiven gaat naar dit ene model. Opnemen
// (server.mjs, record-*.mjs, graph-shots.mjs) weigert een server die iets
// anders meldt. Het endpoint is geen constante: de opnameserver krijgt het uit
// PARAI_LLM_BASE_URL in module/.env, en opnemen eist dat de server precies dat
// HTTPS-adres meldt. Welk endpoint een take gebruikte, staat in de take zelf.
export const MODEL = 'vertex/gemini-3.6-flash-eu'
// endpointOrigin: de oorsprong (https://host[:poort]) van een HTTPS-adres zonder
// gebruikersnaam, query of fragment, zoals de module hem in haar modelbeleid
// meldt; anders null. Er is geen standaardadres.
export function endpointOrigin(raw) {
  let url
  try { url = new URL(raw) } catch { return null }
  if (url.protocol !== 'https:' || !url.hostname || url.username || url.password || url.search || url.hash) return null
  return url.origin
}
// Eerdere modellen, met het moment tot wanneer een take ermee opgenomen mag
// zijn. De takes van 23 september 2026 zijn gemaakt met het model van de module
// op main van dat moment, vertex_ai/gemini-3.5-flash: de wissel naar
// vertex/gemini-3.6-flash-eu kwam pas op 24 september op main. Kiezen, bouwen en de tests aanvaarden zo'n
// take als herkomst; het model in de take blijft de waarheid over hoe hij
// gemaakt is. Een take van ná de datum met dat model wordt geweigerd, en
// opnemen doet nooit met een van deze.
export const HISTORICAL_MODELS = { 'vertex_ai/gemini-3.5-flash': '2026-09-25T00:00:00Z' }

// modelAllowed: het huidige model altijd; een eerder model alleen voor een take
// die vóór zijn einddatum is opgenomen.
export function modelAllowed(model, recordedAt) {
  if (model === MODEL) return true
  if (typeof model !== 'string' || !Object.hasOwn(HISTORICAL_MODELS, model)) return false
  const at = Date.parse(recordedAt)
  return Number.isFinite(at) && at < Date.parse(HISTORICAL_MODELS[model])
}

// De opnameserver van dit gereedschap draait op een eigen poort. Op de poorten
// in FORBIDDEN_PORTS draaien gewoonlijk andere lokale duivenservers (zoals de
// opnameserver van de uitlegvideo); daar wordt nooit tegen opgenomen.
export const PORT = 8094
export const BASE = `http://127.0.0.1:${PORT}`
export const FORBIDDEN_PORTS = new Set([8080, 8081, 8092])
// De eigen databasekopie waarin opnemen gesprekken aanmaakt, de database met
// de kennisbank waarvan zij een kopie is (alleen gelezen), en de
// Postgres-container van de lokale werkomgeving. Een andere opstelling zet ze
// in de omgeving.
export const DATABASE = process.env.ETALAGE_DATABASE || 'duif_etalage'
export const SOURCE_DATABASE = process.env.ETALAGE_SOURCE_DATABASE || 'duif_v3'
export const PG_CONTAINER = process.env.ETALAGE_PG_CONTAINER || 'duif-dev-pg'
// Ze komen in SQL en in een shellregel binnen de container: alleen eenvoudige namen.
for (const [name, value] of Object.entries({ ETALAGE_DATABASE: DATABASE, ETALAGE_SOURCE_DATABASE: SOURCE_DATABASE })) {
  if (!/^[a-z_][a-z0-9_]*$/.test(value)) throw new Error(`${name} moet een eenvoudige databasenaam zijn (kleine letters, cijfers, _)`)
}
if (!/^[\w.-]+$/.test(PG_CONTAINER)) throw new Error('ETALAGE_PG_CONTAINER moet een eenvoudige containernaam zijn')

// Publieksnamen van de etalage en de interne id's van de module.
export const SITE_TO_MODULE = { kennispartner: 'biological', stakeholder: 'individualist', signalerend: 'nonverbal' }
export const MODULE_TO_SITE = Object.fromEntries(Object.entries(SITE_TO_MODULE).map(([site, mod]) => [mod, site]))
export const CHAT_PIGEONS = ['kennispartner', 'stakeholder']
export const MEETING_PIGEONS = ['kennispartner', 'stakeholder', 'signalerend']
export const LANGS = ['nl', 'en']

// De vier chatroutes per vraag, in de sleutels van opnames.json. `a>b` is het
// antwoord van b op de hoekknop "Vraag het ook aan de andere duif" in het
// gesprek met a.
export const CHAT_ROUTES = ['kennispartner', 'stakeholder', 'kennispartner>stakeholder', 'stakeholder>kennispartner']

// Vergadering: zo lang na het einde van de uitspraak mag de duif beginnen te
// reageren. Wat daarna pas begint telt niet; wat binnen het venster begon mag
// uitpraten.
export const MEETING_WINDOW_MS = 25000
// Stilte vóór de uitspraak in de synthetische microfoon: de spraakherkenning
// heeft na het openen even nodig om te verbinden (zoals bij de uitlegvideo).
export const MIC_LEAD_MS = 4000

// WebGL via de GPU, zodat de 3D-duif in headless Chromium laadt en haar
// gebaar maakt. ANGLE op Metal bestaat alleen op macOS (uitlegvideo); op een
// ander systeem vallen de vlaggen weg en rendert Chromium in software. Dat is
// daar niet getest.
export const GPU_FLAGS = process.platform === 'darwin' ? ['--use-angle=metal', '--enable-gpu', '--ignore-gpu-blocklist'] : []

// De stemmen van de vergaderdeelnemers: ElevenLabs, premade, dezelfde als in
// het vergaderfragment van de uitlegvideo, per rol (de Engelse
// sprekersnaam uit vragen.json). Een nieuwe rol zonder stem is een fout.
export const TTS = { provider: 'ElevenLabs', model: 'eleven_multilingual_v2', format: 'mp3_44100_128' }
export const SPEAKER_VOICES = {
  Chair: { voice: 'Charlotte', voiceId: 'XB0fDUnXU5powFXDhCwa' },
  Operations: { voice: 'Daniel', voiceId: 'onwK4e9ZLuTAKqWW03F9' },
  Advisor: { voice: 'Sarah', voiceId: 'EXAVITQu4vr4xnSDxMaL' },
}

// De hoofdcheckout (voor module/.env en de gedeelde node_modules): de map
// boven de gemeenschappelijke .git, ook als dit een extra werkmap van git is.
export function mainCheckout() {
  const common = execFileSync('git', ['-C', REPO_DIR, 'rev-parse', '--path-format=absolute', '--git-common-dir'], { encoding: 'utf8' }).trim()
  return path.dirname(common)
}

// stamp geeft een tijdstempel voor take-id's: seconden, UTC, zonder tekens die
// in een mapnaam lastig zijn.
export function stamp(date = new Date()) {
  return date.toISOString().replace(/[-:]/g, '').replace(/\.\d+Z$/, 'Z')
}

// routeSlug maakt van een chatroute een stuk van een take-id.
export function routeSlug(route) {
  return route.replace('>', '-naar-')
}

// envFile: de module/.env van deze checkout, anders die van de hoofdcheckout
// (een extra werkmap van git heeft er zelf meestal geen). De inhoud gaat alleen de omgeving
// van een proces of een verzoekkop in, nooit een take, log of commit.
export function envFile() {
  const own = path.join(MODULE_DIR, '.env')
  if (existsSync(own)) return own
  const main = path.join(mainCheckout(), 'module', '.env')
  if (existsSync(main)) return main
  throw new Error('geen module/.env gevonden (hier of in de hoofdcheckout)')
}

export function readEnv() {
  return parseEnv(readFileSync(envFile(), 'utf8'))
}
