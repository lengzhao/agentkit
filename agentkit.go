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
	// KeyInSubagent marks a context running inside a delegated child agent.
	KeyInSubagent contextKey = "agentkit.subagent.active"
	// KeyProactiveSendUsed is set when tool/send delivers through the turn emit
	// channel during the current turn.
	KeyProactiveSendUsed contextKey = "agentkit.proactive_send_used"
	// KeyScheduleFireTurn marks a turn started by schedule runtime. Turn-end
	// assistant text may be suppressed when send already delivered the message.
	KeyScheduleFireTurn contextKey = "agentkit.schedule_fire_turn"
	// KeyScheduleStateless marks a schedule-fired turn that must not inherit the
	// delivery conversation's active session history (similar to KeyInSubagent).
	KeyScheduleStateless contextKey = "agentkit.schedule_stateless"
	// KeyTurnEnvelope carries the normalized Route / Conversation / Workspace
	// context for the current turn. Runner sets it before Loop.Dispatch.
	KeyTurnEnvelope contextKey = "agentkit.turn_envelope"
)

// InboundMetaTag is the bracket tag for runner inject prefixes on user messages.
// Example: [meta sender_id=U111 sender_name="Alice" timestamp="..." timezone="UTC"]
const InboundMetaTag = "meta"

// MetadataSkipPromptMeta, when true on MessageEvent.Metadata, tells runner not to
// prepend the optional [meta ...] inbound prefix for that turn.
const MetadataSkipPromptMeta = "skipPromptMeta"

// SessionID identifies a conversation unit for Loop locking and durable history.
// Platforms emit a delivery route (finest grain: channel + optional :t:thread
// + optional :u:user). Runner resolves active-session mappings (/new) and writes
// the resulting conversation into TurnEnvelope before Loop.Dispatch. Outbound
// replies still use the delivery route.
//
// Delivery examples:
//
//	slack:C123ABC
//	slack:C123ABC:t:1712345678.123456:u:U456
//	feishu:oc_xxx:om_yyy
//	cli:default
//
// Only platform plugins decode delivery SessionIDs into IM routing targets.
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
	// Source is a workspace-relative attachment path (e.g. work/upload/foo.png) used
	// for session persistence and vision replay; not sent to model providers.
	// Persisted attachments use type attachment_ref (see cap/media).
	Source string `json:"source,omitempty"`
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

// MessageEvent is the inbound envelope from Platform to Loop.
//
// Platforms should set Envelope.Route on ingress (e.g. common.WithInboundRoute).
// Runner normalizes into TurnEnvelope before Dispatch: Conversation for
// history/lock, Workspace for tenant resources, Route for outbound return path.
type MessageEvent struct {
	// Envelope is optional on ingress; runner fills it when empty.
	Envelope TurnEnvelope `json:"envelope,omitempty"`
	AgentID           AgentID
	PlatformID        string
	UserID            string
	Message           ModelMessage
	// Metadata is optional platform context copied onto persisted user messages.
	Metadata map[string]any `json:"metadata,omitempty"`
	// Reply carries a permission answer as JSON. Decode with runtime/permission.DecodeReply.
	Reply json.RawMessage `json:"reply,omitempty"`
}

// OutboundEvent is the outbound envelope from Agent/Loop to Platform. Route is
// the return address captured at inbound. PlatformID is required; multiplex
// rejects empty values.
type OutboundEvent struct {
	Route      RouteRef
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
