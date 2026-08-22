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
	Text      string              `json:"text"`
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
	msg    agentkit.ModelMessage
	pending []agentkit.LLMEvent
	closed bool
}

func (s *scriptedStream) Recv() (agentkit.LLMEvent, error) {
	if s.closed && len(s.pending) == 0 {
		return agentkit.LLMEvent{}, io.EOF
	}
	if len(s.pending) > 0 {
		ev := s.pending[0]
		s.pending = s.pending[1:]
		return ev, nil
	}

	text := strings.TrimSpace(textOf(s.msg.Content))
	if len(s.msg.ToolCalls) > 0 {
		s.queueToolEvents()
	} else if text != "" {
		empty := agentkit.ModelMessage{Role: "assistant"}
		s.pending = append(s.pending,
			agentkit.LLMEvent{Type: agentkit.AssistantEventStart, Message: &empty},
			agentkit.LLMEvent{Type: agentkit.AssistantEventTextStart, ContentIndex: 0, Message: &empty},
		)
		for _, chunk := range chunkText(text, 8) {
			partial := agentkit.ModelMessage{
				Role:    "assistant",
				Content: []agentkit.ContentPart{{Type: "text", Text: chunk.prefix}},
			}
			s.pending = append(s.pending, agentkit.LLMEvent{
				Type:         agentkit.AssistantEventTextDelta,
				ContentIndex: 0,
				Delta:        chunk.delta,
				Message:      &partial,
			})
		}
	}

	s.pending = append(s.pending, agentkit.LLMEvent{Type: agentkit.LLMEventMessage, Message: &s.msg})
	s.closed = true
	return s.Recv()
}

func (s *scriptedStream) queueToolEvents() {
	empty := agentkit.ModelMessage{Role: "assistant"}
	s.pending = append(s.pending, agentkit.LLMEvent{Type: agentkit.AssistantEventStart, Message: &empty})
	for i, call := range s.msg.ToolCalls {
		cp := call
		partial := agentkit.ModelMessage{Role: "assistant", ToolCalls: []agentkit.ToolCall{cp}}
		s.pending = append(s.pending, agentkit.LLMEvent{
			Type:         agentkit.AssistantEventToolCallStart,
			ContentIndex: i,
			ToolCall:     &cp,
			Message:      &partial,
		})
		if len(call.Input) > 0 {
			s.pending = append(s.pending, agentkit.LLMEvent{
				Type:         agentkit.AssistantEventToolCallDelta,
				ContentIndex: i,
				Delta:        string(call.Input),
				ToolCall:     &cp,
				Message:      &partial,
			})
		}
		s.pending = append(s.pending, agentkit.LLMEvent{
			Type:         agentkit.AssistantEventToolCallEnd,
			ContentIndex: i,
			ToolCall:     &cp,
			Message:      &partial,
		})
	}
}

func (s *scriptedStream) Close() error { return nil }

type textChunk struct {
	prefix string
	delta  string
}

func chunkText(text string, size int) []textChunk {
	if size <= 0 || len(text) <= size {
		return []textChunk{{prefix: text, delta: text}}
	}
	out := make([]textChunk, 0, (len(text)+size-1)/size)
	var prefix strings.Builder
	for i := 0; i < len(text); i += size {
		end := i + size
		if end > len(text) {
			end = len(text)
		}
		delta := text[i:end]
		prefix.WriteString(delta)
		out = append(out, textChunk{prefix: prefix.String(), delta: delta})
	}
	return out
}

func MustToolCall(name, args string) agentkit.ToolCall {
	return agentkit.ToolCall{
		ID:    agentkit.ToolCallID(name + "-call"),
		Name:  name,
		Input: json.RawMessage(args),
	}
}
