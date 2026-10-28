package compaction

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capcompaction "github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
)

type contextSummaryProbe struct {
	t      *testing.T
	called bool
}

func (*contextSummaryProbe) Name() string { return "context-probe" }
func (p *contextSummaryProbe) Stream(_ context.Context, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	p.called = true
	raw, _ := json.Marshal(req.Messages)
	if strings.Contains(string(raw), "OLD_PROJECT_SECRET") {
		p.t.Error("old project summary sent to model")
	}
	if !strings.Contains(string(raw), "FIRST_NEW_MESSAGE") {
		p.t.Error("first new project message omitted")
	}
	return &summaryStream{text: "new project summary"}, nil
}
func TestSummaryIgnoresPreviousProjectCompaction(t *testing.T) {
	ctx := context.Background()
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "context"})
	old, _ := json.Marshal(capcompaction.EventData{Summary: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "OLD_PROJECT_SECRET"}}}})
	if _, err := mem.Append(ctx, agentkit.SessionEvent{Type: agentkit.EventCompaction, AgentID: "agent", Data: old}); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.Append(ctx, agentkit.SessionEvent{Type: agentkit.EventSessionContext, Data: []byte(`{"cwd":"/new","epoch":"new"}`)}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		text := fmt.Sprintf("new %d %s", i, strings.Repeat("x", 100))
		if i == 0 {
			text = "FIRST_NEW_MESSAGE " + text
		}
		if err := sessevents.Default.AppendMessage(ctx, mem, "agent", agentkit.EventUserMessage, agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: text}}}); err != nil {
			t.Fatal(err)
		}
	}
	messages, err := mem.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	probe := &contextSummaryProbe{t: t}
	svc, err := NewSummary(SummaryConfig{KeepRecentTokens: 1}, testSummaryDeps(t, probe))
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Compact(ctx, capcompaction.Request{Session: mem, AgentID: "agent", Messages: messages, Force: true})
	if err != nil || !result.Applied || !probe.called {
		t.Fatalf("applied=%v called=%v err=%v", result.Applied, probe.called, err)
	}
}
