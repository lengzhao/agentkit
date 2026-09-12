package learning

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
)

type stubApplier struct {
	adds int
}

func (s *stubApplier) CaptureMemoryAdd(_ context.Context, text, source string) (string, error) {
	s.adds++
	return "ok", nil
}

func (s *stubApplier) CaptureMemoryRemove(context.Context, string) (string, error) {
	return "removed", nil
}

func (s *stubApplier) CaptureSkillPropose(context.Context, string, string, string, string, string) (string, error) {
	return "proposed", nil
}

func TestApplyCaptureMemoryAdd(t *testing.T) {
	app := &stubApplier{}
	out, err := ApplyCapture(context.Background(), app, "sess-1", CaptureInput{
		Action:  "memory_add",
		Content: "prefers Go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || app.adds != 1 {
		t.Fatalf("unexpected %+v adds=%d", out, app.adds)
	}
}

func TestDigestMessagesSkipsEmpty(t *testing.T) {
	if DigestMessages(nil, 10) != "" {
		t.Fatal("expected empty digest")
	}
	d := DigestMessages([]agentkit.ModelMessage{
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hello"}}},
	}, 10)
	if d != "user: hello" {
		t.Fatalf("digest = %q", d)
	}
}
