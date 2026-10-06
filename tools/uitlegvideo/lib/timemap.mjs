// Tijdkaart voor het afspelen van een take. Een take is live
// opgenomen; in de video mag wachttijd korter (bronnen lezen, de duif die
// nadenkt), maar de volgorde en de tekst nooit. De kaart is daarom één
// monotone, stuksgewijs lineaire functie van opnametijd naar afspeeltijd:
// buiten de ingekorte vensters loopt de klok één op één, binnen een venster
// wordt hij gelijkmatig samengedrukt. Twee berichten wisselen dus nooit van
// volgorde, en een bericht verandert nooit van inhoud — alleen van moment.

// buildTimeMap maakt de kaart uit vensters { from, to, duration } in
// milliseconden opnametijd: [from, to) duurt in de video `duration` ms.
// Vensters mogen niet overlappen; `duration` mag niet langer zijn dan het
// venster zelf (de kaart kort alleen in, rekt nooit op).
export function buildTimeMap(windows = []) {
  const sorted = windows
    .filter((w) => w.to > w.from)
    .map((w) => ({ from: w.from, to: w.to, duration: w.duration }))
    .sort((a, b) => a.from - b.from)
  for (let i = 0; i < sorted.length; i++) {
    const w = sorted[i]
    if (!(w.duration >= 0) || w.duration > w.to - w.from) throw new Error(`timemap: venster ${w.from}–${w.to} kan niet ${w.duration} ms duren`)
    if (i > 0 && w.from < sorted[i - 1].to) throw new Error(`timemap: vensters overlappen bij ${w.from}`)
  }
  // Knooppunten (opname, video); tussen twee knopen lineair. Vóór de eerste
  // knoop loopt de klok ongewijzigd, na de laatste met helling één.
  const knots = []
  let shift = 0
  for (const w of sorted) {
    knots.push([w.from, w.from - shift])
    shift += w.to - w.from - w.duration
    knots.push([w.to, w.to - shift])
  }
  const map = (t) => {
    let i = knots.length - 1
    while (i >= 0 && knots[i][0] > t) i--
    if (i < 0) return t
    const [t0, v0] = knots[i]
    const next = knots[i + 1]
    if (!next) return v0 + (t - t0)
    const [t1, v1] = next
    return t1 === t0 ? v0 : v0 + ((t - t0) * (v1 - v0)) / (t1 - t0)
  }
  map.windows = sorted
  map.saved = shift
  return map
}

// capGaps maakt vensters van elke pauze tussen opeenvolgende momenten die
// langer is dan `maxGap`: zo blijft elke stap even zichtbaar, maar staat
// niemand tien seconden naar een draaiend wieltje te kijken. `within`
// beperkt het tot een deel van de opname.
export function capGaps(times, maxGap, within = { from: -Infinity, to: Infinity }) {
  const inside = [...new Set(times)].filter((t) => t >= within.from && t <= within.to).sort((a, b) => a - b)
  const windows = []
  for (let i = 1; i < inside.length; i++) {
    const gap = inside[i] - inside[i - 1]
    if (gap > maxGap) windows.push({ from: inside[i - 1], to: inside[i], duration: maxGap })
  }
  return windows
}

// mergeWindows voegt vensterlijsten uit meerdere regels samen. Twee regels
// kunnen hetzelfde stuk stilte willen inkorten (twee streams die tegelijk
// lopen); dan wint het vroegste venster en valt het overlappende latere
// stil weg. Zo wordt geen stuk twee keer ingekort.
export function mergeWindows(...lists) {
  const all = lists.flat().sort((a, b) => a.from - b.from)
  const out = []
  for (const w of all) {
    const last = out.at(-1)
    if (last && w.from < last.to) continue
    out.push(w)
  }
  return out
}
