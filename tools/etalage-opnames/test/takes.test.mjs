// Controles over wat er werkelijk in git staat: de takes, keuze.json en het
// gebouwde opnames.json. Zonder takes (een verse checkout zonder opnames) is
// er niets te controleren.
import assert from 'node:assert/strict'
import { existsSync, lstatSync, readdirSync, readFileSync } from 'node:fs'
import path from 'node:path'
import { test } from 'node:test'
import { compile } from '../lib/build.mjs'
import { choiceProblems } from '../lib/choose.mjs'
import { SITE_DIR, TOOL_DIR, endpointOrigin, modelAllowed, readEnv } from '../lib/config.mjs'
import { mediaMs } from '../lib/meeting-take.mjs'
import { bandFiles } from '../lib/media.mjs'
import { envSecrets, listTakes, loadTake, secretProblems } from '../lib/take.mjs'

const ids = await listTakes()
const takes = await Promise.all(ids.map((id) => loadTake(id)))
let secrets = []
try { secrets = envSecrets(readEnv()) } catch { /* geen module/.env: alleen de koppen */ }

test('geen take draagt cookies, autorisatie of een waarde uit module/.env', { skip: !takes.length && 'geen takes' }, () => {
  for (const take of takes) assert.deepEqual(secretProblems(take, secrets), [], take.id)
})

test('geen take bevat een absoluut lokaal pad', { skip: !takes.length && 'geen takes' }, () => {
  for (const take of takes) assert.doesNotMatch(JSON.stringify({ ...take, dir: undefined }), /"(\/Users\/|\/home\/|[A-Z]:\\\\)/, take.id)
})

// Het model is dat van de module op het moment van opnemen: de takes van
// 23 september 2026 zijn van vertex_ai/gemini-3.5-flash (vóór de modelwissel),
// nieuwe van het huidige model.
test('elke take noemt het model, het endpoint en de module-commit van de server', { skip: !takes.length && 'geen takes' }, () => {
  for (const take of takes) {
    assert.ok(modelAllowed(take.server.modelPolicy.model, take.recordedAt), `${take.id}: ${take.server.modelPolicy.model} op ${take.recordedAt}`)
    assert.equal(take.server.modelPolicy.detectionModel, take.server.modelPolicy.model, take.id)
    assert.equal(endpointOrigin(take.server.modelPolicy.endpoint), take.server.modelPolicy.endpoint, take.id)
    assert.match(take.server.module.commit, /^[0-9a-f]{40}$/, take.id)
    assert.ok(take.recordedAt && take.finishedAt, take.id)
    if (take.kind === 'chat' && !take.failure) assert.equal(take.provenance.model, take.server.modelPolicy.model, take.id)
  }
})

test('de stem in een take is er en duurt zo lang als de PCM die de module stuurde', { skip: !takes.some((t) => t.voice?.length) && 'geen stemmen' }, () => {
  for (const take of takes) {
    for (const v of take.voice ?? []) {
      const file = path.join(take.dir, v.file)
      assert.ok(existsSync(file), `${take.id}/${v.file}`)
      // mp3 legt een paar frames stilte aan het begin en eind toe.
      if (v.ms) assert.ok(Math.abs(mediaMs(file) - v.ms) < 150, `${take.id}: ${mediaMs(file)} ms tegen ${v.ms} ms`)
    }
  }
})

const keuzeFile = path.join(TOOL_DIR, 'keuze.json')
const opnamesFile = path.join(SITE_DIR, 'etalage/data/opnames.json')

test('keuze.json wijst voor elk item een bruikbare, consistente take aan', { skip: !existsSync(keuzeFile) && 'geen keuze.json' }, () => {
  const vragen = JSON.parse(readFileSync(path.join(TOOL_DIR, 'vragen.json'), 'utf8'))
  const keuze = JSON.parse(readFileSync(keuzeFile, 'utf8'))
  assert.deepEqual(choiceProblems(keuze, new Map(takes.map((t) => [t.id, t])), vragen, { bands: bandFiles() }), [])
})

test('opnames.json is precies wat build.mjs uit de keuze maakt, en alle media bestaan', { skip: !existsSync(opnamesFile) && 'geen opnames.json' }, () => {
  const read = (file) => JSON.parse(readFileSync(path.join(TOOL_DIR, file), 'utf8'))
  const { opnames } = compile({ vragen: read('vragen.json'), keuze: read('keuze.json'), takesById: new Map(takes.map((t) => [t.id, t])), facts: read('facts.json').facts, bands: bandFiles() })
  const built = JSON.parse(readFileSync(opnamesFile, 'utf8'))
  assert.deepEqual(built, opnames)
  for (const s of built.meeting.statements) {
    for (const byPigeon of Object.values(s.reactions)) {
      for (const r of Object.values(byPigeon)) {
        for (const media of [r.audio, r.sound].filter(Boolean)) assert.ok(existsSync(path.join(SITE_DIR, media)), media)
      }
    }
  }
})

// De publieke map (site/) krijgt van deze stroom alleen json, mp3 en webp;
// geen dotfiles, geen symlinks en geen .md (eisen van de hosting). De
// herkomst staat in tools/etalage-opnames/HERKOMST.md. Alleen de paden van de
// opnamestroom.
test('onder site/ alleen toegestane bestanden van de opnamestroom', () => {
  const present = (p) => { try { lstatSync(p); return true } catch { return false } }
  // De startmappen zelf en de mappen erboven mogen ook geen symlink zijn.
  for (const dir of ['', 'etalage', 'etalage/media', 'etalage/data', 'etalage/media/geluid', 'etalage/media/beeld'].map((p) => path.join(SITE_DIR, p))) {
    if (present(dir)) assert.ok(!lstatSync(dir).isSymbolicLink(), `symlink: ${dir}`)
  }
  const roots = ['etalage/data', 'etalage/media/geluid', 'etalage/media/beeld'].map((p) => path.join(SITE_DIR, p)).filter(present)
  const walk = (dir) => readdirSync(dir).flatMap((name) => {
    const full = path.join(dir, name)
    const stat = lstatSync(full)
    assert.ok(!stat.isSymbolicLink(), `symlink: ${full}`)
    assert.ok(!name.startsWith('.'), `dotfile: ${full}`)
    return stat.isDirectory() ? walk(full) : [full]
  })
  for (const file of roots.flatMap(walk)) {
    const rel = path.relative(SITE_DIR, file)
    if (rel.startsWith('etalage/media/beeld/') && !path.basename(rel).startsWith('kennisgraaf-')) continue
    assert.match(rel, /\.(json|mp3|webp)$/, `niet toegestaan: ${rel}`)
  }
})

const factsFile = path.join(TOOL_DIR, 'facts.json')
test('facts.json: werken zijn de bronknopen min de gedeelde', { skip: !existsSync(factsFile) && 'geen facts.json' }, () => {
  const record = JSON.parse(readFileSync(factsFile, 'utf8'))
  const shared = (record.sharedWorks ?? []).reduce((n, w) => n + w.sources.length - 1, 0)
  assert.equal(record.facts.works, record.facts.sources - shared)
})
