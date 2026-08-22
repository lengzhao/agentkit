package agent_test

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/runtime/sessioncontrol"
	"github.com/lengzhao/agentkit/runtime/tools"
)

// blockingLLM blocks the first Stream.Recv until steer cancels the step context.
type blockingLLM struct {
	release chan struct{}
	started chan struct{}
	calls   int
}

func (b *blockingLLM) Name() string { return "blocking" }

func (b *blockingLLM) Stream(ctx context.Context, _ agentkit.LLMRequest) (agentkit.LLMStream, error) {
	b.calls++
	if b.calls == 1 {
		return &blockingStream{ctx: ctx, started: b.started, release: b.release}, nil
	}
	msg := agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: "second step"}},
	}
	return &instantStream{msg: msg}, nil
}

type blockingStream struct {
	ctx     context.Context
	started chan struct{}
	release chan struct{}
	once    sync.Once
	done    bool
}

func (s *blockingStream) Recv() (agentkit.LLMEvent, error) {
	s.once.Do(func() { close(s.started) })
	if s.done {
		return agentkit.LLMEvent{}, io.EOF
	}
	select {
	case <-s.ctx.Done():
		return agentkit.LLMEvent{}, s.ctx.Err()
	case <-s.release:
		s.done = true
		msg := agentkit.ModelMessage{
			Role:    "assistant",
			Content: []agentkit.ContentPart{{Type: "text", Text: "after release"}},
		}
		return agentkit.LLMEvent{Type: agentkit.LLMEventMessage, Message: &msg}, nil
	}
}

func (s *blockingStream) Close() error { return nil }

func TestSteerInterruptsInFlightStep(t *testing.T) {
	t.Parallel()

	block := &blockingLLM{
		release: make(chan struct{}),
		started: make(chan struct{}),
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	mem, err := session.NewMemory(session.MemoryConfig{ID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	toolRuntime, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := agent.New(agent.Config{ID: "test", Model: "blocking", MaxSteps: 5}, agent.Deps{
		LLM:     block,
		Session: mem,
		Tools:   toolRuntime,
		Prompt:  assembler,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ctrl := sessioncontrol.New()
	turnDone := make(chan error, 1)
	go func() {
		_, err := rt.RunTurn(ctx, agentkit.TurnInput{
			Control: ctrl,
			Message: agentkit.ModelMessage{
				Role:    "user",
				Content: []agentkit.ContentPart{{Type: "text", Text: "start"}},
			},
		})
		turnDone <- err
	}()

	select {
	case <-block.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for LLM stream to start")
	}

	if err := ctrl.Steer(ctx, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "steered mid-turn"}},
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-turnDone:
		if err != nil {
			t.Fatalf("run turn: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for turn to finish after steer")
	}

	msgs, err := mem.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, msg := range msgs {
		if msg.Role == "user" && len(msg.Content) > 0 {
			texts = append(texts, msg.Content[0].Text)
		}
	}
	want := []string{"start", "steered mid-turn"}
	if len(texts) < len(want) {
		t.Fatalf("user messages = %v, want %v", texts, want)
	}
	for i, text := range want {
		if texts[i] != text {
			t.Fatalf("user messages = %v, want prefix %v", texts, want)
		}
	}
}

func TestRunTurnUsesDefaultControlWhenUnset(t *testing.T) {
	t.Parallel()

	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	mem, err := session.NewMemory(session.MemoryConfig{ID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	scripted := newStepScripted([]string{"ok"})
	toolRuntime, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := agent.New(agent.Config{ID: "test", Model: "scripted", MaxSteps: 1}, agent.Deps{
		LLM:     scripted,
		Session: mem,
		Tools:   toolRuntime,
		Prompt:  assembler,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := rt.Control().FollowUp(ctx, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "follow"}},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := rt.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "start"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	follow, err := rt.Control().DrainFollowUps(ctx, agentkit.FollowUpAll)
	if err != nil {
		t.Fatal(err)
	}
	if len(follow) != 1 || follow[0].Content[0].Text != "follow" {
		t.Fatalf("follow-ups = %+v", follow)
	}
}

type stepScripted struct {
	texts []string
	idx   int
}

func newStepScripted(texts []string) *stepScripted {
	return &stepScripted{texts: texts}
}

func (s *stepScripted) Name() string { return "step-scripted" }

func (s *stepScripted) Stream(_ context.Context, _ agentkit.LLMRequest) (agentkit.LLMStream, error) {
	if s.idx >= len(s.texts) {
		return nil, io.ErrUnexpectedEOF
	}
	text := s.texts[s.idx]
	s.idx++
	msg := agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: text}},
	}
	return &instantStream{msg: msg}, nil
}

type instantStream struct {
	msg  agentkit.ModelMessage
	sent bool
}

func (s *instantStream) Recv() (agentkit.LLMEvent, error) {
	if s.sent {
		return agentkit.LLMEvent{}, io.EOF
	}
	s.sent = true
	return agentkit.LLMEvent{Type: agentkit.LLMEventMessage, Message: &s.msg}, nil
}

func (s *instantStream) Close() error { return nil }

func userTexts(sess agentkit.Session, ctx context.Context) []string {
	msgs, err := sess.DeriveMessages(ctx)
	if err != nil {
		return nil
	}
	var out []string
	for _, msg := range msgs {
		if msg.Role == "user" && len(msg.Content) > 0 {
			out = append(out, msg.Content[0].Text)
		}
	}
	return out
}
