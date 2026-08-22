package agentkit

import (
	"encoding/json"
	"time"
)

type AgentID string
type SessionID string
type ToolCallID string
type EventID string
type EventType string
type EventSeq int64

type ModelMessage struct {
	Role        string
	Content     []ContentPart
	ToolCalls   []ToolCall
	ToolResults []ToolResult
	Raw         json.RawMessage
}

type ContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type SessionEvent struct {
	ID        EventID
	Seq       EventSeq
	SessionID SessionID
	AgentID   AgentID
	Type      EventType
	Data      json.RawMessage
	CreatedAt time.Time
}

type MessageEvent struct {
	SessionID  SessionID
	AgentID    AgentID
	PlatformID string
	Message    ModelMessage
}

type OutboundEvent struct {
	SessionID  SessionID
	AgentID    AgentID
	PlatformID string
	Type       EventType
	Data       json.RawMessage
}

type JSONSchema struct {
	Type        string                `json:"type,omitempty"`
	Description string                `json:"description,omitempty"`
	Properties  map[string]JSONSchema `json:"properties,omitempty"`
	Required    []string              `json:"required,omitempty"`
	Items       *JSONSchema           `json:"items,omitempty"`
	Raw         map[string]any        `json:"-"`
}
