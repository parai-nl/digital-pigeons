package duif

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestMeetingControlRecovery_StaleResumeCannotReleaseNewerStop(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "individualist", "autonomous": true, "playbackTracking": true})
	s := rig.session(t, id)
	s.stopSpeaking()
	// This HTTP request was initiated after stop #1 but reaches the handler
	// only after the user has issued stop #2. It carries the old known barrier.
	delayedResume := map[string]any{"turnId": s.settingsSnapshot().TurnID}
	s.stopSpeaking()
	status, _ := rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/resume", delayedResume)
	if !s.settingsSnapshot().AutonomyPaused {
		t.Fatalf("stale resume accepted (%d) and cleared the newer server pause", status)
	}
}
func TestMeetingControlRecovery_PermanentConfigurationFailuresNeverAutoRetry(t *testing.T) {
	for _, message := range []string{"litellm: 400 invalid_request_error: unsupported parameter response_format", "litellm: 404 model custom-detector does not exist", "litellm: 422 Unprocessable Entity"} {
		t.Run(message, func(t *testing.T) {
			svc := newCandidateLLM(candidateStep{err: errors.New(message)})
			rig := eagerRig(t, svc, nil)
			id := rig.openMeeting(t, map[string]any{"pigeonId": "individualist", "autonomous": true})
			d := rig.candidates(t, id)
			rig.say(t, id, "an ordinary synthetic sentence")
			waitDetectorIdle(t, d, 1)
			st := d.status()
			rig.clock.skip(time.Second)
			d.onTick(rig.clock.Now())
			if st.Enabled {
				waitDetectorIdle(t, d, 2)
			}
			if st.Enabled || st.Reason != reasonDetectorConfiguration || len(svc.candidateCalls()) != 1 {
				t.Fatalf("permanent error produced status=%+v and %d model calls", st, len(svc.candidateCalls()))
			}
		})
	}
}

func TestMeetingControlRecovery_ResumeRequiresCurrentBarrierAndSettingsCannotBypassIt(t *testing.T) {
	rig := newMeetingRig(t, nil, nil)
	id := rig.openMeeting(t, map[string]any{"pigeonId": "individualist", "autonomous": true, "playbackTracking": true})
	s := rig.session(t, id)
	s.stopSpeaking()
	if status, _ := rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/resume", map[string]any{}); status != http.StatusBadRequest {
		t.Fatalf("missing barrier=%d", status)
	}
	s.applySettings(boolPointer(false), "", nil)
	s.applySettings(boolPointer(true), "", nil)
	if !s.settingsSnapshot().AutonomyPaused {
		t.Fatal("unversioned settings released Stop")
	}
	current := s.settingsSnapshot().TurnID
	if status, _ := rig.do(t, http.MethodPost, "/v1/meeting/sessions/"+id+"/resume", map[string]any{"turnId": current}); status != http.StatusAccepted {
		t.Fatalf("current resume=%d", status)
	}
	if s.settingsSnapshot().AutonomyPaused {
		t.Fatal("current explicit resume did not clear pause")
	}
	gen, _, cancel, ok := s.beginTurn(true)
	if !ok {
		t.Fatal("automatic turn remained blocked after valid resume")
	}
	cancel()
	s.endTurn(gen)
}
func TestMeetingControlRecovery_TransientFailuresStillRetryWithoutResettingBudget(t *testing.T) {
	for _, message := range []string{"litellm: 429 Too Many Requests", "litellm: 503 Service Unavailable", "litellm: 408 Request Timeout", "network timeout request_id=1401"} {
		t.Run(message, func(t *testing.T) {
			svc := newCandidateLLM(candidateStep{err: errors.New(message)}, pass())
			rig := eagerRig(t, svc, nil)
			id := rig.openMeeting(t, map[string]any{"pigeonId": "individualist", "autonomous": true})
			d := rig.candidates(t, id)
			rig.say(t, id, "an ordinary synthetic sentence")
			waitDetectorIdle(t, d, 1)
			if st := d.status(); !st.Enabled || st.State != "retrying" {
				t.Fatalf("transient failure not recoverable: %+v", st)
			}
			rig.clock.skip(time.Second)
			d.onTick(rig.clock.Now())
			waitDetectorIdle(t, d, 2)
			if d.status().State != "ready" || len(svc.candidateCalls()) != 2 {
				t.Fatal("transient recovery failed")
			}
		})
	}
	if got := classifyDetectorFailure(ErrBudgetExceeded); got != ReasonBudgetExceeded {
		t.Fatalf("budget=%s", got)
	}
}
