package agentkit

import "context"

// PromptAssembler builds the model request prompt from registered sections.
type PromptAssembler interface {
	Assemble(context.Context, PromptRequest) (Prompt, error)
}

type SectionProvider interface {
	Sections() []Section
}

type Section struct {
	Name  string
	Order int
	Build func(context.Context, PromptRequest) (PromptSection, error)
}

type PromptRequest struct {
	SessionID SessionID
	AgentID   AgentID
	Messages  []ModelMessage
	Tools     []ToolSpec
}

type Prompt struct {
	Messages []ModelMessage
	Sections []PromptSection
}

type PromptSection struct {
	Name    string
	Content string
}
