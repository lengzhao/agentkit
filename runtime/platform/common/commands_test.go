package common

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func slashCtx(platform string, delivery agentkit.SessionID, scope session.SessionScope, userID string) SlashContext {
	return SlashContext{
		Route:        agentkit.SessionRoute(platform, string(delivery)),
		SessionScope: scope,
		UserID:       userID,
	}
}

type stubCommand struct {
	name string
	out  string
}

func (c stubCommand) Name() string        { return c.name }
func (c stubCommand) Alias() string       { return "" }
func (c stubCommand) Description() string { return "stub" }
func (c stubCommand) CommandExec(context.Context, string) (string, error) {
	return c.out, nil
}

type stubCommands struct {
	byName map[string]agentkit.Command
}

func (s stubCommands) Dispatch(ctx context.Context, name string, rawArgs string) (string, error) {
	cmd, ok := s.byName[name]
	if !ok {
		return "", agentkit.ErrCommandNotHandled
	}
	return cmd.CommandExec(ctx, rawArgs)
}

func (s stubCommands) List() []agentkit.Command {
	out := make([]agentkit.Command, 0, len(s.byName))
	for _, cmd := range s.byName {
		out = append(out, cmd)
	}
	return out
}

func TestParseSlashCommand(t *testing.T) {
	name, args, ok := ParseSlashCommand("/help topic")
	if !ok || name != "help" || args != "topic" {
		t.Fatalf("got name=%q args=%q ok=%v", name, args, ok)
	}
	if _, _, ok := ParseSlashCommand("hello"); ok {
		t.Fatal("expected non-slash")
	}
}

func TestProcessSlashHelp(t *testing.T) {
	out, err := ProcessSlash(context.Background(), nil, slashCtx("slack", "slack:C:u:U", session.ScopeChannel, ""), "/help")
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != SlashHandled || !strings.Contains(out.Reply, "/help") {
		t.Fatalf("unexpected outcome: %+v", out)
	}
}

func TestFormatHelpMultiline(t *testing.T) {
	cmds := stubCommands{byName: map[string]agentkit.Command{
		"ping": stubCommand{name: "ping", out: "pong"},
	}}
	text := FormatHelp(cmds)
	if !strings.Contains(text, "可用命令:\n") {
		t.Fatalf("expected header newline, got %q", text)
	}
	if strings.Count(text, "\n") < 2 {
		t.Fatalf("expected one command per line, got %q", text)
	}
	if !strings.Contains(text, "/ping") {
		t.Fatalf("missing registered command: %q", text)
	}
}

func TestProcessSlashHelpTopic(t *testing.T) {
	cmds := stubCommands{byName: map[string]agentkit.Command{
		"new": stubCommand{name: "new", out: "started new session"},
	}}
	out, err := ProcessSlash(context.Background(), cmds, slashCtx("cli", session.DefaultCLISessionID, session.ScopeChannel, "cli"), "/help new")
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != SlashHandled || out.Reply != "started new session" {
		t.Fatalf("unexpected outcome: %+v", out)
	}
}

func TestProcessSlashDispatch(t *testing.T) {
	cmds := stubCommands{byName: map[string]agentkit.Command{
		"ping": stubCommand{name: "ping", out: "pong"},
	}}
	out, err := ProcessSlash(context.Background(), cmds, slashCtx("slack", "slack:C:u:U", session.ScopeChannel, ""), "/ping")
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != SlashHandled || out.Reply != "pong" {
		t.Fatalf("unexpected outcome: %+v", out)
	}
}

func TestProcessSlashInjectsUserID(t *testing.T) {
	var gotUser string
	cmds := stubCommands{byName: map[string]agentkit.Command{
		"ping": captureUserCommand{userID: &gotUser},
	}}
	out, err := ProcessSlash(context.Background(), cmds, slashCtx("slack", "slack:C:u:U", session.ScopeChannel, "U123"), "/ping")
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != SlashHandled {
		t.Fatalf("kind = %v", out.Kind)
	}
	if gotUser != "U123" {
		t.Fatalf("user = %q, want U123", gotUser)
	}
}

func TestProcessSlashInjectsMetadata(t *testing.T) {
	var gotEmail string
	cmds := stubCommands{byName: map[string]agentkit.Command{
		"ping": captureMetadataCommand{key: "email", value: &gotEmail},
	}}
	ctx := slashCtx("slack", "slack:C:u:U", session.ScopeChannel, "U123")
	ctx.Metadata = map[string]any{"email": "alice@example.com", "displayName": "Alice"}
	out, err := ProcessSlash(context.Background(), cmds, ctx, "/ping")
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != SlashHandled {
		t.Fatalf("kind = %v", out.Kind)
	}
	if gotEmail != "alice@example.com" {
		t.Fatalf("email = %q", gotEmail)
	}
}

type captureUserCommand struct {
	userID *string
}

func (captureUserCommand) Name() string        { return "ping" }
func (captureUserCommand) Alias() string       { return "" }
func (captureUserCommand) Description() string { return "capture user" }
func (c captureUserCommand) CommandExec(ctx context.Context, _ string) (string, error) {
	*c.userID = session.UserIDFromContext(ctx)
	return "ok", nil
}

