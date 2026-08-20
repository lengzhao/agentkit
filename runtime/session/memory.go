package session

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
)

type MemoryConfig struct {
	ID                 agentkit.SessionID `json:"id"`
	MaxToolResultBytes int                `json:"maxToolResultBytes"`
}

// Memory is an in-memory session backend for tests and ephemeral runs.
type Memory struct {
	mu                 sync.RWMutex
	id                 agentkit.SessionID
	seq                agentkit.EventSeq
	events             []agentkit.SessionEvent
	maxToolResultBytes int
}

func NewMemory(cfg MemoryConfig) (*Memory, error) {
	id := cfg.ID
	if id == "" {
		id = agentkit.SessionID("mem-" + time.Now().UTC().Format("20060102-150405.000"))
	}
	return &Memory{id: id, maxToolResultBytes: cfg.MaxToolResultBytes}, nil
}

func (s *Memory) ID() agentkit.SessionID { return s.id }

func (s *Memory) Append(_ context.Context, event agentkit.SessionEvent) (agentkit.EventSeq, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	event.Seq = s.seq
	event.SessionID = s.id
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	s.events = append(s.events, event)
	return event.Seq, nil
}

func (s *Memory) Read(_ context.Context, from agentkit.EventSeq) ([]agentkit.SessionEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]agentkit.SessionEvent, 0)
	for _, ev := range s.events {
		if ev.Seq > from {
			out = append(out, ev)
		}
	}
	return out, nil
}

func (s *Memory) DeriveMessages(_ context.Context) ([]agentkit.ModelMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return deriveMessages(s.events, s.maxToolResultBytes), nil
}

func toolResultMessage(result agentkit.ToolResult) agentkit.ModelMessage {
	text := ""
	for _, part := range result.Content {
		if part.Type == "text" {
			text += part.Text
		}
	}
	return agentkit.ModelMessage{
		Role: "tool",
		ToolResults: []agentkit.ToolResult{{
			ID:      result.ID,
			Name:    result.Name,
			Content: result.Content,
			Audit:   result.Audit,
		}},
		Content: []agentkit.ContentPart{{Type: "text", Text: text}},
	}
}

func AppendMessage(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, typ agentkit.EventType, msg agentkit.ModelMessage) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = s.Append(ctx, agentkit.SessionEvent{
		AgentID: agentID,
		Type:    typ,
		Data:    raw,
	})
	return err
}

func AppendToolCall(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, call agentkit.ToolCall) error {
	raw, err := json.Marshal(call)
	if err != nil {
		return err
	}
	_, err = s.Append(ctx, agentkit.SessionEvent{
		AgentID: agentID,
		Type:    agentkit.EventToolCall,
		Data:    raw,
	})
	return err
}

func AppendToolResult(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, result agentkit.ToolResult) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = s.Append(ctx, agentkit.SessionEvent{
		AgentID: agentID,
		Type:    agentkit.EventToolResult,
		Data:    raw,
	})
	return err
}
