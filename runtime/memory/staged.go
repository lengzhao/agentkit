package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/lengzhao/agentkit/cap/filesystem"
)

// StagedMemory is a pending memory write awaiting approval.
type StagedMemory struct {
	ID        string    `json:"id"`
	Action    string    `json:"action,omitempty"`  // add | replace | remove; empty = legacy Content encoding
	OldText   string    `json:"oldText,omitempty"` // replace/remove match substring
	Content   string    `json:"content"`           // add body or replace new text
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"createdAt"`
}

// StagedStore persists pending memory entries under the memory root.
// Dir is a filesystem.Service-relative directory.
type StagedStore struct {
	FS  filesystem.Service
	Dir string
}

func (s *StagedStore) path() string {
	return s.Dir + "/pending.json"
}

func (s *StagedStore) List(ctx context.Context) ([]StagedMemory, error) {
	if s.Dir == "" {
		return nil, fmt.Errorf("staged dir is required")
	}
	data, err := s.FS.Read(ctx, s.path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []StagedMemory
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *StagedStore) Add(ctx context.Context, entry StagedMemory) error {
	if entry.ID == "" {
		return fmt.Errorf("staged id is required")
	}
	list, err := s.List(ctx)
	if err != nil {
		return err
	}
	list = append(list, entry)
	return s.save(ctx, list)
}

func (s *StagedStore) Remove(ctx context.Context, id string) (StagedMemory, error) {
	list, err := s.List(ctx)
	if err != nil {
		return StagedMemory{}, err
	}
	for i, e := range list {
		if e.ID == id {
			removed := list[i]
			next := append([]StagedMemory{}, list[:i]...)
			next = append(next, list[i+1:]...)
			if err := s.save(ctx, next); err != nil {
				return StagedMemory{}, err
			}
			return removed, nil
		}
	}
	return StagedMemory{}, fmt.Errorf("staged entry %q not found", id)
}

// Clear removes all staged entries.
func (s *StagedStore) Clear(ctx context.Context) error {
	return s.save(ctx, nil)
}

func (s *StagedStore) save(ctx context.Context, list []StagedMemory) error {
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return s.FS.Write(ctx, s.path(), data)
}
