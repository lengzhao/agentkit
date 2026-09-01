package dreaming_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/learning/dreaming"
)

func TestSweepPromotesHighScoringSignal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	diaryPath := filepath.Join(dir, "DREAMS.md")
	now := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)

	store := &dreaming.Store{Path: statePath}
	st, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	st.Enabled = true
	text := "I prefer concise YAML configs for all deployment tasks"
	for i := 0; i < 3; i++ {
		st.UpsertSignal(dreaming.Signal{
			Text:      text,
			Source:    "session:test",
			SessionID: "sess-a",
		}, now.Add(-time.Duration(i)*24*time.Hour))
	}
	for _, sid := range []string{"sess-b", "sess-c"} {
		st.UpsertSignal(dreaming.Signal{
			Text:      text,
			Source:    "session:" + sid,
			SessionID: sid,
		}, now)
	}
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}

	var promoted []string
	cfg := dreaming.Defaults()
	cfg.MinRecallCount = 3
	cfg.MinUniqueSessions = 2
	cfg.MinScore = 0.5
	res, err := dreaming.Run(cfg, store, &dreaming.Diary{Path: diaryPath}, "", func(text, _ string) error {
		promoted = append(promoted, text)
		return nil
	}, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(promoted) == 0 {
		t.Fatalf("expected promotion, result=%+v", res)
	}
	data, err := os.ReadFile(diaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(data), "Light Sleep") || !contains(string(data), "Deep Sleep") {
		t.Fatalf("diary missing phases: %s", data)
	}
}

func TestIngestSessionsDoesNotRecountProcessedEvents(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sessionsDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeUserEvent(t, filepath.Join(sessionsDir, "cli_default.jsonl"), 1, "remember I prefer small focused patches")

	store := &dreaming.Store{Path: filepath.Join(dir, "state.json")}
	now := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)
	_, first, err := dreaming.IngestSessions(sessionsDir, store, 8, now)
	if err != nil {
		t.Fatal(err)
	}
	if first != 1 {
		t.Fatalf("first ingest signals=%d, want 1", first)
	}
	_, second, err := dreaming.IngestSessions(sessionsDir, store, 8, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if second != 0 {
		t.Fatalf("second ingest signals=%d, want 0", second)
	}
}

func TestSweepReturnsDiaryWriteError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	blockingFile := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &dreaming.Store{Path: filepath.Join(dir, "state.json")}
	st, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	st.Enabled = true
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	_, err = dreaming.Run(dreaming.Defaults(), store, &dreaming.Diary{
		Path: filepath.Join(blockingFile, "DREAMS.md"),
	}, "", nil, "", time.Now())
	if err == nil {
		t.Fatal("expected diary write error")
	}
}

func writeUserEvent(t *testing.T, path string, seq agentkit.EventSeq, text string) {
	t.Helper()
	raw, err := json.Marshal(agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: text}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ev, err := json.Marshal(agentkit.SessionEvent{
		Seq:  seq,
		Type: agentkit.EventUserMessage,
		Data: raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(ev, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub))
}

func indexOf(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
