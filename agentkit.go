package agentkit

import (
	"encoding/json"
	"time"
)

type AgentID string

type contextKey string

const (
	// KeySessionID is the context key for the current opaque SessionID.
	KeySessionID contextKey = "agentkit.session_id"
	// KeyAgentID is the context key for the current AgentID.
	KeyAgentID contextKey = "agentkit.agent_id"
	// KeyPlatformID is the context key for the current inbound/outbound platform.
	KeyPlatformID contextKey = "agentkit.platform_id"
	// KeyUserID is the context key for the current end-user identity, when known.
	KeyUserID contextKey = "agentkit.user_id"
	// KeyTurnID is the context key for the current turn, when one is active.
	KeyTurnID contextKey = "agentkit.turn_id"
	// KeyToolCallID is the context key for the current tool call, when one is active.
	KeyToolCallID contextKey = "agentkit.tool_call_id"
	// KeySessionControl is the context key for per-session steer/follow-up state.
	// Loop sets it before Agent.RunTurn; the value is a runtime/loop.Control that
	// also implements permission.Broker.
	KeySessionControl contextKey = "agentkit.session_control"
	// KeyOutboundEmit is the per-turn outbound hook. Loop sets it before
	// Agent.RunTurn so tools and permission waits can emit outbound events
	// through the same channel as assistant streaming.
	KeyOutboundEmit contextKey = "agentkit.outbound_emit"
)

// SessionID is an opaque conversation routing key. Platforms (platform/slack,
// platform/feishu, platform/cli, ...) generate it following cc-connect
// conventions:
//
//	<platform>:<segment>[:<segment>...]
//
// Examples:
//
//	slack:C123ABC
//	slack:C123ABC:U456
//	slack:C123ABC:t:1712345678.123456
//	feishu:oc_xxx:root:om_yyy
//	cli:default
//
// Loop and Agent treat SessionID as opaque; they never parse platform segments.
// Only platform plugins may encode or decode delivery targets from a SessionID.
type SessionID string

type ToolCallID string
type EventID string
type EventType string
type EventSeq int64

// ModelMessage is model-visible content only. Session routing lives on event
// envelopes and context keys, not on ModelMessage.
type ModelMessage struct {
	Role        string
	Content     []ContentPart
	ToolCalls   []ToolCall
	ToolResults []ToolResult
	Raw         json.RawMessage
}

type ContentPart struct {
	Type   string `json:"type"`
	Text   string `json:"text,omitempty"`
	URL    string `json:"url,omitempty"`
	MIME   string `json:"mime,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type SessionEvent struct {
	ID        EventID
	Seq       EventSeq
	SessionID SessionID
	AgentID   AgentID
	Type      EventType
	Data      json.RawMessage
	CreatedAt time.Time
	// UserID attributes the event to an end user. It is set on user messages
	// when the platform knows who spoke, and is what makes a session shared by a
	// whole Slack channel legible: without it the model reads one undifferentiated
	// stream of user turns. Empty for single-user transports such as the CLI, and
	// for everything the agent itself produces.
	UserID string
}

// MessageEvent is the inbound envelope from Platform to Loop. SessionID is
// required on every message; platforms must assign a stable ID per conversation
// unit (channel, thread, DM, CLI session, etc.).
type MessageEvent struct {
	SessionID  SessionID // required
	AgentID    AgentID
	PlatformID string
	UserID     string
	Message    ModelMessage
	// Reply carries a permission answer as JSON. Decode with permission.DecodeReply.
	Reply json.RawMessage `json:"reply,omitempty"`
}

// OutboundEvent is the outbound envelope from Agent/Loop to Platform. SessionID
// must match the conversation that produced the turn so the platform can route
// the reply to the correct IM target.
type OutboundEvent struct {
	SessionID  SessionID // required
	AgentID    AgentID
	PlatformID string
	UserID     string
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
