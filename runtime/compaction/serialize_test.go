package compaction_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	rtcompaction "github.com/lengzhao/agentkit/runtime/compaction"
)

// 摘要请求本身必须有界：单条巨型消息（如 skill 注入）被截断。
func TestSerializeConversationWithBudgetTruncatesGiantMessage(t *testing.T) {
	t.Parallel()

	msgs := []agentkit.ModelMessage{{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: strings.Repeat("x", 300_000)}},
	}}
	out := rtcompaction.SerializeConversationWithBudget(msgs, 1_000_000)
	if !strings.Contains(out, "chars omitted") {
		t.Fatal("expected truncation marker for giant message")
	}
	if len(out) > 150_000 {
		t.Fatalf("serialized len = %d, want <= 150000 (per-message cap)", len(out))
	}
}

// 总量超预算时丢最旧的消息并标注，保留最新内容。
func TestSerializeConversationWithBudgetDropsOldest(t *testing.T) {
	t.Parallel()

	msgs := make([]agentkit.ModelMessage, 0, 10)
	for i := 0; i < 10; i++ {
		msgs = append(msgs, agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: fmt.Sprintf("msg-%d-", i) + strings.Repeat("y", 50_000)}},
		})
	}
	out := rtcompaction.SerializeConversationWithBudget(msgs, 120_000)
	if !strings.Contains(out, "oldest messages omitted") {
		t.Fatal("expected omission marker for dropped head")
	}
	if strings.Contains(out, "msg-0-") {
		t.Fatal("oldest message should have been dropped")
	}
	if !strings.Contains(out, "msg-9-") {
		t.Fatal("newest message must be kept")
	}
	if len(out) > 200_000 {
		t.Fatalf("serialized len = %d, want bounded", len(out))
	}
}

// 无预算（<=0）时行为与 SerializeConversation 一致。
func TestSerializeConversationWithBudgetZeroMeansUnbounded(t *testing.T) {
	t.Parallel()

	msgs := []agentkit.ModelMessage{{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "hello"}},
	}}
	if got, want := rtcompaction.SerializeConversationWithBudget(msgs, 0), rtcompaction.SerializeConversation(msgs); got != want {
		t.Fatalf("unbounded = %q, want %q", got, want)
	}
}
