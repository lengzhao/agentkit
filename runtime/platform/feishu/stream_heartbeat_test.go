package feishu

import (
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
)

func TestShouldHeartbeatFlush(t *testing.T) {
	st := &streamState{
		startedAt: time.Now(),
		status:    cardStatusWorking,
		progressHandle: &feishuPreviewHandle{messageID: "msg"},
	}
	if !shouldHeartbeatFlush(st) {
		t.Fatal("expected active rich stream to heartbeat")
	}

	st.status = cardStatusDone
	if shouldHeartbeatFlush(st) {
		t.Fatal("expected completed stream to skip heartbeat")
	}

	st.status = cardStatusWorking
	st.progressHandle = nil
	if shouldHeartbeatFlush(st) {
		t.Fatal("expected stream without progress handle to skip heartbeat")
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
