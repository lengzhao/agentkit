package common

import "strings"

const (
	UnknownInteractionForward = "forward"
	UnknownInteractionIgnore  = "ignore"
)

// NormalizeUnknownInteraction maps platform config to forward or ignore (default forward).
func NormalizeUnknownInteraction(mode string) string {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		return UnknownInteractionForward
	}
	if mode == UnknownInteractionIgnore {
		return UnknownInteractionIgnore
	}
	return UnknownInteractionForward
}

// InteractionDedupKey joins segments for short-lived interaction deduplication.
func InteractionDedupKey(parts ...string) string {
	return strings.Join(parts, "\x1e")
}

// FormatCardActionInbound builds agent-visible text for unrecognized IM card/block callbacks.
func FormatCardActionInbound(cardText string, fetchedCard bool, operationBody string, userID string) string {
	var b strings.Builder
	b.WriteString("[card_action]\n")
	if strings.TrimSpace(cardText) != "" {
		b.WriteString("卡片内容:\n")
		b.WriteString(strings.TrimSpace(cardText))
		b.WriteByte('\n')
	} else if fetchedCard {
		b.WriteString("卡片内容: (空)\n")
	}
	b.WriteString("\n用户操作:\n")
	op := strings.TrimSpace(operationBody)
	if op == "" {
		op = "(无操作详情)"
	}
	b.WriteString(op)
	if strings.TrimSpace(userID) != "" {
		b.WriteString("\n操作人: ")
		b.WriteString(userID)
	}
	b.WriteString("\n[/card_action]")
	return b.String()
}
