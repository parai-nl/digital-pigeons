package duif

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

func testPersonas(t *testing.T) *PersonaSet {
	t.Helper()
	set, err := LoadEmbeddedPersonas()
	if err != nil {
		t.Fatalf("LoadEmbeddedPersonas: %v", err)
	}
	return set
}

// gapSectionFor geeft het gatdirectief van de standpunt-duif zoals de
// assemblage het invult: met de naam uit personas.yaml.
func gapSectionFor(p *Persona, lang string) string {
	return fmt.Sprintf(individualistGapDirective[normLang(lang)], inZin(pickLang(p.Naam, lang), lang))
}

func mustGet(t *testing.T, set *PersonaSet, id string) *Persona {
	t.Helper()
	p, ok := set.Get(id)
	if !ok {
		t.Fatalf("persona %q ontbreekt", id)
	}
	return p
}

func TestPrompt_PersonaOnly_NoCitationSection(t *testing.T) {
	set := testPersonas(t)
	got := BuildSystemPrompt(PromptInput{
		Persona: mustGet(t, set, "individualist"),
		Lang:    "en",
		Status:  KBPersonaOnly,
	})
	if strings.Contains(got, citationInstruction["en"]) || strings.Contains(got, citationInstruction["nl"]) {
		t.Error("persona-only prompt mag geen citatie-instructie bevatten")
	}
	if strings.Contains(got, ragContextHeader["en"]) || strings.Contains(got, ragContextHeader["nl"]) {
		t.Error("persona-only prompt mag geen RAG-context bevatten")
	}
	if strings.Contains(got, gapDirective["en"]) || strings.Contains(got, gapDirective["nl"]) {
		t.Error("persona-only prompt mag geen gap-directief bevatten")
	}
}

func TestPrompt_SectionOrder(t *testing.T) {
	set := testPersonas(t)
	p := mustGet(t, set, "individualist")
	got := BuildSystemPrompt(PromptInput{Persona: p, Lang: "en", Status: KBPersonaOnly})

	iBase := strings.Index(got, baseSection["en"])
	iPersona := strings.Index(got, p.Prompt("en"))
	iLang := strings.Index(got, langDirective["en"])
	if iBase < 0 || iPersona < 0 || iLang < 0 {
		t.Fatalf("sectie ontbreekt: basis=%d persona=%d taal=%d", iBase, iPersona, iLang)
	}
	if !(iBase < iPersona && iPersona < iLang) {
		t.Errorf("sectievolgorde fout: basis=%d persona=%d taal=%d (moet basis < persona < taal)", iBase, iPersona, iLang)
	}
}

func TestPrompt_ContainsHonestyRule(t *testing.T) {
	set := testPersonas(t)
	for _, id := range []string{"individualist", "biological"} {
		got := BuildSystemPrompt(PromptInput{Persona: mustGet(t, set, id), Lang: "en", Status: KBPersonaOnly})
		for _, eis := range []string{"eis:P1", "eis:P2"} {
			if !strings.Contains(got, eis) {
				t.Errorf("%s: prompt mist sentinel %s (eerlijkheidsregel)", id, eis)
			}
		}
	}
}

func TestPrompt_LangDirective_DefaultEN(t *testing.T) {
	set := testPersonas(t)
	p := mustGet(t, set, "individualist")
	got := BuildSystemPrompt(PromptInput{Persona: p, Lang: "", Status: KBPersonaOnly})
	if !strings.Contains(got, langDirective["en"]) {
		t.Error("lege lang moet defaulten naar Engels")
	}
	if !strings.Contains(got, p.Prompt("en")) {
		t.Error("lege lang moet het EN-promptbestand kiezen")
	}
}

func TestPrompt_LangDirective_NL(t *testing.T) {
	set := testPersonas(t)
	p := mustGet(t, set, "individualist")
	got := BuildSystemPrompt(PromptInput{Persona: p, Lang: "nl", Status: KBPersonaOnly})
	if !strings.Contains(got, langDirective["nl"]) {
		t.Error("lang=nl moet het NL-taaldirectief geven")
	}
	if !strings.Contains(got, p.Prompt("nl")) {
		t.Error("lang=nl moet het NL-promptbestand kiezen (één bestand per taal)")
	}
	if strings.Contains(got, p.Prompt("en")) {
		t.Error("lang=nl mag niet óók de EN-variant bevatten")
	}
}

