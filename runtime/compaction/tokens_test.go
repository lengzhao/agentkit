package compaction_test

import (
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	rtcompaction "github.com/lengzhao/agentkit/runtime/compaction"
)

// 线上事故形态：gateway 把 base64 当 text 计费，估算必须覆盖所有 part 的
// Text 与 URL，而不是只看 Type=="text"。
func TestEstimateTokensCountsNonTextPartsAndURLs(t *testing.T) {
	t.Parallel()

	msg := agentkit.ModelMessage{
		Role: "user",
		Content: []agentkit.ContentPart{
			{Type: "document", Text: strings.Repeat("x", 4000)},
			{Type: "image_url", URL: "data:image/png;base64," + strings.Repeat("y", 4000)},
		},
	}
	got := rtcompaction.EstimateTokens(msg)
	// (4000 + 4000 + 4) / 4 ≈ 2001
	if got < 2000 {
		t.Fatalf("EstimateTokens = %d, want >= 2000 (non-text Text and URLs must count)", got)
	}
}
