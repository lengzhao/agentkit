package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/config"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/pluginkit/build"
	"github.com/lengzhao/pluginkit/manager"
)

const defaultLogPath = "~/.agentkit/agent.log"

func main() {
	closeLog := setupLogging()
	defer closeLog()

	managerMode := flag.Bool("manager", false, "start plugin manager web UI")
	addr := flag.String("addr", ":8080", "manager HTTP listen address")
	basePath := flag.String("base", config.DefaultBasePath, "L0 base config YAML path")
	overlayPath := flag.String("config", config.DefaultOverlayPath, "L1 override YAML path(s), comma-separated; later files win")
	flag.Parse()

	if *managerMode {
		runManager(*addr, *basePath, *overlayPath)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	doc, err := config.LoadDocument(*basePath, config.SplitOverlayPaths(*overlayPath)...)
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

func runManager(addr, basePath, overlayPath string) {
	opts := manager.Options{
		Addr:          addr,
		ValidateBuild: validateRunnerBuild,
	}
	if merged, err := config.MergeFromFiles(basePath, config.SplitOverlayPaths(overlayPath)...); err == nil {
		opts.InitialYAML = string(merged)
		slog.Info("manager preloaded config", "base", basePath, "overlay", overlayPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		fatal("load config", err)
	}
	if err := manager.Run(opts); err != nil {
		fatal("manager", err)
	}
}

func validateRunnerBuild(ctx context.Context, doc manager.Document) error {
	if doc.Plugin.Use != "runner" {
		return nil
	}
	_, _, err := build.Build[agentkit.Runner](ctx, doc.ToGraph(), doc.RootID)
	return err
}

func setupLogging() func() {
	logPath, err := workspace.Resolve(defaultLogPath)
	if err != nil {
		slog.Error("resolve log path", "err", err)
		return func() {}
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		slog.Error("create log dir", "err", err)
		return func() {}
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		slog.Error("open log file", "err", err)
		return func() {}
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo})))
	slog.Info("logging to file", "path", logPath)
	return func() { _ = f.Close() }
}

func fatal(msg string, err error) {
	fmt.Fprintf(os.Stderr, "error: %s: %v\n", msg, err)
	slog.Error(msg, "err", err)
	os.Exit(1)
}
