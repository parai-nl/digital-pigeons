package duif

import (
	"bytes"
	"context"
	"errors"
	"github.com/coder/websocket"
	parai "github.com/parai-nl/parai-core"
	"github.com/parai-nl/parai-core/llm"
	"github.com/parai-nl/parai-core/llm/litellm"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type limitProvider struct {
	calls   int
	err     error
	request llm.CompletionRequest
	stream  chan llm.StreamChunk
}

// Exercises private providers through their public API, with real loopback
// HTTP/WebSocket traffic. No microphone or external endpoint is touched.
func TestAccess_PrivateProviderLogsRedactBeforeReturningThroughModule(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	sttDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "speech-to-text") {
			defer close(sttDone)
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer conn.CloseNow()
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"message_type":"partial_transcript","text":"`+sentinel+`"}`))
			for {
				if _, _, err := conn.Read(r.Context()); err != nil {
					return
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":{"message":"`+sentinel+`","type":"invalid_request_error"}}`)
	}))
	defer server.Close()
	cfg := &parai.Config{}
	cfg.LLM.BaseURL = server.URL
	cfg.LLM.APIKey = "synthetic-loopback-only"
	cfg.Audio.BaseURL = server.URL
	cfg.Audio.ElevenLabsAPIKey = "stub-key"
	provider := litellm.New(litellm.Config{BaseURL: server.URL, APIKey: "stub-key"})
	m := &Module{}
	if err := m.wirePrivateProviders(&parai.Core{Config: cfg, LLM: provider, Embedder: provider, Logger: logger}); err != nil {
		t.Fatal(err)
	}
	_, err := m.llm.Complete(context.Background(), llm.CompletionRequest{Model: "stub", Messages: []llm.Message{{Role: "user", Content: sentinel}}})
	if err == nil {
		t.Fatal("provider did not return fixture error")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	session, err := m.transcriber.Start(ctx, "en")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-session.Events:
		if event.Text != sentinel {
			t.Fatal("fixture transcript did not traverse private provider")
		}
	case <-ctx.Done():
		t.Fatal("no transcript")
	}
	_ = session.Close()
	<-session.Done
	<-sttDone
	if strings.Contains(logs.String(), sentinel) || !strings.Contains(logs.String(), "provider event") {
		t.Fatal("raw private provider log reached output")
	}
}

func (p *limitProvider) Complete(_ context.Context, r llm.CompletionRequest) (llm.CompletionResponse, error) {
	p.calls++
	p.request = r
	return llm.CompletionResponse{}, p.err
}
func (p *limitProvider) CompleteStream(_ context.Context, r llm.CompletionRequest) (<-chan llm.StreamChunk, error) {
	p.calls++
	p.request = r
	return p.stream, p.err
}

func TestAccess_ModelBudgetStopsEveryPathAndRecoversAfterBound(t *testing.T) {
	p := &limitProvider{err: errors.New("budget exceeded " + sentinel)}
	b := newBoundedLLM(p)
	now := time.Now()
	b.now = func() time.Time { return now }
	if _, err := b.Complete(context.Background(), llm.CompletionRequest{}); !errors.Is(err, ErrBudgetExceeded) || strings.Contains(err.Error(), sentinel) {
		t.Fatal("budget not sanitized")
	}
	if _, err := b.CompleteStream(context.Background(), llm.CompletionRequest{}); !errors.Is(err, ErrBudgetExceeded) || p.calls != 1 {
		t.Fatal("stream bypassed budget brake")
	}
	now = now.Add(5 * time.Minute)
	p.err = nil
	if _, err := b.Complete(context.Background(), llm.CompletionRequest{}); err != nil || p.calls != 2 || p.request.MaxTokens != 8192 {
		t.Fatal("bounded recovery failed")
	}
}

func TestAccess_ModelStreamHasDeadlineReleasesCapacityAndSanitizesErrors(t *testing.T) {
	p := &limitProvider{stream: make(chan llm.StreamChunk)}
	b := newBoundedLLM(p)
	b.timeout = 5 * time.Millisecond
	stream, err := b.CompleteStream(context.Background(), llm.CompletionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case chunk, ok := <-stream:
		if !ok || !errors.Is(chunk.Err, context.DeadlineExceeded) {
			t.Fatal("timeout looked like successful completion")
		}
	case <-time.After(time.Second):
		t.Fatal("hung provider was not bounded")
	}
	if _, ok := <-stream; ok {
		t.Fatal("stream continued after terminal timeout")
	}
	if len(b.slots) != 0 {
		t.Fatal("capacity leaked after timeout")
	}
	for _, raw := range []string{"status 401 " + sentinel, "network " + sentinel} {
		if strings.Contains(b.safeError(errors.New(raw)).Error(), sentinel) {
			t.Fatal("provider content leaked")
		}
	}
}

func TestAccess_ModelTimeoutStillReportsFailureToLateReader(t *testing.T) {
	p := &limitProvider{stream: make(chan llm.StreamChunk, 2)}
	p.stream <- llm.StreamChunk{Content: "Partial text"}
	p.stream <- llm.StreamChunk{Content: "Unconsumed text"}
	b := newBoundedLLM(p)
	b.timeout = 10 * time.Millisecond
	stream, err := b.CompleteStream(context.Background(), llm.CompletionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	// The downstream reader can pause, but must later observe failure, and
	// capacity must be released without waiting for it to resume.
	deadline := time.Now().Add(time.Second)
	for len(b.slots) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(b.slots) != 0 {
		t.Fatal("late reader kept provider capacity occupied")
	}
	var terminal error
	for chunk := range stream {
		terminal = chunk.Err
	}
	if !errors.Is(terminal, context.DeadlineExceeded) {
		t.Fatal("partial answer was closed without timeout failure")
	}
}

func TestAccess_MeetingLimitAtomicAndReleasedByEnd(t *testing.T) {
	m, _ := newTestModule(t, nil)
	e := m.newMeetingEngine()
	e.maxSessions = 2
	e.maxUserSessions = 1
	t.Cleanup(e.shutdown)
	opened := make(chan *meetingSession, 16)
	for i := 0; i < 16; i++ {
		go func() { s, _ := e.open("one", "biological", "en"); opened <- s }()
	}
	var first *meetingSession
	for i := 0; i < 16; i++ {
		if s := <-opened; s != nil {
			if first != nil {
				t.Fatal("concurrent opens bypassed per-account limit")
			}
			first = s
		}
	}
	if first == nil {
		t.Fatal("no session admitted")
	}
	var err error
	if _, err = e.open("one", "biological", "en"); !errors.Is(err, errMeetingCapacity) {
		t.Fatal("account limit absent")
	}
	if _, err = e.open("two", "biological", "en"); err != nil {
		t.Fatal("second account should fit")
	}
	if _, err = e.open("three", "biological", "en"); !errors.Is(err, errMeetingCapacity) {
		t.Fatal("global limit absent")
	}
	first.close(CloseReasonUser)
	if _, err = e.open("one", "biological", "en"); err != nil {
		t.Fatal("closed session did not release capacity")
	}
}
