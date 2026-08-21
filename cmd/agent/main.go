package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/lengzhao/agentkit"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/pluginkit/build"
	"github.com/lengzhao/pluginkit/manager"
)

const defaultConfigPath = "presets/coding.yaml"

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	managerMode := flag.Bool("manager", false, "start plugin manager web UI")
	addr := flag.String("addr", ":8080", "manager HTTP listen address")
	configPath := flag.String("config", defaultConfigPath, "config YAML path")
	flag.Parse()

	if *managerMode {
		if err := manager.Run(manager.Options{
			Addr:          *addr,
			ValidateBuild: validateRunnerBuild,
		}); err != nil {
			fatal("manager", err)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	doc, err := loadDocument(*configPath)
	if err != nil {
		fatal("load config", err)
	}

	runner, _, err := build.Build[agentkit.Runner](ctx, doc.ToGraph(), doc.RootID)
	if err != nil {
		fatal("build runner", err)
	}
	if err := runner.Run(ctx); err != nil && ctx.Err() == nil {
		fatal("run agent", err)
	}
}

func loadDocument(path string) (manager.Document, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return manager.Document{}, err
	}
	return manager.FromYAML(raw)
}

func validateRunnerBuild(ctx context.Context, doc manager.Document) error {
	if doc.Plugin.Use != "runner" {
		return nil
	}
	_, _, err := build.Build[agentkit.Runner](ctx, doc.ToGraph(), doc.RootID)
	return err
}

func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
