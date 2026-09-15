package feishu

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
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
		jobID:         "sub:job:1",
		streamKey:     streamKey,
		parentAgentID: agentkit.AgentID("main"),
		agent:         "cursor",
		task:          "refactor auth",
		toolStepIdx:   make(map[int]int),
		status:        cardStatusWorking,
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
	data := session.SubagentStartData{
		Agent:   "cursor",
		Task:    "do work",
		Async:   true,
		JobID:   "sub:job:2",
		Session: "sub:job:2",
	}
	raw, _ := json.Marshal(data)
	route := session.SessionRouteFromDelivery("feishu", agentkit.SessionID("feishu:oc_test"), "om_parent")
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
