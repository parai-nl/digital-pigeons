package duif

import (
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

// minimalConfig bouwt een geldige config-fixture die tests per geval kapot
// kunnen maken. Inhoud is testdata, geen productieprompt.
func minimalConfig(mutate func(m fstest.MapFS)) fstest.MapFS {
	m := fstest.MapFS{
		"personas.yaml": &fstest.MapFile{Data: []byte(`personas:
  - id: biological
    naam: { en: "Knowledge Partner", nl: "Kennispartner" }
    voice_id: "placeholder-voice"
    prompts: { en: prompts/biological.en.md, nl: prompts/biological.nl.md }
    kennisbank: true
    chat: true
    vergadering: true
    spreeklat:
      prompts: { en: prompts/speakup.en.md, nl: prompts/speakup.nl.md }
      drempel: 0.55
      cooldown_seconden: 45
      beleefdheid: true
    topics:
      - id: nesting
        label: { en: "Nesting", nl: "Nestelen" }
        beschrijving: { en: "where pigeons nest", nl: "waar duiven nestelen" }
        gevoeligheid: 0.5
  - id: individualist
    naam: { en: "Stakeholder", nl: "Stakeholder" }
    voice_id: "placeholder-voice"
    prompts: { en: prompts/individualist.en.md, nl: prompts/individualist.nl.md }
    kennisbank: false
    chat: true
    vergadering: true
    spreeklat:
      prompts: { en: prompts/speakup.en.md, nl: prompts/speakup.nl.md }
      drempel: 0.45
      cooldown_seconden: 45
      beleefdheid: true
      zonder_bron: true
    topics:
      - id: rights
        label: { en: "Rights", nl: "Rechten" }
        beschrijving: { en: "what pigeons are owed", nl: "waar duiven recht op hebben" }
        gevoeligheid: 0.9
gedrag:
  interventie_zelfstandig: false
  interventie_throttle_seconden: 180
  inactiviteit_prompt_seconden: 600
  inactiviteit_sluit_seconden: 120
  vergadering_max_seconden: 7200
  taal_default: en
`)},
		"prompts/biological.en.md":    &fstest.MapFile{Data: []byte("bio prompt en")},
		"prompts/biological.nl.md":    &fstest.MapFile{Data: []byte("bio prompt nl")},
		"prompts/individualist.en.md": &fstest.MapFile{Data: []byte("ego prompt en")},
		"prompts/individualist.nl.md": &fstest.MapFile{Data: []byte("ego prompt nl")},
		"prompts/speakup.en.md":       &fstest.MapFile{Data: []byte("speak bar en")},
		"prompts/speakup.nl.md":       &fstest.MapFile{Data: []byte("speak bar nl")},
	}
	if mutate != nil {
		mutate(m)
	}
	return m
}

func TestLoadEmbeddedPersonas_ThreePigeons(t *testing.T) {
	set, err := LoadEmbeddedPersonas()
	if err != nil {
		t.Fatalf("LoadEmbeddedPersonas: %v", err)
	}
	if got := len(set.Personas); got != 3 {
		t.Fatalf("want 3 personas, got %d", got)
	}
	tests := []struct {
		id                     string
		kennisbank, chat, verg bool
		voiceIDEmpty           bool
	}{
		{"biological", true, true, true, false},
		{"individualist", true, true, true, false},
		{"nonverbal", false, false, true, true},
	}
	for _, tc := range tests {
		p, ok := set.Get(tc.id)
		if !ok {
			t.Fatalf("persona %q ontbreekt", tc.id)
		}
		if p.Kennisbank != tc.kennisbank {
			t.Errorf("%s: kennisbank = %v, want %v", tc.id, p.Kennisbank, tc.kennisbank)
		}
		if p.Chat != tc.chat {
			t.Errorf("%s: chat = %v, want %v", tc.id, p.Chat, tc.chat)
		}
		if p.Vergadering != tc.verg {
			t.Errorf("%s: vergadering = %v, want %v", tc.id, p.Vergadering, tc.verg)
		}
		if (p.VoiceID == "") != tc.voiceIDEmpty {
			t.Errorf("%s: voice_id = %q, want leeg=%v", tc.id, p.VoiceID, tc.voiceIDEmpty)
		}
		for _, lang := range []string{"en", "nl"} {
			if strings.TrimSpace(p.Prompt(lang)) == "" {
				t.Errorf("%s: prompt %s is leeg", tc.id, lang)
			}
		}
		if p.Naam.EN == "" || p.Naam.NL == "" {
			t.Errorf("%s: naam ontbreekt (en=%q nl=%q)", tc.id, p.Naam.EN, p.Naam.NL)
		}
	}
}

func TestLoadEmbeddedPersonas_Gedrag(t *testing.T) {
	set, err := LoadEmbeddedPersonas()
	if err != nil {
		t.Fatalf("LoadEmbeddedPersonas: %v", err)
	}
	g := set.Gedrag
	if g.InterventieZelfstandig {
		t.Error("de gedeelde terugval voor interventie_zelfstandig moet uit staan")
	}
	if g.InterventieThrottleSeconden != 0 {
		t.Errorf("interventie_throttle_seconden = %d, want 0 (de duif houdt haar eigen rust)", g.InterventieThrottleSeconden)
	}
	// De drie klokken van "Ben je er nog?": tien minuten zonder
	// bediening, twee minuten aftelling, twee uur plafond.
	if g.InactiviteitPromptSeconden != 600 {
		t.Errorf("inactiviteit_prompt_seconden = %d, want 600", g.InactiviteitPromptSeconden)
	}
	if g.InactiviteitSluitSeconden != 120 {
		t.Errorf("inactiviteit_sluit_seconden = %d, want 120", g.InactiviteitSluitSeconden)
	}
	if g.VergaderingMaxSeconden != 7200 {
		t.Errorf("vergadering_max_seconden = %d, want 7200", g.VergaderingMaxSeconden)
	}
	if g.TaalDefault != "en" {
		t.Errorf("taal_default = %q, want en (Engels is de standaardtaal)", g.TaalDefault)
	}
}

// De standaardstand van zelfstandig meedoen hoort bij de duif, niet bij de
// vergadering: de signalerende duif doet vanzelf mee — zij heeft
// geen woorden en zou anders helemaal stilvallen — en de twee pratende duiven
// wachten op een uitnodiging. Een duif die zelf niets zegt valt terug op de
// gedeelde stand uit `gedrag:`.
func TestLoadEmbeddedPersonas_ZelfstandigMeedoenPerDuif(t *testing.T) {
	set, err := LoadEmbeddedPersonas()
	if err != nil {
		t.Fatalf("LoadEmbeddedPersonas: %v", err)
	}
	want := map[string]bool{"biological": false, "individualist": false, "nonverbal": true}
	for id, expected := range want {
		p, ok := set.Get(id)
		if !ok {
			t.Fatalf("duif %q ontbreekt in de configuratie", id)
		}
		if got := p.AutonomousDefault(set.Gedrag.InterventieZelfstandig); got != expected {
			t.Errorf("duif %q: zelfstandig meedoen staat standaard %v, want %v", id, got, expected)
		}
	}
	// Zonder eigen stand beslist de gedeelde terugval, en die blijft een
	// waarde die de duif niet overschrijft met een stille false.
	naked := &Persona{ID: "naamloos"}
	if naked.AutonomousDefault(true) != true || naked.AutonomousDefault(false) != false {
		t.Error("een duif zonder eigen stand hoort de gedeelde terugval te volgen")
	}
}

func TestLoadPersonas_UnknownID(t *testing.T) {
	set, err := LoadPersonas(minimalConfig(nil))
	if err != nil {
		t.Fatalf("LoadPersonas: %v", err)
	}
	if _, ok := set.Get("flamingo"); ok {
		t.Error("Get(flamingo) moet false geven")
	}
}

func TestLoadPersonas_MissingPromptFile(t *testing.T) {
	fsys := minimalConfig(func(m fstest.MapFS) {
		delete(m, "prompts/individualist.nl.md")
	})
	if _, err := LoadPersonas(fsys); err == nil {
		t.Fatal("ontbrekende promptfile moet een Init-fout zijn (fail-fast)")
	}
}

func TestLoadPersonas_EmptyPromptFile(t *testing.T) {
	fsys := minimalConfig(func(m fstest.MapFS) {
		m["prompts/biological.en.md"] = &fstest.MapFile{Data: []byte("  \n")}
	})
	if _, err := LoadPersonas(fsys); err == nil {
		t.Fatal("lege promptfile moet een fout zijn")
	}
}

// Een spreektempo buiten wat de TTS-dienst accepteert weigert zij
// met invalid_request. Dat mag niet pas bij de eerste spreekbeurt blijken.
func TestLoadPersonas_TempoOutsideTheSupportedRange(t *testing.T) {
	// .nan hoort erbij: elke vergelijking met NaN is onwaar, dus een controle
	// met twee losse vergelijkingen liet hem door — en daarna past hij niet in
	// het JSON-openingsbericht, zodat élke spreekbeurt stukliep.
	for _, tempo := range []string{"0.4", "1.9", ".nan", ".inf", "-.inf"} {
		fsys := minimalConfig(func(m fstest.MapFS) {
			y := string(m["personas.yaml"].Data)
			y = strings.Replace(y, "id: biological", "id: biological\n    spreektempo: "+tempo, 1)
			m["personas.yaml"] = &fstest.MapFile{Data: []byte(y)}
		})
		_, err := LoadPersonas(fsys)
		if err == nil {
			t.Errorf("spreektempo %s moet een bootfout zijn", tempo)
			continue
		}
		if !strings.Contains(err.Error(), "spreektempo") {
			t.Errorf("fout bij spreektempo %s = %v, verwacht een melding over het spreektempo", tempo, err)
		}
	}
}

func TestLoadPersonas_DuplicateID(t *testing.T) {
	fsys := minimalConfig(func(m fstest.MapFS) {
		y := string(m["personas.yaml"].Data)
		y = strings.ReplaceAll(y, "id: biological", "id: individualist")
		m["personas.yaml"] = &fstest.MapFile{Data: []byte(y)}
	})
	if _, err := LoadPersonas(fsys); err == nil {
		t.Fatal("duplicaat-id moet een fout zijn")
	}
}

func TestLoadPersonas_InvalidID(t *testing.T) {
	fsys := minimalConfig(func(m fstest.MapFS) {
		y := string(m["personas.yaml"].Data)
		y = strings.ReplaceAll(y, "id: biological", `id: "Biological Duif!"`)
		m["personas.yaml"] = &fstest.MapFile{Data: []byte(y)}
	})
	if _, err := LoadPersonas(fsys); err == nil {
		t.Fatal("niet-URL-safe id moet een fout zijn")
	}
}

func TestLoadPersonas_MissingLanguageVariant(t *testing.T) {
	fsys := minimalConfig(func(m fstest.MapFS) {
		y := string(m["personas.yaml"].Data)
		y = strings.ReplaceAll(y, "prompts: { en: prompts/individualist.en.md, nl: prompts/individualist.nl.md }", "prompts: { en: prompts/individualist.en.md }")
		m["personas.yaml"] = &fstest.MapFile{Data: []byte(y)}
	})
	if _, err := LoadPersonas(fsys); err == nil {
		t.Fatal("ontbrekende taalvariant (EN én NL verplicht) moet een fout zijn")
	}
}

func TestLoadPersonas_MissingConfigFile(t *testing.T) {
	if _, err := LoadPersonas(fstest.MapFS{}); err == nil {
		t.Fatal("ontbrekende personas.yaml moet een fout zijn (fail-fast)")
	}
}

func TestLoadPersonas_UnparseableYAML(t *testing.T) {
	fsys := minimalConfig(func(m fstest.MapFS) {
		m["personas.yaml"] = &fstest.MapFile{Data: []byte("personas: [ dit is: geen geldige yaml")}
	})
	if _, err := LoadPersonas(fsys); err == nil {
		t.Fatal("onparsebare yaml moet een fout zijn")
	}
}

func TestLoadPersonas_NoPersonas(t *testing.T) {
	fsys := minimalConfig(func(m fstest.MapFS) {
		m["personas.yaml"] = &fstest.MapFile{Data: []byte("gedrag:\n  taal_default: en\n")}
	})
	if _, err := LoadPersonas(fsys); err == nil {
		t.Fatal("een config zonder personas moet een fout zijn")
	}
}

// --- themalijst --------------------------------------------------------------

// Elke duif die aan tafel kan, heeft thema's — anders kan zij nooit aanslaan en
// is de duif stil zonder dat iemand weet waarom.
func TestLoadEmbeddedPersonas_EveryMeetingPigeonHasUsableTopics(t *testing.T) {
	set, err := LoadEmbeddedPersonas()
	if err != nil {
		t.Fatalf("LoadEmbeddedPersonas: %v", err)
	}
	for _, p := range set.Personas {
		if !p.Vergadering {
			continue
		}
		if len(p.Topics) < 2 {
			t.Errorf("%s heeft %d thema's; een detectielaag met één thema is geen detectielaag", p.ID, len(p.Topics))
		}
		for _, tp := range p.Topics {
			if tp.LabelFor("en") == "" || tp.LabelFor("nl") == "" {
				t.Errorf("%s/%s: label ontbreekt in een van beide talen", p.ID, tp.ID)
			}
			if tp.DescribeFor("en") == tp.DescribeFor("nl") {
				t.Errorf("%s/%s: EN- en NL-beschrijving zijn identiek — een van beide is niet vertaald", p.ID, tp.ID)
			}
			if tp.Gevoeligheid <= 0 || tp.Gevoeligheid > 1 {
				t.Errorf("%s/%s: gevoeligheid %v ligt buiten (0,1]", p.ID, tp.ID, tp.Gevoeligheid)
			}
		}
		if _, ok := p.Topic("bestaat-niet"); ok {
			t.Errorf("%s: Topic() vindt een thema dat er niet is", p.ID)
		}
		if got, ok := p.Topic(p.Topics[0].ID); !ok || got.ID != p.Topics[0].ID {
			t.Errorf("%s: Topic(%q) vindt het eigen thema niet", p.ID, p.Topics[0].ID)
		}
	}
}

func TestLoadEmbeddedPersonas_DetectionKnobs(t *testing.T) {
	set, err := LoadEmbeddedPersonas()
	if err != nil {
		t.Fatalf("LoadEmbeddedPersonas: %v", err)
	}
	g := set.Gedrag
	if g.DetectieGretigheid != EagernessNormal {
		t.Errorf("detectie_gretigheid = %q, want %q", g.DetectieGretigheid, EagernessNormal)
	}
	if g.DetectieVensterWoorden != 3 || g.DetectieIntervalSeconden != 6 {
		t.Errorf("cadans = %d woorden / %d s, want 3 / 6", g.DetectieVensterWoorden, g.DetectieIntervalSeconden)
	}
	if g.StilteMilliseconden != 1500 {
		t.Errorf("stilte_milliseconden = %d, want 1500", g.StilteMilliseconden)
	}
	if g.InterventieThrottleSeconden != 0 {
		t.Errorf("interventie_throttle_seconden = %d, want 0 (de duif houdt haar eigen rust)", g.InterventieThrottleSeconden)
	}
	if g.ArousalHalveringSeconden != 120 {
		t.Errorf("arousal_halvering_seconden = %d, want 120", g.ArousalHalveringSeconden)
	}
}

func TestLoadPersonas_TopicValidation(t *testing.T) {
	const goodTopic = `    topics:
      - id: rights
        label: { en: "Rights", nl: "Rechten" }
        beschrijving: { en: "what pigeons are owed", nl: "waar duiven recht op hebben" }
        gevoeligheid: 0.9
`
	tests := []struct {
		name    string
		replace string
	}{
		{"vergaderduif zonder thema's", ""},
		{"niet-URL-safe topic-id", `    topics:
      - id: "Rights!"
        label: { en: "Rights", nl: "Rechten" }
        beschrijving: { en: "a", nl: "b" }
        gevoeligheid: 0.9
`},
		{"duplicaat topic-id", `    topics:
      - id: rights
        label: { en: "Rights", nl: "Rechten" }
        beschrijving: { en: "a", nl: "b" }
        gevoeligheid: 0.9
      - id: rights
        label: { en: "Rights again", nl: "Rechten opnieuw" }
        beschrijving: { en: "a", nl: "b" }
        gevoeligheid: 0.9
`},
		{"label mist een taal", `    topics:
      - id: rights
        label: { en: "Rights" }
        beschrijving: { en: "a", nl: "b" }
        gevoeligheid: 0.9
`},
		{"beschrijving mist een taal", `    topics:
      - id: rights
        label: { en: "Rights", nl: "Rechten" }
        beschrijving: { en: "a", nl: "  " }
        gevoeligheid: 0.9
`},
		{"gevoeligheid ontbreekt", `    topics:
      - id: rights
        label: { en: "Rights", nl: "Rechten" }
        beschrijving: { en: "a", nl: "b" }
`},
		{"gevoeligheid boven 1", `    topics:
      - id: rights
        label: { en: "Rights", nl: "Rechten" }
        beschrijving: { en: "a", nl: "b" }
        gevoeligheid: 1.4
`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fsys := minimalConfig(func(m fstest.MapFS) {
				y := strings.Replace(string(m["personas.yaml"].Data), goodTopic, tc.replace, 1)
				m["personas.yaml"] = &fstest.MapFile{Data: []byte(y)}
			})
			if _, err := LoadPersonas(fsys); err == nil {
				t.Fatal("dit hoort een bootfout te zijn (fail-fast)")
			}
		})
	}
}

