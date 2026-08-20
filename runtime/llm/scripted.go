package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/lengzhao/agentkit"
)

type ScriptedStep struct {
	Text      string            `json:"text"`
	ToolCalls []agentkit.ToolCall `json:"toolCalls"`
}

type ScriptedConfig struct {
	Model string         `json:"model"`
	Steps []ScriptedStep `json:"steps"`
}

// Scripted replays fixed assistant responses for tests.
type Scripted struct {
	model string
	steps []ScriptedStep
	idx   int
}

func NewScripted(cfg ScriptedConfig) (*Scripted, error) {
	if len(cfg.Steps) == 0 {
		return nil, fmt.Errorf("scripted llm requires at least one step")
	}
	model := cfg.Model
	if model == "" {
		model = "scripted"
	}
	return &Scripted{model: model, steps: cfg.Steps}, nil
}

func (p *Scripted) Name() string { return "scripted" }

func (p *Scripted) Stream(_ context.Context, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	if p.idx >= len(p.steps) {
		return nil, fmt.Errorf("scripted llm exhausted after %d steps", len(p.steps))
	}
	step := p.steps[p.idx]
	p.idx++
	msg := agentkit.ModelMessage{
		Role: "assistant",
		Content: []agentkit.ContentPart{{
			Type: "text",
			Text: step.Text,
		}},
		ToolCalls: step.ToolCalls,
	}
	if req.Model != "" {
		_ = req.Model
	}
	return &scriptedStream{msg: msg}, nil
}

type scriptedStream struct {
	msg     agentkit.ModelMessage
	sent    bool
	closed  bool
}

func (s *scriptedStream) Recv() (agentkit.LLMEvent, error) {
	if s.closed {
		return agentkit.LLMEvent{}, io.EOF
	}
	if !s.sent && strings.TrimSpace(textOf(s.msg.Content)) != "" {
		s.sent = true
		return agentkit.LLMEvent{Type: "text_delta", Message: &s.msg}, nil
	}
	s.closed = true
	cp := s.msg
	return agentkit.LLMEvent{Type: "message", Message: &cp}, io.EOF
}

func (s *scriptedStream) Close() error { return nil }

func MustToolCall(name, args string) agentkit.ToolCall {
	return agentkit.ToolCall{
		ID:    agentkit.ToolCallID(name + "-call"),
		Name:  name,
		Input: json.RawMessage(args),
	}
}
