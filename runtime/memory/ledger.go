package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lengzhao/agentkit/runtime/configfile"
)

// MemoryLedgerEvent is one append-only audit row for memory.md changes.
type MemoryLedgerEvent struct {
	Action  string    `json:"action"`
	Content string    `json:"content,omitempty"`
	Match   string    `json:"match,omitempty"`
	Source  string    `json:"source,omitempty"`
	At      time.Time `json:"at"`
}

// MemoryLedger appends JSON lines under memory/ledger.jsonl (not injected into prompts).
type MemoryLedger struct {
	Path string
}

func (l *MemoryLedger) Append(ev MemoryLedgerEvent) error {
	if l.Path == "" {
		return fmt.Errorf("ledger path is required")
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

// Rewrite truncates and rewrites the ledger (tests).
func (l *MemoryLedger) Rewrite(events []MemoryLedgerEvent) error {
	if l.Path == "" {
		return fmt.Errorf("ledger path is required")
	}
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o755); err != nil {
		return err
	}
	var b []byte
	for _, ev := range events {
		if ev.At.IsZero() {
			ev.At = time.Now().UTC()
		}
		line, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		b = append(b, line...)
		b = append(b, '\n')
	}
	return configfile.WriteAtomic(l.Path, b, 0o644)
}
