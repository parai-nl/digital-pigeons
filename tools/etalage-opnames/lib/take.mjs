// Het takeformaat van de etalage-opnames. Een take is één live opname van één
// item (een chatbeurt of een verse vergadering) tegen de echte module: elk
// verzoek met zijn antwoord, de serverstand (model, commit, kennisbank) en het
// tijdstip. Elke run schrijft zijn take, ook als hij mislukt, en een take wordt
// niet weggegooid; keuze.json wijst er per item één aan, en build.mjs maakt
// daar opnames.json van. Enkele proefruns vóór de eerste volledige ronde zijn
// niet bewaard (README, "Proefruns die niet bewaard zijn").
import { mkdir, readdir, readFile, writeFile } from 'node:fs/promises'
import { existsSync } from 'node:fs'
import path from 'node:path'
import { TAKES_DIR } from './config.mjs'

export const TAKE_FORMAT = 'duiven-etalage-take/1'

export async function saveTake(take, files = []) {
  const dir = path.join(TAKES_DIR, take.id)
  await mkdir(dir, { recursive: true })
  for (const f of files) await writeFile(path.join(dir, f.name), f.data)
  await writeFile(path.join(dir, 'take.json'), JSON.stringify(take, null, 1) + '\n')
  return dir
}

export async function loadTake(id, root = TAKES_DIR) {
  const dir = path.join(root, id)
  const take = JSON.parse(await readFile(path.join(dir, 'take.json'), 'utf8'))
  if (take.format !== TAKE_FORMAT) throw new Error(`${id}: onbekend takeformaat ${take.format}`)
  if (take.id !== id) throw new Error(`${id}: de take noemt zichzelf ${take.id}`)
  take.dir = dir
  return take
}

export async function listTakes(root = TAKES_DIR) {
  if (!existsSync(root)) return []
  const names = (await readdir(root, { withFileTypes: true })).filter((d) => d.isDirectory()).map((d) => d.name)
  return names.filter((n) => existsSync(path.join(root, n, 'take.json'))).sort()
}

// itemKey is de sleutel van een item in keuze.json, gelijk aan de plek in
// opnames.json: chat/<vraag>/<taal>/<route> of meeting/<uitspraak>/<taal>/<duif>.
export function itemKey(take) {
  const i = take.item
  return take.kind === 'chat' ? `chat/${i.question}/${i.lang}/${i.route}` : `meeting/${i.statement}/${i.lang}/${i.pigeon}`
}

// Wat nooit in een take mag: cookies, autorisatie, sleutels. De opname laat ze
// al weg; dit is de controle achteraf, voor het opslaan en in de tests over
// alle takes. `secrets` zijn de echte waarden uit module/.env (als die er is):
// geen enkele mag letterlijk in de take staan.
export function secretProblems(take, secrets = []) {
  const problems = []
  for (const x of take.exchanges ?? []) {
    for (const [name] of x.headers ?? []) if (!(name === 'content-type' || name.startsWith('x-duif-'))) problems.push(`${x.method} ${x.path}: kop ${name}`)
  }
  const text = JSON.stringify({ ...take, dir: undefined })
  for (const value of secrets) if (value.length >= 8 && text.includes(value)) problems.push('een waarde uit module/.env staat in de take')
  return problems
}

// envSecrets leest de geheime waarden uit een .env-tekst: alles met KEY,
// TOKEN, SECRET of PASSWORD in de naam, en het wachtwoord uit een database-URL.
export function envSecrets(env) {
  const out = []
  for (const [name, value] of Object.entries(env)) {
    if (!value) continue
    if (/KEY|TOKEN|SECRET|PASSWORD/.test(name)) out.push(value)
    if (/_URL$/.test(name)) {
      try { const u = new URL(value); if (u.password) out.push(decodeURIComponent(u.password)) } catch { /* geen URL */ }
    }
  }
  return out
}
