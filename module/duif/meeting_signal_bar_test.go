package duif

import (
	"strings"
	"testing"
	"time"
)

// De signalerende duif zweeg bij een gesprek dat over duiven bleef gaan.
// De oorzaak zat in haar teksten, niet in de klok: drie smalle classifier-
// thema's met een nulscore, een persona-prompt die zonder catalogusonderbouwing
// geen signaal toestond, en een regel die zei: dragen de nieuwe regels alleen
// de aanleiding voort, sla dan over. De toetsen hieronder houden vast wat
// daarvoor in de plaats kwam, in beide talen. `flat` komt uit prompt_test.go:
// het plakt promptproza plat en in kleine letters, zodat een andere
// regelafbreking deze toetsen niet kan laten omvallen.

func TestSignalingPigeon_ThemesScoreProximityNotEvidence(t *testing.T) {
	p := mustGet(t, testPersonas(t), "nonverbal")
	if len(p.Topics) != 4 {
		t.Fatalf("de signalerende duif heeft %d thema's, wil er 4", len(p.Topics))
	}
	broad, ok := p.Topic("pigeons-nearby")
	if !ok {
		t.Fatal("het brede thema pigeons-nearby ontbreekt")
	}
	// Geen enkel thema mag nog een nulscore voorschrijven: dat was precies de
	// regel waarmee een algemeen duivengesprek buiten de boot viel.
	for _, topic := range p.Topics {
		for _, lang := range []string{"en", "nl"} {
			text := flat(topic.DescribeFor(lang))
			if text == "" {
				t.Errorf("%s (%s): lege themabeschrijving", topic.ID, lang)
			}
			for _, forbidden := range []string{"scores 0", "krijgt 0", "is not current evidence",
				"geen actuele aanleiding", "is insufficient", "is onvoldoende"} {
				if strings.Contains(text, forbidden) {
					t.Errorf("%s (%s): de oude classifier-tekst %q staat er nog", topic.ID, lang, forbidden)
				}
			}
		}
	}
	// Het brede thema zegt in beide talen dat er geen besluit en geen bewijs
	// nodig is, en dat het laag gescoord wordt (dus een rustig geluid).
	for _, tc := range []struct {
		lang string
		want []string
	}{
		{"en", []string{"without any decision on the table",
			"you do not need a proposal and you do not need proof",
			"score it low (0.15-0.33)"}},
		{"nl", []string{"zonder dat er een besluit ligt",
			"je hebt geen voorstel nodig en geen bewijs",
			"scoor het laag (0,15-0,33)"}},
	} {
		text := flat(broad.DescribeFor(tc.lang))
		for _, want := range tc.want {
			if !strings.Contains(text, want) {
				t.Errorf("pigeons-nearby (%s) mist %q:\n%s", tc.lang, want, text)
			}
		}
	}
	// Het midden van de twee plaatsgebonden thema's was vroeger "een
	// waarneming/beschrijving". Daarmee landde "de kerktoren staat in de
	// steigers" in de nadrukkelijke band. Het midden hoort nu bij een tafel die
	// om een besluit heen draait, en een kale waarneming gaat naar het brede
	// thema.
	for _, id := range []string{"food", "nesting"} {
		topic, ok := p.Topic(id)
		if !ok {
			t.Fatalf("thema %s ontbreekt", id)
		}
		for _, tc := range []struct {
			lang      string
			forbidden []string
			want      []string
		}{
			{"en", []string{"when it is an observation", "when it is a description"},
				[]string{"when the table is circling such a decision",
					"a bare observation or description without a decision belongs in pigeons-nearby, not here"}},
			{"nl", []string{"als het een waarneming is", "als het een beschrijving is"},
				[]string{"als de tafel om zo'n besluit heen draait",
					"een kale waarneming of beschrijving zonder besluit hoort in pigeons-nearby, niet hier"}},
		} {
			text := flat(topic.DescribeFor(tc.lang))
			for _, bad := range tc.forbidden {
				if strings.Contains(text, bad) {
					t.Errorf("%s (%s): het midden staat nog open voor een kale waarneming (%q)", id, tc.lang, bad)
				}
			}
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("%s (%s) mist %q:\n%s", id, tc.lang, want, text)
				}
			}
		}
	}
	// "Steigers" is een waarneming, geen ingreep: het hoort niet meer bij de
	// voorbeelden van nesting, en wel bij het brede thema.
	nesting, _ := p.Topic("nesting")
	for _, tc := range []struct{ lang, word string }{{"en", "scaffolding"}, {"nl", "steigers"}} {
		if strings.Contains(flat(nesting.DescribeFor(tc.lang)), tc.word) {
			t.Errorf("nesting (%s) noemt %q nog als eigen voorbeeld", tc.lang, tc.word)
		}
		if !strings.Contains(flat(broad.DescribeFor(tc.lang)), tc.word) {
			t.Errorf("pigeons-nearby (%s) noemt %q niet als waarneming", tc.lang, tc.word)
		}
	}
	// De lat van het brede thema is de laagste van de vier, zodat een gesprek
	// dat alleen maar over duiven gaat er doorheen komt; en die lat ligt onder
	// de rustige band, zodat zo'n geluid ook echt rustig klinkt.
	bar := p.Spreeklat
	broadBar := bar.Threshold(EagernessNormal, broad.Gevoeligheid)
	for _, topic := range p.Topics {
		if topic.ID == broad.ID {
			continue
		}
		if got := bar.Threshold(EagernessNormal, topic.Gevoeligheid); got <= broadBar {
			t.Errorf("thema %s heeft lat %.2f, wil hoger dan de brede lat %.2f", topic.ID, got, broadBar)
		}
	}
	if broadBar >= bandCalmCeiling {
		t.Errorf("de brede lat is %.2f: een score in de rustige band (< %.2f) haalt hem niet",
			broadBar, bandCalmCeiling)
	}
	// En een score die bij een concreet besluit hoort landt buiten de rustige
	// band, zodat score en geluidsband hetzelfde zeggen.
	if bandForArousal(0.8) == BandCalm {
		t.Error("een hoge score hoort niet in de rustige band te vallen")
	}
}

