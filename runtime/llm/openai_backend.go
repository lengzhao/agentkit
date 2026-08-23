package llm

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	openai "github.com/sashabaranov/go-openai"
)

const (
	openAIAPIChat      = "chat"
	openAIAPIResponses = "responses"
)

type openAIBackend interface {
	stream(ctx context.Context, model string, req agentkit.LLMRequest) (agentkit.LLMStream, error)
}

type chatBackend struct {
	client        *openai.Client
	providerRetry ProviderRetrySettings
}

type responsesBackend struct {
	client        *openai.Client
	reasoning     *OpenAIReasoningConfig
	providerRetry ProviderRetrySettings
}

func (p *OpenAI) backend() (openAIBackend, error) {
	switch p.api {
	case openAIAPIChat:
		return &chatBackend{client: p.client, providerRetry: p.providerRetry}, nil
	case openAIAPIResponses:
		return &responsesBackend{client: p.client, reasoning: p.reasoning, providerRetry: p.providerRetry}, nil
	default:
		return nil, fmt.Errorf("unsupported openai api %q: use %q or %q", p.api, openAIAPIChat, openAIAPIResponses)
	}
}
