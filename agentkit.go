package agentkit

import (
	"context"
	"encoding/json"
	"time"
)

type StartStop interface {
	Start(context.Context) error
	Stop(context.Context) error
}

type PluginID string
type PluginKind string
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

type Message = ModelMessage

type ContentPart struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	MIME       string          `json:"mime,omitempty"`
	Data       []byte          `json:"data,omitempty"`
	URI        string          `json:"uri,omitempty"`
	Attachment *AttachmentRef  `json:"attachment,omitempty"`
	Meta       json.RawMessage `json:"meta,omitempty"`
}

type AttachmentID string

type AttachmentRef struct {
	ID     AttachmentID `json:"id"`
	MIME   string       `json:"mime"`
	Bytes  int64        `json:"bytes,omitempty"`
	Width  int          `json:"width,omitempty"`
	Height int          `json:"height,omitempty"`
	Name   string       `json:"name,omitempty"`
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

type Event = SessionEvent

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
	Additional  *bool                 `json:"additionalProperties,omitempty"`
	Defs        map[string]JSONSchema `json:"$defs,omitempty"`
	Raw         map[string]any        `json:"-"`
}
