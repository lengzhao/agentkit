package agentkit

import "context"

// PromptAssembler builds the model request prompt from registered sections.
type PromptAssembler interface {
	Assemble(context.Context, PromptRequest) ([]ModelMessage, error)
}

type SectionProvider interface {
	Sections() []Section
}

type Section struct {
	Name  string
	Build func(context.Context, PromptRequest) (PromptSection, error)
}

// PromptRequest carries model-visible prompt inputs. Routing context is read
// from TurnEnvelope / SessionIDFromContext / AgentIDFromContext / UserIDFromContext,
// not duplicated here.
type PromptRequest struct {
	Messages []ModelMessage
}

// PromptSection is one section provider contribution before it is folded into
// the leading system message.
type PromptSection struct {
	Name    string
	Content string
}
