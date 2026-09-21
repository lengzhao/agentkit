package dreaming

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
)

// IngestSessions scans recent session JSONL files for grounded signals.
// sessionsDir is a filesystem.Service-relative directory.
func IngestSessions(ctx context.Context, fs filesystem.Service, sessionsDir string, store *Store, limit int, now time.Time) (int, int, error) {
	st, err := store.Load(ctx)
	if err != nil {
		return 0, 0, err
	}
	entries, err := fs.List(ctx, sessionsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, 0, store.Save(ctx, st)
		}
		return 0, 0, err
	}
	type fileInfo struct {
		path string
		mod  time.Time
	}
	var files []fileInfo
	for _, ent := range entries {
		if ent.IsDir {
			continue
		}
		name := ent.Name
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		files = append(files, fileInfo{
			path: strings.TrimSuffix(sessionsDir, "/") + "/" + name,
			mod:  ent.ModTime,
		})
	}
	// newest first
	for i := 0; i < len(files); i++ {
		for j := i + 1; j < len(files); j++ {
			if files[j].mod.After(files[i].mod) {
				files[i], files[j] = files[j], files[i]
			}
		}
	}
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}

	sessionCount := 0
	signalCount := 0
	for _, fi := range files {
		sid := strings.TrimSuffix(fi.path[strings.LastIndex(fi.path, "/")+1:], ".jsonl")
		if shouldSkipSessionID(sid) {
			continue
		}
		n, err := ingestSessionFile(ctx, fs, fi.path, sid, st, now)
		if err != nil {
			continue
		}
		if n > 0 {
			sessionCount++
			signalCount += n
		}
	}
	if err := store.Save(ctx, st); err != nil {
		return sessionCount, signalCount, err
	}
	return sessionCount, signalCount, nil
}

func shouldSkipSessionID(id string) bool {
	lower := strings.ToLower(id)
	prefixes := []string{"schedule", "sub:", "worker", "timer", "cron"}
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

func ingestSessionFile(ctx context.Context, fs filesystem.Service, path, sessionID string, st *State, now time.Time) (int, error) {
	data, err := fs.Read(ctx, path)
	if err != nil {
		return 0, err
	}
	lines := strings.Split(string(data), "\n")
	count := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev agentkit.SessionEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Type != agentkit.EventUserMessage {
			continue
		}
		key := processedEventKey(sessionID, ev, line)
		if _, ok := st.ProcessedEvents[key]; ok {
			continue
		}
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			continue
		}
		text := flattenContent(msg.Content)
		if text == "" || strings.HasPrefix(text, "/") {
			continue
		}
		if !looksLikeMemorySignal(text) {
			st.ProcessedEvents[key] = now
			continue
		}
		st.UpsertSignal(Signal{
			Text:      text,
			Source:    "session:" + sessionID,
			SessionID: sessionID,
		}, now)
		st.ProcessedEvents[key] = now
		count++
	}
	return count, nil
}

func processedEventKey(sessionID string, ev agentkit.SessionEvent, rawLine string) string {
	if ev.Seq > 0 {
		return fmt.Sprintf("%s:%d", sessionID, ev.Seq)
	}
	sum := sha256.Sum256([]byte(rawLine))
	return fmt.Sprintf("%s:%x", sessionID, sum[:8])
}

func flattenContent(parts []agentkit.ContentPart) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type != "text" {
			continue
		}
		t := strings.TrimSpace(p.Text)
		if t == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(t)
	}
	return b.String()
}

func looksLikeMemorySignal(text string) bool {
	if len([]rune(text)) < 12 {
		return false
	}
	lower := strings.ToLower(text)
	needles := []string{
		"prefer", "remember", "always", "never", "from now on", "note that",
		"偏好", "记住", "以后", "默认", "纠正", "不要", "习惯",
	}
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return len([]rune(text)) >= 48
}
