package learning

import (
	"strings"
)

const entrySep = "\n§\n"

// MemoryEntry is one §-delimited block in memory.md.
type MemoryEntry struct {
	Content string
	Meta    string
}

// ParseMemory splits a memory.md body into entries.
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

// RenderMemory serializes entries into a memory.md file body.
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