func TestPrompt_Gap_ContainsGapDirective_NoRAGSection(t *testing.T) {
	set := testPersonas(t)
	got := BuildSystemPrompt(PromptInput{
		Persona: mustGet(t, set, "biological"),
		Lang:    "en",
		Status:  KBGap,
	})
	if !strings.Contains(got, gapDirective["en"]) {
		t.Error("gap-modus moet het gap-directief bevatten")
	}
	if strings.Contains(got, ragContextHeader["en"]) {
		t.Error("gap-modus mag geen RAG-context bevatten")
	}
	if strings.Contains(got, citationInstruction["en"]) {
		t.Error("gap-modus mag geen citatie-instructie bevatten")
	}
}

func TestPrompt_Sources_ContainsRAGContextAndCitation(t *testing.T) {
	set := testPersonas(t)
	got := BuildSystemPrompt(PromptInput{
		Persona: mustGet(t, set, "biological"),
		Lang:    "en",
		Status:  KBSources,
		RAGSection: "<research_sources>\n" +
			"    <source id=\"1\" type=\"destillaat\">\n      <text>Pigeons navigate by magnetoreception.</text>\n    </source>\n" +
			"    <source id=\"2\" type=\"destillaat\">\n      <text>City pigeons prefer ledges.</text>\n    </source>\n" +
			"</research_sources>",
	})
	if !strings.Contains(got, ragContextHeader["en"]) {
		t.Error("sources-modus moet de RAG-context bevatten")
	}
	if !strings.Contains(got, citationInstruction["en"]) {
		t.Error("sources-modus moet de citatie-instructie bevatten")
	}
	if !strings.Contains(got, `<source id="1"`) || !strings.Contains(got, `<source id="2"`) {
		t.Error("bronnen moeten genummerd in de context staan")
	}
	if strings.Contains(got, gapDirective["en"]) {
		t.Error("sources-modus mag geen gap-directief bevatten")
	}
}

// Zonder gerenderde bronnensectie is er niets om naar te verwijzen: dan hoort
// de citatie-instructie er ook niet te staan.
func TestPrompt_SourcesWithoutSection_NoCitationInstruction(t *testing.T) {
	set := testPersonas(t)
	got := BuildSystemPrompt(PromptInput{
		Persona: mustGet(t, set, "biological"),
		Lang:    "en",
		Status:  KBSources,
	})
	if strings.Contains(got, citationInstruction["en"]) {
		t.Error("zonder bronnensectie mag er geen citatie-instructie in de prompt staan")
	}
}

// Het gap-directief moet drie dingen expliciet maken: geen antwoord uit
// eigen kennis, geen citatiemarkeringen, en de vraag als onderzoeksgat.
func TestPrompt_GapDirective_ForbidsCitationsAndNamesTheGap(t *testing.T) {
	for _, lang := range []string{"en", "nl"} {
		got := gapDirective[lang]
		if !strings.Contains(got, "[1]") {
			t.Errorf("%s: het directief moet de citatiemarkering bij naam verbieden:\n%s", lang, got)
		}
	}
	for _, woord := range []string{"general knowledge", "research gap"} {
		if !strings.Contains(gapDirective["en"], woord) {
			t.Errorf("EN-gap-directief mist %q:\n%s", woord, gapDirective["en"])
		}
	}
	for _, woord := range []string{"algemene kennis", "onderzoeksgat"} {
		if !strings.Contains(gapDirective["nl"], woord) {
			t.Errorf("NL-gap-directief mist %q:\n%s", woord, gapDirective["nl"])
		}
	}
}

// Een vervolgvraag over het gesprek en een identiteitsvraag kunnen in elke
// kennisbank-status komen; de twee directieven horen daarom in elke prompt te
// staan, niet alleen in gap-modus.
func TestPrompt_ConversationDirectives_InEveryStatus(t *testing.T) {
	set := testPersonas(t)
	for _, id := range []string{"biological", "individualist"} {
		for _, lang := range []string{"en", "nl"} {
			for _, status := range []KBStatus{KBPersonaOnly, KBSources, KBGap, KBUnavailable} {
				got := BuildSystemPrompt(PromptInput{Persona: mustGet(t, set, id), Lang: lang, Status: status})
				if !strings.Contains(got, conversationFollowUpDirective[lang]) {
					t.Errorf("%s/%s/%s: het gespreksvervolg-directief ontbreekt", id, lang, status)
				}
				if !strings.Contains(got, identityDisclosureDirective[lang]) {
					t.Errorf("%s/%s/%s: het identiteit-op-vraag-directief ontbreekt", id, lang, status)
				}
			}
		}
	}
}

