package learning

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const entrySep = "\n§\n"

// MemoryEntry is one §-delimited block in memory.md.
type MemoryEntry struct {
	Content string
	Meta    string
}

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
	return ParseMemory(string(data)), nil
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
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	body := RenderMemory(entries)
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

func ParseMemory(raw string) []MemoryEntry {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "# ") {
		if i := strings.Index(raw, "\n"); i >= 0 {
			raw = strings.TrimSpace(raw[i+1:])
		} else {
			return nil
		}
	}
	parts := strings.Split(raw, entrySep)
	out := make([]MemoryEntry, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		content, meta := splitMeta(part)
		if content == "" {
			continue
		}
		out = append(out, MemoryEntry{Content: content, Meta: meta})
	}
	return out
}

func RenderMemory(entries []MemoryEntry) string {
	if len(entries) == 0 {
		return "# memory.md\n"
	}
	var b strings.Builder
	b.WriteString("# memory.md\n\n")
	for i, e := range entries {
		if i > 0 {
			b.WriteString(entrySep)
		}
		b.WriteString(strings.TrimSpace(e.Content))
		if e.Meta != "" {
			b.WriteByte('\n')
			b.WriteString(formatMeta(e.Meta))
		}
	}
	b.WriteByte('\n')
	return b.String()
}

func splitMeta(part string) (content, meta string) {
	lines := strings.Split(part, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "<!--") && strings.HasSuffix(trim, "-->") {
			meta = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trim, "<!--"), "-->"))
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n")), meta
}

func formatMeta(meta string) string {
	return "<!-- " + strings.TrimSpace(meta) + " -->"
}
