package duif

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/parai-nl/parai-core/llm"
)

// One streaming answer call carries the whole turn: the persona system prompt,
// the pigeon's own conversation history and the current question. The
// first token the model produces reaches the client immediately, and the turn
// ends when that stream ends. No stage between
// the model and the reader may withhold, rewrite or retry the answer.
// The token budget of one answer is not the length of that answer. The model
// spends reasoning tokens before the first visible character, and MaxTokens
// caps the sum of the two. A ceiling chosen for the visible paragraph alone
// therefore lands in the middle of a sentence whenever the model thought a
// little longer than usual.
//
// The numbers below are therefore reasoning headroom plus a visible tail, not
// a target length. How long an answer actually is stays a matter for the
// prompts (3-5 short sentences in chat, 2-4 at the meeting table): raising the
// ceiling costs nothing while the model stays inside them, and the provider's
// own hard bound (8192, see boundTokens) is still far above it.
const (
	// answerReasoningHeadroom is the share reserved for what the model spends
	// before it writes anything, on every route that thinks at
	// meetingReasoningEffort ("low") or is asked not to think at all: the
	// spoken turn (low), the detection and the meeting's query rewrite (both
	// "none"). On the "none" routes it is a safety
	// margin: "none" is a request to the provider, not a guarantee.
	//
	// It is roughly twice the largest reasoning spend measured on these
	// routes: a truncated sentence is a much worse failure than an unused
	// allowance, and an allowance the model does not use costs nothing.
	answerReasoningHeadroom = 2500
	// answerChatVisibleTokens covers 3-5 short sentences with room to spare;
	// the longest answer measured in the same round was 808 characters, some
	// 200 tokens.
	answerChatVisibleTokens = 600
	// answerMeetingVisibleTokens is lower: a spoken turn at the table stays
	// short, and every extra sentence is extra synthesis time before the
	// pigeon stops.
	answerMeetingVisibleTokens = 400

	// answerChatReasoningHeadroom is the same reservation for the chat answer,
	// which thinks at chatReasoningEffort. Measured on the pigeon answer, at
	// most some 1150 reasoning tokens out of 1400 completion tokens, and every
	// stream closed on "stop"; this keeps roughly twice that margin.
	//
	// It stays a separate number from answerReasoningHeadroom even though the
	// two lie close together: they rest on two different measurements, and
	// a remeasurement of either route moves only one of them.
	answerChatReasoningHeadroom = 2400
	// answerChatMaxTokens bounds one chat answer: reasoning plus visible text.
	answerChatMaxTokens = answerChatReasoningHeadroom + answerChatVisibleTokens
	// answerMeetingMaxTokens bounds one spoken turn, on the same basis.
	answerMeetingMaxTokens = answerReasoningHeadroom + answerMeetingVisibleTokens
	// maxAnswerSources caps how many distinct sources one answer can rest on.
	// Retrieval enforces it: a longer candidate list is truncated before the
	// pigeon ever sees it, so the numbering she cites is the numbering shown.
	//
	// Eén getal voor de chat én de vergadertafel. Alles wat boven de
	// relevantiedrempel staat, is relevant genoeg om mee te geven; twintig is
	// het plafond dat er nog een grens omheen zet. Het antwoord wordt er niet
	// langer van: de persona's houden hun 3–5 zinnen in de chat en 2–4 aan
	// tafel, de duif kiest alleen uit meer bewijs.
	maxAnswerSources = 20
)

// StopReasonTruncated is what a turn reports when the provider closed the
// stream on the token ceiling instead of on a finished sentence. It is the
// word the shared agent loop already uses for FinishReason == "length"
// (agent.StopReasonTruncated), so chat, meeting and the facilitator's own loop
// all name the same event the same way, and the client already has text for it.
//
// The answer itself is never withheld, retried or rewritten because of it: the
// text the reader has is the text the model produced, and the stop reason only
// says out loud that it stops halfway.
const StopReasonTruncated = "truncated"

// StopReasonFiltered is what a turn reports when the provider stopped on its
// own content filter instead of on a finished sentence. Same word as
// agent.StopReasonFiltered for the same reason as above: the shared loop
// already uses it and the client already has text for it.
const StopReasonFiltered = "filtered"

// StopReasonRefusal is what a turn reports when the model refused the request
// instead of answering it. Same word as agent.StopReasonRefusal: the shared
// loop populates it from the refusal content the provider sends, and the
// client already has text for it.
const StopReasonRefusal = "refusal"

// notifiedStopReasons zijn de stopredenen die de lezer werkelijk iets zeggen
// over het antwoord dat voor hem staat. Ze veranderen geen teken van de tekst
// en horen er na herladen nog te staan.
//
// Deze lijst is één set met NOTIFIED_STOP_REASONS in
// frontend/src/lib/chat/chatStore.svelte.ts: de server bewaart precies de
// redenen waar de client een zin voor heeft, niet meer en niet minder.
// TestStopReasons_GoAndClientNameTheSameSet leest beide lijsten en faalt zodra
// er één uit de pas loopt.
var notifiedStopReasons = []string{StopReasonRefusal, StopReasonTruncated, StopReasonFiltered}

