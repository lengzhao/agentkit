package acpremote

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/acpclient"
	"github.com/lengzhao/agentkit/runtime/command"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

const configTestOptions = `[
 {"id":"llm","name":"Model","category":"model","type":"select","currentValue":"sonnet","options":[{"group":"models","name":"Models","options":[{"value":"sonnet","name":"Sonnet"},{"value":"opus","name":"Opus"}]}]},
 {"id":"effort","name":"Effort","category":"thought_level","type":"select","currentValue":"low","options":[{"value":"low","name":"Low"},{"value":"high","name":"High"},{"value":"rejected","name":"Rejected"}]},
 {"id":"fast-mode","name":"Fast mode","category":"model_config","type":"boolean","currentValue":false},
 {"id":"session-mode","name":"Mode","category":"mode","type":"select","currentValue":"agent","options":[{"value":"agent","name":"Agent"},{"value":"plan","name":"Plan"}]}
]`

// A real stdio peer verifies session creation and typed config RPCs, without
// invoking any installed vendor agent or changing its settings.
func TestACPConfigHelper(t *testing.T) {
	mode := os.Getenv("AGENTKIT_CONFIG_TEST")
	if mode == "" {
		return
	}
	options := modelOptions(t, configTestOptions)
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	for {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Session string             `json:"sessionId"`
				ID      string             `json:"configId"`
				Value   json.RawMessage    `json:"value"`
				Type    string             `json:"type"`
				Prompt  []acp.ContentBlock `json:"prompt"`
			} `json:"params"`
		}
		if decoder.Decode(&req) != nil {
			os.Exit(0)
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}}
		case "session/new", "session/resume", "session/load":
			if mode == "slow" {
				time.Sleep(10 * time.Second)
			}
			result = map[string]any{"sessionId": "remote", "configOptions": options}
			if mode == "unsupported" {
				result = map[string]any{"sessionId": "remote"}
			}
		case "session/set_config_option":
			if req.Params.Session != "remote" {
				os.Exit(2)
			}
			if string(req.Params.Value) == `"rejected"` {
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32602, "message": "remote rejected value"}})
				continue
			}
			for i := range options {
				if s := options[i].Select; s != nil && string(s.Id) == req.Params.ID {
					if json.Unmarshal(req.Params.Value, &s.CurrentValue) != nil {
						os.Exit(3)
					}
					// Model changes can also change the available config state.
					if req.Params.ID == "llm" {
						options[1].Select.CurrentValue = "high"
					}
				}
				if b := options[i].Boolean; b != nil && string(b.Id) == req.Params.ID {
					if req.Params.Type != "boolean" || json.Unmarshal(req.Params.Value, &b.CurrentValue) != nil {
						os.Exit(4)
					}
				}
			}
			result = map[string]any{"configOptions": options}
		case "session/prompt":
			if mode != "replay" {
				os.Exit(5)
			}
			f, err := os.OpenFile(os.Getenv("AGENTKIT_CONFIG_PROMPTS"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			if err != nil {
				os.Exit(6)
			}
			_ = json.NewEncoder(f).Encode(req.Params.Prompt)
			_ = f.Close()
			result = map[string]any{"stopReason": "end_turn"}
		default:
			os.Exit(5) // In particular, a slash must never send session/prompt.
		}
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
}

func configTestRuntime(t *testing.T, id, mode string) (*Runtime, agentkit.Command) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cfg := Config{ID: agentkit.AgentID(id), Command: []string{exe, "-test.run=^TestACPConfigHelper$"}, Cwd: dir, Env: map[string]string{"AGENTKIT_CONFIG_TEST": mode}}
	ag, err := New(cfg, Deps{Workspace: &stubWorkspace{}, FS: bindTestFS(t, dir), Telemetry: mustToolkit(t)})
	if err != nil {
		t.Fatal(err)
	}
	a := ag.(*Runtime)
	t.Cleanup(func() {
		a.bridgesMu.Lock()
		defer a.bridgesMu.Unlock()
		for _, b := range a.bridges {
			b.releaseSubprocess()
		}
		if a.configBridge != nil {
			a.configBridge.releaseSubprocess()
		}
	})
	p, ok := ag.(agentkit.CommandProvider)
	if !ok {
		t.Fatal("ACP runtime must contribute per-agent slash commands")
	}
	cmds := p.Commands()
	if len(cmds) != 1 || cmds[0].Name() != "acp-"+strings.ToLower(id) || cmds[0].Alias() != "" {
		t.Fatalf("commands = %v", cmds)
	}
	return a, cmds[0]
}

