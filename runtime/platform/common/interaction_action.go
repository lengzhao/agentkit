package common

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lengzhao/agentkit/cap/permission"
)

const permissionActionPrefix = "perm:"

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

// ConfirmedPermissionCard updates the card after the user picks an option.
func ConfirmedPermissionCard(question, answer string) *Card {
	b := NewCard().Title("✅ "+answer, "green")
	if question != "" {
		b.Markdown(question)
	}
	b.Markdown("**→ " + answer + "**")
	return b.Build()
}
