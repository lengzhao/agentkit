package sessstore

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
	mem, err := newMemory(MemoryConfig{ID: id})
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
	seq, err := s.mem.appendLocked(ctx, event)
	if err != nil {
		s.mu.Unlock()
		return 0, err
	}
	ev := event
	ev.Seq = seq
	raw, err := json.Marshal(ev)
	if err != nil {
		s.mem.rollbackAppendLocked(seq)
		s.mu.Unlock()
		return 0, err
	}
	path := s.path
	compact := ev.Type == agentkit.EventCompaction
	var compactData compaction.EventData
	if compact {
		_ = json.Unmarshal(ev.Data, &compactData)
	}
	s.mu.Unlock()

	if err := appendJSONLLine(path, raw); err != nil {
		s.mu.Lock()
		s.mem.rollbackAppendLocked(seq)
		s.mu.Unlock()
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq = seq
	if compact {
		s.mem.trimCompacted(ev.AgentID, compactData.MemoryCutoffSeq())
	}
	return seq, nil
}

func appendJSONLLine(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return err
	}
	return nil
}

func (s *JSONL) Read(ctx context.Context, from agentkit.EventSeq) ([]agentkit.SessionEvent, error) {
	s.mu.Lock()
	if from == 0 && s.mem.isTrimmed() {
		path := s.path
		s.mu.Unlock()
		return readSessionFile(path, from)
	}
	out := s.mem.readUnlocked(from)
	s.mu.Unlock()
	return out, nil
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

// rollbackAppendLocked drops the last append when durable write failed; JSONL.mu must be held.
func (m *Memory) rollbackAppendLocked(seq agentkit.EventSeq) {
	if len(m.events) == 0 || m.events[len(m.events)-1].Seq != seq {
		return
	}
	m.events = m.events[:len(m.events)-1]
	if len(m.events) == 0 {
		m.seq = 0
		return
	}
	m.seq = m.events[len(m.events)-1].Seq
}
