package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	capmemory "github.com/lengzhao/agentkit/cap/memory"
	"github.com/lengzhao/agentkit/cap/filesystem"
)

// DefaultMemoryCharLimit is the default memory.md body size budget (runes).
const DefaultMemoryCharLimit = 2200

// MemoryStore reads and writes a §-delimited memory.md file with capacity limits.
// Path is a filesystem.Service-relative path (may carry a global:/local: scope prefix).
type MemoryStore struct {
	FS        filesystem.Service
	Path      string
	CharLimit int
	// DocName is the file title in the on-disk header (default memory.md).
	DocName string
}

// NewMemoryStore opens a store at path with the given char limit.
func NewMemoryStore(fs filesystem.Service, path string, charLimit int) *MemoryStore {
	if charLimit <= 0 {
		charLimit = DefaultMemoryCharLimit
	}
	return &MemoryStore{FS: fs, Path: path, CharLimit: charLimit}
}

// Load reads all entries from the store.
func (s *MemoryStore) Load(ctx context.Context) ([]MemoryEntry, error) {
	if s.Path == "" {
		return nil, fmt.Errorf("memory path is required")
	}
	data, err := s.FS.Read(ctx, s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return ParseMemory(string(data)), nil
}

// TotalChars counts runes across entry bodies.
func (s *MemoryStore) TotalChars(entries []MemoryEntry) int {
	n := 0
	for _, e := range entries {
		n += utf8.RuneCountInString(strings.TrimSpace(e.Content))
	}
	return n
}

// MemoryAddResult is the outcome of MemoryStore.Add.
type MemoryAddResult struct {
	Outcome capmemory.AddOutcome
}

// Add merges one fact into memory.md.
func (s *MemoryStore) Add(ctx context.Context, content string) (MemoryAddResult, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return MemoryAddResult{}, fmt.Errorf("memory content is empty")
	}
	if LooksLikeSecret(content) {
		return MemoryAddResult{}, fmt.Errorf("refusing to store content that looks like a secret")
	}
	entries, err := s.Load(ctx)
	if err != nil {
		return MemoryAddResult{}, err
	}
	candidate, outcome := MergeMemoryAdd(entries, content)
	if outcome == capmemory.AddOutcomeDuplicate {
		return MemoryAddResult{Outcome: outcome}, nil
	}
	if s.TotalChars(candidate) > s.CharLimit {
		return MemoryAddResult{}, &MemoryAtCapacityError{
			Used:    s.TotalChars(entries),
			Limit:   s.CharLimit,
			Adding:  utf8.RuneCountInString(content),
			Entries: entries,
		}
	}
	if err := s.Save(ctx, candidate); err != nil {
		return MemoryAddResult{}, err
	}
	return MemoryAddResult{Outcome: outcome}, nil
}

// Replace updates the entry whose body contains oldText.
func (s *MemoryStore) Replace(ctx context.Context, oldText, content string) (MemoryAddResult, error) {
	oldText = strings.TrimSpace(oldText)
	content = strings.TrimSpace(content)
	if oldText == "" {
		return MemoryAddResult{}, fmt.Errorf("old_text is required")
	}
	if content == "" {
		return MemoryAddResult{}, fmt.Errorf("memory content is empty")
	}
	if LooksLikeSecret(content) {
		return MemoryAddResult{}, fmt.Errorf("refusing to store content that looks like a secret")
	}
	entries, err := s.Load(ctx)
	if err != nil {
		return MemoryAddResult{}, err
	}
	idx := -1
	for i, e := range entries {
		if strings.Contains(e.Content, oldText) {
			if idx >= 0 {
				return MemoryAddResult{}, fmt.Errorf("old_text matches multiple entries; be more specific")
			}
			idx = i
		}
	}
	if idx < 0 {
		return MemoryAddResult{}, fmt.Errorf("old_text matched no entries")
	}
	next := append([]MemoryEntry{}, entries...)
	next[idx] = MemoryEntry{Content: content}
	used := s.TotalChars(next)
	if used > s.CharLimit {
		return MemoryAddResult{}, &MemoryAtCapacityError{
			Used:    s.TotalChars(entries),
			Limit:   s.CharLimit,
			Adding:  utf8.RuneCountInString(content),
			Entries: entries,
		}
	}
	if err := s.Save(ctx, next); err != nil {
		return MemoryAddResult{}, err
	}
	return MemoryAddResult{Outcome: capmemory.AddOutcomeReplaced}, nil
}

// Remove deletes the entry whose body contains oldText.
func (s *MemoryStore) Remove(ctx context.Context, oldText string) (string, error) {
	oldText = strings.TrimSpace(oldText)
	if oldText == "" {
		return "", fmt.Errorf("text is required")
	}
	entries, err := s.Load(ctx)
	if err != nil {
		return "", err
	}
	idx := -1
	for i, e := range entries {
		if strings.Contains(e.Content, oldText) {
			if idx >= 0 {
				return "", fmt.Errorf("text matches multiple entries; be more specific")
			}
			idx = i
		}
	}
	if idx < 0 {
		return "", fmt.Errorf("text matched no entries")
	}
	removed := entries[idx].Content
	next := append([]MemoryEntry{}, entries[:idx]...)
	next = append(next, entries[idx+1:]...)
	if err := s.Save(ctx, next); err != nil {
		return "", err
	}
	return removed, nil
}

// Save writes entries atomically (the filesystem backend guarantees atomic replace).
func (s *MemoryStore) Save(ctx context.Context, entries []MemoryEntry) error {
	doc := strings.TrimSpace(s.DocName)
	if doc == "" {
		doc = "memory.md"
	}
	body := RenderMemoryDocument(entries, doc)
	return s.FS.Write(ctx, s.Path, []byte(body))
}

// LooksLikeSecret rejects content that may contain credentials.
func LooksLikeSecret(s string) bool {
	lower := strings.ToLower(s)
	needles := []string{
		"sk-", "api_key", "apikey", "secret_key", "private_key",
		"authorization: bearer", "-----begin ", "aws_secret", "password=",
	}
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}
