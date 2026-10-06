#!/usr/bin/env node
// De opnameserver van de etalage: de echte module, gebouwd uit deze
// checkout met skipauth, op 127.0.0.1:8094, tegen een eigen kopie van de
// kennisbank. Opnemen maakt gesprekken aan in het account; die komen in die
// kopie terecht en nooit in de database waarvan zij gemaakt is (DATABASE en
// SOURCE_DATABASE in lib/config.mjs).
//
//   node tools/etalage-opnames/server.mjs db [--replace]   de eigen kopie maken
//   node tools/etalage-opnames/server.mjs start            bouwen (frontend, schil, binary) en starten
//   node tools/etalage-opnames/server.mjs status
//   node tools/etalage-opnames/server.mjs stop
//
// Het stopt alleen een proces dat het zelf startte en weigert een poort die al iets anders serveert.
// Geheimen uit module/.env gaan alleen de omgeving van het serverproces in;
// ze worden nergens geprint of weggeschreven.
import { execFileSync, spawn } from 'node:child_process'
import { closeSync, cpSync, existsSync, mkdirSync, openSync, readFileSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { BASE, DATABASE, MODEL, MODULE_DIR, PG_CONTAINER, PORT, REPO_DIR, SOURCE_DATABASE, endpointOrigin, mainCheckout, readEnv } from './lib/config.mjs'

const BIN = path.join(MODULE_DIR, 'bin')
const BINARY = path.join(BIN, 'duif-etalage')
const STATE = path.join(BIN, 'etalage-server.json')
const LOG = path.join(BIN, 'etalage-runtime.log')
const action = process.argv[2] || 'status'

const run = (cmd, args, options = {}) => {
  try {
    return execFileSync(cmd, args, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], ...options })
  } catch (err) {
    // De argumenten kunnen een verbindingsadres dragen: alleen het commando.
    throw new Error(`${cmd} ${args[0] ?? ''} faalde (exit ${err.status ?? '?'}): ${String(err.stderr ?? '').split('\n').filter(Boolean).slice(-3).join(' | ')}`)
  }
}
const psql = (db, sql) => run('docker', ['exec', PG_CONTAINER, 'psql', '-U', 'parai', '-d', db, '-Atc', sql]).trim()
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const readState = () => (existsSync(STATE) ? JSON.parse(readFileSync(STATE, 'utf8')) : null)
const ours = (pid) => {
  if (!pid) return false
  try { return run('ps', ['-p', String(pid), '-o', 'command=']).trim().startsWith(BINARY) } catch { return false }
}
const healthy = async () => {
  try { return (await fetch(`${BASE}/api/duif/v1/health`, { signal: AbortSignal.timeout(2000) })).ok } catch { return false }
}

function databaseExists(name) {
  return psql('postgres', `select 1 from pg_database where datname='${name}'`) === '1'
}

