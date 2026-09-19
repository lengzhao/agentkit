package loop_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/telemetry"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

type stubAgent struct {
	id      agentkit.AgentID
	runTurn func(context.Context, agentkit.TurnInput) error
}

func (a stubAgent) ID() agentkit.AgentID { return a.id }
func (a stubAgent) RunTurn(ctx context.Context, input agentkit.TurnInput) error {
	if a.runTurn != nil {
		return a.runTurn(ctx, input)
	}
	return nil
}

func TestDispatchRecordsTelemetryTurn(t *testing.T) {
	t.Parallel()

	rec := &telemetry.RecordingExporter{}
	l, err := loop.New(loop.Config{}, loop.Deps{
		Agents:    []agentkit.Agent{stubAgent{id: "coder"}},
		Telemetry: rec,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = l.Dispatch(context.Background(), agenttest.LoopRequest("cli:default", agentkit.MessageEvent{
		AgentID:    "coder",
		PlatformID: "cli",
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
		},
	}))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	turns, _, _ := rec.Snapshot()
	if len(turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(turns))
	}
	if turns[0].Meta.SessionID != "cli:default" {
		t.Fatalf("session id = %q", turns[0].Meta.SessionID)
	}
	if turns[0].Meta.Input == "" {
		t.Fatal("expected turn input summary")
	}
}

// 平台入站记录的真实附件信息（size/mime/path）必须由 loop 发出一个独立的
// inbound.attachments span，携带结构化 metadata，便于在 Langfuse 上按大小/类型过滤。
func TestDispatchEmitsInboundAttachmentsSpan(t *testing.T) {
	t.Parallel()

	rec := &telemetry.RecordingExporter{}
	l, err := loop.New(loop.Config{}, loop.Deps{
		Agents:    []agentkit.Agent{stubAgent{id: "coder"}},
		Telemetry: rec,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = l.Dispatch(context.Background(), agenttest.LoopRequest("cli:default", agentkit.MessageEvent{
		AgentID:    "coder",
		PlatformID: "cli",
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "看这张图"}},
		},
		Attachments: []agentkit.InboundAttachment{
			{Path: "upload/x.png", MIME: "image/png", Size: 12345, OrigName: "x.png", Image: true},
			{Path: "upload/big.log", MIME: "text/plain", Size: 200000, OrigName: "big.log"},
		},
	}))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	_, observations, _ := rec.Snapshot()
	var span telemetry.RecordedObservation
	for _, obs := range observations {
		if obs.Meta.Name == "inbound.attachments" {
			span = obs
			break
		}
	}
	if span.ID == "" {
		t.Fatalf("no inbound.attachments span emitted; observations = %#v", observations)
	}
	if span.Meta.Kind != captelemetry.KindSpan {
		t.Fatalf("attachment observation kind = %q, want span", span.Meta.Kind)
	}
	if span.Meta.Attributes["attachment_count"] != "2" {
		t.Fatalf("attachment_count = %q, want 2", span.Meta.Attributes["attachment_count"])
	}
	if span.Meta.Attributes["attachment_bytes"] != "212345" {
		t.Fatalf("attachment_bytes = %q, want 212345", span.Meta.Attributes["attachment_bytes"])
	}
	if span.Meta.Attributes["attachments"] == "" {
		t.Fatal("missing structured attachments JSON in span metadata")
	}
}

// 没有附件的 turn 不应发出 inbound.attachments span。
func TestDispatchOmitsAttachmentSpanWhenNoAttachments(t *testing.T) {
	t.Parallel()

	rec := &telemetry.RecordingExporter{}
	l, err := loop.New(loop.Config{}, loop.Deps{
		Agents:    []agentkit.Agent{stubAgent{id: "coder"}},
		Telemetry: rec,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := l.Dispatch(context.Background(), agenttest.LoopRequest("cli:default", agentkit.MessageEvent{
		AgentID:    "coder",
		PlatformID: "cli",
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
		},
	})); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	_, observations, _ := rec.Snapshot()
	for _, obs := range observations {
		if obs.Meta.Name == "inbound.attachments" {
			t.Fatalf("unexpected attachment span emitted: %#v", obs)
		}
	}
}

// 附件属于首个 turn 的输入消息：follow-up turn 复用同一个 req，但不得重复发
// inbound.attachments span（否则后续 trace 会错误地带上首轮的附件）。
func TestDispatchEmitsAttachmentSpanOnlyOnFirstTurn(t *testing.T) {
	t.Parallel()

	followedUp := false
	ag := stubAgent{id: "coder", runTurn: func(ctx context.Context, _ agentkit.TurnInput) error {
		if followedUp {
			return nil
		}
		followedUp = true
		ctrl, _ := ctx.Value(agentkit.KeySessionControl).(interface {
			FollowUp(context.Context, agentkit.ModelMessage) error
		})
		if ctrl == nil {
			t.Fatal("no session control in ctx")
		}
		return ctrl.FollowUp(ctx, agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "接着改"}},
		})
	}}

	rec := &telemetry.RecordingExporter{}
	l, err := loop.New(loop.Config{}, loop.Deps{
		Agents:    []agentkit.Agent{ag},
		Telemetry: rec,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = l.Dispatch(context.Background(), agenttest.LoopRequest("cli:default", agentkit.MessageEvent{
		AgentID:    "coder",
		PlatformID: "cli",
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "看这张图"}},
		},
		Attachments: []agentkit.InboundAttachment{
			{Path: "upload/x.png", MIME: "image/png", Size: 12345, OrigName: "x.png", Image: true},
		},
	}))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if !followedUp {
		t.Fatal("follow-up turn never ran")
	}

	turns, observations, _ := rec.Snapshot()
	if len(turns) != 2 {
		t.Fatalf("turns = %d, want 2 (initial + follow-up)", len(turns))
	}
	spanCount := 0
	for _, obs := range observations {
		if obs.Meta.Name == "inbound.attachments" {
			spanCount++
		}
	}
	if spanCount != 1 {
		t.Fatalf("inbound.attachments spans = %d, want 1 (first turn only)", spanCount)
	}
}