// De gatmelding mag geen identiteitsuitleg meer uitnodigen en moet naar het
// gesprek en de al gebruikte bronnen wijzen.
func TestPrompt_GapDirective_PointsBackAtTheConversation(t *testing.T) {
	for lang, woorden := range map[string][]string{
		"en": {"this conversation", "already used"},
		"nl": {"dit gesprek", "al gebruikte bronnen"},
	} {
		for _, woord := range woorden {
			if !strings.Contains(gapDirective[lang], woord) {
				t.Errorf("%s-gap-directief mist %q:\n%s", lang, woord, gapDirective[lang])
			}
		}
	}
	for lang, verboden := range map[string][]string{
		"en": {"project identity"},
		"nl": {"projectidentiteit"},
	} {
		for _, woord := range verboden {
			if strings.Contains(gapDirective[lang], woord) {
				t.Errorf("%s-gap-directief nodigt nog uit tot identiteitsuitleg (%q)", lang, woord)
			}
		}
	}
}

// flat maakt promptproza vergelijkbaar met een zin uit één regel: in een
// .md-bestand loopt een formulering over een regeleinde heen.
func flat(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

// De individualist kreeg vroeger haar verslagtoon uit een sjabloonzin in haar
// promptbestand en uit de tweedelige outputregel. Beide zijn weg; wat
// blijft is één stem met de citatie op de feitelijke zin. Deze test bewaakt
// dat de sjabloon niet terugsluipt in de sturing — in geen van beide talen.
func TestPrompt_Individualist_NoTemplateSentence(t *testing.T) {
	set := testPersonas(t)
	p := mustGet(t, set, "individualist")
	verboden := map[string][]string{
		"en": {"The available research shows", "My position is...", "at most two position sentences"},
		"nl": {"Het beschikbare onderzoek laat zien", "Mijn standpunt is...", "hoogstens twee standpuntzinnen"},
	}
	for _, lang := range []string{"en", "nl"} {
		bronnen := map[string]string{
			"promptbestand":        p.Prompt(lang),
			"uitvoerdirectief":     individualistOutputDirective[lang],
			"gatdirectief":         gapSectionFor(p, lang),
			"volledige prompt":     BuildSystemPrompt(PromptInput{Persona: p, Lang: lang, Status: KBSources, RAGSection: "<research_sources/>"}),
			"volledige gap-prompt": BuildSystemPrompt(PromptInput{Persona: p, Lang: lang, Status: KBGap}),
		}
		for naam, tekst := range bronnen {
			for _, zin := range verboden[lang] {
				if strings.Contains(flat(tekst), flat(zin)) {
					t.Errorf("%s/%s: de sjabloonsturing %q staat er nog in", lang, naam, zin)
				}
			}
		}
	}
}

// De nieuwe regel moet er ook echt staan, in beide talen: één stem, de citatie
// draagt het onderscheid, en de feiten zonder neutrale aanloop.
func TestPrompt_Individualist_OneVoiceWithCitedFacts(t *testing.T) {
	set := testPersonas(t)
	p := mustGet(t, set, "individualist")
	eisen := map[string][]string{
		"en": {"your own voice", "three to five short sentences", "[n] marker", "no citation marker"},
		"nl": {"je eigen stem", "drie tot vijf korte zinnen", "[n]-markering", "geen citatiemarkering"},
	}
	for _, lang := range []string{"en", "nl"} {
		got := BuildSystemPrompt(PromptInput{Persona: p, Lang: lang, Status: KBSources, RAGSection: "<research_sources/>"})
		for _, woord := range eisen[lang] {
			if !strings.Contains(got, woord) {
				t.Errorf("%s: de individualist-prompt mist %q", lang, woord)
			}
		}
	}
	// De verslagaanloop staat expliciet in de verboden lijst van haar promptbestand.
	aanloop := map[string]string{
		"en": "the available research suggests",
		"nl": "het beschikbare onderzoek suggereert",
	}
	for _, lang := range []string{"en", "nl"} {
		if !strings.Contains(flat(p.Prompt(lang)), aanloop[lang]) {
			t.Errorf("%s: het promptbestand verbiedt de neutrale aanloop %q niet bij naam", lang, aanloop[lang])
		}
	}
}

// Ook in gap-modus (geen bronnen) blijft de lengte- en stemregel staan: daar
// krijgt de individualist geen uitvoerdirectief, alleen het gatdirectief.
func TestPrompt_IndividualistGap_KeepsVoiceAndLength(t *testing.T) {
	set := testPersonas(t)
	p := mustGet(t, set, "individualist")
	eisen := map[string][]string{
		"en": {"your own voice", "three to five short sentences"},
		"nl": {"je eigen stem", "drie tot vijf korte zinnen"},
	}
	for _, lang := range []string{"en", "nl"} {
		got := BuildSystemPrompt(PromptInput{Persona: p, Lang: lang, Status: KBGap})
		if strings.Contains(got, individualistOutputDirective[lang]) {
			t.Errorf("%s: gap-modus hoort het uitvoerdirectief niet te herhalen", lang)
		}
		for _, woord := range eisen[lang] {
			if !strings.Contains(got, woord) {
				t.Errorf("%s: het gap-antwoord van de individualist mist %q", lang, woord)
			}
		}
	}
}

// Aan de vergadertafel geldt de vorm van meetingDirective (2–4 gesproken
// zinnen). De chat-zinnenteller van de individualist mag daar dus niet staan,
// in geen van beide talen en in geen van beide kennisbank-standen.
func TestPrompt_IndividualistMeeting_HasNoChatSentenceCount(t *testing.T) {
	set := testPersonas(t)
	p := mustGet(t, set, "individualist")
	teller := map[string]string{"en": "three to five short sentences", "nl": "drie tot vijf korte zinnen"}
	for _, lang := range []string{"en", "nl"} {
		for _, status := range []KBStatus{KBSources, KBGap} {
			in := PromptInput{Persona: p, Lang: lang, Status: status, RAGSection: "<research_sources/>", Meeting: true}
			vergadering := BuildSystemPrompt(in)
			if strings.Contains(vergadering, teller[lang]) {
				t.Errorf("%s/%s: de vergaderprompt draagt de chat-zinnenteller %q", lang, status, teller[lang])
			}
			if strings.Contains(vergadering, individualistChatFormDirective[lang]) {
				t.Errorf("%s/%s: de chatvorm hoort niet in een vergaderprompt", lang, status)
			}
			if !strings.Contains(vergadering, meetingDirective[lang]) {
				t.Errorf("%s/%s: het vergaderdirectief ontbreekt", lang, status)
			}
			in.Meeting = false
			if chat := BuildSystemPrompt(in); !strings.Contains(chat, teller[lang]) {
				t.Errorf("%s/%s: de chatprompt mist juist de zinnenteller %q", lang, status, teller[lang])
			}
		}
	}
}

// De reikwijdtevoorbeelden leerden haar letterlijk de toeschrijvingsformulering
// ("the authors suggest", "dat paper meldde") die bewust uit haar antwoord is
// gehaald. De
// individualist krijgt daarom een eigen variant; de kennispartner houdt het
// gedeelde blok onveranderd.
func TestPrompt_ScopeExamples_AttributionOnlyForBiological(t *testing.T) {
	set := testPersonas(t)
	toeschrijving := map[string][]string{
		"en": {"the authors suggest this pattern", "That paper reported limited knowledge"},
		"nl": {"suggereren de auteurs dit patroon", "Dat paper meldde beperkte kennis"},
	}
	behoud := map[string]string{"en": "in the setting studied", "nl": "binnen de onderzochte situatie"}
	for _, lang := range []string{"en", "nl"} {
		ego := BuildSystemPrompt(PromptInput{Persona: mustGet(t, set, "individualist"), Lang: lang, Status: KBSources, RAGSection: "<research_sources/>"})
		bio := BuildSystemPrompt(PromptInput{Persona: mustGet(t, set, "biological"), Lang: lang, Status: KBSources, RAGSection: "<research_sources/>"})
		for _, zin := range toeschrijving[lang] {
			if strings.Contains(ego, zin) {
				t.Errorf("%s: de individualist krijgt het toeschrijvingsvoorbeeld %q nog mee", lang, zin)
			}
			if !strings.Contains(bio, zin) {
				t.Errorf("%s: de kennispartner mist het toeschrijvingsvoorbeeld %q", lang, zin)
			}
		}
		// De reikwijdte zelf blijft: modaliteit en onderzochte situatie.
		if !strings.Contains(flat(ego), behoud[lang]) {
			t.Errorf("%s: de individualist verliest de onderzochte situatie uit de reikwijdte", lang)
		}
		if !strings.Contains(ego, evidenceScopeDirective[lang]) {
			t.Errorf("%s: de individualist mist het reikwijdtedirectief zelf", lang)
		}
		if strings.Contains(bio, individualistScopeExamples[lang]) {
			t.Errorf("%s: het gedeelde blok van de kennispartner is vervangen", lang)
		}
	}
}

// De kennispartner verandert niet mee: haar uitvoerregel blijft de hare, en
// de individualist-sturing komt nooit in haar prompt terecht.
func TestPrompt_BiologicalUntouchedByIndividualistRules(t *testing.T) {
	set := testPersonas(t)
	bio := mustGet(t, set, "biological")
	for _, lang := range []string{"en", "nl"} {
		for _, status := range []KBStatus{KBPersonaOnly, KBSources, KBGap} {
			got := BuildSystemPrompt(PromptInput{Persona: bio, Lang: lang, Status: status, RAGSection: "<research_sources/>"})
			if strings.Contains(got, individualistOutputDirective[lang]) {
				t.Errorf("%s/%s: het individualist-uitvoerdirectief hoort niet bij de kennispartner", lang, status)
			}
			if strings.Contains(got, gapSectionFor(mustGet(t, set, "individualist"), lang)) {
				t.Errorf("%s/%s: het individualist-gatdirectief hoort niet bij de kennispartner", lang, status)
			}
		}
		if !strings.Contains(BuildSystemPrompt(PromptInput{Persona: bio, Lang: lang, Status: KBSources, RAGSection: "<research_sources/>"}), biologicalOutputDirective[lang]) {
			t.Errorf("%s: de biologische uitvoerregel ontbreekt", lang)
		}
	}
}

// TestPrompt_LangParity: elke gedragseis is in de EN- én NL-variant van elk
// pratend persona-promptbestand aanwezig, via de eis-ID-sentinels.
func TestPrompt_LangParity(t *testing.T) {
	set := testPersonas(t)
	rules := map[string][]string{
		"biological":    {"P1", "P2", "P3", "P4", "G1", "G2", "G3", "R1", "R2", "R3"},
		"individualist": {"P1", "P2", "P3", "P4", "G4", "R1", "R2", "R3"},
	}
	for id, eisen := range rules {
		p := mustGet(t, set, id)
		for _, lang := range []string{"en", "nl"} {
			prompt := p.Prompt(lang)
			for _, eis := range eisen {
				if !strings.Contains(prompt, "eis:"+eis) {
					t.Errorf("%s (%s): sentinel eis:%s ontbreekt", id, lang, eis)
				}
			}
		}
	}
}

// De code-secties van de prompt-assemblage dragen dezelfde pariteitseis.
func TestPrompt_CodeSectionParity(t *testing.T) {
	sections := map[string]map[string]string{
		"basis":                  baseSection,
		"taaldirectief":          langDirective,
		"rag-context-header":     ragContextHeader,
		"citatie-instructie":     citationInstruction,
		"gap-directief":          gapDirective,
		"bio-uitvoer":            biologicalOutputDirective,
		"individualist-uitvoer":  individualistOutputDirective,
		"individualist-chatvorm": individualistChatFormDirective,
		"individualist-gat":      individualistGapDirective,
		"reikwijdte-vb-ego":      individualistScopeExamples,
		"reikwijdte":             evidenceScopeDirective,
		"reikwijdte-voorbeelden": evidenceScopeExamples,
		"historie-directief":     historyEvidenceDirective,
		"gespreksvervolg":        conversationFollowUpDirective,
		"identiteit-op-vraag":    identityDisclosureDirective,
		"verwante-bronnen":       relatedSourcesDirective,
		"vergader-verdieping":    meetingDeepenDirective,
		"vergader-niets-nieuw":   meetingNothingNewReply,
	}
	for name, m := range sections {
		for _, lang := range []string{"en", "nl"} {
			if strings.TrimSpace(m[lang]) == "" {
				t.Errorf("code-sectie %s mist taalvariant %s", name, lang)
			}
		}
	}
}

// --- facilitator -----------------------------------------------------------

// Dezelfde taalpariteit voor de facilitator: elke gedragseis staat in beide
// taalvarianten. Toetsing op eis-ID-sentinels, niet op zinnen — de lopende
// tekst mag redactioneel wijzigen.
func TestPrompt_FacilitatorLangParity(t *testing.T) {
	set := testPersonas(t)
	if set.Facilitator == nil {
		t.Fatal("de gepubliceerde configuratie hoort een facilitator te dragen")
	}
	eisen := []string{"F1", "F2", "F3", "F4", "F5", "F6", "P1", "P2", "P3", "R1", "R2"}
	for _, lang := range []string{"en", "nl"} {
		prompt := set.Facilitator.Prompt(lang)
		for _, eis := range eisen {
			if !strings.Contains(prompt, "eis:"+eis) {
				t.Errorf("facilitator (%s): sentinel eis:%s ontbreekt", lang, eis)
			}
		}
	}
}

// De facilitator-prompt noemt zijn eigen toolnamen: het model moet de tools
// aan de regels kunnen koppelen die erover gaan.
func TestPrompt_FacilitatorNamesItsTools(t *testing.T) {
	set := testPersonas(t)
	for _, lang := range []string{"en", "nl"} {
		prompt := set.Facilitator.Prompt(lang)
		for _, tool := range []string{toolAskPigeon, toolPigeonProfile, toolClarify, toolRedirectToPigeon} {
			if !strings.Contains(prompt, tool) {
				t.Errorf("facilitator (%s): de prompt noemt de tool %q niet", lang, tool)
			}
		}
	}
}

// De assemblage: eigen basissectie, promptbestand, taaldirectief — en geen
// duif-basissectie, want de facilitator is geen duifpersona.
func TestBuildFacilitatorPrompt_SectionsAndLanguage(t *testing.T) {
	set := testPersonas(t)
	for _, lang := range []string{"en", "nl"} {
		got := BuildFacilitatorPrompt(set.Facilitator, lang)
		iBase := strings.Index(got, facilitatorBaseSection[lang])
		iPrompt := strings.Index(got, set.Facilitator.Prompt(lang))
		iLang := strings.Index(got, langDirective[lang])
		if iBase < 0 || iPrompt < 0 || iLang < 0 {
			t.Fatalf("%s: sectie ontbreekt: basis=%d prompt=%d taal=%d", lang, iBase, iPrompt, iLang)
		}
		if !(iBase < iPrompt && iPrompt < iLang) {
			t.Errorf("%s: sectievolgorde fout: basis=%d prompt=%d taal=%d", lang, iBase, iPrompt, iLang)
		}
		if strings.Contains(got, baseSection[lang]) {
			t.Errorf("%s: de facilitator mag niet als duifpersona aangesproken worden", lang)
		}
		for _, sectie := range []string{ragContextHeader[lang], citationInstruction[lang], gapDirective[lang]} {
			if strings.Contains(got, sectie) {
				t.Errorf("%s: de facilitator heeft geen kennisbank en dus geen bronsecties", lang)
			}
		}
	}
	// Lege taal valt terug op Engels.
	if got := BuildFacilitatorPrompt(set.Facilitator, ""); !strings.Contains(got, langDirective["en"]) {
		t.Error("lege lang moet defaulten naar Engels")
	}
}

// De code-secties van de facilitator dragen dezelfde pariteitseis.
func TestPrompt_FacilitatorCodeSectionParity(t *testing.T) {
	sections := map[string]map[string]string{
		"facilitator-basis":    facilitatorBaseSection,
		"duif-contextaanhef":   pigeonContextHeader,
		"profiel-inleiding":    profileIntro,
		"ambient-aanhef":       ambientHeader,
		"ambient-taal":         ambientLang,
		"ambient-duif":         ambientPigeon,
		"ambient-wedervraag":   ambientClarified,
		"ambient-antwoorden":   ambientAnswersHeader,
		"ambient-vraaglabel":   ambientQuestionLabel,
		"ambient-antwoordabel": ambientAnswerLabel,
		"ambient-bemiddeling":  ambientMediation,
		"profiel-inleiding-kb": profileIntroKB,
		"profiel-themas":       profileThemes,
		"andere-duif-aanhef":   otherPigeonHeader,
		"eerdere-mens-aanhef":  previousHumanHeader,
	}
	for name, m := range sections {
		for _, lang := range []string{"en", "nl"} {
			if strings.TrimSpace(m[lang]) == "" {
				t.Errorf("code-sectie %s mist taalvariant %s", name, lang)
			}
		}
	}
}

// --- de facilitator noemt de duif "de duif" ---------------------------------

// quotedPronounRule zijn de aanhalingen waarin de naamgevingsregel de verboden
// vormen zélf moet noemen om ze te kunnen verbieden. Alleen deze exacte
// aanhalingen vallen weg — niet elk geciteerd fragment, want een langere
// geciteerde passage zou dan een echte "her" kunnen verstoppen.
var quotedPronounRule = []string{
	`"she"`, `"her"`, `"he"`, `"zij"`, `"haar"`, `"hij"`,
	"\u201cshe\u201d", "\u201cher\u201d", "\u201che\u201d",
	"\u201czij\u201d", "\u201chaar\u201d", "\u201chij\u201d",
	"\u2018she\u2019", "\u2018her\u2019", "\u2018zij\u2019", "\u2018haar\u2019",
}

// stripPronounRule haalt uitsluitend die aanhalingen weg.
func stripPronounRule(text string) string {
	for _, quoted := range quotedPronounRule {
		text = strings.ReplaceAll(text, quoted, " ")
	}
	return text
}

// pigeonPronouns geeft de verboden aanspreekvormen terug die in de tekst
// staan: alles wat de duif tot "zij" maakt in plaats van tot "de duif".
func pigeonPronouns(text string) []string {
	var found []string
	fields := strings.FieldsFunc(strings.ToLower(stripPronounRule(text)), func(r rune) bool {
		return !(r >= 'a' && r <= 'z')
	})
	for _, w := range fields {
		switch w {
		case "she", "her", "hers", "zij", "haar":
			found = append(found, w)
		}
	}
	return found
}

// schemaStrings vlakt een tooldefinitie uit tot de teksten die het model leest:
// de beschrijving en elke string in het argumentschema. Via JSON en terug,
// zodat de aanhalingstekens in de tekst weer echte aanhalingstekens zijn en de
// naamgevingsregel herkenbaar blijft.
func schemaStrings(t *testing.T, fn any) []string {
	t.Helper()
	blob, err := json.Marshal(fn)
	if err != nil {
		t.Fatalf("tooldefinitie serialiseren: %v", err)
	}
	var tree any
	if err := json.Unmarshal(blob, &tree); err != nil {
		t.Fatalf("tooldefinitie lezen: %v", err)
	}
	var out []string
	var walk func(any)
	walk = func(node any) {
		switch v := node.(type) {
		case string:
			out = append(out, v)
		case []any:
			for _, item := range v {
				walk(item)
			}
		case map[string]any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(tree)
	return out
}

// Elke tekst die over de duif spreekt en bij de mens terechtkomt — het
// promptbestand van de facilitator, de ambient-context, de persona-aanwijzing,
// de profielzinnen, de vaste omleidingen en de storingsmeldingen — noemt haar
// "the pigeon" / "de duif" en nooit "her" of "haar". Wat de mens leest komt
// hiervandaan, dus dit is de plek waar de aanspreekvorm vastligt.
func TestFacilitatorSteering_CallsThePigeonThePigeon(t *testing.T) {
	set := testPersonas(t)
	texts := map[string]string{}
	for _, lang := range []string{"en", "nl"} {
		texts["promptbestand ("+lang+")"] = set.Facilitator.Prompt(lang)
		texts["facilitator-basis ("+lang+")"] = facilitatorBaseSection[lang]
		texts["ambient-bemiddeling ("+lang+")"] = ambientMediation[lang]
		texts["ambient-antwoorden ("+lang+")"] = ambientAnswersHeader[lang]
		texts["ambient-antwoordlabel ("+lang+")"] = ambientAnswerLabel[lang]
		texts["ambient-duif ("+lang+")"] = ambientPigeon[lang]
		texts["ambient-wedervraag ("+lang+")"] = ambientClarified[lang]
		texts["andere-duif-aanhef ("+lang+")"] = otherPigeonHeader[lang]
		texts["eerdere-mens-aanhef ("+lang+")"] = previousHumanHeader[lang]
		texts["ambient-vraaglabel ("+lang+")"] = ambientQuestionLabel[lang]
		texts["duif-contextaanhef ("+lang+")"] = pigeonContextHeader[lang]
		texts["gat-herinnering ("+lang+")"] = gapReminder[lang]
		texts["redirect-buiten-project ("+lang+")"] = redirectOutsideProject[lang]
		texts["redirect-overschrijfpoging ("+lang+")"] = redirectOverrideAttempt[lang]
		for reason, bericht := range errorMessages {
			texts["foutmelding "+reason+" ("+lang+")"] = bericht[lang]
		}
		texts["profiel-inleiding ("+lang+")"] = profileIntro[lang]
		texts["profiel-inleiding-kb ("+lang+")"] = profileIntroKB[lang]
		texts["profiel-themas ("+lang+")"] = profileThemes[lang]
		for _, persona := range []string{"biological", "individualist"} {
			texts["persona-directief "+persona+" ("+lang+")"] = facilitatorPersonaDirective(persona, "Knowledge Partner", lang)
		}
	}
	for naam, tekst := range texts {
		if got := pigeonPronouns(tekst); len(got) > 0 {
			t.Errorf("%s spreekt over de duif als %v; gebruik \"the pigeon\" / \"de duif\"", naam, got)
		}
	}
}

// Dezelfde eis op de toolset: beschrijvingen en argumentteksten zijn sturing
// die het model letterlijk leest, en de kaderzin komt rechtstreeks uit het
// argument `framing`.
func TestFacilitatorTools_CallThePigeonThePigeon(t *testing.T) {
	at, _ := newTestAgentTurn(t, "en")
	reg, err := newAgentRegistry(at)
	if err != nil {
		t.Fatalf("newAgentRegistry: %v", err)
	}
	for _, tool := range reg.Definitions() {
		for _, tekst := range schemaStrings(t, tool.Function) {
			if got := pigeonPronouns(tekst); len(got) > 0 {
				t.Errorf("tool %q spreekt over de duif als %v: %q", tool.Function.Name, got, tekst)
			}
		}
	}
}

// --- de slotbeurt mag met één reflectievraag eindigen ----------------------

// Het oude directief eiste "end without a question"; dat verbod is opgeheven
// en vervangen door de ruimte voor precies één vraag aan de mens. Beide talen
// dragen dezelfde regel.
func TestFacilitatorMediation_AllowsOneClosingQuestion(t *testing.T) {
	verboden := map[string]string{"en": "without a question", "nl": "zonder vraag"}
	toegestaan := map[string]string{"en": "one short question", "nl": "één korte vraag"}
	plafond := map[string]string{"en": "never more than one", "nl": "meer dan één"}
	for _, lang := range []string{"en", "nl"} {
		directief := ambientMediation[lang]
		if strings.Contains(directief, verboden[lang]) {
			t.Errorf("ambientMediation (%s) verbiedt de slotvraag nog steeds", lang)
		}
		if !strings.Contains(directief, toegestaan[lang]) {
			t.Errorf("ambientMediation (%s) laat de ene slotvraag niet toe", lang)
		}
		if !strings.Contains(directief, plafond[lang]) {
			t.Errorf("ambientMediation (%s) begrenst de slotvraag niet op één", lang)
		}
	}
}

// Het facilitator-promptbestand draagt dezelfde ruimte zelf: het directief zegt wanneer het
// moment daar is, de prompt zegt wat de rol is. Lopen ze uiteen, dan kiest het
// model zelf maar wat.
func TestPrompt_FacilitatorPromptAllowsOneClosingQuestion(t *testing.T) {
	set := testPersonas(t)
	toegestaan := map[string]string{"en": "one short question", "nl": "één korte vraag"}
	verboden := map[string]string{"en": "end without a coaching question", "nl": "eindig zonder coachende"}
	for _, lang := range []string{"en", "nl"} {
		prompt := set.Facilitator.Prompt(lang)
		if !strings.Contains(prompt, toegestaan[lang]) {
			t.Errorf("facilitator (%s): de prompt noemt de ene slotvraag niet", lang)
		}
		if strings.Contains(prompt, verboden[lang]) {
			t.Errorf("facilitator (%s): de prompt verbiedt de slotvraag nog steeds", lang)
		}
	}
}

// Geen enkele duifnaam staat in Go vastgelegd. Hernoem beide duiven
// in de configuratie en de prompts moeten meebewegen — het interventiekader
// aan de vergadertafel, het gatdirectief van de standpunt-duif en de
// tooluitleg van de facilitator. De oude namen mogen nergens meer opduiken.
func TestPrompt_PersonaNamesFollowTheConfiguration(t *testing.T) {
	fsys := minimalConfig(func(m fstest.MapFS) {
		yaml := string(m["personas.yaml"].Data)
		yaml = strings.Replace(yaml,
			`naam: { en: "Knowledge Partner", nl: "Kennispartner" }`,
			`naam: { en: "Archive Pigeon", nl: "Archiefduif" }`, 1)
		yaml = strings.Replace(yaml,
			`naam: { en: "Stakeholder", nl: "Stakeholder" }`,
			`naam: { en: "City Pigeon", nl: "Stadsduif" }`, 1)
		m["personas.yaml"] = &fstest.MapFile{Data: []byte(yaml)}
	})
	set, err := LoadPersonas(fsys)
	if err != nil {
		t.Fatalf("LoadPersonas: %v", err)
	}
	ego := mustGet(t, set, "individualist")

	nieuw := map[string][]string{
		"en": {"Archive Pigeon", "City Pigeon"},
		"nl": {"archiefduif", "stadsduif"},
	}
	oud := []string{"Knowledge Partner", "kennispartner", "Stakeholder", "stakeholder"}

	for _, lang := range []string{"en", "nl"} {
		interventie := PromptInput{Persona: ego, Personas: set, Lang: lang,
			Status: KBPersonaOnly, Meeting: true, Intervention: true}
		gat := PromptInput{Persona: ego, Personas: set, Lang: lang, Status: KBGap}
		prompts := map[string]string{
			"interventiekader": BuildSystemPrompt(interventie),
			"gatdirectief":     BuildSystemPrompt(gat),
		}
		for naam, prompt := range prompts {
			for _, want := range nieuw[lang] {
				if naam == "gatdirectief" && want != nieuw[lang][1] {
					continue // het gatdirectief noemt alleen de standpunt-duif
				}
				if !strings.Contains(prompt, want) {
					t.Errorf("%s/%s: de nieuwe naam %q ontbreekt", lang, naam, want)
				}
			}
			for _, niet := range oud {
				if strings.Contains(prompt, niet) {
					t.Errorf("%s/%s: de oude naam %q staat er nog in", lang, naam, niet)
				}
			}
		}
	}

	// Dezelfde eis geldt voor de tooluitleg die de facilitator leest.
	tool := askPigeonToolDescription(set)
	for _, want := range []string{"Archive Pigeon", "City Pigeon"} {
		if !strings.Contains(tool, want) {
			t.Errorf("tooluitleg ask_pigeon mist de naam %q", want)
		}
	}
	for _, niet := range oud {
		if strings.Contains(tool, niet) {
			t.Errorf("tooluitleg ask_pigeon draagt de oude naam %q nog", niet)
		}
	}
}