// Een duif die niet aan tafel zit heeft geen thema's nodig.
func TestLoadPersonas_NonMeetingPigeonNeedsNoTopics(t *testing.T) {
	fsys := minimalConfig(func(m fstest.MapFS) {
		y := string(m["personas.yaml"].Data)
		y = strings.Replace(y, `    vergadering: true
    spreeklat:
      prompts: { en: prompts/speakup.en.md, nl: prompts/speakup.nl.md }
      drempel: 0.45
      cooldown_seconden: 45
      beleefdheid: true
      zonder_bron: true
    topics:
      - id: rights
        label: { en: "Rights", nl: "Rechten" }
        beschrijving: { en: "what pigeons are owed", nl: "waar duiven recht op hebben" }
        gevoeligheid: 0.9
`, "    vergadering: false\n", 1)
		m["personas.yaml"] = &fstest.MapFile{Data: []byte(y)}
	})
	set, err := LoadPersonas(fsys)
	if err != nil {
		t.Fatalf("een chat-only duif zonder thema's is geldig: %v", err)
	}
	p, _ := set.Get("individualist")
	if len(p.Topics) != 0 {
		t.Errorf("verwacht geen thema's, kreeg %d", len(p.Topics))
	}
}

func TestLoadPersonas_UnknownEagerness(t *testing.T) {
	fsys := minimalConfig(func(m fstest.MapFS) {
		y := string(m["personas.yaml"].Data) + "  detectie_gretigheid: razend\n"
		m["personas.yaml"] = &fstest.MapFile{Data: []byte(y)}
	})
	if _, err := LoadPersonas(fsys); err == nil {
		t.Fatal("een onbekende gretigheidsstand moet een bootfout zijn")
	}
}

