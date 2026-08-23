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

const (
	defaultConfigPath = "./config.yaml"
	defaultLogPath    = ".agent/agent.log"
)

func main() {
	closeLog := setupLogging()
	defer closeLog()

	managerMode := flag.Bool("manager", false, "start plugin manager web UI")
	addr := flag.String("addr", ":8080", "manager HTTP listen address")
	configPath := flag.String("config", defaultConfigPath, "config YAML path")
	flag.Parse()

	if *managerMode {
		runManager(*addr, *configPath)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	doc, err := loadDocument(*configPath)
	if err != nil {
		fatal("load config", err)
	}

	runner, result, err := build.Build[agentkit.Runner](ctx, doc.ToGraph(), doc.RootID)
	if err != nil {
		fatal("build runner", err)
	}
	if err := runner.Run(ctx, result); err != nil && ctx.Err() == nil {
		fatal("run agent", err)
	}
}

func runManager(addr, configPath string) {
	opts := manager.Options{
		Addr:          addr,
		ValidateBuild: validateRunnerBuild,
	}
	if raw, err := os.ReadFile(configPath); err == nil {
		opts.InitialYAML = string(raw)
		slog.Info("manager preloaded config", "path", configPath)
	} else if !os.IsNotExist(err) {
		fatal("read config", err)
	}
	if err := manager.Run(opts); err != nil {
		fatal("manager", err)
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

func setupLogging() func() {
	if err := os.MkdirAll(".agent", 0o755); err != nil {
		slog.Error("create log dir", "err", err)
		return func() {}
	}
	f, err := os.OpenFile(defaultLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		slog.Error("open log file", "err", err)
		return func() {}
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo})))
	slog.Info("logging to file", "path", defaultLogPath)
	return func() { _ = f.Close() }
}

func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
