package learning

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lengzhao/agentkit/runtime/configfile"
	rtlearning "github.com/lengzhao/agentkit/runtime/learning"
)

// MemoryEntry is one §-delimited block in memory.md.
type MemoryEntry = rtlearning.MemoryEntry

// MemoryStore reads and writes a single memory.md file with capacity limits.
type MemoryStore struct {
	Path      string
	CharLimit int
}

func NewMemoryStore(path string, charLimit int) *MemoryStore {
	if charLimit <= 0 {
		charLimit = DefaultCharLimit
	}
	return &MemoryStore{Path: path, CharLimit: charLimit}
}

func (s *MemoryStore) Load() ([]MemoryEntry, error) {
	if s.Path == "" {
		return nil, fmt.Errorf("memory path is required")
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return rtlearning.ParseMemory(string(data)), nil
}

func (s *MemoryStore) TotalChars(entries []MemoryEntry) int {
	n := 0
	for _, e := range entries {
		n += utf8.RuneCountInString(strings.TrimSpace(e.Content))
	}
	return n
}

func (s *MemoryStore) Add(content, source string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("memory content is empty")
	}
	if looksLikeSecret(content) {
		return fmt.Errorf("refusing to store content that looks like a secret")
	}
	entries, err := s.Load()
	if err != nil {
		return err
	}
	meta := fmt.Sprintf("source=%s created_at=%s", source, time.Now().UTC().Format(time.RFC3339))
	candidate := append(append([]MemoryEntry{}, entries...), MemoryEntry{Content: content, Meta: meta})
	if s.TotalChars(candidate) > s.CharLimit {
		return fmt.Errorf("memory at %d/%d chars; adding this entry would exceed the limit", s.TotalChars(entries), s.CharLimit)
	}
	return s.Save(candidate)
}

func (s *MemoryStore) Remove(oldText string) error {
	oldText = strings.TrimSpace(oldText)
	if oldText == "" {
		return fmt.Errorf("text is required")
	}
	entries, err := s.Load()
	if err != nil {
		return err
	}
	idx := -1
	for i, e := range entries {
		if strings.Contains(e.Content, oldText) {
			if idx >= 0 {
				return fmt.Errorf("text matches multiple entries; be more specific")
			}
			idx = i
		}
	}
	if idx < 0 {
		return fmt.Errorf("text matched no entries")
	}
	next := append([]MemoryEntry{}, entries[:idx]...)
	next = append(next, entries[idx+1:]...)
	return s.Save(next)
}

func (s *MemoryStore) Save(entries []MemoryEntry) error {
	body := rtlearning.RenderMemory(entries)
	return configfile.WriteAtomic(s.Path, []byte(body), 0o644)
}

// ParseMemory splits a memory.md body into entries.
func ParseMemory(raw string) []MemoryEntry {
	return rtlearning.ParseMemory(raw)
}

// RenderMemory serializes entries into a memory.md file body.
func RenderMemory(entries []MemoryEntry) string {
	return rtlearning.RenderMemory(entries)
}
