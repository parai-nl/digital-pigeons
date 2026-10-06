package duif

import (
	"testing"

	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// De begeleider bezit geen bronnen. "Geen bron gevonden voor deze vraag" is
// daarmee een uitspraak die hij niet mag doen — ook niet impliciet, doordat
// zijn rij de kennisbankstand van de duif overneemt. De stand hoort
// bij het blok van de duif, en daar staat hij ook.
func TestAgentFacilitator_KnowledgeGapLeavesNoSourceClaimOnHisOwnRows(t *testing.T) {
	m, repo, _, _ := kbAgentModule(t,
		[]kbSeed{{id: "1", samenvatting: "Te vage match.", score: 0.3, paper: "p1"}},
		[]llmtesting.StreamStep{
			askFramed(t, "Ik leg haar de vraag voor."),
			textStep("Daar is geen onderzoek over."),
			textStep("Dit raakt de mensen aan tafel zo."),
		}, nil)

	frames := agentTurnFrames(t, m, map[string]any{"pigeonId": "individualist", "message": "Dromen duiven?", "lang": "nl"})
	sessionID, ok := doneFrameOf(t, frames)["sessionId"].(string)
	if !ok {
		t.Fatal("de beurt sloot zonder sessie-id")
	}
	stored, err := repo.ListMessages(t.Context(), sessionID, 0)
	if err != nil {
		t.Fatal(err)
	}

	var facilitators, answers int
	for _, row := range stored {
		if row.Role != "assistant" {
			continue
		}
		if row.Persona == PersonaFacilitator {
			facilitators++
			if row.KBStatus != "" {
				t.Errorf("de begeleider draagt bronstand %q op %q", row.KBStatus, row.Content)
			}
			continue
		}
		answers++
		if row.KBStatus != string(KBGap) {
			t.Errorf("het blok van de duif hoort de stand wél te dragen, kreeg %q", row.KBStatus)
		}
	}
	// Kader vóór de duif en bemiddeling erna: allebei rijen van de begeleider.
	if facilitators != 2 || answers != 1 {
		t.Fatalf("beurt bewaarde %d begeleiderrijen en %d duifrijen: %+v", facilitators, answers, stored)
	}
}
