package duif

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func statusBody(t *testing.T, m *Module, anonymous bool) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	rec := serveKnowledge(t, m, httptest.NewRequest(http.MethodGet, "/v1/knowledge/status", nil), anonymous)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("antwoord is geen JSON: %v (%s)", err, rec.Body.String())
	}
	return rec, body
}

func TestKnowledgeStatus_RequiresALoggedInUser(t *testing.T) {
	m, _ := newTestModule(t, nil)
	rec, body := statusBody(t, m, true)
	if rec.Code != http.StatusUnauthorized || body["error"] != "unauthorized" {
		t.Fatalf("status %d, body %v", rec.Code, body)
	}
}

func TestKnowledgeStatus_WithoutImporterIsEmpty(t *testing.T) {
	m, _ := newTestModule(t, nil)
	rec, body := statusBody(t, m, false)
	if rec.Code != http.StatusOK || body["state"] != string(ImportEmpty) {
		t.Fatalf("status %d, body %v", rec.Code, body)
	}
	// Geen cache: de stand verandert tijdens een import.
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, wil no-store", got)
	}
	if _, ok := body["kbId"]; ok {
		t.Errorf("een lege kennisbank hoort geen kbId te sturen: %v", body)
	}
}

func TestKnowledgeStatus_ReportsReadyWithCountsInCamelCase(t *testing.T) {
	imp, fake := newTestImporter(t)
	importFixture(t, imp, newMosaicFixture())
	m, _ := newTestModule(t, func(m *Module) {
		m.knowledge = fake
		m.kbImport = imp
	})

	rec, body := statusBody(t, m, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if body["state"] != string(ImportReady) || body["kbId"] != "duiven-proef" {
		t.Fatalf("body = %v", body)
	}
	if body["importedAt"] == "" || body["importedAt"] == nil {
		t.Errorf("importedAt ontbreekt: %v", body)
	}
	counts, ok := body["counts"].(map[string]any)
	if !ok || counts[tableDestillaat] != float64(2) || counts[tablePerspective] != float64(2) {
		t.Errorf("counts = %v", body["counts"])
	}
	if _, ok := body["skipped"]; ok {
		t.Errorf("skipped hoort weg te vallen als hij false is: %v", body)
	}
	if _, ok := body["reason"]; ok {
		t.Errorf("een geslaagde import heeft geen reden: %v", body)
	}
}

func TestKnowledgeStatus_ReportsFailureAsACode(t *testing.T) {
	imp, _ := newTestImporter(t)
	imp.setStatus(importStatus{State: ImportFailed, Reason: ReasonImportEmbeddingMismatch, KBID: "duiven-proef"})
	m, _ := newTestModule(t, func(m *Module) { m.kbImport = imp })

	_, body := statusBody(t, m, false)
	if body["state"] != string(ImportFailed) || body["reason"] != ReasonImportEmbeddingMismatch {
		t.Fatalf("body = %v", body)
	}
}

func TestKnowledgeStatus_ReportsSkipped(t *testing.T) {
	imp, _ := newTestImporter(t)
	imp.setStatus(importStatus{State: ImportReady, KBID: "duiven-proef", Skipped: true})
	m, _ := newTestModule(t, func(m *Module) { m.kbImport = imp })

	_, body := statusBody(t, m, false)
	if body["skipped"] != true {
		t.Fatalf("body = %v", body)
	}
}

func TestKnowledgeStatus_SnapshotDoesNotShareTheCountsMap(t *testing.T) {
	imp, _ := newTestImporter(t)
	imp.setStatus(importStatus{State: ImportReady, Counts: map[string]int{tableDestillaat: 2}})

	got := imp.snapshot()
	got.Counts[tableDestillaat] = 99
	if imp.snapshot().Counts[tableDestillaat] != 2 {
		t.Error("een lezer mag de tellingen van de importer niet kunnen muteren")
	}
}

func TestKnowledgeStatus_EmptyStateIsNeverBlank(t *testing.T) {
	imp, _ := newTestImporter(t)
	imp.setStatus(importStatus{})
	m, _ := newTestModule(t, func(m *Module) { m.kbImport = imp })

	if st := m.knowledgeStatus(); st.State != ImportEmpty {
		t.Fatalf("state = %q, wil empty", st.State)
	}
}

// De statusroute zegt wélk bundelformaat er binnenkwam. Bij een
// duiven/3-import staan het schema, de talen en de brontaal op de wire; bij
// een duiven/2-import blijven de twee taalvelden weg, want die bundel kent ze
// niet. Dat verschil is precies wat een lezer nodig heeft om "eentalig
// corpus" van "vertaling ontbreekt" te onderscheiden.
func TestKnowledgeStatus_ReportsTheBundleFormat(t *testing.T) {
	statusFor := func(t *testing.T, path string) map[string]any {
		t.Helper()
		imp, fake := newTestImporter(t)
		imp.run(context.Background(), path)
		if st := imp.snapshot(); st.State != ImportReady {
			t.Fatalf("import = %+v, wil ready", st)
		}
		m, _ := newTestModule(t, func(m *Module) {
			m.knowledge = fake
			m.kbImport = imp
		})
		_, body := statusBody(t, m, false)
		return body
	}

	t.Run("duiven/3 draagt schema, talen en brontaal", func(t *testing.T) {
		body := statusFor(t, kbV3Write(t, kbV3Fixture(t)))
		if body["bundleSchema"] != kbV3Schema {
			t.Errorf("bundleSchema = %v, wil %q", body["bundleSchema"], kbV3Schema)
		}
		if body["sourceLanguage"] != kbLangSource {
			t.Errorf("sourceLanguage = %v, wil %q", body["sourceLanguage"], kbLangSource)
		}
		want := []any{kbLangSource, kbLangEnglish}
		if got, _ := body["languages"].([]any); !reflect.DeepEqual(got, want) {
			t.Errorf("languages = %v, wil %v", body["languages"], want)
		}
	})

	t.Run("duiven/2 draagt alleen het schema", func(t *testing.T) {
		doc := kbV2Fixture(t)
		body := statusFor(t, kbV3Write(t, doc))
		if body["bundleSchema"] != kbV2Schema {
			t.Errorf("bundleSchema = %v, wil %q", body["bundleSchema"], kbV2Schema)
		}
		for _, key := range []string{"languages", "sourceLanguage"} {
			if _, ok := body[key]; ok {
				t.Errorf("een duiven/2-import hoort geen %s te sturen: %v", key, body)
			}
		}
	})

	// Een herstart leest dezelfde marker terug en mag daar niet minder uit
	// halen dan de import zelf erin schreef.
	t.Run("de markerlezing van een herstart zegt hetzelfde", func(t *testing.T) {
		imp, fake := newTestImporter(t)
		imp.run(context.Background(), kbV3Write(t, kbV3Fixture(t)))

		herstart := testImporter(fake, slog.New(slog.DiscardHandler), false)
		herstart.loadBootStatus(context.Background())
		st := herstart.snapshot()
		if st.BundleSchema != kbV3Schema || st.SourceLanguage != kbLangSource {
			t.Fatalf("na herstart = %+v", st)
		}
		if !reflect.DeepEqual(st.Languages, []string{kbLangSource, kbLangEnglish}) {
			t.Errorf("talen na herstart = %v", st.Languages)
		}
	})
}