func TestACPConfigCommand(t *testing.T) {
	_, cmd := configTestRuntime(t, "claude", "supported")
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("config-test", "claude"), 10*time.Second)
	defer cancel()
	run := func(args string, wants ...string) string {
		t.Helper()
		out, err := cmd.CommandExec(ctx, args)
		if err != nil {
			t.Fatalf("%s: %v", args, err)
		}
		for _, want := range wants {
			if !strings.Contains(out, want) {
				t.Fatalf("%s: missing %q in %s", args, want, out)
			}
		}
		return out
	}
	run("show", "sonnet", "opus", "default: (not provided)", "initial: sonnet", "/acp-claude\n", "/acp-claude model opus", "/acp-claude config effort high")
	out := run("model opus", "current: opus", "initial: sonnet")
	if strings.Contains(out, "\neffort\n") || strings.Contains(out, "\nfast-mode\n") {
		t.Fatalf("model change displayed unrelated options: %s", out)
	}
	run("effort", "current: high")
	run("config fast-mode true", "current: true", "initial: false")
	run("config show fast-mode", "current: true", "true | false")
	for _, args := range []string{"config missing x", "config fast-mode yes", "model missing", "config effort rejected", "config effort high extra", "unknown", "model show extra"} {
		if out, err := cmd.CommandExec(ctx, args); err == nil {
			t.Fatalf("%s unexpectedly succeeded: %s", args, out)
		}
	}
	run("config show effort", "current: high")
}

func TestACPConfigShowDoesNotReplayHistory(t *testing.T) {
	a, cmd := configTestRuntime(t, "claude", "supported")
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "history"})
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"user", "assistant"} {
		typ := agentkit.EventUserMessage
		if role == "assistant" {
			typ = agentkit.EventAssistantMessage
		}
		if err := mustEvents(t).AppendMessage(t.Context(), mem, "claude", typ, agentkit.ModelMessage{Role: role, Content: []agentkit.ContentPart{{Type: "text", Text: "previous message"}}}); err != nil {
			t.Fatal(err)
		}
	}
	a.sessionStore = &steeringTestStore{sess: mem}
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("history", "claude"), 5*time.Second)
	defer cancel()
	out, err := cmd.CommandExec(ctx, "show")
	if err != nil || !strings.Contains(out, "sonnet") {
		t.Fatalf("show sent a history prompt: %s %v", out, err)
	}
}

func TestACPConfigUnsupported(t *testing.T) {
	_, cmd := configTestRuntime(t, "cursor", "unsupported")
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("unsupported", "cursor"), 5*time.Second)
	defer cancel()
	out, err := cmd.CommandExec(ctx, "show")
	if err != nil || !strings.Contains(out, "does not expose") {
		t.Fatalf("show: %s %v", out, err)
	}
	if _, err := cmd.CommandExec(ctx, "model auto"); err == nil {
		t.Fatal("unsupported model change succeeded")
	}
}

func TestACPConfigAgentNames(t *testing.T) {
	for _, id := range []string{"claude.team", "Claude-Team"} {
		configTestRuntime(t, id, "supported")
	}
	a, _ := configTestRuntime(t, "claude", "supported")
	b, _ := configTestRuntime(t, "codex", "supported")
	if _, err := command.NewFromProviders(command.Config{}, []agentkit.CommandProvider{any(a).(agentkit.CommandProvider), any(b).(agentkit.CommandProvider)}); err != nil {
		t.Fatal(err)
	}
	if _, err := command.NewFromProviders(command.Config{}, []agentkit.CommandProvider{any(a).(agentkit.CommandProvider), any(a).(agentkit.CommandProvider)}); err == nil {
		t.Fatal("duplicate agent must fail registration")
	}
}

func TestACPCommandNameUsesAgentID(t *testing.T) {
	a, _ := configTestRuntime(t, "claude", "supported")
	// The existing agent ID is the sole source of the slash command name.
	a.id = "codex"
	a.cfg.ID = a.id
	if got := a.Commands()[0].Name(); got != "acp-codex" {
		t.Fatalf("command name = %q, want acp-codex", got)
	}
}

