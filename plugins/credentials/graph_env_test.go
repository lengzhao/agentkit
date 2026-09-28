package credentials_test

import (
	"testing"

	"github.com/lengzhao/agentkit/plugins/credentials"
)

func TestEnvGraphSourceReadsConfigEnv(t *testing.T) {
	t.Parallel()

	lookup, err := credentials.EnvGraphSource(map[string]any{
		"credentials.env": map[string]any{
			"use": "credentials/env",
			"config": map[string]any{
				"env": map[string]any{
					"AGENTHUB_URL":     "https://agenthub.example.com",
					"AGENTHUB_API_KEY": "pag_test",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if lookup == nil {
		t.Fatal("lookup should not be nil")
	}
	got, ok := lookup("AGENTHUB_URL")
	if !ok || got != "https://agenthub.example.com" {
		t.Fatalf("AGENTHUB_URL=%q ok=%v", got, ok)
	}
}

func TestEnvGraphSourceSkipsMasterKey(t *testing.T) {
	t.Parallel()

	lookup, err := credentials.EnvGraphSource(map[string]any{
		"credentials.integrations": map[string]any{
			"use": "credentials/integrations",
			"config": map[string]any{
				"env": map[string]any{
					"AGENTKIT_SECRETS_KEY": "top-secret",
					"AGENTHUB_URL":         "https://agenthub.example.com",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if lookup == nil {
		t.Fatal("lookup should not be nil")
	}
	if _, ok := lookup("AGENTKIT_SECRETS_KEY"); ok {
		t.Fatal("master key must not be exported to the env graph")
	}
	if got, ok := lookup("AGENTHUB_URL"); !ok || got != "https://agenthub.example.com" {
		t.Fatalf("AGENTHUB_URL=%q ok=%v", got, ok)
	}
}

func TestEnvGraphSourceIgnoresOtherPlugins(t *testing.T) {
	t.Parallel()

	lookup, err := credentials.EnvGraphSource(map[string]any{
		"credentials.default": map[string]any{
			"use": "credentials/vault",
			"config": map[string]any{
				"env": map[string]any{
					"SECRET": "hidden",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if lookup != nil {
		if _, ok := lookup("SECRET"); ok {
			t.Fatal("non credentials/env instances should not contribute")
		}
	}
}
