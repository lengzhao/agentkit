package llm

import (
	"net/http"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

func newOpenAIClient(apiKey, baseURL string, requestTimeout time.Duration) *openai.Client {
	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = strings.TrimRight(baseURL, "/")
	if requestTimeout <= 0 {
		requestTimeout = defaultRequestTimeout
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Bound time to response headers (connect / HTTP handshake). Stream body length
	// is not capped here; TTFB for first model chunk is enforced in streamWithRequestTimeout.
	transport.ResponseHeaderTimeout = requestTimeout
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
