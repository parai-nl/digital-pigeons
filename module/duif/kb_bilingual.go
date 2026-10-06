package duif

import "strings"

// Bilingual is kennisbanktekst in de talen waarin hij bestaat: `{"nl": …}` voor
// een eentalig corpus, `{"nl": …, "en": …}` na de vertaling. Bewust een map en
// geen struct met twee velden: een taal die er niet is hoort ook niet als lege
// string op de wire te staan, want dan kan de client niet zien of hij terug moet
// vallen. Hij past op `Bilingual` in i18n.svelte.ts.
//
// Alleen velden die wérkelijk vertaald zijn krijgen deze vorm. Een citaat, een
// papertitel en een citatie blijven een kale string: die zijn niet vertaald, en
// ze in een taalobject stoppen zou een taalclaim zijn die deze code niet kan
// waarmaken. De wire draagt daarom per veld óf een string óf dit object.
type Bilingual map[string]string

// kbSourceLanguage is de taal waarin de knoop oorspronkelijk geschreven is. Een
// duiven/2-knoop draagt de prop niet; die corpora zijn Nederlands.
func kbSourceLanguage(props map[string]any) string {
	if lang, ok := props["source_language"].(string); ok && lang != "" {
		return lang
	}
	return kbLangSource
}

// bilingualProp leest een vertaald veld uit de rauwe graafprops: de bestaande
// kolom is de brontaal, de zijspoorprop (`titel_en`) de vertaling. Zonder tekst
// in de brontaal is er niets om te tonen en is het antwoord nil — een leeg
// taalobject zou op het scherm een lege regel worden.
func bilingualProp(props map[string]any, key string) Bilingual {
	source, _ := props[key].(string)
	if strings.TrimSpace(source) == "" {
		return nil
	}
	lang := kbSourceLanguage(props)
	out := Bilingual{lang: source}
	if english, ok := props[kbLocalizedProp(key)].(string); ok && strings.TrimSpace(english) != "" {
		out[kbLangEnglish] = english
	}
	return out
}

// mapText past een bewerking toe op elke taal; de talenverzameling verandert
// niet. Het graaflabel kapt zo beide talen op dezelfde manier af.
func (b Bilingual) mapText(f func(string) string) Bilingual {
	if b == nil {
		return nil
	}
	out := make(Bilingual, len(b))
	for lang, text := range b {
		out[lang] = f(text)
	}
	return out
}

// Pick kiest de gevraagde taal en valt terug op de andere taal die er wél is.
// Terugvallen op leeg zou een Engelse lezer een leeg scherm geven op een corpus
// dat alleen Nederlands is; het origineel tonen is dan het eerlijke antwoord.
// Dit is de servertegenhanger van `pickContent` in i18n.svelte.ts.
func (b Bilingual) Pick(lang string) string {
	if text := b[lang]; text != "" {
		return text
	}
	for _, fallback := range []string{kbLangSource, kbLangEnglish} {
		if text := b[fallback]; text != "" {
			return text
		}
	}
	for _, text := range b {
		if text != "" {
			return text
		}
	}
	return ""
}

// graphLabelText leest een wire-label als tekst in één taal. De wire draagt per
// veld óf een kale string (onvertaalde tekst) óf een taalobject; deze functie
// is het serverzijdige equivalent van wat de client doet.
func graphLabelText(label any, lang string) string {
	switch value := label.(type) {
	case string:
		return value
	case Bilingual:
		return value.Pick(lang)
	case map[string]any:
		out := Bilingual{}
		for key, item := range value {
			if s, ok := item.(string); ok {
				out[key] = s
			}
		}
		return out.Pick(lang)
	}
	return ""
}
