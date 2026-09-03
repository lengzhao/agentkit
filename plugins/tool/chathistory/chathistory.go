package chathistory

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/chathistory"
)

type ChatHistoryConfig struct {
	DefaultLimit int `json:"defaultLimit"`
	MaxLimit     int `json:"maxLimit"`
}

type ChatHistoryDeps struct {
	Platform agentkit.Platform `json:"platform"`
}

type ChatHistoryInput struct {
	SessionID string `json:"sessionId,omitempty" jsonschema:"Optional delivery target; defaults to the current inbox"`
	UserID    string `json:"userId,omitempty" jsonschema:"Optional user target when sessionId is omitted"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum messages to return"`
	Before    string `json:"before,omitempty" jsonschema:"Pagination cursor from a previous response"`
	After     string `json:"after,omitempty" jsonschema:"Only messages after this unix timestamp (seconds)"`
	Order     string `json:"order,omitempty" jsonschema:"Sort order: asc or desc (default desc)"`
	Query     string `json:"query,omitempty" jsonschema:"Optional keyword filter applied to message content"`
	Thread    *bool  `json:"thread,omitempty" jsonschema:"When true, scope to the current thread when available (default true)"`
}

type ChatHistoryOutput struct {
	Messages []chathistory.Message `json:"messages"`
	HasMore  bool                  `json:"hasMore"`
	Cursor   string                `json:"cursor,omitempty"`
	Source   string                `json:"source,omitempty"`
	Count    int                   `json:"count"`
}

// NewChatHistory registers tool/chat-history: read transport chat history on demand.
func NewChatHistory(cfg ChatHistoryConfig, deps ChatHistoryDeps) (agentkit.Tool, error) {
	if deps.Platform == nil {
		return nil, fmt.Errorf("tool/chat-history requires platform dependency")
	}
	return agentkit.NewTool[ChatHistoryInput, ChatHistoryOutput]("chat_history", func(ctx context.Context, input ChatHistoryInput) (ChatHistoryOutput, error) {
		return Dispatch(ctx, deps, cfg, input)
	}).
		Description("Read recent chat history from the current conversation or a specified session. Use when you need context from messages the bot has not processed yet, such as earlier group discussion.").
		Build()
}
