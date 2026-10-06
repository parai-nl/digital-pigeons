package duif

import (
	"testing"

	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// De chatkant: het openframe van het duif-blok draagt wat er wegviel.
// Op het openframe en niet op het sources-frame, want dat laatste blijft weg
// bij een kennisgat — en juist dan is "er was wél iets, maar het viel af" wat
// de lezer mist.
// De uitval is beperkt tot harde fouten; hier is dat een
// standpunt zonder leesbare tekst, dat als lege bronkaart op het scherm zou
// komen en als leeg nummer in de prompt.
func TestAgentDrops_ArtifactOpenCarriesWhatFellAway(t *testing.T) {
	m, _, _, _ := kbAgentModule(t,
		[]kbSeed{
			{id: "goed", samenvatting: "Duiven herkennen gezichten.", score: 0.9, quote: "Ze kenden mij.", paper: "lewald1987"},
			{id: "leeg", samenvatting: "   ", score: 0.89, quote: "Van ver al.", paper: "lewald1987"},
		},
		[]llmtesting.StreamStep{
			askBiological(),
			textStep("Wij herkennen jullie echt [1]."),
		}, nil)

	frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "biological", "message": "Kennen duiven mij?", "lang": "nl"})
	open := frames[1]
	dropped, ok := open["dropped"].(map[string]any)
	if !ok {
		t.Fatalf("artifact-open draagt geen uitval: %+v", open)
	}
	if n, _ := dropped["count"].(float64); n < 1 {
		t.Fatalf("count = %v, want minstens één weggevallen kandidaat", dropped["count"])
	}
	reasons, _ := dropped["reasons"].(map[string]any)
	if n, _ := reasons[dropEmptyText].(float64); n < 1 {
		t.Errorf("reden ontbreekt: %+v", reasons)
	}
}

// Een beurt waarin niets wegviel draagt geen melding.
func TestAgentDrops_NothingDroppedIsNoField(t *testing.T) {
	m, _, _, _ := kbAgentModule(t,
		[]kbSeed{{id: "goed", samenvatting: "Duiven herkennen gezichten.", score: 0.9, quote: "Ze kenden mij.", paper: "lewald1987"}},
		[]llmtesting.StreamStep{
			askBiological(),
			textStep("Wij herkennen jullie echt [1]."),
		}, nil)

	frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "biological", "message": "Kennen duiven mij?", "lang": "nl"})
	if _, has := frames[1]["dropped"]; has {
		t.Fatalf("zonder uitval hoort er geen veld te staan: %+v", frames[1])
	}
}