func TestSignalingPigeon_PersonaPromptNoLongerSilencesHer(t *testing.T) {
	p := mustGet(t, testPersonas(t), "nonverbal")
	for _, tc := range []struct {
		lang      string
		forbidden []string
		want      []string
	}{
		{
			lang: "en",
			forbidden: []string{"missing catalogue evidence permits no automatic signal",
				"do not infer volume"},
			want: []string{"she has the lowest bar of the three pigeons",
				"it is not a burden of proof a sound has to clear",
				"the manual demonstration remains available",
				"no meaning label is attached to a sound"},
		},
		{
			lang: "nl",
			forbidden: []string{"zonder catalogusonderbouwing volgt geen automatisch signaal",
				"leid uit tekst geen volume"},
			want: []string{"zij heeft de laagste lat van de drie duiven",
				"zij is geen bewijslat waar een geluid doorheen moet",
				"handmatig demonstreren blijft mogelijk",
				"aan een geluid hangt geen betekenislabel"},
		},
	} {
		text := flat(p.Prompt(tc.lang))
		for _, bad := range tc.forbidden {
			if strings.Contains(text, bad) {
				t.Errorf("[%s] de zwijgregel %q staat nog in de persona-prompt", tc.lang, bad)
			}
		}
		for _, want := range tc.want {
			if !strings.Contains(text, want) {
				t.Errorf("[%s] de persona-prompt mist %q:\n%s", tc.lang, want, text)
			}
		}
	}
	// De grens over wat tekst niet kan vaststellen blijft staan, maar dan waar
	// zij hoort: in het protocol, niet als verbod op scoren in de persona.
	for _, tc := range []struct{ lang, want string }{
		{"en", "text alone cannot establish volume, visible anger, an acoustic event or a pigeon's emotion."},
		{"nl", "tekst alleen bewijst geen volume, zichtbare boosheid, akoestische gebeurtenis of duivenemotie."},
	} {
		if !strings.Contains(flat(candidateSignalProtocol[tc.lang]), tc.want) {
			t.Errorf("[%s] het signaalprotocol mist de grens over wat tekst niet vaststelt", tc.lang)
		}
	}
	// En het protocol zegt nu waar de score over gaat: nabijheid, geen bewijs.
	for _, tc := range []struct{ lang, want string }{
		{"en", "the score is about proximity, not about proof"},
		{"nl", "de score gaat over nabijheid, niet over bewijs"},
	} {
		if !strings.Contains(flat(candidateSignalProtocol[tc.lang]), tc.want) {
			t.Errorf("[%s] het signaalprotocol zegt niet waar de score over gaat", tc.lang)
		}
	}
}

