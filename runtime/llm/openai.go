package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
	openai "github.com/sashabaranov/go-openai"
)

type OpenAIConfig struct {
	// Model is model name.
	Model string `json:"model"`
	// BaseURL is API base URL, e.g. https://api.openai.com/v1.
	BaseURL string `json:"baseUrl"`
	// APIKey is inline key. Prefer APIKeyRef so the secret stays out of the config file.
	APIKey string `json:"apiKey"`
	// APIKeyRef is credentials reference, e.g. env:OPENAI_API_KEY.
	APIKeyRef string `json:"apiKeyRef"`
	// API is chat or responses.
	API string `json:"api"`
	// Reasoning is reasoning effort and summary settings, for models that support them.
	Reasoning *OpenAIReasoningConfig `json:"reasoning,omitempty"`
	// Retry is provider-level retry, separate from the agent's per-step retry.
	Retry *LLMRetryConfig `json:"retry,omitempty"`
}

type OpenAIReasoningConfig struct {
	Effort          string `json:"effort,omitempty"`
	GenerateSummary string `json:"generateSummary,omitempty"`
}

type OpenAIDeps struct {
	Credentials credentials.Store `json:"credentials,omitempty"`
}

type OpenAI struct {
	model         string
	api           string
	reasoning     *OpenAIReasoningConfig
	providerRetry ProviderRetrySettings
	apiKey        string
	client        *openai.Client
}

// NewOpenAI registers llm/openai-compatible: OpenAI-compatible provider, chat or responses API.
//
// Best practices:
//   - Token budgets and compaction/token-limit need reported usage; the chat API supplies it, the responses API may not.
func NewOpenAI(cfg OpenAIConfig, deps OpenAIDeps) (agentkit.LLMProvider, error) {
	model := cfg.Model
	if model == "" {
		model = "gpt-4o"
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	apiKey, err := resolveAPIKey(context.Background(), cfg.APIKey, cfg.APIKeyRef, deps.Credentials)
	if err != nil {
		return nil, err
	}
	return &OpenAI{
		model:         model,
		api:           parseAPIMode(cfg.API),
		reasoning:     cfg.Reasoning,
		providerRetry: defaultProviderRetry(retryProviderConfig(cfg.Retry)),
		apiKey:        apiKey,
		client:        newOpenAIClient(apiKey, baseURL),
	}, nil
}

func retryProviderConfig(cfg *LLMRetryConfig) *ProviderRetrySettings {
	if cfg == nil {
		return nil
	}
	return cfg.Provider
}

func (p *OpenAI) Name() string { return "openai-compatible" }

func (p *OpenAI) Stream(ctx context.Context, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("missing API key: set config.apiKeyRef, config.apiKey, or OPENAI_API_KEY")
	}
	model := req.Model
	if model == "" {
		model = p.model
	}
	backend, err := p.backend()
	if err != nil {
		return nil, err
	}
	return backend.stream(ctx, model, req)
}
