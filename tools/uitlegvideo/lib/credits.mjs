// Naamsvermelding voor het beeld van de duif. Het 3D-model en de
// portretten vallen onder CC BY-SA 3.0 (fototextuur Luc Viatour /
// Lucnix.be); de video moet dat vermelden. De tekst komt bij elke render uit
// de mediacatalogus die video.config.json noemt (render.mjs vergelijkt haar met
// wat de app zelf vermeldt), zodat een nieuw model zijn eigen
// vermelding meeneemt.

// De catalogus is Nederlands (de app gebruikt hem zo); de video is Engels.
// Het veld `creator` heeft de vorm "Maker; rol: naam; rol: naam". Bekende
// rollen worden vertaald; een onbekende vorm blijft letterlijk staan, met een
// waarschuwing, zodat er nooit een half vertaalde of verzonnen vermelding komt.
const ROLES = { fototextuur: 'photo texture by', foto: 'photo by', fotografie: 'photo by', model: 'model by', geluid: 'sound by', opname: 'recording by' }

export function englishCreator(creator) {
  const parts = String(creator ?? '').split(';').map((p) => p.trim()).filter(Boolean)
  if (!parts.length) return { text: String(creator ?? ''), translated: false }
  const out = [parts[0]]
  for (const part of parts.slice(1)) {
    const m = /^([^:]+):\s*(.+)$/.exec(part)
    const role = m && ROLES[m[1].trim().toLowerCase()]
    if (!role) return { text: creator, translated: false }
    out.push(`${role} ${m[2].trim()}`)
  }
  return { text: out.join(', '), translated: true }
}

// creditLines maakt per herkomst één regel voor het beeld dat de video toont:
// het 3D-model, zijn rustbeeld en de portretten. Vrij te gebruiken werk (CC0)
// hoeft geen vermelding. De regel in beeld heeft maker en licentie; de bron
// (een lange URL) staat in `source` voor de ondertitels en de tijdlijn.
export function creditLines(manifest, warn = (m) => console.warn(m)) {
  // Sinds v4 heeft elke duif haar eigen model en rustposter
  // (`native.pigeons`); daarvoor was er één (`native.model`, `native.poster`).
  const pigeons = Object.values(manifest.native?.pigeons ?? {})
  const models = [manifest.native?.model, manifest.native?.poster, ...pigeons.flatMap((p) => [p.model, p.poster])]
  const used = [
    ...models.filter(Boolean).map((entry) => ['3D pigeon', entry]),
    ...(manifest.visual ?? []).filter((v) => v.kind === 'portrait' && v.attribution).map((v) => ['portraits', v]),
  ]
  const groups = new Map()
  for (const [label, entry] of used) {
    const a = entry.attribution
    if (!a || /^CC0/i.test(a.license ?? '')) continue
    const key = [a.creator, a.license, a.source].join('|')
    if (!groups.has(key)) groups.set(key, { labels: new Set(), a })
    groups.get(key).labels.add(label)
  }
  return [...groups.values()].map(({ labels, a }) => {
    const creator = englishCreator(a.creator)
    if (!creator.translated) warn(`naamsvermelding: maker "${a.creator}" heeft geen bekende vorm; hij staat letterlijk in beeld`)
    const what = [...labels].join(' and ')
    return {
      text: `${what[0].toUpperCase()}${what.slice(1)}: ${creator.text} · ${a.license}`,
      license: a.license ?? '',
      source: a.source ?? '',
      licenseUrl: a.licenseUrl ?? '',
    }
  })
}

// modelAttribution is de vermelding van het model dat de app bij deze duif
// toont (MeetingPigeon: haar eigen model, nooit dat van een andere duif).
export function modelAttribution(manifest, pigeon) {
  const pigeons = manifest.native?.pigeons
  return (pigeons ? pigeons[pigeon]?.model : manifest.native?.model)?.attribution ?? null
}
