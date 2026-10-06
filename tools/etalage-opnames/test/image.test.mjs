import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { mkdtempSync, rmSync } from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { test } from 'node:test'
import { contentBox } from '../lib/image.mjs'

// Een canvas van 100×80 in de achtergrondkleur van de kaart, met een duidelijk
// vlak (de graaf) en een vlak dat nauwelijks afwijkt (ruis). Even maten: de
// kleurruimte van ffmpeg rondt een oneven vlak af.
function canvas(boxes) {
  const dir = mkdtempSync(path.join(os.tmpdir(), 'etalage-beeld-'))
  const file = path.join(dir, 'c.png')
  const draw = boxes.map(([x, y, w, h, c]) => `drawbox=x=${x}:y=${y}:w=${w}:h=${h}:color=${c}:t=fill`).join(',')
  execFileSync('ffmpeg', ['-v', 'error', '-f', 'lavfi', '-i', 'color=c=0xf4f6f5:s=100x80', '-vf', draw, '-frames:v', '1', file])
  return { file, done: () => rmSync(dir, { recursive: true, force: true }) }
}

test('bijsnijden tot wat op het canvas staat, met rand, en ruis telt niet', () => {
  const c = canvas([[30, 20, 10, 14, '0x3366cc'], [80, 70, 5, 5, '0xf2f4f3']])
  try {
    assert.deepEqual(contentBox(c.file, 100, 80, 5, 18), { x: 25, y: 15, width: 20, height: 24 })
  } finally {
    c.done()
  }
})

test('de rand blijft binnen het beeld, en een leeg canvas blijft heel', () => {
  const edge = canvas([[0, 70, 10, 10, '0x202020']])
  const empty = canvas([[10, 10, 2, 2, '0xf4f6f5']])
  try {
    assert.deepEqual(contentBox(edge.file, 100, 80, 8, 18), { x: 0, y: 62, width: 18, height: 18 })
    assert.deepEqual(contentBox(empty.file, 100, 80, 8, 18), { x: 0, y: 0, width: 100, height: 80 })
  } finally {
    edge.done()
    empty.done()
  }
})
