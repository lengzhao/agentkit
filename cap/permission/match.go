package permission

import (
	"fmt"
	"strconv"
	"strings"
)

// AllowDenyMatch is the parsed allow/deny reply.
type AllowDenyMatch struct {
	Allow        bool
	Recognized   bool
	UpdatedInput map[string]any
}

// MatchAllowDeny parses a KindAllowDeny reply. Decision takes precedence; Text is
// a CLI fallback when Decision is empty.
func MatchAllowDeny(reply Reply) AllowDenyMatch {
	decision := strings.TrimSpace(strings.ToLower(reply.Decision))
	if decision == "" {
		decision = strings.TrimSpace(strings.ToLower(reply.Text))
	}
	switch decision {
	case "allow", "y", "yes":
		out := AllowDenyMatch{Allow: true, Recognized: true}
		if len(reply.UpdatedInput) > 0 {
			out.UpdatedInput = reply.UpdatedInput
		}
		return out
	case "deny", "n", "no":
		return AllowDenyMatch{Allow: false, Recognized: true}
	default:
		return AllowDenyMatch{Allow: false, Recognized: false}
	}
}

// MatchReply resolves a KindQuestion reply against a question. Uses Selected and
// Text only; Decision is ignored.
func MatchReply(reply Reply, q Question) QuestionResult {
	if len(reply.Selected) > 0 {
		return selectedReply(reply, q)
	}
	text := strings.TrimSpace(reply.Text)
	if text == "" {
		if def := strings.TrimSpace(q.Default); def != "" {
			text = def
		}
	}
	if len(q.Options) == 0 {
		return QuestionResult{Text: text}
	}
	return matchOptionText(text, q.Options)
}

func selectedReply(reply Reply, q Question) QuestionResult {
	out := QuestionResult{Selected: append([]int(nil), reply.Selected...)}
	if len(q.Options) == 0 || len(reply.Selected) == 0 {
		out.Text = strings.TrimSpace(reply.Text)
		return out
	}
	idx := reply.Selected[0]
	if idx >= 0 && idx < len(q.Options) {
		out.Text = q.Options[idx].Label
	}
	return out
}

func matchOptionText(text string, options []Option) QuestionResult {
	labels := make([]string, len(options))
	for i, opt := range options {
		labels[i] = opt.Label
	}
	got := matchLabels(text, labels)
	out := QuestionResult{Text: got.text}
	if got.selected >= 0 {
		out.Selected = []int{got.selected}
	}
	return out
}

type matchedLabel struct {
	text     string
	selected int
}

func matchLabels(text string, options []string) matchedLabel {
	if len(options) == 0 {
		return matchedLabel{text: text, selected: -1}
	}
	if n, err := strconv.Atoi(text); err == nil && n >= 1 && n <= len(options) {
		return matchedLabel{text: options[n-1], selected: n - 1}
	}
	for i, opt := range options {
		if strings.EqualFold(text, strings.TrimSpace(opt)) {
			return matchedLabel{text: opt, selected: i}
		}
	}
	return matchedLabel{text: text, selected: -1}
}

// UnrecognizedAllowDenyReason formats a reason for an unrecognized allow/deny reply.
func UnrecognizedAllowDenyReason(reply Reply) string {
	raw := strings.TrimSpace(reply.Text)
	if raw == "" {
		raw = strings.TrimSpace(reply.Decision)
	}
	return fmt.Sprintf("unrecognized decision %q", raw)
}
