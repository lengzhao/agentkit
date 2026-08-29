package presettest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/config"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/agentkit/runtime/runner"
	"github.com/lengzhao/agentkit/testing/agenttest"
	"github.com/lengzhao/pluginkit/build"
	"github.com/lengzhao/pluginkit/manager"
)

// Load resolves L0 + overlays from the repo root.
func Load(t *testing.T, overlayPaths ...string) manager.Document {
	t.Helper()
	root := agenttest.RepoRoot(t)
	base := filepath.Join(root, config.DefaultBasePath)
	overlays := make([]string, 0, len(overlayPaths))
	for _, path := range overlayPaths {
		overlays = append(overlays, filepath.Join(root, path))
	}
	doc, err := config.LoadDocument(base, overlays...)
	if err != nil {
		t.Fatalf("load preset: %v", err)
	}
	return doc
}

// MustBuildRunner constructs a Runner from a resolved document.
func MustBuildRunner(t *testing.T, doc manager.Document) (agentkit.Runner, *build.Result) {
	t.Helper()
	graph := doc.ToGraph()
	prepareRunnableGraph(graph)
	runnerInst, result, err := build.Build[agentkit.Runner](context.Background(), graph, doc.RootID)
	if err != nil {
		t.Fatalf("build runner: %v", err)
	}
	return runnerInst, result
}

// RunOnceResult captures artifacts from a single preset once-run.
type RunOnceResult struct {
	Runner    agentkit.Runner
	Store     agentkit.SessionStore
	SessionID agentkit.SessionID
}

// RunOnce loads overlays, injects a CLI prompt, chdirs to repo root, and runs until exit.
func RunOnce(t *testing.T, prompt string, overlayPaths ...string) RunOnceResult {
	t.Helper()

	root := agenttest.RepoRoot(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	doc := Load(t, overlayPaths...)
	graph := doc.ToGraph()
	sessionID := injectIsolatedSession(graph, sanitizeTestName(t.Name()))
	injectOncePrompt(graph, prompt)
	prepareRunnableGraph(graph)

	runnerInst, result, err := build.Build[agentkit.Runner](context.Background(), graph, doc.RootID)
	if err != nil {
		t.Fatalf("build runner: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := runnerInst.Run(ctx, result); err != nil {
		t.Fatalf("runner: %v", err)
	}

	rootRunner, ok := runnerInst.(*runner.Root)
	if !ok {
		t.Fatalf("runner type = %T, want *runner.Root", runnerInst)
	}
	store := rootRunner.SessionStore()
	if store == nil {
		t.Fatal("runner session store is nil")
	}
	return RunOnceResult{Runner: runnerInst, Store: store, SessionID: sessionID}
}

func injectIsolatedSession(graph map[string]any, suffix string) agentkit.SessionID {
	id := agentkit.SessionID("cli:it-" + suffix)
	patchSessionDefault(graph, id)
	platform := resolvePlatformNode(graph)
	if platform != nil {
		cfg := asMap(platform["config"])
		cfg["defaultSessionId"] = string(id)
		platform["config"] = cfg
	}
	return id
}

func patchSessionDefault(graph map[string]any, id agentkit.SessionID) {
	node, ok := graph["session.default"].(map[string]any)
	if !ok {
		graph["session.default"] = map[string]any{
			"use":    "session/memory",
			"config": map[string]any{"id": string(id)},
		}
		return
	}
	cfg := asMap(node["config"])
	cfg["id"] = string(id)
	node["config"] = cfg
}

func sanitizeTestName(name string) string {
	replacer := strings.NewReplacer("/", "-", " ", "-", ":", "-")
	return replacer.Replace(name)
}

func injectOncePrompt(graph map[string]any, prompt string) {
	platform := resolvePlatformNode(graph)
	if platform == nil {
		panic("preset graph missing platform node")
	}
	cfg := asMap(platform["config"])
	cfg["prompt"] = prompt
	cfg["once"] = true
	platform["config"] = cfg
}

func prepareRunnableGraph(graph map[string]any) {
	if _, ok := graph["commands.default"]; !ok {
		graph["commands.default"] = map[string]any{"use": "commands/registry"}
	}
	platform := resolvePlatformNode(graph)
	if platform == nil {
		return
	}
	deps := asMap(platform["deps"])
	if _, ok := deps["commands"]; !ok {
		deps["commands"] = "commands.default"
	}
	if _, ok := deps["sessionStore"]; !ok {
		if _, has := graph["sessionStore.default"]; has {
			deps["sessionStore"] = "sessionStore.default"
		}
	}
	platform["deps"] = deps
}

func resolvePlatformNode(graph map[string]any) map[string]any {
	runnerNode, ok := graph["runner.default"].(map[string]any)
	if !ok {
		return nil
	}
	deps := asMap(runnerNode["deps"])
	platformRef := deps["platform"]
	switch ref := platformRef.(type) {
	case string:
		node, ok := graph[ref].(map[string]any)
		if !ok {
			panic(fmt.Sprintf("platform ref %q not found in graph", ref))
		}
		return node
	case map[string]any:
		return ref
	default:
		return nil
	}
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok && m != nil {
		return m
	}
	return map[string]any{}
}
