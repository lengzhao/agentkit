package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
)

type JSONLConfig struct {
	// Path is the log file itself, not a directory.
	Path string `json:"path"`
	// ID is fixed session id.
	ID agentkit.SessionID `json:"id"`
	// UserMessageTemplate is forwarded to the in-memory replay backend.
	UserMessageTemplate string `json:"userMessageTemplate"`
	// MaxLoadedEvents limits non-compaction events kept in memory on load. Zero loads the full file.
	MaxLoadedEvents int `json:"maxLoadedEvents"`
}

// JSONL persists session events as append-only JSON lines.
type JSONL struct {
	mu              sync.Mutex
	id              agentkit.SessionID
	path            string
	seq             agentkit.EventSeq
	maxLoadedEvents int
	mem             *Memory
}

func newJSONL(cfg JSONLConfig) (*JSONL, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("session jsonl path is required")
	}
	id := cfg.ID
	if id == "" {
		id = agentkit.SessionID("jsonl-" + time.Now().UTC().Format("20060102-150405.000"))
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o755); err != nil {
		return nil, err
	}
	mem, err := newMemory(MemoryConfig{ID: id, UserMessageTemplate: cfg.UserMessageTemplate})
	if err != nil {
		return nil, err
	}
	s := &JSONL{
		id:              id,
		path:            cfg.Path,
		maxLoadedEvents: cfg.MaxLoadedEvents,
		mem:             mem,
	}
	if err := s.loadExisting(); err != nil {
		return nil, err
	}
	return s, nil
}

// NewJSONL registers session/jsonl: Append-only JSONL event log for one session.
//
// Best practices:
//   - Reopening a file resumes event sequence numbering, so seq stays unique across restarts.
func NewJSONL(cfg JSONLConfig) (agentkit.Session, error) {
	return newJSONL(cfg)
}

func (s *JSONL) loadExisting() error {
	events, maxSeq, trimmed, err := scanSessionFile(s.path, s.maxLoadedEvents)
	if err != nil {
		return err
	}
	s.seq = maxSeq
	s.mem.setLoadedEvents(events, cutoffsFromCompactions(compactionEvents(events)), trimmed)
	s.mem.resumeSeq(s.seq)
	return nil
}

func compactionEvents(events []agentkit.SessionEvent) []agentkit.SessionEvent {
	out := make([]agentkit.SessionEvent, 0)
	for _, ev := range events {
		if ev.Type == agentkit.EventCompaction {
			out = append(out, ev)
		}
	}
	return out
}

func (s *JSONL) ID() agentkit.SessionID { return s.id }

func (s *JSONL) FilePath() string { return s.path }

// LatestSeq returns the highest event sequence in the durable log.
func (s *JSONL) LatestSeq() agentkit.EventSeq {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seq
}

func (s *JSONL) trimCompacted(agentID agentkit.AgentID, beforeSeq agentkit.EventSeq) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mem.trimCompacted(agentID, beforeSeq)
}

func (s *JSONL) Append(ctx context.Context, event agentkit.SessionEvent) (agentkit.EventSeq, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seq, err := s.mem.appendLocked(ctx, event)
	if err != nil {
		return 0, err
	}
	ev := event
	ev.Seq = seq
	raw, err := json.Marshal(ev)
	if err != nil {
		return 0, err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return 0, err
	}
	s.seq = seq
	if ev.Type == agentkit.EventCompaction {
		var data compaction.EventData
		if err := json.Unmarshal(ev.Data, &data); err == nil {
			s.mem.trimCompacted(ev.AgentID, data.MemoryCutoffSeq())
		}
	}
	return seq, nil
}

func (s *JSONL) Read(ctx context.Context, from agentkit.EventSeq) ([]agentkit.SessionEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if from == 0 && s.mem.isTrimmed() {
		return readSessionFile(s.path, from)
	}
	return s.mem.readUnlocked(from), nil
}

func (s *JSONL) DeriveMessages(ctx context.Context) ([]agentkit.ModelMessage, error) {
	return s.mem.DeriveMessages(ctx)
}

// appendLocked appends without acquiring Memory.mu; JSONL.mu must be held.
func (m *Memory) appendLocked(_ context.Context, event agentkit.SessionEvent) (agentkit.EventSeq, error) {
	m.seq++
	event.Seq = m.seq
	event.SessionID = m.id
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	m.events = append(m.events, event)
	return event.Seq, nil
}
