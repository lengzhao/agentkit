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
	Model     string                 `json:"model"`
	BaseURL   string                 `json:"baseUrl"`
	APIKey    string                 `json:"apiKey"`
	APIKeyRef string                 `json:"apiKeyRef"`
	API       string                 `json:"api"`
	Reasoning *OpenAIReasoningConfig `json:"reasoning,omitempty"`
	Retry     *LLMRetryConfig        `json:"retry,omitempty"`
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
