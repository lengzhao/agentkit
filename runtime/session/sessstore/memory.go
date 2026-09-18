package sessstore

import (
	"context"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session/derive"
)

type MemoryConfig struct {
	// ID is fixed session id.
	ID agentkit.SessionID `json:"id"`
	// MaxToolResultBytes caps tool result text in DeriveMessages (PruneToolResults).
	MaxToolResultBytes int `json:"maxToolResultBytes"`
}

// Memory is an in-memory session backend for tests and ephemeral runs.
type Memory struct {
	mu                 sync.RWMutex
	id                 agentkit.SessionID
	seq                agentkit.EventSeq
	events             []agentkit.SessionEvent
	cutoffByAgent      map[agentkit.AgentID]agentkit.EventSeq
	trimmed            bool
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
	return s.readUnlocked(from), nil
}

// LatestSeq returns the highest event sequence issued by this session.
func (s *Memory) LatestSeq() agentkit.EventSeq {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latestSeq()
}

func (s *Memory) DeriveMessages(ctx context.Context) ([]agentkit.ModelMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return derive.DeriveMessages(ctx, s.events, s.maxToolResultBytes), nil
}
