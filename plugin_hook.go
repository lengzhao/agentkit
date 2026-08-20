package agentkit

import "context"

type HookProvider interface {
	Hooks() []Hook
}

type Hook interface {
	Point() HookPoint
	Order() int
}

type HookPoint string

const (
	HookBeforeStep  HookPoint = "before-step"
	HookBuildPrompt HookPoint = "build-prompt"
	HookBeforeTool  HookPoint = "before-tool"
	HookAfterTool   HookPoint = "after-tool"
	HookLLMRequest  HookPoint = "llm-request"
	HookTurnStop    HookPoint = "turn-stopping"
)

type BeforeStepHook interface {
	Hook
	BeforeStep(context.Context, *BeforeStep) error
}

type BuildPromptHook interface {
	Hook
	BuildPrompt(context.Context, *PromptBuilder) error
}

type BeforeToolHook interface {
	Hook
	BeforeTool(context.Context, *ToolCall) error
}

type AfterToolHook interface {
	Hook
	AfterTool(context.Context, *ToolResult) error
}

type LLMRequestHook interface {
	Hook
	LLMRequest(context.Context, *LLMRequest) error
}

type TurnStoppingHook interface {
	Hook
	TurnStopping(context.Context, *TurnState) (StopDecision, error)
}

type BeforeStep struct {
	SessionID SessionID
	AgentID   AgentID
	Messages  []ModelMessage
}

type PromptBuilder struct {
	Request  PromptRequest
	Sections []PromptSection
}

type TurnState struct {
	SessionID SessionID
	AgentID   AgentID
	Steps     int
}

type StopDecision struct {
	Stop   bool
	Reason string
}
