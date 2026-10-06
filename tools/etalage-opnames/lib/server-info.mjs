// Wat een take over de server vastlegt, en de weigering vooraf. Het model komt
// uit de server zelf (GET /duiven → modelPolicy), niet uit deze code; de
// module-commit uit de bouwstand die server.mjs bij het bouwen schreef, en
// alleen als het proces op de poort ook dat gebouwde proces is.
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { existsSync, readFileSync } from 'node:fs'
import path from 'node:path'
import { DATABASE, FORBIDDEN_PORTS, MODEL, MODULE_DIR, REPO_DIR, SOURCE_DATABASE, endpointOrigin, readEnv } from './config.mjs'

const STATE = path.join(MODULE_DIR, 'bin', 'etalage-server.json')

// policyProblem zegt wat er mis is met het modelbeleid, of null als het klopt.
// `endpoint` is de oorsprong van PARAI_LLM_BASE_URL waarmee de server draait.
export function policyProblem(policy, endpoint) {
  if (!endpoint) return 'PARAI_LLM_BASE_URL in module/.env is geen HTTPS-adres'
  if (!policy || typeof policy !== 'object') return 'de server meldt geen modelbeleid'
  if (policy.model !== MODEL) return `chatmodel is ${policy.model}, niet ${MODEL}`
  if (policy.detectionModel !== MODEL) return `detectiemodel is ${policy.detectionModel}, niet ${MODEL}`
  if (policy.endpoint !== endpoint) return `endpoint is ${policy.endpoint}, niet ${endpoint}`
  return null
}

// checkServer weigert vóór de eerste modelaanroep: een verboden poort, een
// server die niet door server.mjs gebouwd is, of een ander model dan het verplichte.
export async function checkServer(base) {
  const port = Number(new URL(base).port)
  if (FORBIDDEN_PORTS.has(port)) throw new Error(`poort ${port} is van een andere server; daar wordt niet tegen opgenomen`)
  const state = existsSync(STATE) ? JSON.parse(readFileSync(STATE, 'utf8')) : null
  const listening = listeningPid(port)
  if (!state?.pid || String(state.pid) !== listening) throw new Error(`het proces op poort ${port} is niet de opnameserver van server.mjs; start die eerst (node tools/etalage-opnames/server.mjs start)`)
  const duiven = await getJSON(base, '/api/duif/v1/duiven')
  const problem = policyProblem(duiven.modelPolicy, endpointOrigin(readEnv().PARAI_LLM_BASE_URL))
  if (problem) throw new Error(`de server op ${base} gebruikt niet het verplichte model: ${problem}`)
  const knowledge = await getJSON(base, '/api/duif/v1/knowledge/status')
  if (knowledge.state !== 'ready') throw new Error(`de kennisbank is niet klaar (${knowledge.state})`)
  const bundle = Buffer.from(await (await fetch(base + '/modules/duif/bundle.js')).arrayBuffer())
  return {
    base,
    modelPolicy: duiven.modelPolicy,
    knowledge: { kbId: knowledge.kbId, bundleSchema: knowledge.bundleSchema, languages: knowledge.languages, importedAt: knowledge.importedAt, counts: knowledge.counts },
    database: { name: DATABASE, copyOf: SOURCE_DATABASE },
    module: moduleInfo(state.build, createHash('sha256').update(bundle).digest('hex')),
    tool: toolInfo(),
    pigeons: (duiven.duiven ?? []).map((d) => ({ id: d.id, engine: d.engine, chat: d.chat, meeting: d.meeting, autonomousDefault: d.autonomousDefault })),
  }
}

async function getJSON(base, route) {
  const res = await fetch(base + route, { signal: AbortSignal.timeout(10000) })
  if (!res.ok) throw new Error(`GET ${route} gaf ${res.status}`)
  return res.json()
}

function listeningPid(port) {
  try {
    return execFileSync('lsof', ['-nP', `-iTCP:${port}`, '-sTCP:LISTEN', '-t'], { encoding: 'utf8' }).trim().split('\n')[0]
  } catch {
    return ''
  }
}

// sameBuild zegt of twee serverstanden dezelfde build zijn: dezelfde commit,
// dezelfde bundle en hetzelfde bouwmoment.
export function sameBuild(a, b) {
  return a.module.commit === b.module.commit && a.module.bundleSha256 === b.module.bundleSha256 && a.module.builtAt === b.module.builtAt
}

// moduleInfo: de bouwstand van de server zoals een take hem vastlegt. Alleen
// deze velden; een lokaal pad (zoals dat van de parai-core-checkout in een
// oudere bouwstand) komt nooit in een take.
export function moduleInfo(build, bundleSha256) {
  const core = build.paraiCore ? { commit: build.paraiCore.commit, dirty: build.paraiCore.dirty } : undefined
  return { commit: build.commit, moduleTree: build.moduleTree, dirty: build.dirty, ...(core ? { paraiCore: core } : {}), tags: build.tags, builtAt: build.builtAt, binary: build.binary, bundleSha256 }
}

// toolInfo: de commit van dit gereedschap zelf, en of de code ervan (scripts,
// lib/, queries) afwijkt van die commit. Takes, keuzes en uitspraken tellen
// niet mee: die verandert een opname zelf.
export function toolInfo() {
  const git = (...args) => execFileSync('git', ['-C', REPO_DIR, ...args], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim()
  return { commit: git('rev-parse', 'HEAD'), dirty: git('status', '--porcelain', '--', ...TOOL_CODE) !== '' }
}

// codePathspecs maakt git-pathspecs van paden: een map (eindigt op /) krijgt
// alles eronder, een bestand of een glob blijft wat het is. Een /** achter
// een bestand matcht nooit.
export function codePathspecs(paths) {
  return paths.map((p) => `:(glob)${p.endsWith('/') ? `${p}**` : p}`)
}

// De code van het gereedschap: de scripts, lib/ en de SQL-queries.
export const TOOL_CODE = codePathspecs(['tools/etalage-opnames/*.mjs', 'tools/etalage-opnames/lib/', 'tools/etalage-opnames/*.sql'])
