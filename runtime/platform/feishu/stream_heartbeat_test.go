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
	if !shouldHeartbeatFlush(st, false) {
		t.Fatal("expected active rich stream to heartbeat")
	}

	st.cardHandle = &feishuPreviewHandle{messageID: "card"}
	st.progressHandle = nil
	if shouldHeartbeatFlush(st, true) {
		t.Fatal("unified card progress is append-only and should not heartbeat")
	}

	st.status = cardStatusDone
	if shouldHeartbeatFlush(st, true) {
		t.Fatal("expected completed stream to skip heartbeat")
	}

	st.status = cardStatusWorking
	st.cardHandle = nil
	st.progressHandle = nil
	if shouldHeartbeatFlush(st, false) {
		t.Fatal("expected legacy stream without progress handle to skip heartbeat")
	}
	if shouldHeartbeatFlush(st, true) {
		t.Fatal("unified stream should not heartbeat")
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
