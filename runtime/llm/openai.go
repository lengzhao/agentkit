package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
	capllm "github.com/lengzhao/agentkit/cap/llm"
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
	// TimeoutSeconds is max wait for the first model event on a stream (TTFB). 0 uses 180s.
	// Later stream tokens are uncapped. HTTP connect/headers use ResponseHeaderTimeoutSeconds.
	TimeoutSeconds int `json:"timeoutSeconds"`
	// ResponseHeaderTimeoutSeconds caps connect + TLS + HTTP response headers. 0 uses 60s.
	// Independent of timeoutSeconds (headers often return before the first model token).
	ResponseHeaderTimeoutSeconds int `json:"responseHeaderTimeoutSeconds"`
	// Modalities lists supported user input modalities: text, image, audio.
	// Empty defaults to text and image. Use [text] for models that reject vision input.
	// Used as default for config.models[] entries that omit modalities.
	Modalities []string `json:"modalities,omitempty"`
	// Models lists routable model ids for llm/router (optional).
	Models []ModelCatalogEntry `json:"models,omitempty"`
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
	providerName   string
	model          string
	api            string
	hostedTools    []HostedToolConfig
	reasoning      *OpenAIReasoningConfig
	providerRetry  ProviderRetrySettings
	requestTimeout time.Duration
	modalities     []string
	modelModalities map[string][]string
	catalogEntries  []capllm.ModelEntry
	apiKey         string
	client         *openai.Client
}

// NewOpenAI registers llm/openai-compatible: OpenAI-compatible provider, chat or responses API.
//
// Best practices:
//   - hostedTools (e.g. web_search) require api: responses and run on the provider side.
//   - When using hosted web_search, remove tool/web-search-* from the agent tool list to avoid duplicate search.
func NewOpenAI(cfg OpenAIConfig, deps OpenAIDeps) (agentkit.LLMProvider, error) {
	return newOpenAIProvider(cfg, deps, openAIConfig{
		providerName: "openai-compatible",
		api:          parseAPIMode(cfg.API),
	})
}

// NewOpenAIChat registers llm/openai-chat: OpenAI Chat Completions API.
func NewOpenAIChat(cfg OpenAIConfig, deps OpenAIDeps) (agentkit.LLMProvider, error) {
	return newOpenAIProvider(cfg, deps, openAIConfig{
		providerName: "openai-chat",
		api:          openAIAPIChat,
	})
}

// NewOpenAIResponses registers llm/openai-responses: OpenAI Responses API.
func NewOpenAIResponses(cfg OpenAIConfig, deps OpenAIDeps) (agentkit.LLMProvider, error) {
	return newOpenAIProvider(cfg, deps, openAIConfig{
		providerName: "openai-responses",
		api:          openAIAPIResponses,
	})
}

type openAIConfig struct {
	providerName string
	api          string
}

func newOpenAIProvider(cfg OpenAIConfig, deps OpenAIDeps, fixed openAIConfig) (agentkit.LLMProvider, error) {
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
	api := fixed.api
	if api == "" {
		api = parseAPIMode(cfg.API)
	}
	if len(cfg.HostedTools) > 0 && api != openAIAPIResponses {
		return nil, fmt.Errorf("%s: hostedTools requires Responses API", fixed.providerName)
	}
	requestTimeout := resolveRequestTimeout(cfg.TimeoutSeconds)
	headerTimeout := resolveResponseHeaderTimeout(cfg.ResponseHeaderTimeoutSeconds)
	instanceMods := agentkit.NormalizeModalities(cfg.Modalities)
	byID, entries, err := catalogFromConfig(cfg.Models, instanceMods)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", fixed.providerName, err)
	}
	return &OpenAI{
		providerName:    fixed.providerName,
		model:           model,
		api:             api,
		hostedTools:     cfg.HostedTools,
		reasoning:       cfg.Reasoning,
		providerRetry:   defaultProviderRetry(retryProviderConfig(cfg.Retry)),
		requestTimeout:  requestTimeout,
		modalities:      instanceMods,
		modelModalities: byID,
		catalogEntries:  entries,
		apiKey:          apiKey,
		client:          newOpenAIClient(apiKey, baseURL, headerTimeout),
	}, nil
}

func retryProviderConfig(cfg *LLMRetryConfig) *ProviderRetrySettings {
	if cfg == nil {
		return nil
	}
	return cfg.Provider
}

func (p *OpenAI) Name() string {
	if p.providerName != "" {
		return p.providerName
	}
	return "openai-compatible"
}

func (p *OpenAI) Modalities() []string { return append([]string(nil), p.modalities...) }

func (p *OpenAI) ModalitiesForModel(model string) []string {
	return modalitiesForCatalog(p.modelModalities, p.modalities, model)
}

func (p *OpenAI) CatalogModels() []capllm.ModelEntry {
	if len(p.catalogEntries) == 0 {
		return nil
	}
	out := make([]capllm.ModelEntry, len(p.catalogEntries))
	copy(out, p.catalogEntries)
	return out
}

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
	stream, err := backend.stream(ctx, model, req)
	if err != nil {
		return nil, err
	}
	return wrapStreamTTFB(ctx, p.requestTimeout, stream), nil
}
