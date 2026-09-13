package dreaming

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// TopScoredCandidates returns up to topK signals that pass Deep thresholds, highest score first.
func TopScoredCandidates(signals []Signal, cfg Config, now time.Time, topK int) []scored {
	cfg = cfg.Normalized()
	if topK <= 0 {
		topK = 8
	}
	ranked := scoreSignals(signals, cfg, now)
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })
	out := make([]scored, 0, topK)
	for _, item := range ranked {
		if !passesThreshold(item, cfg) {
			continue
		}
		out = append(out, item)
		if len(out) >= topK {
			break
		}
	}
	return out
}

// FormatReviewCandidateBlock formats grounded candidates for background-review digest.
func FormatReviewCandidateBlock(items []scored) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Grounded memory candidates (from dreaming signals; promote only if durable and not already in memory.md):\n")
	for i, item := range items {
		text := strings.TrimSpace(item.Signal.Text)
		if text == "" {
			continue
		}
		fmt.Fprintf(&b, "%d. (score=%.2f) %s\n", i+1, item.Score, text)
	}
	return strings.TrimRight(b.String(), "\n")
}
