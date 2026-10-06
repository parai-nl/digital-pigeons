package duif

import (
	"os"
	"regexp"
	"slices"
	"testing"

	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/llmtesting"
)

// refusalStep is een gescripte iteratie waarin het model eerst tekst streamt
// en dan zijn weigering meldt. De gedeelde agent-lus noemt dat "refusal".
func refusalStep(tekst, weigering string) llmtesting.StreamStep {
	return llmtesting.StreamStep{Chunks: []llm.StreamChunk{
		{Content: tekst},
		{Refusal: weigering},
		{FinishReason: "stop"},
	}}
}

// De weigering hoort dezelfde melding te dragen vóór en ná het herladen. Live
// zet de client de badge op de facilitatorrij uit het done-frame; na herladen
// moet diezelfde rij de reden zelf nog dragen.
func TestRefusal_BadgeSurvivesAReload(t *testing.T) {
	m, repo, _ := newAgentModule(t, []llmtesting.StreamStep{
		askCoolsingel(),
		textStep("Die brede stoep is van mij."),
		refusalStep("Daar wil ik nog iets bij zeggen. ", "I cannot help with that"),
	})
	frames := agentTurnFrames(t, m, map[string]any{
		"pigeonId": "individualist", "message": "Wat vind jij van de Coolsingel?", "lang": "nl",
	})
	done := findFrame(frames, "done")
	if done == nil || done["stopReason"] != "refusal" {
		t.Fatalf("done = %+v, wil stopReason %q", done, "refusal")
	}
	var facilitator []ChatMessage
	for _, msg := range repo.messagesWithRole("assistant") {
		if msg.Persona == PersonaFacilitator {
			facilitator = append(facilitator, msg)
		}
	}
	if len(facilitator) == 0 {
		t.Fatal("geen facilitatorrij bewaard")
	}
	laatste := facilitator[len(facilitator)-1]
	if laatste.StopReason != "refusal" {
		t.Errorf("bewaarde stopreden = %q, wil %q (badge verdwijnt bij herladen)", laatste.StopReason, "refusal")
	}
}

// clientNotifiedStopReasons leest NOTIFIED_STOP_REASONS uit de clientbron.
// Bewust geen JS-parser: de constante is één regel met stringliteralen, en een
// regel die niet meer op die vorm past hoort op te vallen.
var clientStopReasonList = regexp.MustCompile(`(?m)^export const NOTIFIED_STOP_REASONS = \[([^\]]*)\] as const`)

var clientStopReasonItem = regexp.MustCompile(`'([^']+)'`)

func clientNotifiedStopReasons(t *testing.T) []string {
	t.Helper()
	const pad = "frontend/src/lib/chat/chatStore.svelte.ts"
	bron, err := os.ReadFile(pad)
	if err != nil {
		t.Fatalf("clientbron lezen: %v", err)
	}
	m := clientStopReasonList.FindSubmatch(bron)
	if m == nil {
		t.Fatalf("NOTIFIED_STOP_REASONS niet gevonden in %s", pad)
	}
	var uit []string
	for _, item := range clientStopReasonItem.FindAllStringSubmatch(string(m[1]), -1) {
		uit = append(uit, item[1])
	}
	if len(uit) == 0 {
		t.Fatalf("NOTIFIED_STOP_REASONS staat leeg in %s", pad)
	}
	return uit
}

// De server bewaart precies de redenen waarvoor de client een zin heeft. Loopt
// één van de twee lijsten uit de pas, dan draagt een rij na herladen ofwel een
// lege badge ofwel geen badge waar er live wel één stond.
func TestStopReasons_GoAndClientNameTheSameSet(t *testing.T) {
	client := clientNotifiedStopReasons(t)
	server := slices.Clone(notifiedStopReasons)
	slices.Sort(client)
	slices.Sort(server)
	if !slices.Equal(client, server) {
		t.Errorf("client %v, server %v: de twee lijsten horen dezelfde set te zijn", client, server)
	}
	// En wat op de lijst staat, overleeft ook werkelijk de beurt.
	for _, reden := range notifiedStopReasons {
		if got := storedStopReason(reden); got != reden {
			t.Errorf("storedStopReason(%q) = %q, wil %q", reden, got, reden)
		}
	}
	for _, reden := range []string{"natural", "max_iter", "tool_error", "error", "", "REFUSAL"} {
		if got := storedStopReason(reden); got != "" {
			t.Errorf("storedStopReason(%q) = %q, wil leeg", reden, got)
		}
	}
}
