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

const defaultConfigPath = "./config.yaml"

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	managerMode := flag.Bool("manager", false, "start plugin manager web UI")
	addr := flag.String("addr", ":8080", "manager HTTP listen address")
	configPath := flag.String("config", defaultConfigPath, "config YAML path")
	flag.Parse()

	configExplicit := flagPassed("config")

	if *managerMode {
		runManager(*addr, *configPath, configExplicit)
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

func runManager(addr, configPath string, saveOnChange bool) {
	initialYAML, err := readConfigYAML(configPath)
	if err != nil {
		fatal("load config", err)
	}

	opts := manager.Options{
		Addr:          addr,
		InitialYAML:   initialYAML,
		ValidateBuild: validateRunnerBuild,
		OnChange: func(ctx context.Context, evt manager.DocumentEvent) error {
			slog.Info("config changed",
				"reason", evt.Reason,
				"operation", evt.Operation,
				"rootId", evt.Document.RootID,
				"errors", countDiagnosticErrors(evt.Diagnostics),
			)
			if saveOnChange {
				return saveConfigYAML(configPath, evt.YAML)
			}
			return nil
		},
		OnBuild: func(ctx context.Context, evt manager.DocumentEvent) error {
			errs := countDiagnosticErrors(evt.Diagnostics)
			if errs > 0 {
				slog.Warn("build has errors", "count", errs, "rootId", evt.Document.RootID)
				return nil
			}
			slog.Info("build ok",
				"rootId", evt.Document.RootID,
				"kind", evt.Document.Plugin.Use,
			)
			return saveConfigYAML(configPath, evt.YAML)
		},
	}
	if err := manager.Run(opts); err != nil {
		fatal("manager", err)
	}
}

func loadDocument(path string) (manager.Document, error) {
	raw, err := readConfigYAML(path)
	if err != nil {
		return manager.Document{}, err
	}
	return manager.FromYAML([]byte(raw))
}

func readConfigYAML(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func saveConfigYAML(path, yaml string) error {
	if yaml == "" {
		return nil
	}
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		return err
	}
	slog.Info("config saved", "path", path)
	return nil
}

func validateRunnerBuild(ctx context.Context, doc manager.Document) error {
	if doc.Plugin.Use != "runner" {
		return nil
	}
	_, _, err := build.Build[agentkit.Runner](ctx, doc.ToGraph(), doc.RootID)
	return err
}

func countDiagnosticErrors(diags []manager.Diagnostic) int {
	n := 0
	for _, d := range diags {
		if d.Severity == "error" {
			n++
		}
	}
	return n
}

func flagPassed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
