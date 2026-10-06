#!/usr/bin/env node
// Kaart- en kopportret per duif uit de eigen Blender-renders van v4.
//
// v4 rendert elk portret met een eigen camera (elke plek een eigen, passend
// gekadreerde render), dus hier wordt niets uitgesneden, alleen geschaald en
// omgezet.
//
// Invoer per duif, vierkant en transparant: <duif>-kaart.png en <duif>-kop.png,
// met <duif> = biologisch, individualistisch of signalerend (de Engelse id's
// biological, individualist en nonverbal worden ook herkend). Uitvoer:
// stadsduif-v4-<duif>-kaart.webp (512 px) en stadsduif-v4-<duif>-kop.webp
// (256 px), de bestandsnamen uit het manifest.
//
// Gebruik: node tools/design-model/portraits_v4.mjs <renders-map> <uitvoermap>
// De uitvoermap is gewoonlijk <assets-root>/model-v4/web, de map waar
// prepare-meeting-media.mjs v4 leest. Vereist ffmpeg, ffprobe en
// cwebp op het pad.

import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync, rmSync, statSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

export const PIGEONS = [['biologisch', 'biological'], ['individualistisch', 'individualist'], ['signalerend', 'nonverbal']]
export const SIZES = { kaart: 512, kop: 256 }
// Pixelformaten van ffprobe met een echt alfakanaal; `gray` of `pal8` tellen niet.
const ALPHA_FORMATS = ['rgba', 'rgba64be', 'rgba64le', 'ya8', 'ya16be', 'ya16le']

function probe(file) {
  const out = JSON.parse(execFileSync('ffprobe', ['-v', 'error', '-select_streams', 'v:0', '-show_entries', 'stream=width,height,pix_fmt', '-of', 'json', file], { encoding: 'utf8' }))
  return out.streams[0]
}

export function convertPortraits(renders, destination) {
  mkdirSync(destination, { recursive: true })
  const written = []
  for (const [dutch, english] of PIGEONS) for (const [kind, size] of Object.entries(SIZES)) {
    const source = [dutch, english].map(name => path.join(renders, `${name}-${kind}.png`)).find(existsSync)
    if (!source) throw new Error(`Render ontbreekt: ${dutch}-${kind}.png in ${renders}`)
    const { width, height, pix_fmt: format } = probe(source)
    // Een eigen camera per render: vierkant aanleveren, want een uitsnede zou
    // precies de keuze zijn die bij de render hoort en niet hier.
    if (width !== height) throw new Error(`${path.basename(source)} is ${width}×${height}, niet vierkant`)
    if (width < size) throw new Error(`${path.basename(source)} is ${width} px; nooit opschalen naar ${size}`)
    if (!ALPHA_FORMATS.includes(format)) throw new Error(`${path.basename(source)} heeft geen alfakanaal (${format})`)
    const intermediate = path.join(destination, `stadsduif-v4-${dutch}-${kind}.scale.png`)
    const target = path.join(destination, `stadsduif-v4-${dutch}-${kind}.webp`)
    try {
      execFileSync('ffmpeg', ['-y', '-loglevel', 'error', '-i', source, '-vf', `scale=flags=lanczos:w=${size}:h=${size}`, '-frames:v', '1', intermediate])
      // Verliesgevende WebP met verliesloze alfa, zoals de v3-portretten: de
      // transparante rand blijft hard, het verenkleed weegt een fractie van PNG.
      execFileSync('cwebp', ['-quiet', '-q', '92', '-alpha_q', '100', '-m', '6', '-sharp_yuv', intermediate, '-o', target])
    } finally { rmSync(intermediate, { force: true }) }
    written.push({ file: path.basename(target), source: path.basename(source), width: size, height: size, bytes: statSync(target).size })
  }
  return written
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  if (!process.argv[2] || !process.argv[3]) {
    process.stderr.write('Gebruik: node tools/design-model/portraits_v4.mjs <renders-map> <uitvoermap>\n')
    process.exitCode = 1
  } else {
    const destination = path.resolve(process.argv[3])
    process.stdout.write(JSON.stringify(convertPortraits(path.resolve(process.argv[2]), destination), null, 2) + '\n')
  }
}
