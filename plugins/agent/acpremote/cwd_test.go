package acpremote

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessctx"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

func TestCwdPeer(t *testing.T) {
	record := os.Getenv("AGENTKIT_CWD_RECORD")
	if record == "" {
		return
	}
	dec, enc := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	for {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if dec.Decode(&req) != nil {
			os.Exit(0)
		}
		cwd, _ := os.Getwd()
		f, err := os.OpenFile(record, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			os.Exit(2)
		}
		_ = json.NewEncoder(f).Encode(map[string]any{"method": req.Method, "cwd": cwd, "params": req.Params})
		_ = f.Close()
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}}
		case "session/new":
			result = map[string]any{"sessionId": "peer"}
		case "session/prompt":
			result = map[string]any{"stopReason": "end_turn"}
		default:
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32601, "message": "unsupported"}})
			continue
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
}

func TestDynamicCwdReplacesWarmProcessAndIsolatesReplay(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	aDir := filepath.Join(root, "a")
	bDir := filepath.Join(root, "b")
	for _, d := range []string{aDir, bDir} {
		if err := os.Mkdir(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	record := filepath.Join(root, "rpc.jsonl")
	exe, _ := os.Executable()
	ws := rtworkspace.Static(root)
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{ID: "test", Command: []string{exe, "-test.run=^TestCwdPeer$"}, Cwd: root, Env: map[string]string{"AGENTKIT_CWD_RECORD": record}}
	makeRuntime := func() *Runtime {
		ag, err := New(cfg, Deps{Workspace: ws, FS: bindTestFS(t, root), SessionStore: store, SessionEvents: sessevents.Default, Telemetry: mustToolkit(t)})
		if err != nil {
			t.Fatal(err)
		}
		a := ag.(*Runtime)
		t.Cleanup(func() { a.pool.Close() })
		return a
	}
	a := makeRuntime()
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("cwd-session", "test"), 15*time.Second)
	defer cancel()
	bind := store.(agentkit.SessionCwdStore)
	if err := bind.SetCwdBind(ctx, "cwd-session", aDir); err != nil {
		t.Fatal(err)
	}
	run := func(a *Runtime, text string) {
		t.Helper()
		err := a.RunTurn(ctx, agentkit.TurnInput{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: text}}}, Emit: func(context.Context, agentkit.OutboundEvent) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
	}
	run(a, "OLD_PROJECT_SECRET")
	if err := bind.SetCwdBind(ctx, "cwd-session", bDir); err != nil {
		t.Fatal(err)
	}
	run(a, "NEW_PROJECT_FIRST")
	a.pool.Close()
	// Cold restart must replay only the new project's history even without resume support.
	a = makeRuntime()
	run(a, "NEW_PROJECT_SECOND")
	raw, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	newCount := 0
	promptCount := 0
	for _, line := range lines {
		var r struct {
			Method string          `json:"method"`
			Cwd    string          `json:"cwd"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		if r.Method == "session/new" {
			newCount++
			var v struct {
				Cwd string `json:"cwd"`
			}
			_ = json.Unmarshal(r.Params, &v)
			if v.Cwd != r.Cwd {
				t.Fatalf("process and session cwd differ: %s", line)
			}
		}
		if r.Method == "session/prompt" {
			promptCount++
			if r.Cwd == bDir && strings.Contains(string(r.Params), "OLD_PROJECT_SECRET") {
				t.Fatalf("old history leaked: %s", line)
			}
		}
	}
	if newCount != 3 || promptCount != 4 {
		t.Fatalf("new=%d prompts=%d; RPCs:\n%s", newCount, promptCount, raw)
	}
}

func TestCwdChangeCannotReplaceReservedTurnProcess(t *testing.T) {
	a, _ := configTestRuntime(t, "test", "supported")
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("reserved", "test"), 10*time.Second)
	defer cancel()
	lease := acquireTestLease(t, a, "reserved")
	defer lease.Release()
	b := lease.Bridge()
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "reserved"})
	store := sessstore.NewStaticStore(mem)
	var err error
	ctx, err = sessctx.Prepare(ctx, store, a.workspace)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.ensureSession(ctx, "reserved", "test", store, false); err != nil {
		t.Fatal(err)
	}
	before := b.proc.Load()
	// RunTurn has reserved the bridge but has not installed its update emitter.
	b.setTurn(turnState{ctx: ctx})
	defer b.clearTurn()

	ctx = rctx.WithExecution(ctx, rctx.Execution{SessionID: "reserved", Cwd: t.TempDir()})
	if _, err := b.ensureSession(ctx, "reserved", "test", store, false); err == nil {
		t.Fatal("config command replaced reserved process")
	}
	if b.proc.Load() != before || !before.alive() {
		t.Fatal("active process was replaced")
	}
	b.clearTurn()
	if _, err := b.ensureSession(ctx, "reserved", "test", store, false); err != nil {
		t.Fatal(err)
	}
	if b.proc.Load() == before {
		t.Fatal("next request did not apply cwd change")
	}
}

func TestCwdUsesResolvedSessionForConfigCommands(t *testing.T) {
	a, _ := configTestRuntime(t, "test", "supported")
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("entry-before-new", "test"), 10*time.Second)
	defer cancel()
	lease := acquireTestLease(t, a, "active-after-new")
	defer lease.Release()
	b := lease.Bridge()
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "active-after-new"})
	store := sessstore.NewStaticStore(mem)
	a.sessionStore = store
	if err := store.SetActiveSession(ctx, "entry-before-new", "active-after-new"); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := store.SetCwdBind(ctx, "active-after-new", project); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Commands()[0].CommandExec(ctx, "model"); err != nil {
		t.Fatal(err)
	}
	if got := b.proc.Load().cwd; got != project {
		t.Fatalf("config used cwd %q, want active session cwd %q", got, project)
	}
}

func TestCwdConfigTransitionKeepsModelOnNextTurn(t *testing.T) {
	a, cmd := configTestRuntime(t, "test", "supported")
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "config-cwd"})
	store := sessstore.NewStaticStore(mem)
	a.sessionStore = store
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("config-cwd", "test"), 10*time.Second)
	defer cancel()
	_ = store.SetCwdBind(ctx, "config-cwd", t.TempDir())
	if _, err := cmd.CommandExec(ctx, "model"); err != nil {
		t.Fatal(err)
	}
	_ = store.SetCwdBind(ctx, "config-cwd", t.TempDir())
	if _, err := cmd.CommandExec(ctx, "model opus"); err != nil {
		t.Fatal(err)
	}
	lease := acquireTestLease(t, a, "config-cwd")
	defer lease.Release()
	b := lease.Bridge()
	before := b.proc.Load()
	next, err := sessctx.Prepare(ctx, store, a.workspace)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.ensureSession(next, "config-cwd", "test", store, true); err != nil {
		t.Fatal(err)
	}
	if b.proc.Load() != before {
		t.Fatal("next turn discarded newly configured remote session")
	}
	options, _ := before.configSnapshot()
	option := subcommandConfigOption(options, "model")
	if option == nil || option.CurrentValue != "opus" {
		t.Fatalf("model lost: %+v", option)
	}
}

func TestCwdConfigWaitsForTurnInitialization(t *testing.T) {
	a, cmd := configTestRuntime(t, "test", "supported")
	lease := acquireTestLease(t, a, "initializing")
	defer lease.Release()
	b := lease.Bridge()
	if err := b.contextMu.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer b.contextMu.Unlock()
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("initializing", "test"), 30*time.Millisecond)
	defer cancel()
	if _, err := cmd.CommandExec(ctx, "model"); err == nil {
		t.Fatal("configuration bypassed turn initialization lock")
	}
	if b.proc.Load() != nil {
		t.Fatal("configuration initialized process during reserved turn")
	}
}

func TestCwdPendingConfigurationDoesNotResetEarlierTurnSnapshot(t *testing.T) {
	a, cmd := configTestRuntime(t, "test", "replay")
	a.cfg.Env["AGENTKIT_CONFIG_PROMPTS"] = filepath.Join(t.TempDir(), "prompts.jsonl")
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "pending-cwd"})
	store := sessstore.NewStaticStore(mem)
	a.sessionStore = store
	a.sessionEvents = sessevents.Default
	base, cancel := context.WithTimeout(agenttest.TurnContext("pending-cwd", "test"), 10*time.Second)
	defer cancel()
	first := t.TempDir()
	second := t.TempDir()
	_ = store.SetCwdBind(base, "pending-cwd", first)
	stale, err := sessctx.Prepare(base, store, a.workspace)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.SetCwdBind(base, "pending-cwd", second)
	if _, err := cmd.CommandExec(base, "model opus"); err != nil {
		t.Fatal(err)
	}
	// A configuration command must not move the durable model-history boundary.
	events, _ := mem.Read(base, 0)
	var marker struct {
		Cwd string `json:"cwd"`
	}
	if err := json.Unmarshal(events[len(events)-1].Data, &marker); err != nil {
		t.Fatal(err)
	}
	if marker.Cwd != first {
		t.Fatalf("configuration reset active context to %q", marker.Cwd)
	}
	before := requireBridge(t, a, "pending-cwd").proc.Load()
	if err := a.RunTurn(rctx.WithoutSessionContext(stale), agentkit.TurnInput{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "new task"}}}, Emit: func(context.Context, agentkit.OutboundEvent) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	after := requireBridge(t, a, "pending-cwd").proc.Load()
	if before != after || after.cwd != second {
		t.Fatal("turn did not reuse pending configuration in the selected directory")
	}
}

func TestRemoteTurnPreservesPreparedCwdSnapshot(t *testing.T) {
	a, _ := configTestRuntime(t, "test", "replay")
	a.cfg.Env["AGENTKIT_CONFIG_PROMPTS"] = filepath.Join(t.TempDir(), "prompts.jsonl")
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "fixed-cwd"})
	store := sessstore.NewStaticStore(mem)
	a.sessionStore = store
	a.sessionEvents = sessevents.Default
	base, cancel := context.WithTimeout(agenttest.TurnContext("fixed-cwd", "test"), 10*time.Second)
	defer cancel()
	first, second := t.TempDir(), t.TempDir()
	if err := store.SetCwdBind(base, "fixed-cwd", first); err != nil {
		t.Fatal(err)
	}
	fixed, err := sessctx.Prepare(base, store, a.workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetCwdBind(base, "fixed-cwd", second); err != nil {
		t.Fatal(err)
	}
	if err = a.RunTurn(fixed, agentkit.TurnInput{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "read the attachment"}}}, Emit: func(context.Context, agentkit.OutboundEvent) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	if got := requireBridge(t, a, "fixed-cwd").proc.Load().cwd; got != first {
		t.Fatalf("remote cwd = %s, want prepared %s", got, first)
	}
}
