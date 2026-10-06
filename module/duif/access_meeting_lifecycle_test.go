package duif

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMeetingAuthorization_OutlivesOpenRequestAndDisconnectedEvents(t *testing.T) {
	var allowed atomic.Bool
	allowed.Store(true)
	var calls atomic.Int32
	m, _ := newTestModule(t, nil)
	m.access = newAccessPolicy(nil)
	m.access.recheck = 2 * time.Millisecond
	m.access.valid = func(r *http.Request) bool {
		calls.Add(1)
		if r.Header.Get("Cookie") != "sid=synthetic-original" || r.Header.Get("Authorization") != "Bearer synthetic" {
			t.Error("original credentials changed")
		}
		if r.Body != nil || r.URL.RawQuery != "" || r.URL.Path != "/api/duif/v1/health" || r.Header.Get("X-Private") != "" || r.Context().Err() != nil {
			t.Error("request content or cancelled context retained")
		}
		return allowed.Load()
	}
	e := m.newMeetingEngine()
	t.Cleanup(e.shutdown)
	s, err := e.open(testUser.ID, "nonverbal", "en")
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.transcript.Append(TranscriptSegment{Text: sentinel, At: e.clock.Now(), Seq: 1})
	s.mu.Unlock()
	requestCtx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("POST", "/api/duif/v1/meeting/sessions?private="+sentinel, strings.NewReader(sentinel)).WithContext(requestCtx)
	r.Header.Set("Cookie", "sid=synthetic-original")
	r.Header.Set("Authorization", "Bearer synthetic")
	r.Header.Set("X-Private", sentinel)
	r = m.authorizeMeetingLifetime(r)
	bindMeetingAuthorization(r, s)
	_, disconnect := s.subscribe()
	disconnect()
	cancel()
	waitFor(t, func() bool { return calls.Load() > 0 }, "session authorization did not run after HTTP/SSE ended")
	if s.isClosed() {
		t.Fatal("valid disconnected session was closed")
	}
	allowed.Store(false)
	waitFor(t, s.isClosed, "revoked original credential left meeting alive")
	waitFor(t, func() bool { return s.ctx.Err() != nil }, "revocation did not cancel provider/detector context")
	if _, ok := e.session(s.id, testUser.ID); ok {
		t.Fatal("closed meeting remains addressable to same owner")
	}
	s.mu.Lock()
	n := len(s.transcript.segments)
	s.mu.Unlock()
	if n != 0 {
		t.Fatal("revocation retained meeting contents")
	}
	if s.ctx.Err() == nil {
		t.Fatal("revocation did not cancel provider/detector context")
	}
	before := calls.Load()
	time.Sleep(10 * time.Millisecond)
	if calls.Load() != before {
		t.Fatal("authorization monitor continued after session close")
	}
}

func TestMeetingAuthorization_NormalEndDoesNotRevokeOtherSessions(t *testing.T) {
	m, _ := newTestModule(t, nil)
	m.access = newAccessPolicy(nil)
	m.access.recheck = time.Hour
	var called atomic.Bool
	m.access.valid = func(*http.Request) bool { called.Store(true); return false }
	e := m.newMeetingEngine()
	t.Cleanup(e.shutdown)
	a, _ := e.open("owner-a", "nonverbal", "en")
	b, _ := e.open("owner-b", "nonverbal", "en")
	r := m.authorizeMeetingLifetime(httptest.NewRequest("POST", "/api/duif/v1/meeting/sessions", nil))
	bindMeetingAuthorization(r, a)
	a.close(CloseReasonUser)
	time.Sleep(5 * time.Millisecond)
	if called.Load() || b.isClosed() {
		t.Fatal("normal session end affected authorization/other owner")
	}
}
