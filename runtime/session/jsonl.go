package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
)

type JSONLConfig struct {
	Path string             `json:"path"`
	ID   agentkit.SessionID `json:"id"`
}

// JSONL persists session events as append-only JSON lines.
type JSONL struct {
	mu   sync.Mutex
	id   agentkit.SessionID
	path string
	seq  agentkit.EventSeq
	mem  *Memory
}

func NewJSONL(cfg JSONLConfig) (*JSONL, error) {
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
	mem, err := NewMemory(MemoryConfig{ID: id})
	if err != nil {
		return nil, err
	}
	s := &JSONL{id: id, path: cfg.Path, mem: mem}
	if err := s.loadExisting(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *JSONL) loadExisting() error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev agentkit.SessionEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			return err
		}
		s.mem.events = append(s.mem.events, ev)
		if ev.Seq > s.seq {
			s.seq = ev.Seq
		}
	}
	return sc.Err()
}

func (s *JSONL) ID() agentkit.SessionID { return s.id }

func (s *JSONL) Append(ctx context.Context, event agentkit.SessionEvent) (agentkit.EventSeq, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seq, err := s.mem.Append(ctx, event)
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
	return seq, nil
}

func (s *JSONL) Read(ctx context.Context, from agentkit.EventSeq) ([]agentkit.SessionEvent, error) {
	return s.mem.Read(ctx, from)
}

func (s *JSONL) DeriveMessages(ctx context.Context) ([]agentkit.ModelMessage, error) {
	return s.mem.DeriveMessages(ctx)
}
