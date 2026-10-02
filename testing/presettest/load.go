package presettest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

const smokeNoAPIKeyOverlay = "presets/smoke-no-api-key.yaml"

// Load resolves L0 + overlays from the repo root.
func Load(t *testing.T, overlayPaths ...string) manager.Document {
	t.Helper()
	root := agenttest.RepoRoot(t)
	base := filepath.Join(root, config.DefaultBasePath)
	overlays := make([]string, 0, len(overlayPaths)+1)
	overlays = append(overlays, filepath.Join(root, smokeNoAPIKeyOverlay))
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

// RunWorker loads a platform/worker preset, runs each task as one turn, then exits at EOF.
func RunWorker(t *testing.T, tasks []string, overlayPaths ...string) RunOnceResult {
	t.Helper()
	if len(tasks) == 0 {
		t.Fatal("RunWorker requires at least one task prompt")
	}
	return runPresetPlatform(t, overlayPaths, func(graph map[string]any, sessionID agentkit.SessionID) {
		injectWorkerTasks(graph, tasks)
	})
}

// RunOnce loads overlays, injects a CLI prompt, chdirs to repo root, and runs until exit.
func RunOnce(t *testing.T, prompt string, overlayPaths ...string) RunOnceResult {
	t.Helper()
	return runPresetPlatform(t, overlayPaths, func(graph map[string]any, sessionID agentkit.SessionID) {
		injectOncePrompt(graph, prompt)
	})
}

func runPresetPlatform(t *testing.T, overlayPaths []string, patch func(map[string]any, agentkit.SessionID)) RunOnceResult {
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
	injectWorkerPlatform(graph, sessionID)
	if patch != nil {
		patch(graph, sessionID)
	}
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

func injectWorkerTasks(graph map[string]any, tasks []string) {
	platform := resolvePlatformNode(graph)
	if platform == nil || platformUse(platform) != "platform/worker" {
		panic("injectWorkerTasks requires platform/worker in preset graph")
	}
	cfg := asMap(platform["config"])
	specs := make([]any, len(tasks))
	for i, p := range tasks {
		specs[i] = p
	}
	cfg["tasks"] = specs
	delete(cfg, "prompt")
	platform["config"] = cfg
}

func injectIsolatedSession(graph map[string]any, suffix string) agentkit.SessionID {
	// File-backed sessionStore persists under workspace; reuse a fixed id across
	// local reruns would accumulate events and break exact-count assertions.
	var nonce [4]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		panic(fmt.Sprintf("session id nonce: %v", err))
	}
	id := agentkit.SessionID("cli:it-" + suffix + "-" + hex.EncodeToString(nonce[:]))
	patchSessionDefault(graph, id)
	platform := resolvePlatformNode(graph)
	if platform != nil && platformUse(platform) == "platform/cli" {
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
	if platformUse(platform) != "platform/worker" {
		cfg["once"] = true
	}
	platform["config"] = cfg
}

// injectWorkerPlatform pins worker to a fixed session id that matches the
// isolated static session store used in integration tests.
func injectWorkerPlatform(graph map[string]any, sessionID agentkit.SessionID) {
	platform := resolvePlatformNode(graph)
	if platform == nil || platformUse(platform) != "platform/worker" {
		return
	}
	cfg := asMap(platform["config"])
	cfg["sessionMode"] = "fixed"
	cfg["sessionId"] = string(sessionID)
	platform["config"] = cfg
}

func platformUse(platform map[string]any) string {
	use, _ := platform["use"].(string)
	return use
}

func prepareRunnableGraph(graph map[string]any) {
	// Preset once-runs exit on platform EOF; background schedule runtimes would
	// otherwise keep receiveLoop alive until the test context times out.
	if runnerNode, ok := graph["runner.default"].(map[string]any); ok {
		deps := asMap(runnerNode["deps"])
		deps["schedules"] = []any{}
		// catalogCommands → loop.default pulls L0 agents + llm.router/deepseek into the build.
		delete(deps, "catalogCommands")
		delete(deps, "init")
		runnerNode["deps"] = deps
	}

	platform := resolvePlatformNode(graph)
	if platform != nil && platformUse(platform) == "platform/cli" {
		if _, ok := graph["commands.default"]; !ok {
			graph["commands.default"] = map[string]any{"use": "commands/registry"}
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
