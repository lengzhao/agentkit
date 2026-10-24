package acpremote

import (
	"context"
	"errors"
	capsandbox "github.com/lengzhao/agentkit/cap/sandbox"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit/testing/agenttest"
)

func TestACPConfigSessionReset(t *testing.T) {
	_, cmd := configTestRuntime(t, "claude", "supported")
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("reset", "claude"), 10*time.Second)
	defer cancel()
	for _, tc := range []struct{ args, want string }{
		{"model opus", "current: opus"},
		{"model reset", "current: sonnet"},
		{"effort high", "current: high"},
		{"effort reset", "current: low"},
		{"config fast-mode true", "current: true"},
		{"config fast-mode reset", "current: false"},
	} {
		out, err := cmd.CommandExec(ctx, tc.args)
		if err != nil || !strings.Contains(out, tc.want) {
			t.Fatalf("%s = %s, %v; want %s", tc.args, out, err, tc.want)
		}
	}
}

func TestACPGlobalResetDoesNotNeedRemote(t *testing.T) {
	a, cmd := configTestRuntime(t, "claude", "supported")
	ctx := context.Background()
	for _, args := range []string{"model -g opus", "effort -g high"} {
		if _, err := cmd.CommandExec(ctx, args); err != nil {
			t.Fatal(err)
		}
	}
	a.cfg.Command = []string{"/missing-acp-agent"}
	out, err := cmd.CommandExec(ctx, "model -g reset")
	if err != nil || !strings.Contains(out, "global default: (not set)") {
		t.Fatalf("reset model: %s, %v", out, err)
	}
	out, err = cmd.CommandExec(ctx, "effort -g")
	if err != nil || !strings.Contains(out, "global default: high") {
		t.Fatalf("reset affected effort: %s, %v", out, err)
	}
	if _, err := cmd.CommandExec(ctx, "config -g reset"); err != nil {
		t.Fatal(err)
	}
	out, err = cmd.CommandExec(ctx, "config -g show")
	if err != nil || !strings.Contains(out, "global defaults: (not set)") {
		t.Fatalf("reset all: %s, %v", out, err)
	}
}

func TestACPConfigDefaultTimeout(t *testing.T) {
	a, cmd := configTestRuntime(t, "claude", "slow")
	start := time.Now()
	_, err := cmd.CommandExec(context.Background(), "model -g opus")
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	if elapsed < 4500*time.Millisecond || elapsed > 6500*time.Millisecond {
		t.Fatalf("default timeout took %s, want about 5 seconds", elapsed)
	}
	if a.configBridge != nil && a.configBridge.proc.Load() != nil {
		t.Fatal("timed-out validator survived")
	}
}

func TestACPConfigQueueHonorsCancellation(t *testing.T) {
	a, cmd := configTestRuntime(t, "claude", "supported")
	b := a.bridgeFor("queued")
	if err := b.configMu.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer b.configMu.Unlock()
	ctx, cancel := context.WithTimeout(agenttest.TurnContext("queued", "claude"), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := cmd.CommandExec(ctx, "model")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("queued request did not time out: %v", err)
	}
}

func TestACPGlobalReadBypassesValidationLock(t *testing.T) {
	a, cmd := configTestRuntime(t, "claude", "supported")
	if err := a.configBridgeMu.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer a.configBridgeMu.Unlock()
	store := &globalConfigStore{fs: a.fs, agentID: a.id}
	key, err := a.workspace.Resolve(context.Background(), store.path())
	if err != nil {
		t.Fatal(err)
	}
	value, _ := globalConfigLocks.LoadOrStore(key, &requestMutex{})
	lock := value.(*requestMutex)
	if err := lock.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	out, err := cmd.CommandExec(ctx, "model -g")
	if err != nil || !strings.Contains(out, "(not set)") {
		t.Fatalf("global read blocked: %s, %v", out, err)
	}
	_, err = cmd.CommandExec(ctx, "model -g opus")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("global write ignored queue deadline: %v", err)
	}
}

type configTestTenantKey struct{}
type configTestWorkspace struct{ root string }

func (w configTestWorkspace) Resolve(ctx context.Context, rel string) (string, error) {
	if strings.HasPrefix(rel, "global:") {
		return filepath.Join(w.root, "global", strings.TrimPrefix(rel, "global:")), nil
	}
	tenant, _ := ctx.Value(configTestTenantKey{}).(string)
	return filepath.Join(w.root, tenant, rel), nil
}

type configTestSandbox struct {
	capsandbox.Service
	dirs []string
}

func (s *configTestSandbox) WrapArgv(_ context.Context, cwd string, argv []string) ([]string, error) {
	s.dirs = append(s.dirs, cwd)
	return argv, nil
}
func TestACPGlobalValidationUsesEachWorkspace(t *testing.T) {
	a, cmd := configTestRuntime(t, "claude", "supported")
	root := t.TempDir()
	a.cfg.Cwd = ""
	a.workspace = configTestWorkspace{root: root}
	sandbox := &configTestSandbox{Service: capsandbox.Disabled()}
	a.sandbox = sandbox
	for _, tenant := range []string{"a", "b"} {
		ctx := context.WithValue(context.Background(), configTestTenantKey{}, tenant)
		if _, err := cmd.CommandExec(ctx, "model -g opus"); err != nil {
			t.Fatal(err)
		}
		if proc := a.configBridge.proc.Load(); proc != nil {
			t.Fatal("validator survived command")
		}
	}
	if len(sandbox.dirs) != 2 || sandbox.dirs[0] != filepath.Join(root, "a", "work") || sandbox.dirs[1] != filepath.Join(root, "b", "work") {
		t.Fatalf("wrong sandbox workspaces: %v", sandbox.dirs)
	}
}
