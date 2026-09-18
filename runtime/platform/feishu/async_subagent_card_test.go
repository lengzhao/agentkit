package feishu

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
)

func TestAsyncSubagentRoutesChildToolEventsNotParent(t *testing.T) {
	p := &Platform{
		progressStyle:             "card",
		showToolProgress:          true,
		useInteractiveCard:        true,
		asyncSubagentProgressCard: true,
	}
	streamKey := agentkit.SessionID("feishu:oc_test:reply:om_parent")

	card := &asyncSubagentCard{
		state:         streamStateLiteral(streamStateData{toolStepIdx: make(map[int]int), status: cardStatusWorking}),
		jobID:         "sub:job:1",
		streamKey:     streamKey,
		parentAgentID: agentkit.AgentID("main"),
		agent:         "cursor",
		task:          "refactor auth",
	}
	p.registerAsyncSubagentCard(card)

	parentEv := agentkit.OutboundEvent{AgentID: agentkit.AgentID("main"), Type: agentkit.EventMessageUpdate}
	if got := p.asyncSubagentForEvent(streamKey, parentEv); got != nil {
		t.Fatal("parent agent tool updates should not route to async card")
	}

	childEv := agentkit.OutboundEvent{AgentID: agentkit.AgentID("cursor"), Type: agentkit.EventMessageUpdate}
	if got := p.asyncSubagentForEvent(streamKey, childEv); got == nil {
		t.Fatal("child agent updates should route to async card")
	}
}

func TestHandleRichSubagentAsyncStartRegistersCard(t *testing.T) {
	p := &Platform{
		progressStyle:             "card",
		showToolProgress:          true,
		useInteractiveCard:        true,
		asyncSubagentProgressCard: true,
	}
	streamKey := agentkit.SessionID("feishu:oc_test:reply:om_parent")
	data := sessevents.SubagentStartData{
		Agent:   "cursor",
		Task:    "do work",
		Async:   true,
		JobID:   "sub:job:2",
		Session: "sub:job:2",
	}
	raw, _ := json.Marshal(data)
	route := rctx.SessionRouteFromDelivery("feishu", agentkit.SessionID("feishu:oc_test"), "om_parent")
	if outboundStreamKey(agentkit.OutboundEvent{Route: route}) != streamKey {
		t.Fatalf("stream key mismatch")
	}
	err := p.handleRichSubagentEvent(context.Background(), agentkit.OutboundEvent{
		AgentID: agentkit.AgentID("main"),
		Type:    agentkit.EventSubagentStart,
		Data:    raw,
		Route:   route,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.asyncSubagentForStream(streamKey) == nil {
		t.Fatal("expected async subagent card registered for stream")
	}
}

func TestAsyncSubagentCardAppliesToolStart(t *testing.T) {
	p := &Platform{
		progressStyle:             "card",
		showToolProgress:          true,
		showThinking:              true,
		useInteractiveCard:        true,
		asyncSubagentProgressCard: true,
	}
	streamKey := agentkit.SessionID("feishu:oc_test:reply:om_parent")
	card := &asyncSubagentCard{
		state:         streamStateLiteral(streamStateData{toolStepIdx: make(map[int]int)}),
		jobID:         "sub:job:3",
		streamKey:     streamKey,
		parentAgentID: agentkit.AgentID("assistant"),
		agent:         "cursor",
		task:          "analyze",
	}
	p.registerAsyncSubagentCard(card)

	handled, err := p.handleAsyncSubagentStreamUpdate(context.Background(), streamKey, agentkit.OutboundEvent{
		AgentID: agentkit.AgentID("cursor"),
		Type:    agentkit.EventMessageUpdate,
	}, agentkit.AssistantMessageEvent{
		Type:         agentkit.AssistantEventToolCallStart,
		ContentIndex: 2,
		ID:           "call_1",
		ToolName:     "Shell",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected tool start to be handled by async card")
	}
	st := card.state
	if len(st.steps) != 1 || st.steps[0].Status != "running" {
		t.Fatalf("steps = %+v, want one running tool step", st.steps)
	}
}
