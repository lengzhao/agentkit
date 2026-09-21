package compaction

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capcompaction "github.com/lengzhao/agentkit/cap/compaction"
)

func TestPruneReportsNotAppliedWhenNothingTrimmed(t *testing.T) {
	t.Parallel()

	svc, err := NewPrune(PruneConfig{MaxToolResultBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Compact(context.Background(), capcompaction.Request{
		Messages: []agentkit.ModelMessage{{
			Role: "tool",
			ToolResults: []agentkit.ToolResult{{
				ID:      "call-1",
				Name:    "read",
				Content: "short",
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied {
		t.Fatal("prune must not report applied when no tool result was truncated")
	}
}

func TestPruneReportsAppliedWhenTruncating(t *testing.T) {
	t.Parallel()

	svc, err := NewPrune(PruneConfig{MaxToolResultBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Compact(context.Background(), capcompaction.Request{
		Messages: []agentkit.ModelMessage{{
			Role: "tool",
			ToolResults: []agentkit.ToolResult{{
				ID:      "call-1",
				Name:    "read",
				Content: strings.Repeat("x", 500),
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied {
		t.Fatal("prune must report applied when a tool result was truncated")
	}
	if len(result.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(result.Messages))
	}
	got := result.Messages[0].ToolResults[0].Content
	if len(got) >= 500 {
		t.Fatalf("content not truncated, len=%d", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("truncated content should carry marker, got %q", got[len(got)-40:])
	}
}