// halveNaam pakt een `naam: { en: ..., nl: ... }` en houdt alleen de Engelse
// helft over.
var halveNaam = regexp.MustCompile(`naam: \{ en: "([^"]+)", nl: "[^"]+" \}`)

// Elke duif heeft een EN- én NL-naam: een halve naam zou in de UI als lege
// duif landen.
func TestLoadPersonas_MissingName(t *testing.T) {
	fsys := minimalConfig(func(m fstest.MapFS) {
		// Knip de NL-helft uit de eerste naamregel. Bewust op vorm en niet op
		// de naam zelf: een hernoeming mag deze test niet breken.
		y := halveNaam.ReplaceAllString(string(m["personas.yaml"].Data), `naam: { en: "$1" }`)
		m["personas.yaml"] = &fstest.MapFile{Data: []byte(y)}
	})
	if _, err := LoadPersonas(fsys); err == nil {
		t.Fatal("ontbrekende NL-naam moet een fout zijn (EN én NL verplicht)")
	}
}

// --- engine en facilitator ------------------------------------------------

// De gepubliceerde configuratie zet de motor per duif: de kennispartner op
// de pipeline, de individualistische op de facilitator.
func TestLoadEmbeddedPersonas_EnginePerPigeon(t *testing.T) {
	set, err := LoadEmbeddedPersonas()
	if err != nil {
		t.Fatalf("LoadEmbeddedPersonas: %v", err)
	}
	want := map[string]string{"biological": EngineAgent, "individualist": EngineAgent, "nonverbal": EnginePipeline}
	for id, engine := range want {
		p, ok := set.Get(id)
		if !ok {
			t.Fatalf("persona %q ontbreekt", id)
		}
		if p.EngineOr() != engine {
			t.Errorf("%s: engine = %q, want %q", id, p.EngineOr(), engine)
		}
	}
}

