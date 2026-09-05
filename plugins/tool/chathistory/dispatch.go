package chathistory

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit/cap/chathistory"
	capsdelivery "github.com/lengzhao/agentkit/cap/delivery"
	rtdelivery "github.com/lengzhao/agentkit/runtime/delivery"
)

func Dispatch(ctx context.Context, deps runtimeDeps, cfg ChatHistoryConfig, input ChatHistoryInput) (ChatHistoryOutput, error) {
	if deps.router == nil {
		return ChatHistoryOutput{}, fmt.Errorf("tool/chat-history requires history dependency")
	}

	route, err := rtdelivery.ResolveRoute(ctx, capsdelivery.RouteInput{
		SessionID: input.SessionID,
		UserID:    input.UserID,
	})
	if err != nil {
		return ChatHistoryOutput{}, err
	}

	provider := deps.router.ChatHistoryFor(route.PlatformID)
	if provider == nil {
		return ChatHistoryOutput{}, nil
	}

	thread := true
	if input.Thread != nil {
		thread = *input.Thread
	}

	result, err := provider.ReadChatHistory(ctx, chathistory.Request{
		SessionID: route.SessionID,
		UserID:    route.UserID,
		Limit:     clampLimit(input.Limit, cfg),
		Before:    strings.TrimSpace(input.Before),
		After:     strings.TrimSpace(input.After),
		Order:     strings.TrimSpace(input.Order),
		Query:     strings.TrimSpace(input.Query),
		Thread:    thread,
	})
	if err != nil {
		return ChatHistoryOutput{}, err
	}

	messages := result.Messages
	if q := strings.TrimSpace(input.Query); q != "" {
		messages = filterByQuery(messages, q)
	}

	return ChatHistoryOutput{
		Messages: messages,
		HasMore:  result.HasMore,
		Cursor:   result.Cursor,
		Source:   result.Source,
		Count:    len(messages),
	}, nil
}

func clampLimit(limit int, cfg ChatHistoryConfig) int {
	max := cfg.MaxLimit
	if max <= 0 {
		max = 50
	}
	if limit <= 0 {
		limit = cfg.DefaultLimit
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > max {
		limit = max
	}
	return limit
}

func filterByQuery(messages []chathistory.Message, query string) []chathistory.Message {
	q := strings.ToLower(query)
	out := make([]chathistory.Message, 0, len(messages))
	for _, m := range messages {
		if strings.Contains(strings.ToLower(m.Content), q) {
			out = append(out, m)
		}
	}
	return out
}
