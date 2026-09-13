package learning

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
)

type stubMemoryCapture struct {
	adds int
}

func (s *stubMemoryCapture) CaptureMemoryReplace(_ context.Context, _, content, _ string) (string, error) {
	return "replaced: " + content, nil
}

func (s *stubMemoryCapture) CaptureMemoryAdd(_ context.Context, text, source string) (string, error) {
	s.adds++
	return "ok", nil
}

func (s *stubMemoryCapture) CaptureMemoryRemove(context.Context, string) (string, error) {
	return "removed", nil
}

type stubSkillProposer struct{}

func (stubSkillProposer) CaptureSkillPropose(context.Context, string, string, string, string, string) (string, error) {
	return "proposed", nil
}

func TestApplyCaptureMemoryAdd(t *testing.T) {
	mem := &stubMemoryCapture{}
	out, err := ApplyCapture(context.Background(), mem, stubSkillProposer{}, "sess-1", CaptureInput{
		Action:  "memory_add",
		Content: "prefers Go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || mem.adds != 1 {
		t.Fatalf("unexpected %+v adds=%d", out, mem.adds)
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
