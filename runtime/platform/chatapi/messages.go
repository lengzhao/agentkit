package chatapi

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

type historyMessage struct {
	ID        string
	Role      string
	Content   string
	UserID    string
	CreatedAt int64
	Index     int
}

func parseMessageCursor(cursor string) (conversationID string, entryIndex int, err error) {
	if cursor == "" {
		return "", -1, nil
	}
	idx := strings.LastIndex(cursor, ":")
	if idx <= 0 || idx >= len(cursor)-1 {
		return "", 0, fmt.Errorf("invalid cursor")
	}
	conversationID = cursor[:idx]
	entryIndex, err = strconv.Atoi(cursor[idx+1:])
	if err != nil || entryIndex < 0 {
		return "", 0, fmt.Errorf("invalid cursor")
	}
	return conversationID, entryIndex, nil
}

func historyMessagesFromSession(ctx context.Context, conversationID string, sess agentkit.Session) ([]historyMessage, error) {
	chat, err := common.MessagesFromSession(ctx, sess)
	if err != nil {
		return nil, err
	}
	out := make([]historyMessage, len(chat))
	for i, m := range chat {
		out[i] = historyMessage{
			ID:        messageID(conversationID, i),
			Role:      m.Role,
			Content:   m.Content,
			UserID:    m.UserID,
			CreatedAt: m.CreatedAt,
			Index:     i,
		}
	}
	return out, nil
}

func historyMessageToAPI(m historyMessage) map[string]any {
	msg := map[string]any{
		"id":         m.ID,
		"role":       m.Role,
		"content":    m.Content,
		"created_at": m.CreatedAt,
	}
	if m.UserID != "" {
		msg["user_id"] = m.UserID
	}
	switch m.Role {
	case "user":
		msg["query"] = m.Content
	case "assistant":
		msg["answer"] = m.Content
	}
	return msg
}

func historyUserID(c *conversation, headerUser string) string {
	if u := strings.TrimSpace(headerUser); u != "" {
		return u
	}
	if c != nil {
		return strings.TrimSpace(c.CreatedBy)
	}
	return ""
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func paginateMessages(items []historyMessage, cursor string, limit int) ([]historyMessage, bool, string, error) {
	limit = clampLimit(limit)
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}

	start := 0
	if cursor != "" {
		_, entryIndex, err := parseMessageCursor(cursor)
		if err != nil {
			return nil, false, "", err
		}
		found := false
		for i, m := range items {
			if m.Index == entryIndex {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			return nil, false, "", fmt.Errorf("not found")
		}
	}

	end := start + limit
	hasMore := end < len(items)
	if end > len(items) {
		end = len(items)
	}
	page := items[start:end]
	var nextCursor string
	if hasMore && len(page) > 0 {
		nextCursor = page[len(page)-1].ID
	}
	return page, hasMore, nextCursor, nil
}

func (p *Platform) handleConversationMessages(w http.ResponseWriter, r *http.Request, conversationID string) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "invalid request")
		return
	}
	channel, ok := p.resolveChannel(w, r)
	if !ok {
		return
	}
	if p.sessionStore == nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	c, err := p.resolveConversation(r.Context(), channel, conversationID, "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if c == nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	user := optionalUser(r, p.userHeader)

	sess, err := p.historySession(r.Context(), channel, conversationID, historyUserID(c, user))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if cursor != "" {
		cursorConv, _, err := parseMessageCursor(cursor)
		if err != nil || cursorConv != conversationID {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
	}

	items := []historyMessage{}
	if sess != nil {
		var err error
		items, err = historyMessagesFromSession(r.Context(), conversationID, sess)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	page, hasMore, nextCursor, err := paginateMessages(items, cursor, limit)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	messages := make([]map[string]any, len(page))
	for i, m := range page {
		messages[i] = historyMessageToAPI(m)
	}
	data := map[string]any{
		"limit":    clampLimit(limit),
		"has_more": hasMore,
		"messages": messages,
	}
	if nextCursor != "" {
		data["next_cursor"] = nextCursor
	}
	writeOK(w, http.StatusOK, data)
}