type captureMetadataCommand struct {
	key   string
	value *string
}

func (captureMetadataCommand) Name() string        { return "ping" }
func (captureMetadataCommand) Alias() string       { return "" }
func (captureMetadataCommand) Description() string { return "capture metadata" }
func (c captureMetadataCommand) CommandExec(ctx context.Context, _ string) (string, error) {
	*c.value = session.MetadataString(session.EnvelopeFromContext(ctx), c.key)
	return "ok", nil
}

func TestProcessSlashInjectsPlatformID(t *testing.T) {
	var gotPlatform string
	delivery := session.BuildDeliverySessionID("chat-api", "default_channel", "conv_1", "")
	cmds := stubCommands{byName: map[string]agentkit.Command{
		"ping": captureSessionCommand{gotPlatform: &gotPlatform},
	}}
	out, err := ProcessSlash(context.Background(), cmds, slashCtx("chat-api", delivery, session.ScopeChannel, ""), "/ping")
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != SlashHandled {
		t.Fatalf("kind = %v", out.Kind)
	}
	if gotPlatform != "chat-api" {
		t.Fatalf("platform = %q, want chat-api", gotPlatform)
	}
}

func TestProcessSlashNewUsesSessionScopeEntryKey(t *testing.T) {
	delivery := session.BuildDeliverySessionID("slack", "D0AK8MAHW22", "", "U02LNUW8KV5")
	var gotEntry agentkit.SessionID
	cmds := stubCommands{byName: map[string]agentkit.Command{
		"new": captureSessionCommand{entryKey: &gotEntry},
	}}
	out, err := ProcessSlash(context.Background(), cmds, slashCtx("slack", delivery, session.ScopeChannel, "U02LNUW8KV5"), "/new")
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != SlashHandled {
		t.Fatalf("kind = %v", out.Kind)
	}
	if gotEntry != "slack:D0AK8MAHW22" {
		t.Fatalf("command ctx entry key = %q, want slack:D0AK8MAHW22", gotEntry)
	}
}

func TestProcessSlashNewUserScopeEntryKey(t *testing.T) {
	delivery := session.BuildDeliverySessionID("slack", "D0AK8MAHW22", "", "U02LNUW8KV5")
	var gotEntry agentkit.SessionID
	cmds := stubCommands{byName: map[string]agentkit.Command{
		"new": captureSessionCommand{entryKey: &gotEntry},
	}}
	out, err := ProcessSlash(context.Background(), cmds, slashCtx("slack", delivery, session.ScopeUser, "U02LNUW8KV5"), "/new")
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != SlashHandled {
		t.Fatalf("kind = %v", out.Kind)
	}
	want := session.ApplyScope(delivery, session.ScopeUser, "U02LNUW8KV5")
	if gotEntry != want {
		t.Fatalf("command ctx entry key = %q, want %q", gotEntry, want)
	}
}

type captureSessionCommand struct {
	entryKey    *agentkit.SessionID
	gotPlatform *string
}

func (c captureSessionCommand) Name() string        { return "new" }
func (c captureSessionCommand) Alias() string       { return "" }
func (c captureSessionCommand) Description() string { return "capture" }
func (c captureSessionCommand) CommandExec(ctx context.Context, _ string) (string, error) {
	if c.entryKey != nil {
		*c.entryKey = session.ActiveEntryKeyFromContext(ctx)
	}
	if c.gotPlatform != nil {
		*c.gotPlatform = session.PlatformFromContext(ctx)
	}
	return "ok", nil
}

func TestProcessSlashUnknownForwards(t *testing.T) {
	out, err := ProcessSlash(context.Background(), stubCommands{byName: nil}, slashCtx("slack", "slack:C:u:U", session.ScopeChannel, ""), "/missing")
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != SlashForward || !strings.Contains(out.Reply, "转发给 Agent") {
		t.Fatalf("unexpected outcome: %+v", out)
	}
}

type adminRegistry struct {
	stubCommands
	admins []string
}

func (r adminRegistry) EnrichSlashContext(ctx context.Context) context.Context {
	userID := session.UserIDFromContext(ctx)
	for _, id := range r.admins {
		if strings.EqualFold(id, userID) {
			return context.WithValue(ctx, agentkit.KeyIsAdmin, true)
		}
	}
	return ctx
}

func (r adminRegistry) Dispatch(ctx context.Context, name string, rawArgs string) (string, error) {
	ctx = r.EnrichSlashContext(ctx)
	if name == "shell" && !agentkit.IsAdmin(ctx) {
		return "", agentkit.ErrCommandForbidden
	}
	return r.stubCommands.Dispatch(ctx, name, rawArgs)
}

func TestProcessSlashForbidden(t *testing.T) {
	cmds := adminRegistry{
		stubCommands: stubCommands{byName: map[string]agentkit.Command{
			"shell": stubCommand{name: "shell", out: "ran"},
		}},
		admins: []string{"U1"},
	}
	out, err := ProcessSlash(context.Background(), cmds, slashCtx("slack", "slack:C", session.ScopeChannel, "U2"), "/shell echo hi")
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != SlashHandled || out.Reply != UnauthorizedMessage {
		t.Fatalf("unexpected outcome: %+v", out)
	}
}
