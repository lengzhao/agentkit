package learning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lengzhao/agentkit/runtime/configfile"
)

const stagedSubdir = "memory/.staged"

// StagedMemory is a pending memory write awaiting approval.
type StagedMemory struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"createdAt"`
}

// StagedStore persists pending memory entries under workspace local root.
type StagedStore struct {
	Dir string
}

func (s *StagedStore) path() string {
	return filepath.Join(s.Dir, "pending.json")
}

func (s *StagedStore) List() ([]StagedMemory, error) {
	if s.Dir == "" {
		return nil, fmt.Errorf("staged dir is required")
	}
	data, err := os.ReadFile(s.path())
	if err != nil {
		if os.IsNotExist(err) {
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

func (s *StagedStore) Add(entry StagedMemory) error {
	if entry.ID == "" {
		return fmt.Errorf("staged id is required")
	}
	list, err := s.List()
	if err != nil {
		return err
	}
	list = append(list, entry)
	return s.save(list)
}

func (s *StagedStore) Remove(id string) (StagedMemory, error) {
	list, err := s.List()
	if err != nil {
		return StagedMemory{}, err
	}
	for i, e := range list {
		if e.ID == id {
			removed := list[i]
			next := append([]StagedMemory{}, list[:i]...)
			next = append(next, list[i+1:]...)
			if err := s.save(next); err != nil {
				return StagedMemory{}, err
			}
			return removed, nil
		}
	}
	return StagedMemory{}, fmt.Errorf("staged entry %q not found", id)
}

// Clear removes all staged entries.
func (s *StagedStore) Clear() error {
	return s.save(nil)
}

func (s *StagedStore) save(list []StagedMemory) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return configfile.WriteAtomic(s.path(), data, 0o644)
}
