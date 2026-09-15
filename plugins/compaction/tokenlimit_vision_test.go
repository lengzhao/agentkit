package compaction

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capcompaction "github.com/lengzhao/agentkit/cap/compaction"
)

func TestTokenLimitIgnoresHydratedVisionDataURLSize(t *testing.T) {
	t.Parallel()

	inner := &countingService{}
	svc, err := NewTokenLimit(TokenLimitConfig{MaxTokens: 10_000}, TokenLimitDeps{
		Services: []capcompaction.Service{inner},
	})
	if err != nil {
		t.Fatal(err)
	}

	messages := []agentkit.ModelMessage{{
		Role: "user",
		Content: []agentkit.ContentPart{{
			Type: "image_url",
			URL:  "data:image/jpeg;base64," + strings.Repeat("A", 600_000),
		}},
	}}
	if _, err := svc.Compact(context.Background(), capcompaction.Request{Messages: messages}); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 0 {
		t.Fatalf("compaction inner calls = %d, want 0 for hydrated vision placeholder estimate", inner.calls)
	}
}
