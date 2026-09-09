package session_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

func sessionCommands(t *testing.T, store agentkit.SessionStore) agentkit.CommandProvider {
	t.Helper()
	provider, err := session.NewCommands(session.CommandsConfig{}, session.CommandsDeps{SessionStore: store})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func cliSlashContext(delivery agentkit.SessionID) agentkit.TurnEnvelope {
	return session.MergeEnvelopeMetadata(agentkit.TurnEnvelope{
		Conversation: string(delivery),
		Workspace:    string(delivery),
		Route:        session.SessionRouteFromDelivery("cli", delivery, ""),
		Actor:        agentkit.ActorRef{UserID: "cli"},
	}, map[string]any{
		session.MetadataSessionScope: string(session.ScopeChannel),
	})
}

func TestStoreCommands(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	provider := sessionCommands(t, store)
	commands := provider.Commands()
	if len(commands) != 2 {
		t.Fatalf("commands=%d want 2", len(commands))
	}

	ctx := session.ApplyEnvelopeToContext(context.Background(), cliSlashContext(session.DefaultCLISessionID))
	for _, cmd := range commands {
		switch cmd.Name() {
		case "new":
			out, err := cmd.CommandExec(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(out, "cli:") {
				t.Fatalf("unexpected new session id: %q", out)
			}
			active, err := store.(agentkit.ActiveSessionStore).ActiveSession(context.Background(), session.DefaultCLISessionID)
			if err != nil {
				t.Fatal(err)
			}
			if active != agentkit.SessionID(out) {
				t.Fatalf("active session = %q, want %q", active, out)
			}
		case "session":
			if _, err := cmd.CommandExec(ctx, ""); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("unexpected command %q", cmd.Name())
		}
	}
}

func TestNewCommandUpdatesActiveSession(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	provider := sessionCommands(t, store)
	var newCmd agentkit.Command
	for _, cmd := range provider.Commands() {
		if cmd.Name() == "new" {
			newCmd = cmd
			break
		}
	}
	if newCmd == nil {
		t.Fatal("missing /new command")
	}

	stable := agentkit.SessionID("slack:C001:t:123:u:U111")
	entry := session.ApplyScope(stable, session.ScopeChannel, "U111")
	ctx := session.ApplyEnvelopeToContext(context.Background(), session.MergeEnvelopeMetadata(agentkit.TurnEnvelope{
		Conversation: string(entry),
		Workspace:    string(entry),
		Route:        session.SessionRoute("slack", string(stable)),
		Actor:        agentkit.ActorRef{UserID: "U111"},
	}, map[string]any{
		session.MetadataSessionScope: string(session.ScopeChannel),
	}))
	out, err := newCmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, string(entry)+":new:") {
		t.Fatalf("new logical session = %q", out)
	}
	active, err := store.(agentkit.ActiveSessionStore).ActiveSession(context.Background(), entry)
	if err != nil {
		t.Fatal(err)
	}
	if active != agentkit.SessionID(out) {
		t.Fatalf("active session = %q, want %q", active, out)
	}
}

func TestNewCommandForCLIUsesActiveSession(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	provider := sessionCommands(t, store)
	var newCmd agentkit.Command
	for _, cmd := range provider.Commands() {
		if cmd.Name() == "new" {
			newCmd = cmd
			break
		}
	}
	if newCmd == nil {
		t.Fatal("missing /new command")
	}

	ctx := session.ApplyEnvelopeToContext(context.Background(), cliSlashContext(session.DefaultCLISessionID))
	out, err := newCmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.(agentkit.ActiveSessionStore).ActiveSession(context.Background(), session.DefaultCLISessionID)
	if err != nil {
		t.Fatal(err)
	}
	if active != agentkit.SessionID(out) {
		t.Fatalf("active session = %q, want %q", active, out)
	}
}

func TestActiveEntryKeyFromContextRespectsUserScope(t *testing.T) {
	t.Parallel()

	delivery := session.BuildDeliverySessionID("slack", "D0AK8MAHW22", "", "U02LNUW8KV5")
	env := session.WithMetadataScope(agentkit.TurnEnvelope{
		Route: session.SessionRoute("slack", string(delivery)),
		Actor: agentkit.ActorRef{UserID: "U02LNUW8KV5"},
	}, session.ScopeUser)
	ctx := session.ApplyEnvelopeToContext(context.Background(), env)
	got := session.ActiveEntryKeyFromContext(ctx)
	want := session.ApplyScope(delivery, session.ScopeUser, "U02LNUW8KV5")
	if got != want {
		t.Fatalf("entry key = %q, want %q", got, want)
	}
}
