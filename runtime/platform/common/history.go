package common

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
)

// ChatMessage is a user-facing message derived from session events for IM / history UIs.
type ChatMessage struct {
	Role      string
	Content   string
	UserID    string
	CreatedAt int64
}

// MessagesFromSession reads all session events and derives chat-visible messages.
func MessagesFromSession(ctx context.Context, sess agentkit.Session) ([]ChatMessage, error) {
	events, err := sess.Read(ctx, 0)
	if err != nil {
		return nil, err
	}
	return MessagesFromEvents(events), nil
}

// MessagesFromEvents derives chat-visible messages from session events.
func MessagesFromEvents(events []agentkit.SessionEvent) []ChatMessage {
	out := make([]ChatMessage, 0)
	for _, ev := range events {
		if m := MessageFromEvent(ev); m != nil {
			out = append(out, *m)
		}
	}
	return out
}

// MessageFromEvent maps one session event to a chat message when applicable.
func MessageFromEvent(ev agentkit.SessionEvent) *ChatMessage {
	switch ev.Type {
	case agentkit.EventUserMessage:
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			return nil
		}
		text := ModelMessageText(msg)
		if text == "" {
			return nil
		}
		role := msg.Role
		if role == "" {
			role = "user"
		}
		return &ChatMessage{
			Role:      role,
			Content:   text,
			UserID:    ev.UserID,
			CreatedAt: unixOrZero(ev.CreatedAt),
		}
	case agentkit.EventAssistantMessage:
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			return nil
		}
		text := ModelMessageText(msg)
		if text == "" {
			return nil
		}
		role := msg.Role
		if role == "" {
			role = "assistant"
		}
		return &ChatMessage{
			Role:      role,
			Content:   text,
			UserID:    ev.UserID,
			CreatedAt: unixOrZero(ev.CreatedAt),
		}
	case agentkit.EventToolCall:
		var call agentkit.ToolCall
		if err := json.Unmarshal(ev.Data, &call); err != nil {
			return nil
		}
		text := formatAskUserToolCall(call)
		if text == "" {
			return nil
		}
		return &ChatMessage{
			Role:      "assistant",
			Content:   text,
			CreatedAt: unixOrZero(ev.CreatedAt),
		}
	case agentkit.EventToolResult:
		var result agentkit.ToolResult
		if err := json.Unmarshal(ev.Data, &result); err != nil {
			return nil
		}
		text := askUserAnswerFromResult(result)
		if text == "" {
			return nil
		}
		return &ChatMessage{
			Role:      "user",
			Content:   text,
			UserID:    ev.UserID,
			CreatedAt: unixOrZero(ev.CreatedAt),
		}
	case agentkit.EventPermissionRequest:
		var payload permission.RequestPayload
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			return nil
		}
		text := FormatPermissionRequest(payload)
		if text == "" {
			return nil
		}
		return &ChatMessage{
			Role:      "assistant",
			Content:   text,
			CreatedAt: unixOrZero(ev.CreatedAt),
		}
	default:
		return nil
	}
}

// ModelMessageText joins non-empty text parts from a model message.
func ModelMessageText(msg agentkit.ModelMessage) string {
	var parts []string
	for _, part := range msg.Content {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// FormatAskUserPrompt formats ask_user question and numbered options for display.
func FormatAskUserPrompt(question string, options []string) string {
	question = strings.TrimSpace(question)
	if question == "" {
		return ""
	}
	if len(options) == 0 {
		return question
	}
	var b strings.Builder
	b.WriteString(question)
	b.WriteByte('\n')
	for i, opt := range options {
		if strings.TrimSpace(opt) == "" {
			continue
		}
		fmt.Fprintf(&b, "%d. %s\n", i+1, opt)
	}
	return strings.TrimSpace(b.String())
}

// FormatPermissionRequest formats a permission request as assistant-visible text.
func FormatPermissionRequest(payload permission.RequestPayload) string {
	switch payload.Kind {
	case permission.KindQuestion:
		if payload.Question == nil {
			return ""
		}
		opts := make([]string, 0, len(payload.Question.Options))
		for _, opt := range payload.Question.Options {
			opts = append(opts, opt.Label)
		}
		return FormatAskUserPrompt(payload.Question.Prompt, opts)
	case permission.KindAllowDeny:
		prompt := strings.TrimSpace(payload.Reason)
		if prompt == "" {
			prompt = "是否允许执行该操作？"
		}
		if payload.ToolCall != nil && payload.ToolCall.Name != "" {
			return fmt.Sprintf("是否允许执行工具 %q？\n%s", payload.ToolCall.Name, prompt)
		}
		return prompt
	default:
		return ""
	}
}

type askUserCallInput struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
	Default  string   `json:"default"`
}

type askUserResultOutput struct {
	Answered bool   `json:"answered"`
	Answer   string `json:"answer"`
}

func formatAskUserToolCall(call agentkit.ToolCall) string {
	if call.Name != "ask_user" {
		return ""
	}
	var input askUserCallInput
	if err := json.Unmarshal(call.Input, &input); err != nil {
		return ""
	}
	return FormatAskUserPrompt(input.Question, input.Options)
}

func askUserAnswerFromResult(result agentkit.ToolResult) string {
	if result.Name != "ask_user" {
		return ""
	}
	text := strings.TrimSpace(result.Content)
	if text == "" {
		return ""
	}
	var out askUserResultOutput
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return ""
	}
	if out.Answered && strings.TrimSpace(out.Answer) != "" {
		return strings.TrimSpace(out.Answer)
	}
	return ""
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}
