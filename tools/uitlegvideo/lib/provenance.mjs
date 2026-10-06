// Herkomst van de server die opneemt of afspeelt: de broncommit van
// het proces dat op de poort luistert en de hash van zijn frontendbundle.
// Alleen voor de verantwoording in takes en captures; niets hangt ervan af.
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'

// appProvenance: de werkmap van het serverproces is de module-map van zijn
// checkout; daar staat de commit.
export async function appProvenance(base) {
  const out = {}
  try {
    const port = new URL(base).port
    const pid = execFileSync('lsof', ['-nP', `-iTCP:${port}`, '-sTCP:LISTEN', '-t'], { encoding: 'utf8' }).trim().split('\n')[0]
    const cwd = execFileSync('lsof', ['-a', '-p', pid, '-d', 'cwd', '-Fn'], { encoding: 'utf8' }).split('\n').find((l) => l.startsWith('n')).slice(1)
    out.commit = execFileSync('git', ['-C', cwd, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim()
    out.dirty = execFileSync('git', ['-C', cwd, 'status', '--porcelain', '--', '.'], { encoding: 'utf8' }).trim() !== ''
    out.branch = execFileSync('git', ['-C', cwd, 'rev-parse', '--abbrev-ref', 'HEAD'], { encoding: 'utf8' }).trim()
  } catch {
    out.commit = null
    out.note = 'broncommit niet vast te stellen (server niet lokaal of geen git)'
  }
  const res = await fetch(base + '/modules/duif/bundle.js')
  out.bundleSha256 = createHash('sha256').update(Buffer.from(await res.arrayBuffer())).digest('hex')
  return out
}
