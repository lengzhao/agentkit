package agentkit

import "context"

// LLMProvider streams model responses for an already assembled request.
type LLMProvider interface {
	Name() string
	Stream(context.Context, LLMRequest) (LLMStream, error)
}

type ModelCatalog interface {
	Models(context.Context) ([]ModelInfo, error)
}

type LLMRequest struct {
	Model    string
	Messages []ModelMessage
	Tools    []ToolSpec
}

type ModelInfo struct {
	Provider string
	ID       string
	Input    []Modality
	Output   []Modality
}

type Modality string

const (
	ModalityText  Modality = "text"
	ModalityImage Modality = "image"
	ModalityAudio Modality = "audio"
	ModalityVideo Modality = "video"
	ModalityFile  Modality = "file"
)

type LLMStream interface {
	Recv() (LLMEvent, error)
	Close() error
}

type LLMEvent struct {
	Type         string
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