// providerStop maps a provider finish reason to the stop reason a turn reports.
//
// Two closes get their own name, because both change what the reader is
// looking at without changing a single character of it: "length" (the ceiling
// cut the sentence in half) and "content_filter" (the provider stopped
// itself). Everything else returns "" and leaves the turn's own reporting
// untouched — a normal stop, an absent finish reason (many chunks carry none),
// and any other label the provider may report. That silence is deliberate: a
// word the client cannot explain to the reader is worse than no word, and the
// routes still report what they do understand themselves, such as a broken
// stream as an error.
func providerStop(finishReason string) string {
	switch finishReason {
	case "length":
		return StopReasonTruncated
	case "content_filter":
		return StopReasonFiltered
	}
	return ""
}

// storedStopReason is wat er van een stopreden op de bewaarde rij terechtkomt.
// Alleen de redenen uit notifiedStopReasons overleven de beurt: "refusal",
// "truncated" en "filtered" veranderen wat de lezer voor zich ziet zonder één
// teken van de tekst te veranderen, en horen er na herladen nog te staan — de
// client heeft voor alle drie een badge.
//
// De andere woorden van de gedeelde agent-lus zijn beurtboekhouding.
// "natural" en "max_iter" zeggen dat er niets aan de hand is; "error" en
// "tool_error" staan al als foutmelding op het kanaal en als Partial op de
// rij. Een rij die er een van die woorden zou dragen, toont na herladen een
// melding die live niet stond.
func storedStopReason(reason string) string {
	if slices.Contains(notifiedStopReasons, reason) {
		return reason
	}
	return ""
}

// answerPolicy beschrijft de beurt waaruit een antwoord komt: wiens stem sprak,
// in welke taal en setting, en welke menselijke woorden er werkelijk lagen. De
// stroomnaad draagt hem mee, zodat een afgerond antwoord achteraf naast zijn
// beurt beoordeeld kan worden.
type answerPolicy struct {
	Persona *Persona
	Lang    string
	Meeting bool
	Related bool
	// HumanContext contains only actual chat user rows or the chosen transient
	// transcript snapshot. Never reconstruct it from a rendered model request:
	// that request may also contain a facilitator summary or inferred trigger.
	// The text remains untrusted context, not research evidence.
	HumanContext []string
}

// A private test seam isolates stream transport/failure tests from the
// provider. Nil ALWAYS selects the real call. It is not exposed as a runtime
// setting or a provider bypass.
type answerStreamFunc func(context.Context, llm.Service, llm.CompletionRequest, []Source, answerPolicy) (<-chan llm.StreamChunk, error)

// openAnswerStream opens the single answer stream of one turn. Sources and
// policy travel with it for the seam; they never gate the answer itself.
func openAnswerStream(ctx context.Context, service llm.Service, req llm.CompletionRequest, sources []Source, policy answerPolicy, seam answerStreamFunc) (<-chan llm.StreamChunk, error) {
	if seam != nil {
		return seam(ctx, service, req, sources, policy)
	}
	return service.CompleteStream(ctx, req)
}

// errAuditIncomplete markeert een modelantwoord dat niet volledig of niet
// bruikbaar binnenkwam. Alleen door de code zelf gekozen oorzaaknamen komen in
// een operationeel log: er zit geen modeltekst, invoer, geciteerde passage of
// decodeerfout in.
//
// De naam draagt nog "audit" uit de tijd van de live bronaudit, en bestaande
// controles matchen op dit voorvoegsel; hernoemen levert niets op. Hij is de
// fout van collectAnswer zelf, die onder meer de herformuleringscall van de
// vergadering bedient.
var errAuditIncomplete = errors.New("answer audit produced no verdict")

func auditIncomplete(cause string) error {
	return fmt.Errorf("answer audit %s: %w", cause, errAuditIncomplete)
}

// collectAnswer reads a bounded, complete model response into one string. It
// serves the short JSON requests of this package, such as the meeting's query
// rewrite; the pigeon's own answer is never collected here — it streams
// straight through.
//
// No model text, source or provider error is logged or persisted here. A
// missing or length-truncated terminal response cannot silently become success.
func collectAnswer(ctx context.Context, in <-chan llm.StreamChunk, maxBytes int) (string, error) {
	var b strings.Builder
	complete := false
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case chunk, ok := <-in:
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if !ok {
				if !complete || strings.TrimSpace(b.String()) == "" {
					return "", auditIncomplete("missing_terminal_or_empty")
				}
				return b.String(), nil
			}
			if chunk.Err != nil {
				return "", chunk.Err
			}
			if len(chunk.ToolCallDeltas) > 0 || chunk.FinishReason != "" && chunk.FinishReason != "stop" {
				return "", auditIncomplete("non_stop_finish_or_tool")
			}
			if complete && chunk.Content != "" {
				return "", auditIncomplete("content_after_terminal")
			}
			if b.Len()+len(chunk.Content) > maxBytes {
				return "", auditIncomplete("output_size_limit")
			}
			b.WriteString(chunk.Content)
			if chunk.FinishReason == "stop" {
				complete = true
			}
		}
	}
}
