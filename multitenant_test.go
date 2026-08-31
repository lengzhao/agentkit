package agentkit_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	_ "github.com/lengzhao/agentkit/plugins"
	"github.com/lengzhao/agentkit/runtime/session"
	rw "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/pluginkit/build"
)

// writeStep is one scripted turn: call write, then say a word. llm/scripted
// keeps a single cursor across turns, so a multi-turn test scripts them in order.
func writeStep(path, content string) []any {
	return []any{
		map[string]any{
			"text": "",
			"toolCalls": []any{
				map[string]any{
					"id":    "call-" + path + "-" + content,
					"name":  "write",
					"input": `{"path":"` + path + `","content":"` + content + `"}`,
				},
			},
		},
		map[string]any{"text": "done"},
	}
}

func multiTenantGraph(localBase string, pinned map[string]any, steps []any) map[string]any {
	workspaceCfg := map[string]any{
		"global":    filepath.Join(localBase, "_global"),
		"localBase": localBase,
		"scope":     "local",
	}
	if len(pinned) > 0 {
		workspaceCfg["tenants"] = pinned
	}
	workspaceNode := map[string]any{
		"use":    "workspace/tenant",
		"config": workspaceCfg,
	}
	return map[string]any{
		"agent": map[string]any{
			"use": "agent/coding",
			"config": map[string]any{
				"id":       "test",
				"maxSteps": 5,
			},
			"deps": map[string]any{
				"sessionStore": map[string]any{
					"use":    "session/store",
					"config": map[string]any{"dir": "sessions"},
					"deps":   map[string]any{"workspace": workspaceNode},
				},
				"llm": map[string]any{
					"use":    "llm/scripted",
					"config": map[string]any{"steps": steps},
				},
				"prompt": map[string]any{"use": "prompt/assembler/default"},
				"tools": map[string]any{
					"use": "tools/runtime",
					"deps": map[string]any{
						"toolPacks": []any{
							map[string]any{
								"use": "tool/fs-workspace",
								"config": map[string]any{
									"root":     "work",
									"maxBytes": 1048576,
								},
								"deps": map[string]any{"workspace": workspaceNode},
							},
						},
						"approval": map[string]any{"use": "approval/auto-allow"},
					},
				},
			},
		},
	}
}

func runTurn(t *testing.T, ag agentkit.Agent, sessionID agentkit.SessionID, userID, text string) {
	t.Helper()
	if userID != "" {
		text = "[agentkit sender_id=" + userID + "]\n" + text
	}
	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, sessionID)
	if userID != "" {
		ctx = context.WithValue(ctx, agentkit.KeyUserID, userID)
	}
	err := ag.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		},
	})
	if err != nil {
		t.Fatalf("run turn for %s: %v", sessionID, err)
	}
}

// Requirement 3: two Slack channels write to two different working directories,
// with no per-channel configuration. Requirement 1 comes along with it: each
// channel's session log lands under its own root.
func TestMultiTenantChannelsGetSeparateWorkdirs(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	steps := append(writeStep("notes.txt", "from C001"), writeStep("notes.txt", "from C002")...)
	ag, _, err := build.Build[agentkit.Agent](context.Background(), multiTenantGraph(base, nil, steps), "agent")
	if err != nil {
		t.Fatalf("build agent: %v", err)
	}

	runTurn(t, ag, session.SlackSessionIDForScope(session.ScopeChannel, "C001", "", "U111"), "U111", "写个 notes")
	runTurn(t, ag, session.SlackSessionIDForScope(session.ScopeChannel, "C002", "", "U999"), "U999", "写个 notes")

	rootA := filepath.Join(base, "slack_C001")
	rootB := filepath.Join(base, "slack_C002")

	for root, want := range map[string]string{rootA: "from C001", rootB: "from C002"} {
		raw, err := os.ReadFile(filepath.Join(root, "work", "notes.txt"))
		if err != nil {
			t.Fatalf("read notes in %s: %v", root, err)
		}
		if string(raw) != want {
			t.Fatalf("%s/work/notes.txt = %q, want %q", root, raw, want)
		}
	}
	// Same relative path, two files: the tenants never saw each other's write.
	for root, sessionFile := range map[string]string{
		rootA: "slack_C001.jsonl",
		rootB: "slack_C002.jsonl",
	} {
		if _, err := os.Stat(filepath.Join(root, "sessions", sessionFile)); err != nil {
			t.Fatalf("session log missing under %s: %v", root, err)
		}
	}
	if entries, err := os.ReadDir(filepath.Join(rootA, "sessions")); err != nil || len(entries) != 1 {
		t.Fatalf("tenant A sessions dir = %v (err %v), want exactly its own log", entries, err)
	}
}

