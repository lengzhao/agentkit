package agentkit

import (
	"encoding/json"
	"errors"
	"strings"
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
	// KeyDeliverySessionID is the inbound delivery SessionID for the current turn
	// (finest grain). Outbound routing should prefer this over KeySessionID when
	// both are present.
	KeyDeliverySessionID contextKey = "agentkit.delivery_session_id"
	// KeyStoreSessionID is the logical SessionID used for model-visible history.
	// It may differ from KeySessionID when a stable IM delivery/effective key has
	// been switched to a fresh history with /new.
	KeyStoreSessionID contextKey = "agentkit.store_session_id"
	// KeyInSubagent marks a context running inside a delegated child agent. While
	// set, session append/recovery must target KeySessionID only and must not
	// inherit a parent's KeyStoreSessionID mapping.
	KeyInSubagent contextKey = "agentkit.subagent.active"
	// KeyMessageMetadata is optional platform metadata for the current inbound turn.
	KeyMessageMetadata contextKey = "agentkit.message_metadata"
	// KeyProactiveSendUsed is set when tool/send delivers through the turn emit
	// channel during the current turn.
	KeyProactiveSendUsed contextKey = "agentkit.proactive_send_used"
	// KeyScheduleFireTurn marks a turn started by schedule runtime. Turn-end
	// assistant text may be suppressed when send already delivered the message.
	KeyScheduleFireTurn contextKey = "agentkit.schedule_fire_turn"
	// KeyScheduleStateless marks a schedule-fired turn that must not inherit the
	// delivery conversation's active session history (similar to KeyInSubagent).
	KeyScheduleStateless contextKey = "agentkit.schedule_stateless"
)

// InboundMetaTag is the bracket tag for runner inject prefixes on user messages.
// Example: [meta sender_id=U111 sender_name="Alice" timestamp="..." timezone="UTC"]
const InboundMetaTag = "meta"

// MetadataSkipPromptMeta, when true on MessageEvent.Metadata, tells runner not to
// prepend the optional [meta ...] inbound prefix for that turn.
const MetadataSkipPromptMeta = "skipPromptMeta"

// SessionID identifies a conversation unit. Platforms emit a delivery SessionID
// (finest grain: channel + optional :t:thread + optional :u:user). Runner applies
// sessionScope to derive the effective SessionID used for Loop locking and
// session history; outbound replies still use the delivery id.
//
// Delivery examples:
//
//	slack:C123ABC
//	slack:C123ABC:t:1712345678.123456:u:U456
//	feishu:oc_xxx:om_yyy
//	cli:default
//
// Loop treats the effective SessionID as opaque. Agents read and append history
// using the logical store SessionID resolved for the turn. Only platform plugins
// decode delivery SessionIDs into IM routing targets.
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
	// Metadata carries optional platform fields for replay-time user-message
	// templates (display name, channel label, etc.). Persisted on user messages.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// MessageEvent is the inbound envelope from Platform to Loop. SessionID is the
// conversation key for loop locking and history. When DeliverySessionID is set,
// outbound routing uses it instead of SessionID (schedule side sessions).
type MessageEvent struct {
	SessionID         SessionID // required: loop/history id
	DeliverySessionID SessionID // optional: platform delivery override
	AgentID           AgentID
	PlatformID        string
	UserID            string
	Message           ModelMessage
	// Metadata is optional platform context copied onto persisted user messages.
	Metadata map[string]any `json:"metadata,omitempty"`
	// Reply carries a permission answer as JSON. Decode with permission.DecodeReply.
	Reply json.RawMessage `json:"reply,omitempty"`
}

// OutboundEvent is the outbound envelope from Agent/Loop to Platform. SessionID
// must match the conversation that produced the turn so the platform can route
// the reply to the correct IM target. PlatformID is required; multiplex rejects
// empty values instead of broadcasting to every leaf platform.
type OutboundEvent struct {
	SessionID  SessionID // required
	AgentID    AgentID
	PlatformID string // required
	UserID     string
	Type       EventType
	Data       json.RawMessage
}

// ErrOutboundPlatformRequired is returned when OutboundEvent.PlatformID is empty.
var ErrOutboundPlatformRequired = errors.New("outbound event requires platformID")

// RequirePlatformID reports whether the event names a target platform.
func (e OutboundEvent) RequirePlatformID() error {
	if strings.TrimSpace(e.PlatformID) == "" {
		return ErrOutboundPlatformRequired
	}
	return nil
}

type JSONSchema struct {
	Type        string                `json:"type,omitempty"`
	Description string                `json:"description,omitempty"`
	Properties  map[string]JSONSchema `json:"properties,omitempty"`
	Required    []string              `json:"required,omitempty"`
	Items       *JSONSchema           `json:"items,omitempty"`
	Raw         map[string]any        `json:"-"`
}