func TestACPConfigIdleUpdatesAndIsolation(t *testing.T) {
	a, cmd := configTestRuntime(t, "claude", "supported")
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("first", "claude"), 10*time.Second)
	defer cancel()
	if _, err := cmd.CommandExec(ctx, "model opus"); err != nil {
		t.Fatal(err)
	}
	b := a.bridgeFor("first")
	proc := b.proc.Load()
	update := acp.SessionNotification{SessionId: "remote", Update: acp.SessionUpdate{ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{ConfigOptions: modelOptions(t, configTestOptions)}}}
	// Idle notifications still update state when there is no thinking emitter.
	if err := proc.client.SessionUpdate(ctx, update); err != nil {
		t.Fatal(err)
	}
	out, err := cmd.CommandExec(ctx, "show")
	if err != nil || !strings.Contains(out, "current: sonnet") {
		t.Fatalf("idle update lost: %s %v", out, err)
	}
	update.SessionId = "unrelated-session"
	update.Update.ConfigOptionUpdate.ConfigOptions = modelOptions(t, `[{"id":"llm","name":"Model","type":"select","category":"model","currentValue":"wrong","options":[]}]`)
	if err := proc.client.SessionUpdate(ctx, update); err != nil {
		t.Fatal(err)
	}
	out, err = cmd.CommandExec(ctx, "model show")
	if err != nil || strings.Contains(out, "wrong") {
		t.Fatalf("unrelated notification leaked: %s %v", out, err)
	}
	other := agenttest.TurnContext("second", "claude")
	other, otherCancel := context.WithTimeout(other, 5*time.Second)
	defer otherCancel()
	if _, err := cmd.CommandExec(ctx, "model opus"); err != nil {
		t.Fatal(err)
	}
	out, err = cmd.CommandExec(other, "show")
	if err != nil || !strings.Contains(out, "current: sonnet") {
		t.Fatalf("session override leaked: %s %v", out, err)
	}
}

func TestACPConfigRestoreState(t *testing.T) {
	for _, strategy := range []acpclient.RestoreStrategy{acpclient.StrategyResume, acpclient.StrategyLoad} {
		t.Run(string(strategy), func(t *testing.T) {
			a, cmd := configTestRuntime(t, "claude", "supported")
			a.sessionRestore = acpclient.NewRestorer(strategy)
			ctx, cancel := context.WithTimeout(agenttest.TurnContext("restore", "claude"), 10*time.Second)
			defer cancel()
			if _, err := cmd.CommandExec(ctx, "model opus"); err != nil {
				t.Fatal(err)
			}
			a.bridgeFor("restore").releaseSubprocess()
			out, err := cmd.CommandExec(ctx, "config show")
			if err != nil || !strings.Contains(out, "current: sonnet") || !strings.Contains(out, "fast-mode") {
				t.Fatalf("restore options lost: %s %v", out, err)
			}
		})
	}
}

func TestACPConfigSurvivesSlashRequestCancellation(t *testing.T) {
	a, cmd := configTestRuntime(t, "claude", "supported")
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("request", "claude"), 5*time.Second)
	if _, err := cmd.CommandExec(ctx, "model opus"); err != nil {
		cancel()
		t.Fatal(err)
	}
	proc := a.bridgeFor("request").proc.Load()
	cancel() // HTTP request has finished; this is not a session shutdown.
	select {
	case <-proc.done:
		t.Fatal("ending slash request killed the configured ACP session")
	case <-time.After(100 * time.Millisecond):
	}
	ctx2, cancel2 := context.WithTimeout(agenttest.TurnContext("request", "claude"), 5*time.Second)
	defer cancel2()
	out, err := cmd.CommandExec(ctx2, "show")
	if err != nil || !strings.Contains(out, "current: opus") {
		t.Fatalf("configuration lost after HTTP request: %s %v", out, err)
	}
	shutdown, ok := any(a).(agentkit.ShutdownHookProvider)
	if !ok {
		t.Fatal("runtime must clean up slash-owned processes on shutdown")
	}
	for _, hook := range shutdown.ShutdownHooks() {
		hook()
	}
	select {
	case <-proc.done:
	case <-time.After(3 * time.Second):
		t.Fatal("ACP process survived runtime shutdown")
	}
}

