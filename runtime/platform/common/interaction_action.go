package common

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lengzhao/agentkit/cap/permission"
)

const permissionActionPrefix = "perm:"
const permissionConversationKey = "conversation_id"

// PermissionConversationFromExtra reads the Loop conversation id echoed on card buttons.
func PermissionConversationFromExtra(extra map[string]string) string {
	if extra == nil {
		return ""
	}
	return strings.TrimSpace(extra[permissionConversationKey])
}

// PermissionActionValue is the short action token stored in card button values.
func PermissionActionValue(index int) string {
	return fmt.Sprintf("%s%d", permissionActionPrefix, index)
}

// PermissionDecisionValue is the action token stored in allow/deny card buttons.
func PermissionDecisionValue(decision string) string {
	return permissionActionPrefix + decision
}

// PermissionCallbackData builds Telegram callback_data (perm:<requestID>:<index>).
func PermissionCallbackData(requestID string, index int) string {
	return fmt.Sprintf("%s%s:%d", permissionActionPrefix, requestID, index)
}

// ParsePermissionCallback decodes Telegram callback_data.
func ParsePermissionCallback(data string) (requestID string, index int, ok bool) {
	if !strings.HasPrefix(data, permissionActionPrefix) {
		return "", 0, false
	}
	rest := strings.TrimPrefix(data, permissionActionPrefix)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		return "", 0, false
	}
	idx, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, false
	}
	return parts[0], idx, true
}

// PermissionReplyFromAction extracts a typed permission reply from card callback payloads.
func PermissionReplyFromAction(actionVal, userID string, extra map[string]string) (permission.Reply, bool) {
	if extra == nil {
		return permission.Reply{}, false
	}
	requestID := strings.TrimSpace(extra["request_id"])
	if requestID == "" || !strings.HasPrefix(actionVal, permissionActionPrefix) {
		return permission.Reply{}, false
	}
	reply := permission.Reply{
		RequestID: requestID,
		UserID:    userID,
		Text:      strings.TrimSpace(extra["answer_text"]),
		Decision:  strings.TrimSpace(extra["decision"]),
	}
	if selected := strings.TrimSpace(extra["selected"]); selected != "" {
		if idx, err := strconv.Atoi(selected); err == nil {
			reply.Selected = []int{idx}
		}
	}
	return reply, true
}

// ConfirmedCardFromReply builds a non-interactive card after the user responds.
func ConfirmedCardFromReply(reply permission.Reply, extra map[string]string) *Card {
	if strings.TrimSpace(reply.Decision) != "" {
		title := strings.TrimSpace(extra["perm_label"])
		if title == "" {
			title = confirmedDecisionTitle(reply.Decision)
		}
		return ConfirmedAllowDenyCard(title, extra["perm_body"], extra["perm_color"])
	}
	question := strings.TrimSpace(extra["askq_question"])
	answer := strings.TrimSpace(reply.Text)
	if answer == "" {
		answer = strings.TrimSpace(reply.Decision)
	}
	return ConfirmedPermissionCard(question, answer)
}

func confirmedDecisionTitle(decision string) string {
	switch decision {
	case "allow":
		return "✅ 允许"
	case "deny":
		return "❌ 拒绝"
	case "allow_all":
		return "✅ 全部允许"
	default:
		if decision == "" {
			return "✅ 已确认"
		}
		return "✅ " + decision
	}
}

// ConfirmedPermissionCard updates the card after the user picks an option.
func ConfirmedPermissionCard(question, answer string) *Card {
	b := NewCard().Title("✅ "+answer, "green")
	if question != "" {
		b.Markdown(question)
	}
	b.Markdown("**→ " + answer + "**")
	card := b.Build()
	card.Static = true
	return card
}

// ConfirmedAllowDenyCard updates an allow/deny card after the user decides.
func ConfirmedAllowDenyCard(title, body, color string) *Card {
	if strings.TrimSpace(title) == "" {
		title = "✅ 已确认"
	}
	if color == "" {
		color = "green"
	}
	b := NewCard().Title(title, color)
	if strings.TrimSpace(body) != "" {
		b.Markdown(body)
	}
	card := b.Build()
	card.Static = true
	return card
}
