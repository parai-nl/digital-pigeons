import { isLang, type Lang } from '../i18n.svelte'

// De taal van de gastheer. In de parai-core-schil kiest de gebruiker de taal
// één keer, in de schil zelf, en de schil houdt `<html lang>` daarmee in de
// pas. De module heeft daarom geen eigen taalkiezer meer — zij
// leest hier af wat de schil zegt en volgt dat. De DevShell doet in standalone
// precies hetzelfde met `<html lang>` (met de bewaarde voorkeur als bron),
// zodat er één regel te begrijpen valt.
//
// `<html lang>` is het enige pad: daar staat de taal die de schil werkelijk
// koos, en de schil zet het vóór zij een module mount. Opslag leest dit
// bestand bewust niet: de
// privacywachters staan buiten de eigen taalvoorkeur geen opslag-API toe, en
// de MutationObserver hieronder vangt een attribuut dat later nog wisselt.
//
// Dit bestand kent alleen de gastheer; wat er met een taalwissel gebeurt
// tijdens een vergadering beslist de schil van de module (App.svelte).
function fromDocumentLang(doc: Document): Lang | null {
  // "nl-NL" telt als Nederlands: de schil schrijft een BCP-47-tag, wij kennen
  // alleen de basistaal.
  const tag = doc.documentElement?.getAttribute('lang')?.trim().toLowerCase().split('-')[0]
  return isLang(tag) ? tag : null
}

// hostLanguage geeft de taal van de gastheer, of null wanneer er geen gastheer
// is die er een uitspreekt (een test in jsdom, een pagina zonder lang-attribuut).
// Null betekent: houd het bij je eigen voorkeur.
export function hostLanguage(doc: Document | undefined = globalThis.document): Lang | null {
  if (!doc) return null
  return fromDocumentLang(doc)
}

// observeHostLanguage volgt latere wissels. De schil herlaadt de pagina bij een
// taalwissel, maar dat is haar keuze en niet onze aanname: verandert `lang`
// binnen dezelfde mount, dan verhuist de module mee.
export function observeHostLanguage(
  onChange: (lang: Lang) => void,
  doc: Document | undefined = globalThis.document,
): () => void {
  const root = doc?.documentElement
  if (!root || typeof MutationObserver === 'undefined') return () => {}
  const observer = new MutationObserver(() => {
    const lang = hostLanguage(doc)
    if (lang) onChange(lang)
  })
  observer.observe(root, { attributes: true, attributeFilter: ['lang'] })
  return () => observer.disconnect()
}
