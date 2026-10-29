package workspace_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	cw "github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/rctx"
	ws "github.com/lengzhao/agentkit/runtime/workspace"
)

func directoryContext(instance, channel, thread, session string) context.Context {
	route := rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "lark", Instance: instance, ChannelID: channel, ThreadID: thread})
	env := rctx.ResolveEnvelope(agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Route: route}}, rctx.DefaultRoutePolicy(agentkit.SessionScopeThread))
	env.Conversation = session
	return rctx.ApplyEnvelopeToContext(context.Background(), env)
}

func TestWorkspaceDirectoryScopes(t *testing.T) {
	for _, scope := range []string{"global", "channel", "thread", "session"} {
		t.Run(scope, func(t *testing.T) {
			global, local := t.TempDir(), t.TempDir()
			cfg := ws.TenantConfig{Global: global, LocalBase: local, WorkspaceScope: scope}
			service, err := ws.NewTenant(cfg)
			if err != nil {
				t.Fatal(err)
			}
			resolve := func(instance, channel, thread, session string) string {
				t.Helper()
				dir, err := service.(cw.DefaultDirectoryResolver).DefaultWorkingDirectory(directoryContext(instance, channel, thread, session))
				if err != nil {
					t.Fatal(err)
				}
				return dir
			}
			a := resolve("lark1", "C1", "T1", "S1")
			b := resolve("lark1", "C1", "T2", "S2")
			switched := resolve("lark1", "C1", "T1", "S2")
			if (a == b) != (scope == "global" || scope == "channel") {
				t.Fatalf("sharing: %s %s", a, b)
			}
			if (a == switched) != (scope != "session") {
				t.Fatalf("session switch: %s %s", a, switched)
			}
			if other := resolve("lark2", "C1", "T1", "S1"); (other == a) != (scope == "global") {
				t.Fatalf("instance collision %s", other)
			}
			if other := resolve("lark1", "C2", "T1", "S1"); (other == a) != (scope == "global") {
				t.Fatalf("channel collision %s", other)
			}
			rebuilt, err := ws.NewTenant(cfg)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := rebuilt.(cw.DefaultDirectoryResolver).DefaultWorkingDirectory(directoryContext("lark1", "C1", "T1", "S1"))
			if err != nil || restored != a {
				t.Fatalf("unstable restart %s %v", restored, err)
			}
			if scope == "global" && a != filepath.Join(global, "work") {
				t.Fatalf("global = %s", a)
			}
		})
	}
}

func TestDirectoryThreadFallbackAndOverrides(t *testing.T) {
	service, err := ws.NewTenant(ws.TenantConfig{Global: t.TempDir(), LocalBase: t.TempDir(), WorkspaceScope: "channel", WorkspaceScopes: map[string]string{"lark1": "thread"}})
	if err != nil {
		t.Fatal(err)
	}
	d := service.(cw.DefaultDirectoryResolver)
	root := directoryContext("lark1", "C1", "", "S1")
	got, err := d.DefaultWorkingDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	want, err := service.Resolve(root, "local:work")
	if err != nil || filepath.Dir(filepath.Dir(got)) != want {
		t.Fatalf("fallback = %s want %s, %v", got, want, err)
	}
	a, _ := d.DefaultWorkingDirectory(directoryContext("lark1", "C1", "T1", "S1"))
	b, _ := d.DefaultWorkingDirectory(directoryContext("lark1", "C1", "T2", "S2"))
	if a == b {
		t.Fatal("override ignored")
	}
	a, _ = d.DefaultWorkingDirectory(directoryContext("lark2", "C1", "T1", "S1"))
	b, _ = d.DefaultWorkingDirectory(directoryContext("lark2", "C1", "T2", "S2"))
	if a != b {
		t.Fatal("base policy ignored")
	}
}

func TestLegacyDirectoryPolicyDoesNotOverrideConsumerDefaults(t *testing.T) {
	service, err := ws.NewTenant(ws.TenantConfig{Global: t.TempDir(), LocalBase: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.(cw.DefaultDirectoryResolver).DefaultWorkingDirectory(directoryContext("lark1", "C1", "T1", "S1"))
	if err != nil || got != "" {
		t.Fatalf("legacy override = %s, %v", got, err)
	}
}

func TestExplicitChannelPolicyAvoidsLegacySanitizationCollisions(t *testing.T) {
	service, err := ws.NewTenant(ws.TenantConfig{Global: t.TempDir(), LocalBase: t.TempDir(), WorkspaceScope: "channel", OmitPlatformPrefix: true})
	if err != nil {
		t.Fatal(err)
	}
	d := service.(cw.DefaultDirectoryResolver)
	seen := map[string]bool{}
	for _, item := range [][2]string{{"bot1", "C/1"}, {"bot1", "C_1"}, {"bot2", "C/1"}} {
		dir, err := d.DefaultWorkingDirectory(directoryContext(item[0], item[1], "", "S1"))
		if err != nil {
			t.Fatal(err)
		}
		if seen[dir] {
			t.Fatalf("identity collision: %s", dir)
		}
		seen[dir] = true
	}
}

func TestDefaultWorkspaceExplicitPolicy(t *testing.T) {
	root, global := t.TempDir(), t.TempDir()
	service, err := ws.New(ws.Config{Global: global, Local: root, WorkspaceScope: "thread"})
	if err != nil {
		t.Fatal(err)
	}
	d := service.(cw.DefaultDirectoryResolver)
	a, err := d.DefaultWorkingDirectory(directoryContext("bot1", "C1", "T1", "S1"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.DefaultWorkingDirectory(directoryContext("bot1", "C1", "T2", "S2"))
	if err != nil || a == b || !strings.HasPrefix(a, root+string(filepath.Separator)) {
		t.Fatalf("default workspace isolation = %s %s %v", a, b, err)
	}
	for _, scope := range []string{"invalid", "local"} {
		if _, err = ws.New(ws.Config{WorkspaceScope: scope}); err == nil {
			t.Fatalf("invalid scope accepted: %s", scope)
		}
		if _, err = ws.NewTenant(ws.TenantConfig{WorkspaceScopes: map[string]string{"bot1": scope}}); err == nil {
			t.Fatalf("invalid override accepted: %s", scope)
		}
	}
}
