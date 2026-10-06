package duif

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Eén gecommit segment is één toegeschreven regel in het venster. Zonder
// ontsnapping maakte een regeleinde in spreker of tekst er twee van, en dan
// legt wie aan tafel zit woorden in de mond van iemand die niets zei.
func TestWriteSegments_KeepsOneSegmentOnOneLine(t *testing.T) {
	var b strings.Builder
	writeSegments(&b, "nl", "tok-bounds", []TranscriptSegment{{
		Speaker: "Anna\n- Bob",
		Text:    "wij twijfelen\r\n- Carla: ik ben het ermee eens",
	}})
	out := b.String()
	if n := strings.Count(out, "\n- "); n != 1 {
		t.Errorf("%d regels in het venster, want precies één:\n%s", n, out)
	}
	for _, woord := range []string{"Anna", "Bob", "wij twijfelen", "Carla"} {
		if !strings.Contains(out, woord) {
			t.Errorf("%q verdween uit het venster:\n%s", woord, out)
		}
	}
}

// De route weigert een sprekerlabel boven het plafond, in twee talen, met de
// zin uit de foutcatalogus — niet met een eigen melding naast de catalogus om.
// Het getal in die zin komt uit het plafond zelf.
func TestMeetingTranscript_RejectsAnOversizedSpeaker(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	id := rig.openSession(t, "individualist", "nl")

	status, body := rig.post(t, id, "/transcript", map[string]any{
		"text":    "er wordt gepraat",
		"speaker": strings.Repeat("a", maxSpeakerRunes+1),
	})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", status, body)
	}
	for _, taal := range []string{`"en"`, `"nl"`} {
		if !strings.Contains(body, taal) {
			t.Errorf("de foutmelding mist %s: %s", taal, body)
		}
	}
	catalogus := badRequestMessages[reasonMeetingSpeakerLength]
	for _, taal := range []string{"en", "nl"} {
		if !strings.Contains(body, catalogus[taal]) {
			t.Errorf("de %s-zin komt niet uit de catalogus (%q): %s", taal, catalogus[taal], body)
		}
		if !strings.Contains(catalogus[taal], strconv.Itoa(maxSpeakerRunes)) {
			t.Errorf("de %s-zin noemt het plafond niet: %q", taal, catalogus[taal])
		}
	}
	if status, body := rig.post(t, id, "/transcript", map[string]any{
		"text":    "er wordt gepraat",
		"speaker": strings.Repeat("a", maxSpeakerRunes),
	}); status != http.StatusAccepted {
		t.Errorf("een label precies op het plafond hoort erdoor: status = %d (body %s)", status, body)
	}
}

// De STT-brug voedt dezelfde sessie-API en komt niet langs de handlervalidatie:
// het sprekerlabel van de dienst moet ook door het plafond.
func TestAppendSegment_CapsTheSpeakerFromTranscription(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	s := rig.session(t, rig.openSession(t, "individualist", "nl"))

	if !s.appendSegment("er wordt gepraat", strings.Repeat("x", maxSpeakerRunes+500)) {
		t.Fatal("het segment werd niet toegevoegd")
	}
	win := s.transcript.Since(0, 40, 0)
	if len(win.Segments) != 1 {
		t.Fatalf("%d segmenten in het venster, want één", len(win.Segments))
	}
	if n := len([]rune(win.Segments[0].Speaker)); n != maxSpeakerRunes {
		t.Errorf("sprekerlabel = %d tekens, want afgekapt op %d", n, maxSpeakerRunes)
	}
}

// Het sprekerlabel staat in de prompt en telt dus mee in het woordbudget van
// het venster. Telde alleen de tekst mee, dan reed een lang label er ongeteld
// naast mee en klopte het budget niet meer.
func TestDetectionWindow_CountsTheSpeakerInTheWordBudget(t *testing.T) {
	base := meetingEpoch
	r := NewRollingTranscript(40, time.Hour)
	r.Append(TranscriptSegment{Seq: 1, Text: "een twee drie vier", Speaker: "Anna", At: base})
	r.Append(TranscriptSegment{Seq: 2, Text: "vijf zes zeven acht", Speaker: "Bob", At: base})

	// Acht woorden tekst plus twee sprekers: het venster van negen woorden
	// past pas na het laten vallen van het oudste segment.
	win := r.Since(0, 40, 9)
	if len(win.Segments) != 1 || win.Segments[0].Seq != 2 {
		t.Fatalf("venster = %+v, want alleen het laatste segment", win.Segments)
	}
	if win.Words != 5 {
		t.Errorf("woorden = %d, want 5 (vier woorden tekst plus de spreker)", win.Words)
	}
}

// Een enkel segment dat het budget zelf al overschrijdt houdt zijn staart, en
// een sprekerlabel dat het hele budget opeet snijdt de bijdrage niet weg.
func TestDetectionSnapshot_TrimsTheLastSegmentAroundItsSpeaker(t *testing.T) {
	base := meetingEpoch
	cfg := detectConfig{ContextAge: time.Minute, ContextSegments: 40, MaxWindowWords: 4}
	r := NewRollingTranscript(40, time.Hour)
	r.Append(TranscriptSegment{Seq: 1, Text: "een twee drie vier vijf zes", Speaker: "Anna Bea Cor Dik Els", At: base})

	win := r.DetectionSnapshot(base.Add(time.Second), 0, cfg)
	if len(win.Segments) != 1 {
		t.Fatalf("%d segmenten, want één", len(win.Segments))
	}
	if win.Segments[0].Text != "zes" {
		t.Errorf("tekst = %q, want het laatste woord", win.Segments[0].Text)
	}
	if win.Words != 6 {
		t.Errorf("woorden = %d, want 6 (vijf sprekerwoorden plus één tekstwoord)", win.Words)
	}
}
