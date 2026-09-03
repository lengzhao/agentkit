package config_test

import (
	"testing"

	"github.com/lengzhao/agentkit/config"
)

func TestEnvContextProcessEnvWinsOverMap(t *testing.T) {
	t.Setenv("WINNER", "from-process")

	ctx, err := config.BuildEnvContext(nil, config.WithEnvLookup(config.MapEnvLookup(map[string]string{
		"WINNER": "from-map",
	})))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ctx.Lookup("WINNER")
	if !ok || got != "from-process" {
		t.Fatalf("WINNER=%q ok=%v", got, ok)
	}
}

func TestEnvContextGraphSource(t *testing.T) {
	t.Parallel()

	ctx, err := config.BuildEnvContext(map[string]any{}, config.WithGraphEnvSource(func(map[string]any) (config.EnvLookup, error) {
		return config.MapEnvLookup(map[string]string{"TOKEN": "from-graph"}), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ctx.Lookup("TOKEN")
	if !ok || got != "from-graph" {
		t.Fatalf("TOKEN=%q ok=%v", got, ok)
	}
}
