#!/usr/bin/env node
// Spreekt de vergaderuitspraken uit vragen.json uit met een bestaande
// spraaksynthese (ElevenLabs, premade stemmen, dezelfde als in het
// vergaderfragment van de uitlegvideo), zodat ze bij het opnemen door een
// synthetische microfoon het echte pad van de module in gaan: microfoon →
// spraakherkenning → vergadering.
//
//   node tools/etalage-opnames/speak.mjs            ontbrekende of gewijzigde uitspraken maken
//   node tools/etalage-opnames/speak.mjs --force    alles opnieuw
//
// Uitvoer: uitspraken/<id>-<taal>.mp3 en uitspraken/uitspraken.json (tekst,
// stem, model, duur, hash). Een uitspraak waarvan tekst en stem niet
// veranderden wordt niet opnieuw gemaakt. Dit is onze eigen, verzonnen invoer;
// wat de module ervan verstond staat per take in het transcript.
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { existsSync } from 'node:fs'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { parseArgs } from 'node:util'
import { LANGS, SPEAKER_VOICES, TOOL_DIR, TTS, readEnv } from './lib/config.mjs'

const { values: args } = parseArgs({ options: { force: { type: 'boolean', default: false }, vragen: { type: 'string', default: path.join(TOOL_DIR, 'vragen.json') } } })
const DIR = path.join(TOOL_DIR, 'uitspraken')
const MANIFEST = path.join(DIR, 'uitspraken.json')
const vragen = JSON.parse(await readFile(args.vragen, 'utf8'))
const manifest = existsSync(MANIFEST) ? JSON.parse(await readFile(MANIFEST, 'utf8')) : { provider: TTS.provider, model: TTS.model, lines: {} }
await mkdir(DIR, { recursive: true })
let key = null

for (const statement of vragen.meeting) {
  const voice = SPEAKER_VOICES[statement.speaker.en]
  if (!voice) throw new Error(`geen stem voor spreker ${statement.speaker.en}; voeg hem toe aan SPEAKER_VOICES in lib/config.mjs`)
  for (const lang of LANGS) {
    const name = `${statement.id}-${lang}`
    const file = path.join(DIR, `${name}.mp3`)
    const text = statement.text[lang]
    const known = manifest.lines[name]
    if (!args.force && known && known.text === text && known.voiceId === voice.voiceId && known.model === TTS.model && existsSync(file)) {
      console.log(`${name}: ongewijzigd`)
      continue
    }
    key ??= readEnv().PARAI_AUDIO_ELEVENLABS_API_KEY
    if (!key) throw new Error('module/.env heeft geen PARAI_AUDIO_ELEVENLABS_API_KEY')
    // Geen language_code: eleven_multilingual_v2 herkent de taal uit de tekst
    // (de API weigert taalhandhaving bij dit model).
    const res = await fetch(`https://api.elevenlabs.io/v1/text-to-speech/${voice.voiceId}?output_format=${TTS.format}`, {
      method: 'POST',
      headers: { 'xi-api-key': key, 'Content-Type': 'application/json', Accept: 'audio/mpeg' },
      body: JSON.stringify({ text, model_id: TTS.model }),
      signal: AbortSignal.timeout(60000),
    })
    if (!res.ok) throw new Error(`${name}: spraaksynthese gaf HTTP ${res.status}`)
    const mp3 = Buffer.from(await res.arrayBuffer())
    await writeFile(file, mp3)
    const seconds = Number(execFileSync('ffprobe', ['-v', 'error', '-show_entries', 'format=duration', '-of', 'csv=p=0', file], { encoding: 'utf8' }).trim())
    manifest.lines[name] = {
      statement: statement.id,
      lang,
      speaker: statement.speaker[lang],
      text,
      voice: voice.voice,
      voiceId: voice.voiceId,
      model: TTS.model,
      file: `${name}.mp3`,
      durationMs: Math.round(seconds * 1000),
      sha256: createHash('sha256').update(mp3).digest('hex'),
      madeAt: new Date().toISOString(),
    }
    console.log(`${name}: ${voice.voice}, ${seconds.toFixed(2)} s`)
  }
}
manifest.provider = TTS.provider
manifest.model = TTS.model
manifest.placeholder = vragen.placeholder === true
await writeFile(MANIFEST, JSON.stringify(manifest, null, 1) + '\n')
