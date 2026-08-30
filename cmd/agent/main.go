package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/config"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/pluginkit/build"
	"github.com/lengzhao/pluginkit/manager"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "scaffold" {
		runScaffold(os.Args[2:])
		return
	}

	managerMode := flag.Bool("manager", false, "start plugin manager web UI")
	addr := flag.String("addr", ":8080", "manager HTTP listen address")
	basePath := flag.String("base", config.DefaultBasePath, "L0 base config YAML path")
	overlayPath := flag.String("config", config.DefaultOverlayPath, "L1 override YAML path(s), comma-separated; later files win")
	logLevel := flag.String("log-level", envOr("AGENTKIT_LOG_LEVEL", "debug"), "log level: debug, info, warn, error")
	flag.Parse()
	initLogging(*logLevel)

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
	logConfigLoaded(*basePath, *overlayPath, doc)

	runner, result, err := build.Build[agentkit.Runner](ctx, doc.ToGraph(), doc.RootID)
	if err != nil {
		fatal("build runner", err)
	}
	logRunnerBuilt(result)
	slog.Info("runner starting")

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

func fatal(msg string, err error) {
	fmt.Fprintf(os.Stderr, "error: %s: %v\n", msg, err)
	slog.Error(msg, "err", err)
	os.Exit(1)
}
