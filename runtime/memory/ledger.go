package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lengzhao/agentkit/cap/filesystem"
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
// Path is a filesystem.Service-relative path.
type MemoryLedger struct {
	FS   filesystem.Service
	Path string
}

func (l *MemoryLedger) Append(ctx context.Context, ev MemoryLedgerEvent) error {
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
	return l.FS.Append(ctx, l.Path, append(line, '\n'))
}

// Rewrite truncates and rewrites the ledger (tests).
func (l *MemoryLedger) Rewrite(ctx context.Context, events []MemoryLedgerEvent) error {
	if l.Path == "" {
		return fmt.Errorf("ledger path is required")
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
	return l.FS.Write(ctx, l.Path, b)
}
