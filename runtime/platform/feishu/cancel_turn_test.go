package feishu

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestCancelledTurnEndProgressFooter(t *testing.T) {
	p := &Platform{progressStyle: "card", showToolProgress: true}
	st := p.streamState(agentkit.SessionID("session-cancel"))
	st.startedAt = time.Now().Add(-2 * time.Second)
	st.progressStartedAt = st.startedAt
	st.steps = []toolStep{{Kind: toolStepKindTool, Name: "read", Status: "running"}}

	md := p.renderProgressMarkdown(st, false)
	md = appendCancelledProgressFooter(md, "/stop")
	if !strings.Contains(md, "> 已取消（/stop）") {
		t.Fatalf("expected cancelled footer, got %q", md)
	}
}

func TestCancelledBodyText(t *testing.T) {
	if got := cancelledBodyText("/stop"); got != "任务已取消（/stop）" {
		t.Fatalf("body text = %q", got)
	}
	if got := cancelledBodyText("cancelled"); got != "任务已取消" {
		t.Fatalf("body text = %q", got)
	}
}

func TestParseTurnEndDataCancelled(t *testing.T) {
	raw, err := json.Marshal(session.TurnEndData{Steps: 2, Cancelled: true, StopReason: "/stop"})
	if err != nil {
		t.Fatal(err)
	}
	data := parseTurnEndData(agentkit.OutboundEvent{Data: raw})
	if !data.Cancelled || data.StopReason != "/stop" || data.Steps != 2 {
		t.Fatalf("parsed = %+v", data)
	}
}

func TestDefaultReactionEmojis(t *testing.T) {
	cfg := Config{}
	cancelled := cfg.CancelledEmoji
	if cancelled == "" {
		cancelled = "HEARTBROKEN"
	}
	errEmoji := cfg.ErrorEmoji
	if errEmoji == "" {
		errEmoji = "CrossMark"
	}
	if cancelled != "HEARTBROKEN" {
		t.Fatalf("cancelledEmoji default = %q", cancelled)
	}
	if errEmoji != "CrossMark" {
		t.Fatalf("errorEmoji default = %q", errEmoji)
	}
}

func TestTurnTriggerPrefersStoredInboundMessage(t *testing.T) {
	p := &Platform{}
	sessionID := agentkit.SessionID("session-trigger")
	trigger := replyContext{messageID: "trigger-msg", processingReactionID: "rx-1"}
	stopMsg := replyContext{messageID: "stop-msg", processingReactionID: "rx-2"}
	p.turnTriggers.Store(sessionID, trigger)
	p.deliveries.Store(sessionID, stopMsg)

	got, ok := p.turnTriggerFor(sessionID)
	if !ok || got.messageID != "trigger-msg" {
		t.Fatalf("turn trigger = %+v, ok=%v", got, ok)
	}
	if _, still := p.turnTriggers.Load(sessionID); still {
		t.Fatal("turn trigger should be cleared after read")
	}
}
