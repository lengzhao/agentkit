package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/lengzhao/agentkit"
	_ "github.com/lengzhao/agentkit/plugins/all"
	"github.com/lengzhao/pluginkit/build"
	"gopkg.in/yaml.v3"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	configPath := "config.yaml"
	if len(os.Args) > 1 && !looksLikePrompt(os.Args[1]) {
		configPath = os.Args[1]
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		fatal("read config", err)
	}
	var graph map[string]any
	if err := yaml.Unmarshal(raw, &graph); err != nil {
		fatal("parse config", err)
	}

	runner, _, err := build.Build[agentkit.Runner](ctx, graph, "runner")
	if err != nil {
		fatal("build runner", err)
	}
	if err := runner.Run(ctx); err != nil && ctx.Err() == nil {
		fatal("run agent", err)
	}
}

func looksLikePrompt(arg string) bool {
	return len(arg) > 0 && arg[0] != '-' && !stringsHasSuffix(arg, ".yaml") && !stringsHasSuffix(arg, ".yml")
}

func stringsHasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
