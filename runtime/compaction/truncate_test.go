package compaction_test

import (
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capscompaction "github.com/lengzhao/agentkit/cap/compaction"
	rtcompaction "github.com/lengzhao/agentkit/runtime/compaction"
)

// 记录大小超预算的消息里，可 hydrate 的附件 part 必须被替换成文本提示，
// 否则压缩后的 retained tail 会在下一轮 hydrate 时重新膨胀。
func TestBoundOversizedIndexedMessagesNeutralizesGiantAttachments(t *testing.T) {
	t.Parallel()

	indexed := []capscompaction.IndexedMessage{
		{
			Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{
				{Type: "attachment_ref", Source: "upload/huge.png", MIME: "image/png"},
				{Type: "text", Text: "看这张图"},
			}},
			Seq:          1,
			LogicalChars: 3_000_000,
		},
		{
			Message:      agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "小消息"}}},
			Seq:          2,
			LogicalChars: 20,
		},
	}
	out, changed := rtcompaction.BoundOversizedIndexedMessages(indexed, 80_000)
	if !changed {
		t.Fatal("expected changes for oversized attachment message")
	}
	for _, part := range out[0].Content {
		if part.Type == "attachment_ref" {
			t.Fatalf("attachment part should have been replaced by a text hint: %#v", part)
		}
	}
	joined := ""
	for _, part := range out[0].Content {
		joined += part.Text + "\n"
	}
	if !strings.Contains(joined, "upload/huge.png") {
		t.Fatalf("hint should keep the source path, got %q", joined)
	}
	if !strings.Contains(joined, "看这张图") {
		t.Fatalf("text parts must be preserved, got %q", joined)
	}
	if out[1].Content[0].Text != "小消息" {
		t.Fatalf("small message must stay untouched, got %#v", out[1].Content[0])
	}
}

// 文本截断能力保持不变：巨型 text 仍按 maxChars 截断。
func TestBoundOversizedIndexedMessagesTruncatesGiantText(t *testing.T) {
	t.Parallel()

	indexed := []capscompaction.IndexedMessage{{
		Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{
			{Type: "text", Text: strings.Repeat("x", 300_000)},
		}},
		Seq: 1,
	}}
	out, changed := rtcompaction.BoundOversizedIndexedMessages(indexed, 80_000)
	if !changed {
		t.Fatal("expected truncation")
	}
	if len(out[0].Content[0].Text) > 90_000 {
		t.Fatalf("text len = %d, want truncated near 80000", len(out[0].Content[0].Text))
	}
}

// 截断标记不能把正文重新撑破预算，否则 truncate-only 每一轮都认为“还有超大消息”
// 而不断写入新的 compaction 事件（线上 8 步里叠了 12 次）。
func TestBoundOversizedIndexedMessagesIsIdempotent(t *testing.T) {
	t.Parallel()

	indexed := []capscompaction.IndexedMessage{{
		Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{
			{Type: "text", Text: strings.Repeat("x", 10_000)},
		}},
		Seq: 1,
	}}
	once, changed := rtcompaction.BoundOversizedIndexedMessages(indexed, 2000)
	if !changed {
		t.Fatal("expected first-pass truncation")
	}
	if len(once[0].Content[0].Text) > 2000 {
		t.Fatalf("truncated text len = %d, want <= 2000", len(once[0].Content[0].Text))
	}
	again := []capscompaction.IndexedMessage{{Message: once[0], Seq: 1}}
	_, changed = rtcompaction.BoundOversizedIndexedMessages(again, 2000)
	if changed {
		t.Fatal("second pass must be a no-op once the message already fits the budget")
	}
}

func TestFitIndexedMessagesToBudgetNeutralizesAllAttachments(t *testing.T) {
	t.Parallel()

	indexed := []capscompaction.IndexedMessage{
		{Seq: 1, Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{
			{Type: "attachment_ref", Source: "upload/a.png", MIME: "image/png"},
			{Type: "text", Text: "看图"},
		}}},
		{Seq: 2, Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{
			{Type: "text", Text: "继续"},
		}}},
	}
	out, changed := rtcompaction.FitIndexedMessagesToBudget(indexed, 2000)
	if !changed {
		t.Fatal("attachment refs must be neutralized even when the stored message is small")
	}
	for _, part := range out[0].Content {
		if part.Type == "attachment_ref" {
			t.Fatalf("still hydratable: %#v", part)
		}
	}
}

func TestFitIndexedMessagesToBudgetDropsOldestToFitTotal(t *testing.T) {
	t.Parallel()

	indexed := make([]capscompaction.IndexedMessage, 0, 10)
	for i := 0; i < 10; i++ {
		indexed = append(indexed, capscompaction.IndexedMessage{
			Seq: agentkit.EventSeq(i + 1),
			Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{
				{Type: "text", Text: strings.Repeat("y", 500)},
			}},
		})
	}
	out, changed := rtcompaction.FitIndexedMessagesToBudget(indexed, 1200)
	if !changed {
		t.Fatal("expected oldest messages to be dropped")
	}
	total := 0
	for _, msg := range out {
		for _, part := range msg.Content {
			total += len(part.Text)
		}
	}
	if total > 1200 {
		t.Fatalf("total chars = %d, want <= 1200", total)
	}
	if !strings.Contains(out[len(out)-1].Content[0].Text, "y") {
		t.Fatal("newest message must be kept")
	}
}