func TestSignalingPigeon_SpeakBarJudgesTheWholeConversation(t *testing.T) {
	bar := mustGet(t, testPersonas(t), "nonverbal").Spreeklat
	for _, tc := range []struct {
		lang      string
		forbidden []string
		want      []string
	}{
		{
			lang: "en",
			forbidden: []string{"so judge only what is new",
				"only continue the reason that the earlier ones already carried"},
			want: []string{"judge the running conversation as a whole",
				"a conversation that keeps being about pigeons keeps being a reason",
				"guarding the rest is not your job; the clock does that",
				"no decision has to be on the table"},
		},
		{
			lang: "nl",
			forbidden: []string{"beoordeel dus alleen wat nieuw is",
				"dragen de nieuwe regels alleen de aanleiding voort"},
			want: []string{"beoordeel daarom het lopende gesprek als geheel",
				"een gesprek dat over duiven blijft gaan, blijft aanleiding",
				"het is niet jouw werk om de rust te bewaken; dat doet de klok",
				"er hoeft geen besluit te liggen"},
		},
	} {
		text := flat(bar.Instruction(tc.lang))
		for _, bad := range tc.forbidden {
			if strings.Contains(text, bad) {
				t.Errorf("[%s] de overslaanregel %q staat nog in eis:M4", tc.lang, bad)
			}
		}
		for _, want := range tc.want {
			if !strings.Contains(text, want) {
				t.Errorf("[%s] de spreeklat mist %q:\n%s", tc.lang, want, text)
			}
		}
		// De sentinels blijven, ook na het herschrijven.
		for _, eis := range []string{"eis:m1", "eis:m2", "eis:m3", "eis:m4"} {
			if !strings.Contains(text, eis) {
				t.Errorf("[%s] %s is bij het herschrijven verdwenen", tc.lang, eis)
			}
		}
	}
}

// Het venster van de signalerende duif vraagt om een oordeel over het geheel;
// dat van de pratende duiven houdt de scheiding context/nieuw.
func TestCandidateWindow_SignalPigeonScoresTheWholeWindow(t *testing.T) {
	win := transcriptWindow{
		Segments: []TranscriptSegment{
			{Seq: 1, At: meetingEpoch, Speaker: "Anneke", Text: "Er zitten duiven op het dak van het station."},
			{Seq: 2, At: meetingEpoch, Speaker: "Bram", Text: "Die zie ik daar elke ochtend zitten."},
		},
		Fresh: 1,
		Words: 16,
	}
	set := testPersonas(t)
	signalling := mustGet(t, set, "nonverbal")
	talkative := mustGet(t, set, "biological")
	for _, lang := range []string{"en", "nl"} {
		signal := renderCandidateWindow(lang, "tok-signal", "nonverbal", win, nil, signalling.SignalsOnly())
		if !strings.Contains(signal, candidateSignalWindowNote[lang]) {
			t.Errorf("[%s] het signaalvenster vraagt niet om een oordeel over het geheel:\n%s", lang, signal)
		}
		if !strings.Contains(signal, candidateSignalContextHeader[lang]) {
			t.Errorf("[%s] het signaalvenster mist de eigen contextkop", lang)
		}
		if strings.Contains(signal, candidateContextHeader[lang]) {
			t.Errorf("[%s] het signaalvenster noemt het eerdere gesprek nog alleen context", lang)
		}
		// Beide segmenten staan erin: de duif scoort het gesprek, niet alleen
		// de laatste regel.
		for _, seg := range win.Segments {
			if !strings.Contains(signal, seg.Text) {
				t.Errorf("[%s] het signaalvenster mist een segment", lang)
			}
		}
		talking := renderCandidateWindow(lang, "tok-signal", "biological", win, nil, talkative.SignalsOnly())
		if strings.Contains(talking, candidateSignalWindowNote[lang]) {
			t.Errorf("[%s] de pratende duiven kregen de signaalinstructie erbij", lang)
		}
		if !strings.Contains(talking, candidateContextHeader[lang]) || !strings.Contains(talking, candidateFreshHeader[lang]) {
			t.Errorf("[%s] het venster van de pratende duiven is veranderd", lang)
		}
	}
}

