package chatapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
)

func startRunSSE(p *Platform, run *runState, sse *sseWriter, convID string) {
	go p.serveRunSSE(context.Background(), run, sse, convID, run.id)
}

func TestDisconnectDoesNotRemoveRun(t *testing.T) {
	p, err := New(Config{ListenAddr: ":0"}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	conv, err := plat.conversations.create("default_channel", "u1")
	if err != nil {
		t.Fatal(err)
	}
	sessionID := agentkit.SessionID(engineSessionKey("default_channel", conv.ID))
	runID := newRunID()
	rec := httptest.NewRecorder()
	sse, err := newSSEWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	run := newRunState(runID, "u1", "default_channel", "", sessionID, conv.ID, "m1", plat, sse)
	if !plat.pending.create(run) {
		t.Fatal("pending create failed")
	}
	plat.setActiveConv(conv.ID, runID)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		plat.serveRunSSE(ctx, run, sse, conv.ID, runID)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("serveRunSSE did not return after disconnect")
	}
	if plat.pending.get(runID) == nil {
		t.Fatal("run should remain after disconnect while turn active")
	}
}

func TestResumeReplaysLastTextDeltaAfterDisconnect(t *testing.T) {
	p, err := New(Config{ListenAddr: ":0"}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	conv, err := plat.conversations.create("default_channel", "u1")
	if err != nil {
		t.Fatal(err)
	}
	sessionID := agentkit.SessionID(engineSessionKey("default_channel", conv.ID))
	runID := newRunID()
	rec := httptest.NewRecorder()
	sse, err := newSSEWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	run := newRunState(runID, "u1", "default_channel", "", sessionID, conv.ID, "m1", plat, sse)
	if !plat.pending.create(run) {
		t.Fatal("pending create failed")
	}
	plat.setActiveConv(conv.ID, runID)

	ctx, cancel := context.WithCancel(context.Background())
	go plat.serveRunSSE(ctx, run, sse, conv.ID, runID)

	release := make(chan struct{})
	go func() {
		<-release
		run.mu.Lock()
		run.answerText = "final-after-disconnect"
		run.mu.Unlock()
		run.signal()
		time.Sleep(80 * time.Millisecond)
		plat.pending.finish(runID, pendingResult{answer: "final-after-disconnect"})
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()
	time.Sleep(30 * time.Millisecond)
	close(release)
	time.Sleep(60 * time.Millisecond)

	run = plat.pending.get(runID)
	if run == nil {
		t.Fatal("run should remain after disconnect while turn active")
	}
	run.mu.Lock()
	ev := run.lastRecoverableEvent
	run.mu.Unlock()
	if ev == nil || ev.name != "text_delta" {
		t.Fatalf("expected cached text_delta, got %#v", ev)
	}
	payload, _ := ev.payload.(map[string]any)
	if payload["text"] != "final-after-disconnect" {
		t.Fatalf("cached text = %#v", payload)
	}
	if payload["replace"] != true {
		t.Fatalf("expected replace:true, got %#v", payload)
	}

	resumeReq := httptest.NewRequest(http.MethodPost, "/v1/chat-messages",
		strings.NewReader(`{"run_id":`+jsonQuote(runID)+`}`))
	resumeReq.Header.Set("X-Chat-API-Channel", "default_channel")
	resumeReq.Header.Set("X-Chat-API-User", "u1")
	resumeReq.Header.Set("Content-Type", "application/json")
	resumeReq.Header.Set("Accept", "text/event-stream")
	resumeRec := httptest.NewRecorder()
	plat.handleChatMessages(resumeRec, resumeReq)

	events := parseSSE(resumeRec.Body.String())
	if !hasSSEEvent(events, "text_delta") {
		t.Fatalf("missing text_delta on resume: %#v body=%s", events, resumeRec.Body.String())
	}
	foundReplace := false
	for _, e := range events {
		if e.Name != "text_delta" {
			continue
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(e.Data), &m)
		if m["text"] == "final-after-disconnect" && m["replace"] == true {
			foundReplace = true
		}
	}
	if !foundReplace {
		t.Fatalf("resume text_delta missing replace snapshot: %s", resumeRec.Body.String())
	}
	if !hasSSEEvent(events, "message_end") {
		t.Fatalf("missing message_end: %#v", events)
	}
}

func TestResumeUnknownRunReturnsMessageEnd(t *testing.T) {
	p, err := New(Config{ListenAddr: ":0"}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat-messages",
		strings.NewReader(`{"run_id":"run_does_not_exist"}`))
	req.Header.Set("X-Chat-API-Channel", "default_channel")
	req.Header.Set("X-Chat-API-User", "u1")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	rec := httptest.NewRecorder()
	plat.handleChatMessages(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if !hasSSEEvent(parseSSE(rec.Body.String()), "message_end") {
		t.Fatalf("missing message_end: %s", rec.Body.String())
	}
}

func TestResumeWhileAttachedReturnsConflict(t *testing.T) {
	p, err := New(Config{ListenAddr: ":0"}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	conv, err := plat.conversations.create("default_channel", "u1")
	if err != nil {
		t.Fatal(err)
	}
	sessionID := agentkit.SessionID(engineSessionKey("default_channel", conv.ID))
	runID := newRunID()
	rec := httptest.NewRecorder()
	sse, err := newSSEWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	run := newRunState(runID, "u1", "default_channel", "", sessionID, conv.ID, "m1", plat, sse)
	if !plat.pending.create(run) {
		t.Fatal("pending create failed")
	}
	plat.setActiveConv(conv.ID, runID)
	go plat.serveRunSSE(context.Background(), run, sse, conv.ID, runID)

	resumeReq := httptest.NewRequest(http.MethodPost, "/v1/chat-messages",
		strings.NewReader(`{"run_id":`+jsonQuote(runID)+`}`))
	resumeReq.Header.Set("X-Chat-API-Channel", "default_channel")
	resumeReq.Header.Set("X-Chat-API-User", "u1")
	resumeReq.Header.Set("Content-Type", "application/json")
	resumeReq.Header.Set("Accept", "text/event-stream")
	resumeRec := httptest.NewRecorder()
	plat.handleChatMessages(resumeRec, resumeReq)
	if resumeRec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s, want 409", resumeRec.Code, resumeRec.Body.String())
	}
}

func TestDetachSeedsPingWhenNoRecoverableCache(t *testing.T) {
	p, err := New(Config{ListenAddr: ":0"}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	run := newRunState("run_ping", "u", "ch", "", "sess", "c", "c:0", p.(*Platform), nil)
	run.detach()
	ev := run.peekLastRecoverable()
	if ev == nil || ev.name != "ping" {
		t.Fatalf("expected ping cache on disconnect, got %#v", ev)
	}
	payload, _ := ev.payload.(map[string]any)
	if payload["run_id"] != "run_ping" {
		t.Fatalf("ping run_id=%#v", payload["run_id"])
	}
}

func TestResumeWakesWhenFinishAfterAttach(t *testing.T) {
	p, err := New(Config{ListenAddr: ":0"}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	conv, err := plat.conversations.create("default_channel", "u1")
	if err != nil {
		t.Fatal(err)
	}
	sessionID := agentkit.SessionID(engineSessionKey("default_channel", conv.ID))
	runID := newRunID()
	rec := httptest.NewRecorder()
	sse, err := newSSEWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	run := newRunState(runID, "u1", "default_channel", "", sessionID, conv.ID, "m1", plat, sse)
	if !plat.pending.create(run) {
		t.Fatal("pending create failed")
	}
	plat.setActiveConv(conv.ID, runID)

	ctx, cancel := context.WithCancel(context.Background())
	go plat.serveRunSSE(ctx, run, sse, conv.ID, runID)
	cancel()
	time.Sleep(30 * time.Millisecond)

	resumeReq := httptest.NewRequest(http.MethodPost, "/v1/chat-messages",
		strings.NewReader(`{"run_id":`+jsonQuote(runID)+`}`))
	resumeReq.Header.Set("X-Chat-API-Channel", "default_channel")
	resumeReq.Header.Set("X-Chat-API-User", "u1")
	resumeReq.Header.Set("Content-Type", "application/json")
	resumeReq.Header.Set("Accept", "text/event-stream")
	resumeRec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		plat.handleChatMessages(resumeRec, resumeReq)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	plat.pending.finish(runID, pendingResult{answer: "done-after-resume"})

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("resume SSE hung after detached finish; want message_end wake")
	}
	if !hasSSEEvent(parseSSE(resumeRec.Body.String()), "message_end") {
		t.Fatalf("missing message_end: %s", resumeRec.Body.String())
	}
	if plat.pending.get(runID) != nil {
		t.Fatal("run should be deleted after resume consumed terminal")
	}
}

type sseEvent struct {
	Name string
	Data string
}

func parseSSE(body string) []sseEvent {
	var events []sseEvent
	for _, block := range strings.Split(body, "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var name, data string
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "event: ") {
				name = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") {
				data = strings.TrimPrefix(line, "data: ")
			}
		}
		if name != "" {
			events = append(events, sseEvent{Name: name, Data: data})
		}
	}
	return events
}

func hasSSEEvent(events []sseEvent, name string) bool {
	for _, e := range events {
		if e.Name == name {
			return true
		}
	}
	return false
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
