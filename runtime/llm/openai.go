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
	// HostedTools are OpenAI Responses API built-in tools (e.g. web_search) executed
	// server-side. Requires api: responses.
	HostedTools []HostedToolConfig `json:"hostedTools,omitempty"`
	// Reasoning is reasoning effort and summary settings, for models that support them.
	Reasoning *OpenAIReasoningConfig `json:"reasoning,omitempty"`
	// Retry is provider-level retry, separate from the agent's per-step retry.
	Retry *LLMRetryConfig `json:"retry,omitempty"`
}

type HostedToolConfig struct {
	// Type is the Responses API tool type, e.g. web_search or file_search.
	Type string `json:"type"`
	// Parameters are tool-specific options, e.g. search_context_size for web_search.
	Parameters map[string]any `json:"parameters,omitempty"`
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
	hostedTools   []HostedToolConfig
	reasoning     *OpenAIReasoningConfig
	providerRetry ProviderRetrySettings
	apiKey        string
	client        *openai.Client
}

// NewOpenAI registers llm/openai-compatible: OpenAI-compatible provider, chat or responses API.
//
// Best practices:
//   - hostedTools (e.g. web_search) require api: responses and run on the provider side.
//   - When using hosted web_search, remove tool/web-search-* from the agent tool list to avoid duplicate search.
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
	api := parseAPIMode(cfg.API)
	if len(cfg.HostedTools) > 0 && api != openAIAPIResponses {
		return nil, fmt.Errorf("llm/openai-compatible: hostedTools requires api: responses")
	}
	return &OpenAI{
		model:         model,
		api:           api,
		hostedTools:   cfg.HostedTools,
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
