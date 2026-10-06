// Verzoeken koppelen aan de opname. Tijdens het afspelen stelt de
// echte frontend dezelfde vragen als tijdens de opname; elk verzoek krijgt het
// opgenomen antwoord op dezelfde methode en hetzelfde pad, in dezelfde
// volgorde. Sessie-id's staan letterlijk in de opgenomen antwoorden, dus de
// paden die de app daarna opbouwt zijn ook letterlijk gelijk.
//
// Deze functie wordt ook als init-script de pagina in gestuurd
// (Function.prototype.toString): hij mag dus niets van buiten gebruiken.
export function createMatcher(exchanges) {
  const queues = new Map()
  for (const exchange of exchanges) {
    const key = exchange.method.toUpperCase() + ' ' + exchange.path
    if (!queues.has(key)) queues.set(key, { items: [], next: 0 })
    queues.get(key).items.push(exchange)
  }
  // Wat de app herhaalt zonder dat het de inhoud raakt — microfoonblokken,
  // afspeelbevestigingen en gewone GET's — mag vaker komen dan in de opname:
  // dan krijgt het het laatste opgenomen antwoord nog eens. Een beurt of een
  // nieuwe sessie nooit: een tweede antwoord verzinnen is precies wat niet mag.
  const repeatable = (method, path) => method === 'GET' || /\/(audio|playback)$/.test(path.split('?')[0])
  return function next(method, path) {
    const m = method.toUpperCase()
    const queue = queues.get(m + ' ' + path)
    if (!queue) return null
    if (queue.next < queue.items.length) return queue.items[queue.next++]
    return repeatable(m, path) ? queue.items[queue.items.length - 1] : null
  }
}

// requestPath maakt van een URL of pad het vergelijkbare deel: pad plus
// zoekdeel, zonder oorsprong.
export function requestPath(url, base = 'http://localhost') {
  const u = new URL(url, base)
  return u.pathname + u.search
}
