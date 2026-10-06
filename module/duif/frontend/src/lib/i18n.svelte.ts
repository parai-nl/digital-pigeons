import { chatEn, chatNl } from './i18n/chat'
import { commonEn, commonNl } from './i18n/common'
import { graphEn, graphNl } from './i18n/graph'
import { meetingEn, meetingNl } from './i18n/meeting'

export type Lang = 'en' | 'nl'

// Bilingual is één tweetalig label of melding zoals de wire hem stuurt
// ({en, nl}); niet elke taal hoeft erin te staan. Dit is de enige vorm waarin
// servertekst de UI binnenkomt.
export type Bilingual = Partial<Record<Lang, string>>

export const en = { ...commonEn, ...chatEn, ...meetingEn, ...graphEn } as const
export type MsgKey = keyof typeof en
export const nl: Record<MsgKey, string> = { ...commonNl, ...chatNl, ...meetingNl, ...graphNl }

// De vier woordenboeken worden met de spread samengevoegd, dus een sleutel die
// in twee tabellen staat verdwijnt stilletjes. Deze assertie maakt dat een
// compileerfout: overlappen ze, dan is `false` geen geldige Expect<true>.
// Vier tabellen zijn zes paren; ze staan er allemaal.
type Expect<T extends true> = T
type Overlap<A, B> = Extract<keyof A, keyof B>
type DictionaryOverlap =
  | Overlap<typeof commonEn, typeof chatEn>
  | Overlap<typeof commonEn, typeof meetingEn>
  | Overlap<typeof commonEn, typeof graphEn>
  | Overlap<typeof chatEn, typeof meetingEn>
  | Overlap<typeof chatEn, typeof graphEn>
  | Overlap<typeof meetingEn, typeof graphEn>
export type DictionariesAreDisjoint = Expect<[DictionaryOverlap] extends [never] ? true : false>

export const dictionaries: Record<Lang, Record<MsgKey, string>> = { en, nl }

export function isLang(value: unknown): value is Lang {
  return value === 'en' || value === 'nl'
}

// isMsgKey vervangt de `as MsgKey`-casts: een reason-code van de server wordt
// pas een sleutel als het woordenboek hem echt kent.
export function isMsgKey(value: string): value is MsgKey {
  return Object.hasOwn(en, value)
}

export function interpolate(template: string, vars?: Record<string, string | number>): string {
  if (!vars) return template
  return template.replace(/\{(\w+)\}/g, (match, name: string) =>
    Object.hasOwn(vars, name) ? String(vars[name]) : match,
  )
}

// UiLocale is de reactieve taalstand van de module. Eén exemplaar per mount,
// gedeeld via de mount-context; componenten roepen locale.t(...) aan. De keuze
// kan via de services uitsluitend de bewuste UI-taalvoorkeur bewaren.
export class UiLocale {
  lang = $state<Lang>('en')
  // chosen scheidt "de gebruiker koos deze taal" van "dit is nog de startwaarde".
  // Alleen in het tweede geval mag de serverconfig de taal nog zetten: een
  // gekozen taal wint van elke default.
  chosen = $state(false)

  constructor(initial: Lang = 'en', private readonly persist?: (lang: Lang) => void) {
    this.lang = initial
  }

  // choose is de taalkeuze van de gebruiker; hij legt de keuze vast.
  choose = (lang: Lang): void => {
    this.chosen = true
    this.lang = lang
    this.persist?.(lang)
  }

  t = (key: MsgKey, vars?: Record<string, string | number>): string =>
    interpolate(dictionaries[this.lang][key] ?? en[key], vars)

  // fromServer kiest de servermelding in de UI-taal; de server kent de reden,
  // dus die tekst is leidend. Zonder bruikbare tekst valt hij terug op een
  // reason-code-sleutel en daarna op `fallback` — nooit op ruwe fouttekst.
  fromServer = (message: Bilingual | null | undefined, reason?: string, fallback: MsgKey = 'error.generic'): string => {
    const own = message?.[this.lang]
    if (own) return own
    if (message?.en) return message.en
    if (reason) {
      const key = 'error.' + reason
      if (isMsgKey(key)) return this.t(key)
    }
    return this.t(fallback)
  }

  // pick kiest uit een tweetalig label ({en, nl}) de UI-taal, met terugval.
  pick = (label: Bilingual | null | undefined, fallback = ''): string => label?.[this.lang] || label?.en || fallback

  // pickContent doet hetzelfde voor kennisbanktekst, maar valt terug op de
  // ándere taal in plaats van op het Engels. Het verschil telt: op een corpus
  // dat alleen Nederlands is hoort een Engelse lezer het origineel te zien, niet
  // een lege regel. Wat er dan staat is niet zijn taal, en `contentLang` zegt
  // welke taal het wél is, zodat het scherm dat kan melden.
  pickContent = (text: Bilingual | null | undefined, fallback = ''): string =>
    text?.[this.lang] || text?.[this.lang === 'nl' ? 'en' : 'nl'] || fallback

  // contentLang is de taal waarin pickContent uitkomt, of null als er niets
  // staat.
  contentLang = (text: Bilingual | null | undefined): Lang | null => {
    const other: Lang = this.lang === 'nl' ? 'en' : 'nl'
    if (text?.[this.lang]) return this.lang
    if (text?.[other]) return other
    return null
  }
}