// Een onbekende motor is een bootfout: hij zou anders stil op de pipeline
// terugvallen en dan mist de duif haar facilitator zonder dat iemand het ziet.
func TestLoadPersonas_UnknownEngine(t *testing.T) {
	fsys := minimalConfig(func(m fstest.MapFS) {
		y := string(m["personas.yaml"].Data)
		y = strings.ReplaceAll(y, "    kennisbank: false\n", "    kennisbank: false\n    engine: turbo\n")
		m["personas.yaml"] = &fstest.MapFile{Data: []byte(y)}
	})
	if _, err := LoadPersonas(fsys); err == nil {
		t.Fatal("een onbekende engine moet een bootfout zijn (fail-fast)")
	}
}

// Een lege engine blijft de pipeline: bestaande configuratie zonder het veld
// verandert niet van gedrag.
func TestLoadPersonas_EmptyEngineIsPipeline(t *testing.T) {
	set, err := LoadPersonas(minimalConfig(nil))
	if err != nil {
		t.Fatalf("LoadPersonas: %v", err)
	}
	p, _ := set.Get("individualist")
	if p.EngineOr() != EnginePipeline {
		t.Errorf("engine = %q, want %q", p.EngineOr(), EnginePipeline)
	}
	if set.Facilitator != nil {
		t.Error("zonder agent-duif hoort er geen facilitator geladen te zijn")
	}
}

