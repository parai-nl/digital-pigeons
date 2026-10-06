package duif

import "strings"

// These are imported property types, not a new framework schema. All mapped
// v2 fields must have an explicit rule; extending the mapper without extending
// this contract fails closed. Required fields/ancestry are checked separately.
//
// "localized" en "localized-list" zijn de twee vormen die duiven/3 toevoegt:
// in duiven/2 zijn ze een kale string respectievelijk een lijst strings, in
// duiven/3 een object met beide talen. Zie module/README.md, "Het
// bundelformaat duiven/3".
var kbV2MappedKinds = map[string]string{
	"samenvatting": "localized", "primary_themes": "strings", "secondary_themes": "strings",
	"domain": "string", "scope": "string", "abstraction_level": "string", "persona": "string", "ext_id": "string",
	"text": "string", "speaker": "string", "quote_index": "integer", "grounded": "boolean",
	"bron_id": "string", "bron_key": "string", "destillaat_id": "string",
	"title": "string", "storage_key": "string", "interview_id": "string", "key": "string",
	"citatie": "string", "doi": "string", "categorie": "string", "citatie_confidence": "confidence",
	"citatie_ontbreekt": "string", "citatie_bron": "string", "type": "string", "source_kind": "string",
	"titel": "localized", "beschrijving": "localized", "kernconcepten": "localized-list", "taxonomy_level": "string",
	"destillaat_count": "integer", "naam": "localized", "kernstandpunt": "localized",
}

// De twee talen van duiven/3. De brontaal is Nederlands (het Nederlands blijft
// volwaardig); het Engels is de vertaling ernaast.
const (
	kbLangSource  = "nl"
	kbLangEnglish = "en"
)

// kbLocalizedSuffix is het achtervoegsel van de zijspoorprop waarin de Engelse
// tekst landt: `titel` blijft de brontaal, `titel_en` draagt de vertaling. Zo
// blijven core's getypeerde reads, inspectiequery's op de opslag en het
// graaflabel lezen wat ze altijd al lazen, en is het Engels puur additief.
const kbLocalizedSuffix = "_" + kbLangEnglish

// kbLocalizedProp geeft de naam van de zijspoorprop bij een bronveld.
func kbLocalizedProp(key string) string { return key + kbLocalizedSuffix }

// kbLocalizedKind zegt of een prop in duiven/3 tweetalig is, en in welke vorm.
func kbLocalizedKind(key string) (string, bool) {
	switch kbV2MappedKinds[key] {
	case "localized":
		return "localized", true
	case "localized-list":
		return "localized-list", true
	}
	return "", false
}

func kbV2MappedPropertiesValid(schema, label string, row map[string]any) bool {
	for _, key := range kbNodeProps[kbLabelTables[label]] {
		kind, known := kbV2MappedKinds[key]
		if !known {
			return false
		}
		value, present := row[key]
		if !present {
			continue
		}
		// Optional means absent is allowed. An explicitly present null or a
		// value of the wrong type is not silently dropped/coerced by v2.
		switch kind {
		case "localized":
			if _, _, ok := kbLocalizedText(schema, value); !ok {
				return false
			}
		case "localized-list":
			if _, _, ok := kbLocalizedList(schema, value); !ok {
				return false
			}
		case "string":
			if _, ok := value.(string); !ok {
				return false
			}
		case "strings":
			if !kbAllStrings(value) {
				return false
			}
		case "integer":
			if _, ok := kbV2Integer(value); !ok {
				return false
			}
		case "boolean":
			if _, ok := value.(bool); !ok {
				return false
			}
		case "confidence":
			s, ok := value.(string)
			if !ok || s != "high" && s != "medium" && s != "low" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// kbLocalizedText leest een gelokaliseerd tekstveld. duiven/2 kent alleen de
// kale string en die is per definitie de brontaal. duiven/3 eist het volledige
// object met precies `nl` en `en`, allebei gevuld: een ontbrekende vertaling is
// een onaffe bundel, geen reden om stil terug te vallen op het Nederlands.
func kbLocalizedText(schema string, value any) (source, english string, ok bool) {
	if schema != kbV3Schema {
		s, isString := value.(string)
		return s, "", isString
	}
	obj, isObject := value.(map[string]any)
	if !isObject || len(obj) != 2 {
		return "", "", false
	}
	source, okSource := kbNonEmptyString(obj[kbLangSource])
	english, okEnglish := kbNonEmptyString(obj[kbLangEnglish])
	if !okSource || !okEnglish {
		return "", "", false
	}
	return source, english, true
}

// kbLocalizedList is dezelfde regel voor een lijst: in duiven/3 twee lijsten
// van gelijke lengte, element voor element elkaars vertaling. Ongelijke lengtes
// zouden betekenen dat er een kernconcept is weggevallen of bijgekomen, en dat
// is geen vertaling meer.
func kbLocalizedList(schema string, value any) (source, english []any, ok bool) {
	if schema != kbV3Schema {
		items, isList := value.([]any)
		if !isList || !kbAllStrings(items) {
			return nil, nil, false
		}
		return items, nil, true
	}
	obj, isObject := value.(map[string]any)
	if !isObject || len(obj) != 2 {
		return nil, nil, false
	}
	source, okSource := obj[kbLangSource].([]any)
	english, okEnglish := obj[kbLangEnglish].([]any)
	if !okSource || !okEnglish || len(source) != len(english) || !kbAllStrings(source) || !kbAllStrings(english) {
		return nil, nil, false
	}
	return source, english, true
}

// kbAllStrings toetst een lijst uit de bundel op louter strings.
func kbAllStrings(value any) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if _, ok := item.(string); !ok {
			return false
		}
	}
	return true
}

func kbNonEmptyString(value any) (string, bool) {
	s, ok := value.(string)
	return s, ok && strings.TrimSpace(s) != ""
}
