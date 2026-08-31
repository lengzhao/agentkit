package llm_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/pluginkit/build"
)

func TestOpenAIResolvesAPIKeyRef(t *testing.T) {
	t.Setenv("AGENTKIT_OPENAI_KEY", "resolved-key")

	graph := map[string]any{
		"credentials": map[string]any{"use": "credentials/env"},
		"llm": map[string]any{
			"use": "llm/openai-compatible",
			"config": map[string]any{
				"apiKeyRef": "env:AGENTKIT_OPENAI_KEY",
			},
			"deps": map[string]any{
				"credentials": "credentials",
			},
		},
	}

	provider, _, err := build.Build[agentkit.LLMProvider](context.Background(), graph, "llm")
	if err != nil {
		t.Fatalf("build llm: %v", err)
	}

	_, err = provider.Stream(context.Background(), agentkit.LLMRequest{
		Messages: []agentkit.ModelMessage{{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}}}},
	})
	if err != nil && strings.Contains(err.Error(), "missing API key") {
		t.Fatalf("api key not resolved: %v", err)
	}
}

func TestOpenAIBuildFailsWhenAPIKeyRefMissing(t *testing.T) {
	graph := map[string]any{
		"credentials": map[string]any{"use": "credentials/env"},
		"llm": map[string]any{
			"use": "llm/openai-compatible",
			"config": map[string]any{
				"apiKeyRef": "env:AGENTKIT_MISSING_KEY",
			},
			"deps": map[string]any{
				"credentials": "credentials",
			},
		},
	}

	_, _, err := build.Build[agentkit.LLMProvider](context.Background(), graph, "llm")
	if err == nil {
		t.Fatal("expected build error for missing apiKeyRef")
	}
}

func TestOpenAIBuildUsesEnvFallbackWithoutAPIKeyRef(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fallback-key")

	graph := map[string]any{
		"llm": map[string]any{
			"use":    "llm/openai-compatible",
			"config": map[string]any{},
		},
	}

	provider, _, err := build.Build[agentkit.LLMProvider](context.Background(), graph, "llm")
	if err != nil {
		t.Fatalf("build llm: %v", err)
	}
	if provider == nil {
		t.Fatal("expected provider")
	}
}

func TestOpenAIBuildRejectsHostedToolsWithoutResponsesAPI(t *testing.T) {
	graph := map[string]any{
		"llm": map[string]any{
			"use": "llm/openai-compatible",
			"config": map[string]any{
				"api": "chat",
				"hostedTools": []map[string]any{
					{"type": "web_search"},
				},
			},
		},
	}

	_, _, err := build.Build[agentkit.LLMProvider](context.Background(), graph, "llm")
	if err == nil {
		t.Fatal("expected build error for hostedTools with chat api")
	}
}
