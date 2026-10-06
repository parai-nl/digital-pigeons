#!/usr/bin/env node
// Vult keuze.json aan: per item één take, met een reden. Een item zonder
// keuze (of met een automatische keuze) krijgt de nieuwste bruikbare take;
// een handmatige keuze (zonder "auto": true) blijft altijd staan.
//
//   node tools/etalage-opnames/keuze.mjs           aanvullen en de stand tonen
//   node tools/etalage-opnames/keuze.mjs --check   alleen controleren (exit 1 bij problemen)
//
// Handmatig kiezen: zet bij het item "take" op de gewenste take-id, schrijf
// een eigen "reason" en haal "auto" weg.
import { existsSync } from 'node:fs'
import { readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { parseArgs } from 'node:util'
import { TOOL_DIR } from './lib/config.mjs'
import { autoChoose, choiceProblems, expectedItems, takeProblems } from './lib/choose.mjs'
import { bandFiles } from './lib/media.mjs'
import { itemKey, listTakes, loadTake } from './lib/take.mjs'

const { values: args } = parseArgs({ options: { check: { type: 'boolean', default: false } } })
const FILE = path.join(TOOL_DIR, 'keuze.json')
const vragen = JSON.parse(await readFile(path.join(TOOL_DIR, 'vragen.json'), 'utf8'))
const takes = await Promise.all((await listTakes()).map((id) => loadTake(id)))
const bands = bandFiles()
let keuze = existsSync(FILE) ? JSON.parse(await readFile(FILE, 'utf8')) : { note: '', items: {} }
keuze.note = 'Per item één take voor opnames.json, met de reden. "auto": true is door keuze.mjs gekozen (de nieuwste bruikbare take) en mag opnieuw bepaald worden; haal "auto" weg om een keuze vast te zetten. Elke run schrijft zijn take in takes/, ook een mislukte, en takes worden niet weggegooid; alleen enkele proefruns vóór de eerste volledige ronde zijn niet bewaard, zie README, "Proefruns die niet bewaard zijn".'
if (!args.check) {
  const result = autoChoose(keuze, takes, vragen, { bands })
  keuze = result.keuze
  await writeFile(FILE, JSON.stringify(keuze, null, 1) + '\n')
  if (result.changes.length) console.log(`keuze gewijzigd voor ${result.changes.length} items`)
}
const byId = new Map(takes.map((t) => [t.id, t]))
const counts = new Map()
for (const t of takes) counts.set(itemKey(t), (counts.get(itemKey(t)) ?? 0) + 1)
for (const key of expectedItems(vragen)) {
  const c = keuze.items[key]
  const take = c && byId.get(c.take)
  console.log(`${key.padEnd(46)} ${String(counts.get(key) ?? 0).padStart(2)} takes  ${take ? `${take.id}${c.auto ? '' : ' (vast)'}` : '— geen keuze'}`)
}
for (const t of takes) {
  const p = takeProblems(t, vragen, { bands })
  if (p.length) console.log(`  niet bruikbaar: ${t.id}: ${p.join('; ')}`)
}
const problems = choiceProblems(keuze, byId, vragen, { bands })
if (problems.length) {
  console.log(`\n${problems.length} problemen:\n  ${problems.join('\n  ')}`)
  process.exitCode = 1
} else {
  console.log('\nelk item heeft een bruikbare, consistente take')
}
