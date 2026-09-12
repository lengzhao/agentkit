package feishu

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
)

func TestFormatStreamHeartbeatTimestamp(t *testing.T) {
	tm := time.Date(2026, 9, 11, 15, 4, 5, 0, time.UTC)
	got := formatStreamHeartbeatTimestamp(tm)
	if got != "2026-09-11 15:04:05" {
		t.Fatalf("timestamp = %q", got)
	}
}

func TestIsCardStreamingClosedError(t *testing.T) {
	err := fmt.Errorf("feishu: stream card content code=300309 msg=ErrMsg: streaming mode is closed")
	if !isCardStreamingClosedError(err) {
		t.Fatal("expected streaming closed detection")
	}
}

func TestUnifiedProgressStreamMarkdownIncludesHeartbeats(t *testing.T) {
	p := &Platform{progressStyle: "card", showToolProgress: true}
	st := &streamState{
		steps: []toolStep{
			{Kind: toolStepKindTool, Name: "Terminal", Summary: "git checkout dev"},
		},
		progressHeartbeatLines: []string{
			"> ⏱ 2026-09-12 21:00:00",
			"> ⏱ 2026-09-12 21:00:10",
		},
	}
	got := p.unifiedProgressStreamMarkdown(st, true)
	if !strings.Contains(got, "**Terminal**") {
		t.Fatalf("expected tool line in progress stream, got %q", got)
	}
	if !strings.Contains(got, "> ⏱ 2026-09-12 21:00:00") || !strings.Contains(got, "> ⏱ 2026-09-12 21:00:10") {
		t.Fatalf("expected heartbeat lines after tools, got %q", got)
	}
	i := strings.Index(got, "> ⏱ 2026-09-12 21:00:00")
	j := strings.Index(got, "**Terminal**")
	if i < j {
		t.Fatalf("heartbeats should follow tool lines, got %q", got)
	}
}

func TestUnifiedTurnEndBodyMarkdownIncludesHeartbeats(t *testing.T) {
	st := &streamState{
		bodyText: "你好",
		bodyHeartbeatLines: []string{
			"> ⏱ 2026-09-12 21:00:00",
			"> ⏱ 2026-09-12 21:00:10",
		},
		lastStreamedBody: "你好\n> ⏱ 2026-09-12 21:00:00",
	}
	got := unifiedTurnEndBodyMarkdown(st)
	if !strings.Contains(got, "你好") {
		t.Fatalf("missing body: %q", got)
	}
	if !strings.Contains(got, "> ⏱ 2026-09-12 21:00:10") {
		t.Fatalf("expected all heartbeat lines, got %q", got)
	}
}

func TestFlushUnifiedStreamTimestampAppendsHeartbeatLine(t *testing.T) {
	p := &Platform{progressStyle: "card", showToolProgress: true}
	sessionID := agentkit.SessionID("sess-heartbeat")
	st := p.streamState(sessionID)
	st.mu.Lock()
	st.startedAt = time.Now()
	st.mu.Unlock()

	if err := p.flushUnifiedStreamTimestamp(context.Background(), sessionID); err != nil {
		t.Fatalf("flush: %v", err)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.progressHeartbeatLines) != 1 {
		t.Fatalf("expected 1 heartbeat line, got %d", len(st.progressHeartbeatLines))
	}
	if !strings.HasPrefix(st.progressHeartbeatLines[0], "> ⏱ ") {
		t.Fatalf("unexpected line %q", st.progressHeartbeatLines[0])
	}
}

func TestUnifiedTurnEndProgressMarkdownMergesHeartbeatsBeforeFooter(t *testing.T) {
	p := &Platform{progressStyle: "card", showToolProgress: true}
	st := &streamState{
		startedAt: time.Now().Add(-time.Minute),
		steps: []toolStep{
			{Kind: toolStepKindTool, Name: "Terminal", Summary: "ok"},
		},
		progressHeartbeatLines: []string{"> ⏱ 2026-09-12 21:00:00"},
	}
	got := p.unifiedTurnEndProgressMarkdown(st)
	if !strings.Contains(got, "**Terminal**") {
		t.Fatalf("missing tool line: %q", got)
	}
	if !strings.Contains(got, "> ⏱ 2026-09-12 21:00:00") {
		t.Fatalf("missing heartbeat: %q", got)
	}
	footerIdx := strings.Index(got, "> ⏱ 用时")
	hbIdx := strings.Index(got, "> ⏱ 2026-09-12")
	if footerIdx < 0 || hbIdx < 0 || hbIdx >= footerIdx {
		t.Fatalf("heartbeat should precede summary footer, got %q", got)
	}
}

func TestUnifiedTurnEndBodyMarkdownFallsBackToLastStreamed(t *testing.T) {
	st := &streamState{
		lastStreamedBody: "仅 flush 过的正文\n> ⏱ 2026-09-12 21:00:00",
	}
	got := unifiedTurnEndBodyMarkdown(st)
	if got != st.lastStreamedBody {
		t.Fatalf("got %q, want lastStreamedBody", got)
	}
}

func TestMergeProgressHeartbeatsBeforeFooter(t *testing.T) {
	progressMD := "- **Terminal** ok\n\n---\n\n> ⏱ 用时 1 分 · 1 个工具"
	lines := []string{"> ⏱ 2026-09-12 21:00:00", "> ⏱ 2026-09-12 21:00:10"}
	got := mergeProgressHeartbeatsBeforeFooter(progressMD, lines)
	if !strings.Contains(got, "**Terminal**") {
		t.Fatalf("missing tool line: %q", got)
	}
	if !strings.Contains(got, "> ⏱ 2026-09-12 21:00:10") {
		t.Fatalf("missing heartbeat: %q", got)
	}
	if !strings.Contains(got, "> ⏱ 用时 1 分") {
		t.Fatalf("missing footer: %q", got)
	}
	footerIdx := strings.Index(got, "> ⏱ 用时")
	hbIdx := strings.Index(got, "> ⏱ 2026-09-12 21:00:10")
	if hbIdx >= footerIdx {
		t.Fatalf("heartbeats should appear before summary footer, got %q", got)
	}
}
