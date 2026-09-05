package workspace_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
	rw "github.com/lengzhao/agentkit/runtime/workspace"
)

func tenantCtx(id string) context.Context {
	return session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: id})
}

func newTenantSvc(t *testing.T, cfg rw.TenantConfig) *rw.TenantService {
	t.Helper()
	svc, err := rw.NewTenant(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts, ok := svc.(*rw.TenantService)
	if !ok {
		t.Fatalf("NewTenant returned %T", svc)
	}
	return ts
}

// Requirement: different Slack channels get different working directories, with
// no per-channel configuration at all.
func TestTenantRootsAreIsolatedByDefault(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	svc := newTenantSvc(t, rw.TenantConfig{Global: t.TempDir(), LocalBase: base})

	a, err := svc.Resolve(tenantCtx("slack:C001"), "sessions")
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Resolve(tenantCtx("slack:C002"), "sessions")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("two channels share a root: %q", a)
	}
	if want := filepath.Join(base, "slack_C001", "sessions"); a != want {
		t.Fatalf("C001 = %q, want %q", a, want)
	}
	if want := filepath.Join(base, "slack_C002", "sessions"); b != want {
		t.Fatalf("C002 = %q, want %q", b, want)
	}
}

func TestTenantOmitPlatformPrefix(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	svc := newTenantSvc(t, rw.TenantConfig{
		Global:             t.TempDir(),
		LocalBase:          base,
		OmitPlatformPrefix: true,
	})

	got, err := svc.Resolve(tenantCtx("slack:C001"), "sessions")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "C001", "sessions"); got != want {
		t.Fatalf("C001 = %q, want %q", got, want)
	}

	got, err = svc.Resolve(tenantCtx("chat-api:slack_D0AK8MAHW22:t:conv1"), "sessions")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "slack_D0AK8MAHW22", "sessions"); got != want {
		t.Fatalf("chat-api channel = %q, want %q", got, want)
	}
}

// Requirement: one channel keeps one working directory however its history is
// split — threads and per-user sessions must not fork the workdir.
func TestOneChannelKeepsOneRootAcrossSessionScopes(t *testing.T) {
	t.Parallel()

	svc := newTenantSvc(t, rw.TenantConfig{Global: t.TempDir(), LocalBase: t.TempDir()})

	ids := []string{
		"slack:C001",
		"slack:C001:t:1712345678.9",
		"slack:C001:t:1799999999.1",
		"slack:C001:u:U456",
	}
	var first string
	for i, id := range ids {
		got, err := svc.Resolve(tenantCtx(id), "repo")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("session %q resolved to %q, want %q", id, got, first)
		}
	}
}

func TestTenantPinnedRoot(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	svc := newTenantSvc(t, rw.TenantConfig{
		Global:    t.TempDir(),
		LocalBase: t.TempDir(),
		Tenants: map[string]rw.TenantEntry{
			"slack:C001": {Root: project},
		},
	})

	got, err := svc.Resolve(tenantCtx("slack:C001:t:17.9"), "src")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(project, "src"); got != want {
		t.Fatalf("pinned root resolve = %q, want %q", got, want)
	}
}

// global: is the one root every tenant shares, so a skills library installed
// once is visible to every channel.
func TestGlobalRootIsSharedAcrossTenants(t *testing.T) {
	t.Parallel()

	global := t.TempDir()
	svc := newTenantSvc(t, rw.TenantConfig{Global: global, LocalBase: t.TempDir()})

	a, err := svc.Resolve(tenantCtx("slack:C001"), "global:skills")
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Resolve(tenantCtx("slack:C002"), "global:skills")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("global root differs per tenant: %q vs %q", a, b)
	}
	if want := filepath.Join(global, "skills"); a != want {
		t.Fatalf("global resolve = %q, want %q", a, want)
	}
}

// A per-tenant root sits beside its siblings, so ".." must not resolve — this is
// the difference from workspace/default, where one level up is the project root.
func TestTenantRootRefusesParentEscape(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	project := t.TempDir()
	svc := newTenantSvc(t, rw.TenantConfig{
		Global:    t.TempDir(),
		LocalBase: base,
		Tenants:   map[string]rw.TenantEntry{"slack:C002": {Root: project}},
	})

	for _, rel := range []string{"..", "../slack_C002", "../../etc", "sub/../../peer", "local:.."} {
		if got, err := svc.Resolve(tenantCtx("slack:C001"), rel); err == nil {
			t.Fatalf("resolve(%q) = %q, want error", rel, got)
		}
	}
	// A pinned root is a boundary too.
	if got, err := svc.Resolve(tenantCtx("slack:C002"), ".."); err == nil {
		t.Fatalf("pinned root resolve(..) = %q, want error", got)
	}
	// The tenant's own subtree still resolves.
	if _, err := svc.Resolve(tenantCtx("slack:C001"), "a/b/c"); err != nil {
		t.Fatalf("in-root resolve failed: %v", err)
	}
}

// Timers, cron tasks and direct library calls carry no session, and must not
// land inside some real tenant's directory.
func TestNoSessionUsesDefaultTenantDir(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	svc := newTenantSvc(t, rw.TenantConfig{Global: t.TempDir(), LocalBase: base})

	got, err := svc.Resolve(context.Background(), "sessions")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, rw.DefaultTenantDir, "sessions"); got != want {
		t.Fatalf("no-session resolve = %q, want %q", got, want)
	}
}

func TestTenantScopeGlobalDefault(t *testing.T) {
	t.Parallel()

	global := t.TempDir()
	svc := newTenantSvc(t, rw.TenantConfig{Global: global, LocalBase: t.TempDir(), Scope: "global"})

	got, err := svc.Resolve(tenantCtx("slack:C001"), "mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(global, "mcp.json"); got != want {
		t.Fatalf("unprefixed with scope=global = %q, want %q", got, want)
	}
}

func TestTenantConfigValidation(t *testing.T) {
	t.Parallel()

	if _, err := rw.NewTenant(rw.TenantConfig{Scope: "sideways"}); err == nil {
		t.Fatal("bad scope accepted")
	}
	if _, err := rw.NewTenant(rw.TenantConfig{Tenants: map[string]rw.TenantEntry{"slack:C1": {}}}); err == nil {
		t.Fatal("tenant without root accepted")
	}
	if _, err := rw.NewTenant(rw.TenantConfig{Tenants: map[string]rw.TenantEntry{"": {Root: "/tmp"}}}); err == nil {
		t.Fatal("empty tenant key accepted")
	}
}

func TestTenantKeyFromContext(t *testing.T) {
	t.Parallel()

	if got := session.WorkspaceFromContext(tenantCtx("slack:C001:t:17.9")); got != "slack:C001" {
		t.Fatalf("WorkspaceFromContext = %q", got)
	}
	if got := session.WorkspaceFromContext(context.Background()); got != "" {
		t.Fatalf("WorkspaceFromContext without session = %q", got)
	}
}

func TestTenantUsesEnvelopeWorkspace(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	svc := newTenantSvc(t, rw.TenantConfig{Global: t.TempDir(), LocalBase: base})
	env := agentkit.TurnEnvelope{
		Route:        agentkit.SessionRoute("slack", "slack:C001:t:17.9"),
		Conversation: "schedule:job:1",
		Workspace:    "slack:C001",
	}
	ctx := session.ApplyEnvelopeToContext(context.Background(), env)

	got, err := svc.Resolve(ctx, "work/marker.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, "slack_C001", "work", "marker.txt")
	if got != want {
		t.Fatalf("resolve = %q, want %q", got, want)
	}
}
