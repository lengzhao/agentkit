package session

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
)

type MemoryConfig struct {
	// ID is fixed session id.
	ID agentkit.SessionID `json:"id"`
	// MaxToolResultBytes truncates tool results as they are appended.
	MaxToolResultBytes int `json:"maxToolResultBytes"`
}

// Memory is an in-memory session backend for tests and ephemeral runs.
type Memory struct {
	mu                 sync.RWMutex
	id                 agentkit.SessionID
	seq                agentkit.EventSeq
	events             []agentkit.SessionEvent
	maxToolResultBytes int
}

func newMemory(cfg MemoryConfig) (*Memory, error) {
	id := cfg.ID
	if id == "" {
		id = agentkit.SessionID("mem-" + time.Now().UTC().Format("20060102-150405.000"))
	}
	return &Memory{id: id, maxToolResultBytes: cfg.MaxToolResultBytes}, nil
}

// NewMemory registers session/memory: Ephemeral in-memory session. Nothing survives the process.
func NewMemory(cfg MemoryConfig) (agentkit.Session, error) {
	return newMemory(cfg)
}

func (s *Memory) ID() agentkit.SessionID { return s.id }

// resumeSeq continues numbering above seq, for backends that preload history.
func (s *Memory) resumeSeq(seq agentkit.EventSeq) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if seq > s.seq {
		s.seq = seq
	}
}

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
	return agentkit.ModelMessage{
		Role: "tool",
		ToolResults: []agentkit.ToolResult{{
			ID:      result.ID,
			Name:    result.Name,
			Content: result.Content,
			Audit:   result.Audit,
		}},
	}
}

func AppendMessage(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, typ agentkit.EventType, msg agentkit.ModelMessage) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	event := agentkit.SessionEvent{
		AgentID: agentID,
		Type:    typ,
		Data:    raw,
	}
	// Attribute user turns to whoever sent them. Only user messages carry this:
	// stamping the assistant with the user who prompted it would make the reply
	// look like that person's words on replay.
	if typ == agentkit.EventUserMessage {
		event.UserID, _ = ctx.Value(agentkit.KeyUserID).(string)
	}
	_, err = s.Append(ctx, event)
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
