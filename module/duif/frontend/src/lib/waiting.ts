// Lang wachten moet eerlijk zijn in plaats van stil.
//
// De modelcalls via de proxy duren onvoorspelbaar lang: hetzelfde verzoek is
// de ene keer in twee seconden klaar en de andere keer pas na een minuut. Dat
// lossen wij niet op, maar een spinner die na tien seconden nog precies
// hetzelfde zegt als na één seconde, leest als een vastgelopen scherm. Vanaf
// een drempel toont de wachtstand daarom hoe lang het al duurt.
//
// De drempel ligt op acht seconden: korter dan dat is een gewone wachttijd en
// een tikkende teller zou er alleen maar onrust bij zetten.
export const WAIT_ELAPSED_AFTER_MS = 8_000

// elapsedSuffix geeft het staartje achter de wachttekst ("… 23 s"), of een
// lege string zolang de drempel niet gehaald is. `since` is het moment waarop
// het wachten begon; 0 betekent: er wordt niet gewacht.
export function elapsedSuffix(since: number, now: number): string {
  if (since <= 0) return ''
  const elapsed = now - since
  if (!Number.isFinite(elapsed) || elapsed < WAIT_ELAPSED_AFTER_MS) return ''
  return ` ${Math.floor(elapsed / 1000)} s`
}