func TestACPConfigDeferredHistoryReplayedOnNextTurn(t *testing.T) {
	a, cmd := configTestRuntime(t, "claude", "replay")
	marker := filepath.Join(t.TempDir(), "prompts.jsonl")
	a.cfg.Env["AGENTKIT_CONFIG_PROMPTS"] = marker
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "replay"})
	if err != nil {
		t.Fatal(err)
	}
	a.sessionEvents = mustEvents(t)
	a.sessionStore = &steeringTestStore{sess: mem}
	for _, role := range []string{"user", "assistant"} {
		typ := agentkit.EventUserMessage
		if role == "assistant" {
			typ = agentkit.EventAssistantMessage
		}
		if err := a.sessionEvents.AppendMessage(t.Context(), mem, a.id, typ, agentkit.ModelMessage{Role: role, Content: []agentkit.ContentPart{{Type: "text", Text: "previous " + role}}}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("replay", "claude"), 5*time.Second)
	defer cancel()
	if _, err := cmd.CommandExec(ctx, "model opus"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("slash unexpectedly sent a prompt")
	}
	if err := a.RunTurn(ctx, agentkit.TurnInput{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "next turn"}}}, Emit: func(context.Context, agentkit.OutboundEvent) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "previous user") || !strings.Contains(lines[0], "previous assistant") || strings.Contains(lines[0], "next turn") || !strings.Contains(lines[1], "next turn") {
		t.Fatalf("incorrect history/turn prompts: %s", raw)
	}
}

