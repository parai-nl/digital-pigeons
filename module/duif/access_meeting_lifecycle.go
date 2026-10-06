package duif

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

type meetingAuthorizationKey struct{}
type meetingAuthorizationBinder func(*meetingSession)

// The authorization belongs to the original meeting-open credential, not to
// whichever browser happens to hold an events connection. A normal shell
// logout may close SSE before its next access tick and make DELETE return 401.
// Keeping this guard on the session closes that gap without writing any data.
func (m *Module) authorizeMeetingLifetime(r *http.Request) *http.Request {
	if m.access == nil || m.access.valid == nil {
		return r
	}
	// Retain only credentials needed by the existing framework gate. Neither
	// request content, query strings, user context nor an arbitrary path belongs
	// in a background session monitor. This snapshot is never logged/persisted.
	probe := (&http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Path: "/api/duif/v1/health"},
		Header: make(http.Header),
	}).WithContext(context.Background())
	for _, name := range []string{"Cookie", "Authorization"} {
		probe.Header[name] = append([]string(nil), r.Header.Values(name)...)
	}
	bind := meetingAuthorizationBinder(func(s *meetingSession) {
		go m.watchMeetingAuthorization(s, probe)
	})
	return r.WithContext(context.WithValue(r.Context(), meetingAuthorizationKey{}, bind))
}

func bindMeetingAuthorization(r *http.Request, s *meetingSession) {
	if bind, ok := r.Context().Value(meetingAuthorizationKey{}).(meetingAuthorizationBinder); ok {
		bind(s)
	}
}

func (m *Module) watchMeetingAuthorization(s *meetingSession, probe *http.Request) {
	ticker := time.NewTicker(m.access.recheck)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if !m.access.valid(probe) {
				s.close("access_ended")
				return
			}
		}
	}
}
