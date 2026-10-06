import { isLang, UiLocale, type Lang } from '../i18n.svelte'
import { hostLanguage } from './hostLanguage'

const LANGUAGE_KEY = 'duif.ui-language'

// This isolated adapter stores a validated UI preference, never conversation
// or meeting content. Storage may be unavailable in a restricted browser.
export function storedLanguage(): Lang | null {
  let saved: unknown
  try { saved = globalThis.localStorage?.getItem(LANGUAGE_KEY) } catch { /* unavailable */ }
  return isLang(saved) ? saved : null
}

export function storeLanguage(lang: Lang): void {
  if (!isLang(lang)) return
  try { globalThis.localStorage?.setItem(LANGUAGE_KEY, lang) } catch { /* in-memory preference still works */ }
}

// De gastheer gaat voor: spreekt de schil (of de DevShell die haar nabootst)
// een taal uit in `<html lang>`, dan begint de module
// daarin. De bewaarde voorkeur is de terugval voor een mount zonder
// gastheertaal.
export function createBrowserLocale(): UiLocale {
  const saved = storedLanguage()
  const host = hostLanguage()
  const locale = new UiLocale(host ?? saved ?? 'en', storeLanguage)
  // chosen houdt de serverconfig van de taal af: een keuze wint van de default. Een taal van de
  // gastheer is net zo goed een keuze van de gebruiker als een bewaarde.
  if (host || saved) locale.chosen = true
  return locale
}

// followHostLanguage past een latere taalwissel van de gastheer toe. Er wordt
// niets bewaard: de gastheer onthoudt zijn eigen keuze al, en de module hoort
// er geen tweede, afwijkende kopie van te bewaren.
export function followHostLanguage(locale: UiLocale, lang: Lang): void {
  locale.lang = lang
  locale.chosen = true
}
