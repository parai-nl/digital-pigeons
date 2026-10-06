// Het taalmodel voor een nieuwe opname: alle modelaanroepen van de duiven gaan
// naar dit ene model. Dit is de regel die nu
// geldt; wat een bestaande take gebruikte is zijn herkomst en staat in de take
// zelf (`app.modelPolicy`, en bij de chat de provenance per antwoord). Afspelen
// en monteren doen geen modelaanroep en kijken daar dus niet naar: een take
// die op een eerdere alias is opgenomen blijft eerlijk opgenomen en blijft
// afspeelbaar.
export const RECORD_MODEL = 'vertex/gemini-3.6-flash-eu'

// endpointOrigin: de oorsprong (https://host[:poort]) van een HTTPS-adres zonder
// gebruikersnaam, query of fragment, zoals de module hem in haar modelbeleid
// meldt; anders null. Er is geen standaardadres: opnemen vergelijkt met
// PARAI_LLM_BASE_URL uit de omgeving, het adres waarmee de opnameserver draait.
export function endpointOrigin(raw) {
  let url
  try { url = new URL(raw) } catch { return null }
  if (url.protocol !== 'https:' || !url.hostname || url.username || url.password || url.search || url.hash) return null
  return url.origin
}

// Eerdere aliassen als herkomst, met het moment tot wanneer een take ermee
// opgenomen kan zijn: de wissel naar RECORD_MODEL kwam op 24 september 2026 in
// de hoofdtak. Dezelfde grens als de etalage-opnames
// (tools/etalage-opnames/lib/config.mjs).
export const HISTORICAL_MODELS = { 'vertex_ai/gemini-3.5-flash': '2026-09-25T00:00:00Z' }

// takeModelAllowed zegt of een take met dit model (herkomst) geldig is: het
// huidige model altijd, een eerdere alias alleen voor een take die vóór haar
// einddatum is opgenomen. Dezelfde regel als modelAllowed in de etalage-opnames.
export function takeModelAllowed(model, recordedAt) {
  if (model === RECORD_MODEL) return true
  if (typeof model !== 'string' || !Object.hasOwn(HISTORICAL_MODELS, model)) return false
  const at = Date.parse(recordedAt)
  return Number.isFinite(at) && at < Date.parse(HISTORICAL_MODELS[model])
}

// recordPolicyError zegt waarom een server met dit modelbeleid (`modelPolicy`
// uit GET /api/duif/v1/duiven) niet mag opnemen, of null als het mag. Chat en
// detectie moeten allebei precies het verplichte model via de Parai-proxy
// gebruiken; een eerdere alias telt als een ander model.
export function recordPolicyError(policy, configured = process.env.PARAI_LLM_BASE_URL) {
  const endpoint = endpointOrigin(configured)
  if (!endpoint) return 'kan niet controleren: zet PARAI_LLM_BASE_URL op het HTTPS-adres waarmee de opnameserver draait'
  if (policy?.model === RECORD_MODEL && policy?.detectionModel === RECORD_MODEL && policy?.endpoint === endpoint) return null
  return `gebruikt niet ${RECORD_MODEL} via ${endpoint}: ${JSON.stringify(policy ?? null)}`
}