// De band van een geluid komt niet rechtstreeks uit de score.
// De score is een impuls op de arousal-meter van de sessie
// (`max(verval(arousal, Δt), score)`) en `playSound` leest de meterstand van
// dát moment. Vanuit rust klinkt een lage score dus rustig, maar kort na een
// alarm klinkt dezelfde lage score nog nadrukkelijk: de meter werkt na. Deze
// test houdt dat verschil vast, zodat de toelichting in personas.yaml en de
// code niet uit elkaar kunnen lopen.
func TestNonverbalPigeon_SoundBandComesFromTheMeterNotFromTheScoreAlone(t *testing.T) {
	openSignalMeeting := func(t *testing.T, rig *meetingRig) (string, *eventStream) {
		t.Helper()
		id := rig.openMeeting(t, map[string]any{
			"pigeonId": "nonverbal", "lang": "nl", "autonomous": true,
		})
		return id, rig.events(t, id)
	}

	t.Run("vanuit rust klinkt een lage score rustig", func(t *testing.T) {
		rig := nonverbalRig(t, newCandidateLLM(speak(0.25, "pigeons-nearby", "")), nil)
		id, es := openSignalMeeting(t, rig)
		rig.say(t, id, "er zitten duiven op de rand van het stationsdak")

		sound := nextEvent(t, es.ch, EventSound)
		if sound.Sound == nil || sound.Sound.Band != BandCalm {
			t.Fatalf("een score van 0,25 vanuit rust hoort een %q-geluid te geven, kreeg %+v", BandCalm, sound.Sound)
		}
	})

	t.Run("kort na een alarm klinkt dezelfde lage score nog niet rustig", func(t *testing.T) {
		rig := nonverbalRig(t, newCandidateLLM(
			speak(0.80, "threat-and-removal", ""),
			speak(0.25, "pigeons-nearby", ""),
		), nil)
		id, es := openSignalMeeting(t, rig)

		rig.say(t, id, "we vangen ze volgende maand allemaal weg bij het plein")
		first := nextEvent(t, es.ch, EventSound)
		if first.Sound == nil || first.Sound.Band != BandAlarm {
			t.Fatalf("een score van 0,80 hoort een %q-geluid te geven, kreeg %+v", BandAlarm, first.Sound)
		}

		// Langer dan de cooldown van deze duif (20 s), korter dan de
		// halveringstijd van de meter (120 s): de klok laat haar spreken, de
		// meter is nog niet gezakt.
		rig.clock.skip(45 * time.Second)
		rig.say(t, id, "ik zie ze 's ochtends gewoon op het dak zitten")
		second := nextEvent(t, es.ch, EventSound)
		if second.Sound == nil {
			t.Fatal("de tweede kandidaat gaf geen geluid")
		}
		if second.Sound.Band == BandCalm {
			t.Errorf("de meter werkt niet na: dezelfde score van 0,25 gaf al %q", BandCalm)
		}
		if got := decayArousal(0.80, 45*time.Second, 2*time.Minute); got < bandCalmCeiling {
			t.Errorf("de meter zou na 45 s op %.3f staan, onder de rustgrens %.2f — de opzet meet niets",
				got, bandCalmCeiling)
		}
	})
}

// Het `ambient`-gesprek bevat één segment dat alleen door de
// laagste lat heen komt. Zonder de volgorde van de latten (breed lager dan
// smal) zou dat segment stil blijven, en dan meet de replay de volgorde ook
// echt in plaats van hem alleen te veronderstellen.
func TestConversationFixtures_AmbientHasASegmentOnlyTheBroadBarLetsThrough(t *testing.T) {
	p := mustGet(t, testPersonas(t), "nonverbal")
	broad, ok := p.Topic("pigeons-nearby")
	if !ok {
		t.Fatal("het brede thema pigeons-nearby ontbreekt")
	}
	broadBar := p.Spreeklat.Threshold(EagernessNormal, broad.Gevoeligheid)
	// De laagste lat van de overige thema's is de grens waar dit segment onder
	// moet blijven.
	narrowest := 1.0
	for _, topic := range p.Topics {
		if topic.ID == broad.ID {
			continue
		}
		if bar := p.Spreeklat.Threshold(EagernessNormal, topic.Gevoeligheid); bar < narrowest {
			narrowest = bar
		}
	}
	fixtures := loadConversationFixtures(t)
	for _, lang := range []string{"nl", "en"} {
		var found bool
		for _, conv := range fixtures.Conversations {
			if conv.ID != "ambient" || conv.Lang != lang {
				continue
			}
			for _, seg := range conv.Segments {
				score := seg.scoreFor("nonverbal")
				if seg.Topic == broad.ID && score > broadBar && score < narrowest {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("ambient/%s heeft geen segment tussen de brede lat (%.2f) en de smalste andere lat (%.2f)",
				lang, broadBar, narrowest)
		}
	}
}

// De signaalvlag van het venster is een eigenschap van de duif, geen
// testconstante: `SignalsOnly` is de ene plek waar staat wie er alleen
// signaleert.
func TestPersona_SignalsOnlyNamesTheSignalingPigeon(t *testing.T) {
	set := testPersonas(t)
	for _, tc := range []struct {
		id   string
		want bool
	}{
		{"nonverbal", true}, {"biological", false}, {"individualist", false},
	} {
		if got := mustGet(t, set, tc.id).SignalsOnly(); got != tc.want {
			t.Errorf("%s.SignalsOnly() = %v, wil %v", tc.id, got, tc.want)
		}
	}
	if (*Persona)(nil).SignalsOnly() {
		t.Error("een lege persona signaleert niets")
	}
}