// Een agent-duif zonder facilitator-blok is een bootfout: die duif zou pas bij
// de eerste chatbeurt zonder stem blijken te zitten.
func TestLoadPersonas_AgentEngineWithoutFacilitatorFails(t *testing.T) {
	fsys := minimalConfig(func(m fstest.MapFS) {
		y := string(m["personas.yaml"].Data)
		y = strings.ReplaceAll(y, "    kennisbank: false\n", "    kennisbank: false\n    engine: agent\n")
		m["personas.yaml"] = &fstest.MapFile{Data: []byte(y)}
	})
	if _, err := LoadPersonas(fsys); err == nil {
		t.Fatal("engine: agent zonder facilitator-blok moet een bootfout zijn")
	}
}

// Het facilitator-blok draagt dezelfde prompt-eisen als een persona: beide
// talen, gevulde bestanden. Een naam heeft hij niet — die komt uit de
// frontend-i18n.
func TestLoadPersonas_FacilitatorValidation(t *testing.T) {
	metFacilitator := func(blok string) fstest.MapFS {
		return minimalConfig(func(m fstest.MapFS) {
			m["personas.yaml"] = &fstest.MapFile{Data: []byte(string(m["personas.yaml"].Data) + blok)}
			m["prompts/facilitator.en.md"] = &fstest.MapFile{Data: []byte("facilitator en")}
			m["prompts/facilitator.nl.md"] = &fstest.MapFile{Data: []byte("facilitator nl")}
		})
	}
	const goedBlok = `
facilitator:
  prompts: { en: prompts/facilitator.en.md, nl: prompts/facilitator.nl.md }
`
	set, err := LoadPersonas(metFacilitator(goedBlok))
	if err != nil {
		t.Fatalf("geldig facilitator-blok: %v", err)
	}
	if set.Facilitator == nil {
		t.Fatal("facilitator niet geladen")
	}
	if set.Facilitator.Prompt("nl") != "facilitator nl" || set.Facilitator.Prompt("en") != "facilitator en" {
		t.Errorf("facilitator = %+v", set.Facilitator)
	}
	// De facilitator is geen duif: hij hoort niet in de personalijst.
	if _, ok := set.Get("facilitator"); ok {
		t.Error("de facilitator mag geen persona zijn (hij staat niet in /v1/duiven)")
	}

	kapot := map[string]string{
		"zonder prompts": `
facilitator: {}
`,
		"zonder NL-prompt": `
facilitator:
  prompts: { en: prompts/facilitator.en.md }
`,
		"promptfile bestaat niet": `
facilitator:
  prompts: { en: prompts/weg.en.md, nl: prompts/facilitator.nl.md }
`,
	}
	for naam, blok := range kapot {
		if _, err := LoadPersonas(metFacilitator(blok)); err == nil {
			t.Errorf("%s: moet een bootfout zijn", naam)
		}
	}
}