// A channel pinned to an existing project directory works in that directory.
func TestMultiTenantPinnedProjectDir(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	project := t.TempDir()
	agentkitDir := filepath.Join(project, ".agentkit")
	pinned := map[string]any{
		"slack:C001": map[string]any{"root": agentkitDir},
	}
	ag, _, err := build.Build[agentkit.Agent](context.Background(), multiTenantGraph(base, pinned, writeStep("out.txt", "pinned")), "agent")
	if err != nil {
		t.Fatalf("build agent: %v", err)
	}

	runTurn(t, ag, agentkit.SessionID("slack:C001:t:1712345678.9"), "U111", "写文件")

	raw, err := os.ReadFile(filepath.Join(agentkitDir, "work", "out.txt"))
	if err != nil {
		t.Fatalf("read pinned output: %v", err)
	}
	if string(raw) != "pinned" {
		t.Fatalf("out.txt = %q", raw)
	}
	if _, err := os.Stat(filepath.Join(project, "out.txt")); err == nil {
		t.Fatal("tool write escaped work/ into project root")
	}
}

// Requirements 2 and 3 together: two people in one channel share one session and
// one working directory, and the replayed history names who said what.
func TestMultiTenantSharedChannelSessionIdentifiesUsers(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	steps := append(writeStep("a.txt", "first"), writeStep("b.txt", "second")...)
	graph := multiTenantGraph(base, nil, steps)
	ag, result, err := build.Build[agentkit.Agent](context.Background(), graph, "agent")
	if err != nil {
		t.Fatalf("build agent: %v", err)
	}
	_ = result

	sessionID := session.SlackSessionIDForScope(session.ScopeChannel, "C001", "", "U111")
	runTurn(t, ag, sessionID, "U111", "建个 a.txt")
	runTurn(t, ag, sessionID, "U222", "再建个 b.txt")

	root := filepath.Join(base, "slack_C001")
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(root, "work", name)); err != nil {
			t.Fatalf("work/%s missing from shared workdir: %v", name, err)
		}
	}

	// One session file, not two.
	entries, err := os.ReadDir(filepath.Join(root, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("sessions dir holds %d files, want 1 shared session", len(entries))
	}

	svc, err := rw.NewTenant(rw.TenantConfig{
		Global:    filepath.Join(base, "_global"),
		LocalBase: base,
		Scope:     "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := session.NewStore(session.StoreConfig{Dir: "sessions"}, session.StoreDeps{Workspace: svc})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, sessionID)
	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var userTexts []string
	for _, msg := range replay {
		if msg.Role == "user" {
			userTexts = append(userTexts, contentText(msg))
		}
	}
	joined := strings.Join(userTexts, "\n")
	if !strings.Contains(joined, "sender_id=U111") {
		t.Fatalf("history does not name U111:\n%s", joined)
	}
	if !strings.Contains(joined, "sender_id=U222") {
		t.Fatalf("history does not name U222:\n%s", joined)
	}
	if !strings.Contains(joined, "建个 a.txt") || !strings.Contains(joined, "再建个 b.txt") {
		t.Fatalf("history lost message text:\n%s", joined)
	}
}
