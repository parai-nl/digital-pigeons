#!/usr/bin/env node
// Bouwt site/etalage/data/opnames.json uit de gekozen takes (keuze.json), de
// vragen (vragen.json) en de feiten (facts.json), en zet de audio klaar: de
// stem van elke gesproken reactie als mp3 (mono, 64 kbit/s, uit de take) en de drie
// veldgeluiden van de module (CC0); hun herkomst staat in HERKOMST.md. Geen
// modelaanroepen.
//
//   node tools/etalage-opnames/build.mjs
//
// Weigert als een item geen bruikbare take heeft, als een vergelijking niet
// voortbouwt op het gekozen brongesprek, of als een take een ander model meldt
// dan het verplichte model (MODEL in lib/config.mjs).
import { createHash } from 'node:crypto'
import { existsSync, mkdirSync, readdirSync, readFileSync, unlinkSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { SITE_DIR, TOOL_DIR } from './lib/config.mjs'
import { compile } from './lib/build.mjs'
import { KOER_DIR, STEM_DIR } from './lib/map.mjs'
import { MODULE_MEDIA, bandFiles, fieldSounds } from './lib/media.mjs'
import { loadTake } from './lib/take.mjs'

const read = (file) => JSON.parse(readFileSync(path.join(TOOL_DIR, file), 'utf8'))
const vragen = read('vragen.json')
const keuze = read('keuze.json')
const facts = read('facts.json').facts
const takesById = new Map()
for (const { take } of Object.values(keuze.items)) if (!takesById.has(take) && existsSync(path.join(TOOL_DIR, 'takes', take, 'take.json'))) takesById.set(take, await loadTake(take))
const sounds = fieldSounds()
const { opnames, voices, sounds: usedSounds, warnings } = compile({ vragen, keuze, takesById, facts, bands: bandFiles(sounds) })
for (const w of warnings) console.log(`let op: ${w}`)

// De stemmen: de take bewaart de stem al als mp3 in de vorm van de pagina
// (lib/meeting-take.mjs); hier wordt dat bestand ongewijzigd gekopieerd.
const stemDir = path.join(SITE_DIR, STEM_DIR)
mkdirSync(stemDir, { recursive: true })
const wanted = new Set()
for (const v of voices) {
  const target = path.join(SITE_DIR, v.target)
  wanted.add(path.basename(target))
  const bytes = readFileSync(path.join(v.dir, v.file))
  if (!existsSync(target) || !readFileSync(target).equals(bytes)) writeFileSync(target, bytes)
}
for (const name of readdirSync(stemDir)) if (name.endsWith('.mp3') && !wanted.has(name)) unlinkSync(path.join(stemDir, name))

// De veldgeluiden: alle drie, gecontroleerd tegen de hash in de catalogus
// van de module. De herkomst staat in tools/etalage-opnames/HERKOMST.md (onder
// site/ alleen json, mp3 en webp); elk bestand moet daar met zijn hash staan.
const koerDir = path.join(SITE_DIR, KOER_DIR)
mkdirSync(koerDir, { recursive: true })
const herkomst = readFileSync(path.join(TOOL_DIR, 'HERKOMST.md'), 'utf8')
for (const s of Object.values(sounds)) {
  const bytes = readFileSync(path.join(MODULE_MEDIA, s.file))
  if (createHash('sha256').update(bytes).digest('hex') !== s.sha256) throw new Error(`${s.file} wijkt af van de hash in de mediacatalogus`)
  if (!herkomst.includes(s.file) || !herkomst.includes(s.sha256)) throw new Error(`${s.file} staat niet met hash ${s.sha256} in HERKOMST.md`)
  writeFileSync(path.join(koerDir, s.file), bytes)
}
for (const used of usedSounds) if (!existsSync(path.join(SITE_DIR, used))) throw new Error(`${used} staat in opnames.json maar niet in ${KOER_DIR}`)
for (const name of readdirSync(koerDir)) if (!name.endsWith('.mp3')) unlinkSync(path.join(koerDir, name))

const file = path.join(SITE_DIR, 'etalage/data/opnames.json')
mkdirSync(path.dirname(file), { recursive: true })
writeFileSync(file, JSON.stringify(opnames, null, 1) + '\n')
const size = (p) => readFileSync(p).length
console.log(`${path.relative(process.cwd(), file)}: ${Math.round(size(file) / 1024)} KB, ${opnames.recording.takes} takes, ${opnames.recording.model}, ${opnames.recording.recordedFrom} – ${opnames.recording.recordedTo}${opnames.recording.placeholder ? ', plaatshouders' : ''}`)
console.log(`stemmen: ${voices.length} mp3 (${Math.round(voices.reduce((n, v) => n + size(path.join(SITE_DIR, v.target)), 0) / 1024)} KB); veldgeluiden: ${Object.keys(sounds).length}`)

