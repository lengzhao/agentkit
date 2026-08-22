package agentkit

import "context"

// LLMProvider streams model responses for an already assembled request.
// LLMRequest carries model-visible messages only; session routing is handled
// by the agent runtime before and after the provider boundary.
type LLMProvider interface {
	Name() string
	Stream(context.Context, LLMRequest) (LLMStream, error)
}

type LLMRequest struct {
	Model    string
	Messages []ModelMessage
	Tools    []ToolSpec
}

type LLMStream interface {
	Recv() (LLMEvent, error)
	Close() error
}

// LLMEventMessage carries a finalized (or snapshot) ModelMessage rather than a
// delta. It is internal to the provider/agent boundary and is never forwarded
// to platforms, so it has no counterpart in the Pi RPC event set.
const LLMEventMessage AssistantMessageEventType = "message"

// LLMEvent reuses AssistantMessageEventType so provider output and the
// platform-facing stream share one vocabulary. Providers emit the text_*,
// thinking_* and toolcall_* values plus LLMEventMessage; AssistantEventDone and
// AssistantEventError are wire-only and never produced here.
type LLMEvent struct {
	Type         AssistantMessageEventType
	Message      *ModelMessage
	ContentIndex int
	Delta        string
	ToolCall     *ToolCall
	Usage        *Usage
	Raw          []byte
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}