func TestACPConfigTurnDoesNotEmitModel(t *testing.T) {
	a, _ := configTestRuntime(t, "claude", "replay")
	a.cfg.Env["AGENTKIT_CONFIG_PROMPTS"] = filepath.Join(t.TempDir(), "prompts.jsonl")
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("no-model", "claude"), 5*time.Second)
	defer cancel()
	if err := a.RunTurn(ctx, agentkit.TurnInput{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hello"}}}, Emit: func(_ context.Context, event agentkit.OutboundEvent) error {
		if strings.Contains(string(event.Data), "[model:") {
			t.Error("turn emitted model metadata")
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
}

func TestParseACPCommand(t *testing.T) {
	for _, tc := range []struct {
		args string
		want acpCommandArgs
	}{
		{"", acpCommandArgs{action: "navigation"}},
		{"  show  ", acpCommandArgs{action: "show"}},
		{"model", acpCommandArgs{action: "show", target: "model"}},
		{"model show", acpCommandArgs{action: "show", target: "model"}},
		{"model opus", acpCommandArgs{action: "set", target: "model", value: "opus"}},
		{"model -g", acpCommandArgs{global: true, action: "show", target: "model"}},
		{"model -g opus", acpCommandArgs{global: true, action: "set", target: "model", value: "opus"}},
		{"model reset", acpCommandArgs{action: "reset", target: "model"}},
		{"model -g reset", acpCommandArgs{global: true, action: "reset", target: "model"}},
		{"config -g reset", acpCommandArgs{global: true, action: "reset"}},
		{"config -g old-option reset", acpCommandArgs{global: true, action: "reset", id: "old-option"}},
		{"mode --global plan", acpCommandArgs{global: true, action: "set", target: "mode", value: "plan"}},
		{"config -g show", acpCommandArgs{global: true, action: "show"}},
		{"config -g show effort", acpCommandArgs{global: true, action: "show", id: "effort"}},
		{"config -g fast-mode false", acpCommandArgs{global: true, action: "set", id: "fast-mode", value: "false"}},
		{"mode", acpCommandArgs{action: "show", target: "mode"}},
		{"mode plan", acpCommandArgs{action: "set", target: "mode", value: "plan"}},
		{"effort show", acpCommandArgs{action: "show", target: "effort"}},
		{"effort high", acpCommandArgs{action: "set", target: "effort", value: "high"}},
		{"config", acpCommandArgs{action: "show"}},
		{"config show", acpCommandArgs{action: "show"}},
		{"config show effort", acpCommandArgs{action: "show", id: "effort"}},
		{"config effort", acpCommandArgs{action: "show", id: "effort"}},
		{"config effort high", acpCommandArgs{action: "set", id: "effort", value: "high"}},
		{"config fast-mode false", acpCommandArgs{action: "set", id: "fast-mode", value: "false"}},
		{"config model default", acpCommandArgs{action: "set", id: "model", value: "default"}},
	} {
		t.Run(tc.args, func(t *testing.T) {
			got, err := parseACPCommand(tc.args)
			if err != nil || got != tc.want {
				t.Fatalf("parse(%q) = %+v, %v; want %+v", tc.args, got, err, tc.want)
			}
		})
	}
	for _, args := range []string{"config reset", "help", "--help", "-h", "unknown", "show extra", "help extra", "--help extra", "model show extra", "config show effort extra", "config effort high extra"} {
		t.Run(args, func(t *testing.T) {
			if got, err := parseACPCommand(args); err == nil {
				t.Fatalf("parse(%q) succeeded: %+v", args, got)
			}
		})
	}
}

func TestACPConfigMalformedCommandDoesNotConnect(t *testing.T) {
	a, cmd := configTestRuntime(t, "claude", "supported")
	for _, args := range []string{"show extra", "model show extra", "config effort high extra", "unknown"} {
		if _, err := cmd.CommandExec(context.Background(), args); err == nil {
			t.Fatalf("%q succeeded", args)
		}
	}
	if len(a.bridges) != 0 {
		t.Fatal("malformed syntax initialized a bridge")
	}
}

func TestACPConfigShowFiltersOptions(t *testing.T) {
	c := acpCommand{runtime: &Runtime{id: "claude"}}
	options := modelOptions(t, configTestOptions)
	out := c.show(options, map[string]string{"llm": "sonnet", "effort": "low", "fast-mode": "false"}, "effort")
	for _, want := range []string{"\neffort\n  current: low\n  initial: low\n  default: (not provided)\n", "low (Low)", "high (High)", "example: /acp-claude config effort high"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %s", want, out)
		}
	}
	for _, excluded := range []string{"\nllm\n", "\nfast-mode\n"} {
		if strings.Contains(out, excluded) {
			t.Fatalf("filtered output contains %q: %s", excluded, out)
		}
	}
	if strings.Contains(out, c.navigation()) || strings.Contains(out, "model: sonnet") {
		t.Fatal("filtered result included global help or unrelated model summary")
	}
}

func TestACPConfigBareCommandIsNavigation(t *testing.T) {
	a, cmd := configTestRuntime(t, "claude", "supported")
	out, err := cmd.CommandExec(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"mode", "model", "effort", "config"} {
		if !strings.Contains(out, "/acp-claude "+sub) {
			t.Fatalf("missing %s navigation in %s", sub, out)
		}
	}
	if strings.Contains(out, "available:") || strings.Contains(out, "initial:") {
		t.Fatalf("navigation contains configuration details: %s", out)
	}
	if len(a.bridges) != 0 {
		t.Fatal("navigation connected to remote agent")
	}
}

func TestACPConfigSubcommandRepliesAreScoped(t *testing.T) {
	_, cmd := configTestRuntime(t, "claude", "supported")
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("scoped", "claude"), 5*time.Second)
	defer cancel()
	for _, args := range []string{"mode", "mode plan", "model", "model opus", "effort", "effort high", "config fast-mode true"} {
		out, err := cmd.CommandExec(ctx, args)
		if err != nil {
			t.Fatalf("%s: %v", args, err)
		}
		want := "llm"
		if strings.HasPrefix(args, "mode ") || args == "mode" {
			want = "session-mode"
		}
		if strings.HasPrefix(args, "effort") {
			want = "effort"
		}
		if strings.HasPrefix(args, "config") {
			want = "fast-mode"
		}
		for _, id := range []string{"llm", "effort", "fast-mode", "session-mode"} {
			if strings.Contains(out, "\n"+id+"\n") != (id == want) {
				t.Fatalf("%s returned wrong option scope: %s", args, out)
			}
		}
		if strings.Contains(out, "show current model and all config options") {
			t.Fatalf("%s appended full help: %s", args, out)
		}
	}
}

func TestACPConfigNavigationHasNoHelpCommand(t *testing.T) {
	a, cmd := configTestRuntime(t, "claude", "supported")
	out, err := cmd.CommandExec(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "/acp-claude help") {
		t.Fatalf("navigation includes redundant help command: %s", out)
	}
	for _, args := range []string{"help", "--help", "-h"} {
		if _, err := cmd.CommandExec(context.Background(), args); err == nil {
			t.Fatalf("removed %q still accepted", args)
		}
	}
	if len(a.bridges) != 0 {
		t.Fatal("navigation or invalid command connected to remote")
	}
}
