package chathistory

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// Message is one chat-visible message from the transport platform.
type Message struct {
	ID          string `json:"id"`
	SenderID    string `json:"senderId,omitempty"`
	SenderName  string `json:"senderName,omitempty"`
	Role        string `json:"role"`
	Content     string `json:"content"`
	Timestamp   int64  `json:"timestamp"`
	ThreadID    string `json:"threadId,omitempty"`
	MessageType string `json:"messageType,omitempty"`
}

// Request describes a history read against a delivery session.
type Request struct {
	SessionID agentkit.SessionID
	UserID    string
	Limit     int
	Before    string // page cursor (page_token)
	After     string // start time, unix seconds
	Order     string // asc | desc
	Query     string // optional client-side keyword filter
	Thread    bool   // when true and session has :t:, scope to thread messages
}

// Result is a page of transport history.
type Result struct {
	Messages []Message
	HasMore  bool
	Cursor   string
	Source   string
}

// Provider reads chat history from the underlying transport.
type Provider interface {
	ReadChatHistory(ctx context.Context, req Request) (Result, error)
}

// Router resolves a leaf platform by PlatformID.
type Router interface {
	ChatHistoryFor(platformID string) Provider
}
