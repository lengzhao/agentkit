package feishu

import (
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
)

func TestShouldCardReactionHeartbeat(t *testing.T) {
	st := &streamState{
		startedAt: time.Now(),
		status:    cardStatusWorking,
	}
	if shouldCardReactionHeartbeat(st) {
		t.Fatal("expected no heartbeat before reply card exists")
	}

	st.progressHandle = &feishuPreviewHandle{messageID: "msg"}
	if !shouldCardReactionHeartbeat(st) {
		t.Fatal("expected heartbeat when progress card is active")
	}

	st.status = cardStatusDone
	if shouldCardReactionHeartbeat(st) {
		t.Fatal("expected completed stream to skip heartbeat")
	}
}

func TestClearStreamStopsProgressHeartbeat(t *testing.T) {
	p := &Platform{progressStyle: "card"}
	sessionID := agentkit.SessionID("session-1")
	st := p.streamState(sessionID)
	stopped := make(chan struct{})
	st.mu.Lock()
	st.heartbeatStop = func() { close(stopped) }
	st.mu.Unlock()

	p.clearStream(sessionID)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("expected clearStream to stop progress heartbeat")
	}
	if _, ok := p.streams.Load(sessionID); ok {
		t.Fatal("expected stream state to be removed")
	}
}
