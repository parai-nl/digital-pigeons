package duif

import (
	"encoding/json"
	"io/fs"
	"testing"
	"testing/fstest"
)

func mediaFixture(t *testing.T) (fstest.MapFS, *PersonaSet, map[string]any) {
	t.Helper()
	sub, err := fs.Sub(configFS, "config")
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{}
	if err := fs.WalkDir(sub, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(sub, name)
		if err != nil {
			return err
		}
		files[name] = &fstest.MapFile{Data: data}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	set, err := LoadPersonas(files)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(files["media-manifest.v1.json"].Data, &manifest); err != nil {
		t.Fatal(err)
	}
	return files, set, manifest
}
func TestSoundMediaManifestBindsMeasuredDurationAndIdentity(t *testing.T) {
	files, set, _ := mediaFixture(t)
	if err := applySoundManifest(files, set); err != nil {
		t.Fatal(err)
	}
	p, _ := set.Get("nonverbal")
	for _, sound := range p.Geluiden {
		if sound.DurationMs < 200 || sound.DurationMs > 15000 || sound.DurationMs != set.sounds[sound.ID].DurationMs {
			t.Fatalf("duration absent or inconsistent for %s", sound.ID)
		}
	}
}
func TestSoundMediaManifestRejectsInvalidDelivery(t *testing.T) {
	for _, kind := range []string{"missing", "hash", "size", "duration", "path", "duplicate", "incomplete", "unknown", "approval", "rights", "origin"} {
		t.Run(kind, func(t *testing.T) {
			files, set, manifest := mediaFixture(t)
			records := manifest["sounds"].([]any)
			first := records[0].(map[string]any)
			switch kind {
			case "missing":
				delete(files, first["file"].(string))
			case "hash":
				first["sha256"] = "bad"
			case "size":
				first["bytes"] = 1
			case "duration":
				first["durationMs"] = 15001
			case "path":
				first["file"] = "sounds/../personas.yaml"
			case "duplicate":
				manifest["sounds"] = append(records, first)
			case "incomplete":
				manifest["sounds"] = records[1:]
			case "unknown":
				first["id"] = "not-configured"
			case "approval":
				first["status"] = "approved"
				first["kind"] = "field-recording"
			case "rights":
				first["status"] = "approved"
				first["kind"] = "field-recording"
				first["approval"] = map[string]any{"reviewedBy": "test", "reviewedAt": "2026-09-07"}
			case "origin":
				first["origin"] = nil
			}
			data, _ := json.Marshal(manifest)
			files["media-manifest.v1.json"] = &fstest.MapFile{Data: data}
			if err := applySoundManifest(files, set); err == nil {
				t.Fatal("invalid delivery accepted")
			}
		})
	}
}
func TestPickSoundAvoidsImmediateRepeatWithoutLeavingEligibleBand(t *testing.T) {
	sounds := []Sound{{ID: "signal-a", Band: BandCalm}, {ID: "signal-b", Band: BandCalm}, {ID: "signal-c", Band: BandAlarm}}
	previous := ""
	for range 10 {
		selected, ok := pickSoundAvoid(sounds, .1, func(int) int { return 0 }, previous)
		if !ok || selected.ID == previous || selected.Band != BandCalm {
			t.Fatalf("repeat or wrong band: %+v", selected)
		}
		previous = selected.ID
	}
	single, ok := pickSoundAvoid(sounds[2:], 1, func(int) int { return 0 }, "signal-c")
	if !ok || single.ID != "signal-c" {
		t.Fatal("single recording must remain usable")
	}
}
func TestAdmittedSoundRemembersAssetOnlyForCurrentGeneration(t *testing.T) {
	// No microphone/model is involved: this exercises the same guarded emitter
	// that records the asset alongside the uniquely identified sound event.
	session := &meetingSession{turnGen: 2, subs: map[int]chan MeetingEvent{}, lastSoundID: "initial"}
	if session.emitTurn(1, MeetingEvent{Type: EventSound, Sound: &SoundRef{ID: "stale"}}) {
		t.Fatal("stale sound admitted")
	}
	if session.lastSoundID != "initial" {
		t.Fatal("stale sound affected variation")
	}
}

func TestSoundEventsCarryMeasuredDurationUniqueIDAndSessionVariation(t *testing.T) {
	engine, clock := newTestEngine(t, nil, func(engine *meetingEngine) { engine.pickIndex = func(int) int { return 0 } })
	session, err := engine.open("media-test-user", "nonverbal", "en")
	if err != nil {
		t.Fatal(err)
	}
	events, closeEvents := session.subscribe()
	defer closeEvents()
	previous, eventID := "", ""
	for range 4 {
		ref, _, ok := engine.playSound(session, clock.Now(), 0)
		if !ok {
			t.Fatal("manual sound not admitted")
		}
		event := nextEvent(t, events, EventSound)
		if event.Sound == nil || event.Sound.ID != ref.ID || ref.ID == previous {
			t.Fatalf("wrong or repeated asset: %+v", event.Sound)
		}
		configured := engine.personas.sounds[ref.ID]
		if event.DurationMS != configured.DurationMs || event.DurationMS < 200 {
			t.Fatalf("measured duration absent: %d", event.DurationMS)
		}
		if event.EventID == "" || event.EventID == eventID {
			t.Fatal("unique event identity absent")
		}
		previous, eventID = ref.ID, event.EventID
	}
}
