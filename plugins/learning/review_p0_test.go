package learning

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/lengzhao/agentkit"
	rtlearning "github.com/lengzhao/agentkit/runtime/learning"
	rttools "github.com/lengzhao/agentkit/runtime/tools"
	"github.com/lengzhao/agentkit/runtime/session"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

type reviewScriptedLLM struct {
	steps []agentkit.ModelMessage
}

func (m *reviewScriptedLLM) Name() string { return "review-scripted" }

func (m *reviewScriptedLLM) Stream(ctx context.Context, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	if len(m.steps) == 0 {
		return &oneShotStream{msg: agentkit.ModelMessage{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: "ok"}}}}, nil
	}
	msg := m.steps[0]
	m.steps = m.steps[1:]
	return &oneShotStream{msg: msg}, nil
}

type oneShotStream struct {
	msg agentkit.ModelMessage
	done bool
}

func (s *oneShotStream) Recv() (agentkit.LLMEvent, error) {
	if s.done {
		return agentkit.LLMEvent{}, io.EOF
	}
	s.done = true
	return agentkit.LLMEvent{Type: agentkit.LLMEventMessage, Message: &s.msg}, io.EOF
}

func (s *oneShotStream) Close() error { return nil }

func TestRunReviewStagesMemoryWithScriptedLLM(t *testing.T) {
	dir := t.TempDir()
	ws, err := rtworkspace.New(rtworkspace.Config{Local: dir, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	approve := true
	svc := &Service{memoryRoot: ".", review: ReviewServiceConfig{WriteApproval: &approve}, workspace: ws}
	tool, err := NewLearnCapture(struct{}{}, struct {
		Learning *Service `json:"learning"`
	}{Learning: svc})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := rttools.NewRuntime(rttools.RuntimeConfig{AllowTools: []string{"learn_capture"}}, rttools.RuntimeDeps{Tools: []agentkit.Tool{tool}})
	if err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(rtlearning.CaptureInput{Action: "memory_add", Content: "likes integration tests"})
	llm := &reviewScriptedLLM{steps: []agentkit.ModelMessage{{
		Role: "assistant",
		ToolCalls: []agentkit.ToolCall{{
			ID:    "c1",
			Name:  "learn_capture",
			Input: json.RawMessage(args),
		}},
	}}}
	ctx := session.WithWorkspaceService(context.Background(), ws)
	ctx = session.WithConversation(ctx, "test-session")

	_, err = rtlearning.RunReview(ctx, rtlearning.ReviewConfig{MaxSteps: 3}, llm, rt, "test-model", []agentkit.ModelMessage{
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "please remember I like tests"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := svc.stagedStore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Content != "likes integration tests" {
		t.Fatalf("staged = %+v", list)
	}
}
