// Lichte opmaak in duif-antwoorden, zonder HTML. De pratende duiven schrijven
// af en toe **vet** of *cursief* en een kopje als `## Titel`; de chat rendert
// tekst als losse segmenten rond de citatiebadges, dus een markdown-renderer
// past daar niet in. Deze splitser levert runs op die de component als
// <strong>/<em> zet — alles blijft tekst, niets wordt als HTML geïnterpreteerd.
export type InlineRun = { kind: 'text' | 'strong' | 'em'; value: string }

const STRONG = /\*\*([^*\n]+?)\*\*/
const EM = /(?<![*\w])\*([^*\n]+?)\*(?![*\w])/

// inlineRuns splitst één tekstsegment in runs. Een kopregel (`#`, `##`, …)
// wordt vet, zonder de hekjes. Ongepaarde sterretjes blijven gewoon staan.
export function inlineRuns(text: string): InlineRun[] {
  const out: InlineRun[] = []
  const lines = text.split('\n')
  lines.forEach((line, i) => {
    const heading = /^\s{0,3}#{1,6}\s+(.*)$/.exec(line)
    if (heading) {
      out.push({ kind: 'strong', value: heading[1].replace(/\*\*/g, '') })
    } else {
      splitEmphasis(line, out)
    }
    if (i < lines.length - 1) out.push({ kind: 'text', value: '\n' })
  })
  return merge(out)
}

function splitEmphasis(line: string, out: InlineRun[]): void {
  let rest = line
  while (rest.length > 0) {
    const s = STRONG.exec(rest)
    const e = EM.exec(rest)
    const next = pick(s, e)
    if (!next) {
      out.push({ kind: 'text', value: rest })
      return
    }
    const [m, kind] = next
    if (m.index > 0) out.push({ kind: 'text', value: rest.slice(0, m.index) })
    out.push({ kind, value: m[1] })
    rest = rest.slice(m.index + m[0].length)
  }
}

function pick(s: RegExpExecArray | null, e: RegExpExecArray | null): [RegExpExecArray, 'strong' | 'em'] | null {
  if (s && (!e || s.index <= e.index)) return [s, 'strong']
  if (e) return [e, 'em']
  return null
}

function merge(runs: InlineRun[]): InlineRun[] {
  const out: InlineRun[] = []
  for (const r of runs) {
    if (r.value === '') continue
    const last = out[out.length - 1]
    if (last && last.kind === r.kind) last.value += r.value
    else out.push({ ...r })
  }
  return out
}
