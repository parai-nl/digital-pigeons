package duif

import (
	"bytes"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestAccess_ConcurrencyQuotaAndRecovery(t *testing.T) {
	p := newAccessPolicy(nil)
	var mu sync.Mutex
	var releases []func()
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if release, ok := p.acquire("same", true); ok {
				mu.Lock()
				releases = append(releases, release)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(releases) != 2 {
		t.Fatalf("admitted=%d", len(releases))
	}
	for _, release := range releases {
		release()
		release()
	}
	if release, ok := p.acquire("another", true); !ok {
		t.Fatal("other account blocked")
	} else {
		release()
	}
}

func TestAccess_ProviderLogNeverCarriesMessageAttrsOrGroups(t *testing.T) {
	var buf bytes.Buffer
	logger := privateProviderLogger(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	logger.With("text", sentinel).WithGroup(sentinel).Debug(sentinel, "raw", sentinel, "error", sentinel)
	if strings.Contains(buf.String(), sentinel) || !strings.Contains(buf.String(), "provider event") {
		t.Fatal("provider record not redacted")
	}
}

// De versie staat twee keer: hier en in noticeCopy.ts, want de client stuurt
// hem mee bij het bevestigen. Lopen ze uiteen, dan antwoordt de server 409 op
// elke bevestiging en komt de lezer nooit voorbij de uitleg. Dat is geen fout
// die je in de UI ziet aankomen, dus valt hij hier om.
func TestNoticeVersion_MatchesTheFrontendCopy(t *testing.T) {
	source, err := os.ReadFile("frontend/src/lib/access/noticeCopy.ts")
	if err != nil {
		t.Fatalf("noticeCopy.ts onleesbaar: %v", err)
	}
	match := regexp.MustCompile(`noticeVersion\s*=\s*'([^']+)'`).FindSubmatch(source)
	if match == nil {
		t.Fatal("noticeCopy.ts noemt geen noticeVersion")
	}
	if got := string(match[1]); got != noticeVersion {
		t.Errorf("noticeCopy.ts staat op %q, de server op %q", got, noticeVersion)
	}
}
