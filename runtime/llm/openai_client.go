package llm

import (
	"net/http"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

func newOpenAIClient(apiKey, baseURL string) *openai.Client {
	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = strings.TrimRight(baseURL, "/")
	cfg.HTTPClient = &http.Client{Timeout: 10 * time.Minute}
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