// De zichtbare rol van de facilitator staat per duif in de configuratie.
// Rond de individualist bemiddelt hij hardop; rond de kennispartner blijft hij
// bewust intern.
func TestLoadEmbeddedPersonas_FacilitatorRolePerPigeon(t *testing.T) {
	set, err := LoadEmbeddedPersonas()
	if err != nil {
		t.Fatalf("LoadEmbeddedPersonas: %v", err)
	}
	want := map[string]FacilitatorRole{
		"biological":    {Framing: false, Mediation: false},
		"individualist": {Framing: true, Mediation: true},
	}
	for id, role := range want {
		p, ok := set.Get(id)
		if !ok {
			t.Fatalf("persona %q ontbreekt", id)
		}
		if p.FacilitatorFraming() != role.Framing || p.FacilitatorMediation() != role.Mediation {
			t.Errorf("%s: framing=%v mediation=%v, want %+v", id, p.FacilitatorFraming(), p.FacilitatorMediation(), role)
		}
	}
	if p, _ := set.Get("nonverbal"); p.FacilitatorRole != nil {
		t.Error("een duif zonder chat heeft geen facilitator-rol")
	}
}

// Zonder blok blijft de facilitator stil: bestaande configuratie zonder het
// veld verandert niet van gedrag.
func TestLoadPersonas_MissingFacilitatorRoleIsSilent(t *testing.T) {
	set, err := LoadPersonas(minimalConfig(nil))
	if err != nil {
		t.Fatalf("LoadPersonas: %v", err)
	}
	p, _ := set.Get("individualist")
	if p.FacilitatorFraming() || p.FacilitatorMediation() {
		t.Errorf("zonder blok hoort de facilitator te zwijgen: %+v", p.FacilitatorRole)
	}
}

// Een facilitator-blok op een pipeline-duif is een configuratiefout: daar is
// geen facilitator, dus de schakelaar zou stil niets doen.
func TestLoadPersonas_FacilitatorRoleOnPipelinePigeonFails(t *testing.T) {
	fsys := minimalConfig(func(m fstest.MapFS) {
		y := string(m["personas.yaml"].Data)
		y = strings.ReplaceAll(y, "    kennisbank: false\n",
			"    kennisbank: false\n    facilitator: { framing: true, mediation: true }\n")
		m["personas.yaml"] = &fstest.MapFile{Data: []byte(y)}
	})
	if _, err := LoadPersonas(fsys); err == nil {
		t.Fatal("een facilitator-blok zonder engine: agent moet een bootfout zijn")
	}
}