if (action === 'db') {
  // Een kopie via pg_dump/pg_restore binnen de container: CREATE DATABASE …
  // TEMPLATE kan niet zolang een andere server met de bron verbonden is. De
  // dump leest de bron alleen.
  if (databaseExists(DATABASE)) {
    if (!process.argv.includes('--replace')) throw new Error(`${DATABASE} bestaat al; --replace gooit hem weg en kopieert opnieuw`)
    if (ours(readState()?.pid)) throw new Error('stop eerst de opnameserver')
    psql('postgres', `drop database ${DATABASE}`)
  }
  const dump = `/tmp/etalage-${SOURCE_DATABASE}.dump`
  run('docker', ['exec', PG_CONTAINER, 'sh', '-c', `pg_dump -U parai -Fc -f ${dump} ${SOURCE_DATABASE} && createdb -U parai ${DATABASE} && pg_restore -U parai -d ${DATABASE} --no-owner ${dump}; rc=$?; rm -f ${dump}; exit $rc`], { timeout: 600000 })
  console.log(`${DATABASE} gemaakt als kopie van ${SOURCE_DATABASE}`)
} else if (action === 'status') {
  const state = readState()
  console.log(JSON.stringify({ running: ours(state?.pid), healthy: await healthy(), url: `${BASE}/duif`, database: DATABASE, build: state?.build ?? null }, null, 1))
} else if (action === 'stop') {
  const state = readState()
  if (!ours(state?.pid)) {
    console.log('geen eigen opnameserver actief')
  } else {
    process.kill(state.pid, 'SIGINT')
    for (let i = 0; i < 100 && ours(state.pid); i++) await sleep(100)
    if (ours(state.pid)) throw new Error('de opnameserver sluit nog af; probeer het zo opnieuw')
    writeFileSync(STATE, JSON.stringify({ ...state, pid: null }, null, 1), { mode: 0o600 })
    console.log(`opnameserver gestopt; ${DATABASE} blijft bestaan`)
  }
} else if (action === 'start') {
  const state = readState()
  if (ours(state?.pid)) { console.log(`opnameserver draait al op ${BASE}/duif`); process.exit(0) }
  if (await healthy()) throw new Error(`poort ${PORT} serveert al iets anders; dat wordt niet vervangen`)
  if (!databaseExists(DATABASE)) throw new Error(`${DATABASE} bestaat niet; maak hem eerst met: node tools/etalage-opnames/server.mjs db`)

  // De private afhankelijkheden: siblings naast de hoofdcheckout, tenzij de
  // omgeving iets anders zegt (README van de module).
  const siblings = path.dirname(mainCheckout())
  const uiDir = process.env.PARAI_UI_DIR || path.join(siblings, 'parai-ui')
  const graphDir = process.env.PARAI_GRAPH_DIR || path.join(siblings, 'parai-graph')
  const coreBuild = path.join(process.env.PARAI_CORE_DIR || path.join(siblings, 'parai-core'), 'frontend/build')
  if (!existsSync(path.join(coreBuild, 'index.html'))) throw new Error(`geen gebouwde Parai-schil in ${coreBuild}`)

  const commit = run('git', ['-C', REPO_DIR, 'rev-parse', 'HEAD']).trim()
  const tree = run('git', ['-C', REPO_DIR, 'rev-parse', 'HEAD:module']).trim()
  const dirty = run('git', ['-C', REPO_DIR, 'status', '--porcelain', '--', 'module']).trim() !== ''
  if (dirty && !process.argv.includes('--allow-dirty')) throw new Error('module/ heeft niet-gecommitte wijzigingen; een opname hoort bij een commit (--allow-dirty om toch te bouwen)')

  console.log('frontend bouwen …')
  const frontend = path.join(MODULE_DIR, 'duif/frontend')
  const frontendEnv = { ...process.env, PARAI_UI_DIR: uiDir, PARAI_GRAPH_DIR: graphDir }
  run('bun', ['install', '--silent', '--frozen-lockfile'], { cwd: frontend, env: frontendEnv, timeout: 600000 })
  run('bun', ['run', 'build'], { cwd: frontend, env: frontendEnv, timeout: 600000 })
  mkdirSync(path.join(MODULE_DIR, 'static'), { recursive: true })
  cpSync(coreBuild, path.join(MODULE_DIR, 'static'), { recursive: true, filter: (file) => !file.endsWith('.map') })
  console.log('binary bouwen (skipauth) …')
  mkdirSync(BIN, { recursive: true, mode: 0o700 })
  // Welke parai-core. Standaard de pin uit go.mod (GOWORK=off): dat is de
  // core die de module vastlegt, dezelfde die make
  // verify en make e2e controleren. Alleen wie bewust een andere core wil
  // opnemen zet PARAI_CORE_SRC; dan komt er een eigen go.work in de
  // genegeerde bin/-map. Een lokale go.work in module/ telt nooit mee.
  // Geen lokaal pad in de vastgelegde core: dat hoort niet in een take.
  const coreSrc = process.env.PARAI_CORE_SRC
  let core
  let gowork = 'off'
  if (coreSrc) {
    if (!existsSync(path.join(coreSrc, 'go.mod'))) throw new Error(`geen parai-core-broncode in ${coreSrc} (PARAI_CORE_SRC)`)
    gowork = path.join(BIN, 'etalage.go.work')
    writeFileSync(gowork, `go 1.26.5\n\nuse ${MODULE_DIR}\n\nreplace github.com/parai-nl/parai-core => ${coreSrc}\n`)
    core = {
      commit: run('git', ['-C', coreSrc, 'rev-parse', 'HEAD']).trim(),
      dirty: run('git', ['-C', coreSrc, 'status', '--porcelain']).trim() !== '',
    }
  } else {
    // De pseudo-versie draagt maar twaalf tekens van de commit; de volledige
    // hash staat in het .info-bestand van de modulecache.
    const pin = JSON.parse(run('go', ['list', '-m', '-json', 'github.com/parai-nl/parai-core'], { cwd: MODULE_DIR, env: { ...process.env, GOWORK: 'off' } }))
    const info = path.join(run('go', ['env', 'GOMODCACHE']).trim(), 'cache/download/github.com/parai-nl/parai-core/@v', `${pin.Version}.info`)
    const hash = existsSync(info) ? JSON.parse(readFileSync(info, 'utf8')).Origin?.Hash : undefined
    core = { commit: hash || pin.Version.split('-').at(-1), dirty: false }
  }
  run('go', ['build', '-tags', 'skipauth', '-o', BINARY, '.'], { cwd: MODULE_DIR, env: { ...process.env, GOWORK: gowork }, timeout: 600000 })

  const config = readEnv()
  // Thuismapvariabelen (HOME, *_HOME) uit een lokale .env horen niet in het
  // serverproces; dat houdt zijn eigen omgeving.
  for (const name of Object.keys(config)) if (/(^|_)home$/i.test(name)) delete config[name]
  if (!config.PARAI_DB_URL) throw new Error('module/.env heeft geen PARAI_DB_URL')
  const db = new URL(config.PARAI_DB_URL)
  db.pathname = `/${DATABASE}`
  const env = { ...process.env, ...config, PARAI_DB_URL: db.href, PORT: String(PORT), DUIF_BIND_HOST: '127.0.0.1', DUIF_IMPORT_KB: '', DUIF_IMPORT_KB_FORCE: '0' }
  const log = openSync(LOG, 'a', 0o600)
  const child = spawn(BINARY, [], { cwd: MODULE_DIR, env, detached: true, stdio: ['ignore', log, log] })
  closeSync(log)
  child.unref()
  const build = { commit, moduleTree: tree, dirty, paraiCore: core, tags: ['skipauth'], builtAt: new Date().toISOString(), binary: path.relative(REPO_DIR, BINARY) }
  writeFileSync(STATE, JSON.stringify({ pid: child.pid, port: PORT, database: DATABASE, build }, null, 1), { mode: 0o600 })
  let up = false
  for (let i = 0; i < 120 && !up; i++) { up = await healthy(); if (!up) { if (!ours(child.pid)) break; await sleep(500) } }
  if (!up) throw new Error(`de opnameserver kwam niet op; zie ${path.relative(REPO_DIR, LOG)}`)
  const policy = (await (await fetch(`${BASE}/api/duif/v1/duiven`)).json()).modelPolicy
  const endpoint = endpointOrigin(readEnv().PARAI_LLM_BASE_URL)
  const ok = Boolean(endpoint) && policy?.model === MODEL && policy?.detectionModel === MODEL && policy?.endpoint === endpoint
  console.log(`opnameserver op ${BASE}/duif (database ${DATABASE}, commit ${commit.slice(0, 10)})`)
  console.log(`modelbeleid van de server: ${JSON.stringify(policy)} ${ok ? '— klopt met het verplichte model' : '— KLOPT NIET met het verplichte model; niet opnemen'}`)
  if (!ok) process.exitCode = 1
} else {
  throw new Error('gebruik: node tools/etalage-opnames/server.mjs db|start|status|stop')
}
