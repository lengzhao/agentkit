package llm

import (
	"net/http"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

func newOpenAIClient(apiKey, baseURL string, responseHeaderTimeout time.Duration) *openai.Client {
	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = strings.TrimRight(baseURL, "/")
	if responseHeaderTimeout <= 0 {
		responseHeaderTimeout = defaultResponseHeaderTimeout
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Bound time until HTTP response headers (connect / TLS / gateway). Streaming body
	// and first model token are not capped here; see streamWithRequestTimeout (TTFB).
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	cfg.HTTPClient = &http.Client{
		Timeout:   0,
		Transport: transport,
	}
	return openai.NewClientWithConfig(cfg)
}

func parseAPIMode(api string) string {
	switch strings.ToLower(strings.TrimSpace(api)) {
	case "", "chat", "chat-completions", "chat_completions":
		return openAIAPIChat
	case "responses", "response":
		return openAIAPIResponses
	default:
		return strings.ToLower(strings.TrimSpace(api))
	}
}
