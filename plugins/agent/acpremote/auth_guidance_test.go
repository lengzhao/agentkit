package acpremote

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/testing/agenttest"
	"os"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

func TestAuthGuidanceWithoutConfiguredHint(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "test-secret-do-not-print")
	rt := &Runtime{cfg: Config{ID: "claude", Command: []string{"npx", "-y", "@agentclientprotocol/claude-agent-acp@0.93.0"}}}
	hint := rt.authHintForError(fmt.Errorf("acp session/new: %w", acp.NewAuthRequired(map[string]any{"secret": "server-secret-do-not-print"})))
	for _, want := range []string{"认证", "export", "ANTHROPIC_AUTH_TOKEN=已设置"} {
		if !strings.Contains(hint, want) {
			t.Errorf("missing %s in guidance: %s", want, hint)
		}
	}
	if strings.Contains(hint, "test-secret-do-not-print") || strings.Contains(hint, "server-secret-do-not-print") {
		t.Fatal("guidance leaked credentials")
	}
}

func TestAuthHintDoesNotMislabelOtherErrors(t *testing.T) {
	rt := &Runtime{cfg: Config{ID: "claude", AuthHint: "run login"}}
	for _, err := range []error{nil, errors.New("connection refused"), context.Canceled, context.DeadlineExceeded, acp.NewInvalidParams(nil)} {
		if hint := rt.authHintForError(err); hint != "" {
			t.Errorf("unrelated error %v misclassified as auth: %s", err, hint)
		}
	}
}

func TestAuthGuidanceReportsEffectiveChildEnvironment(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "inherited-secret")
	rt := &Runtime{cfg: Config{ID: "claude", Env: map[string]string{"ANTHROPIC_API_KEY": "", "ANTHROPIC_AUTH_TOKEN": "configured-secret"}}}
	hint := rt.authHintForError(acp.NewAuthRequired(nil))
	for _, want := range []string{"ANTHROPIC_API_KEY=未设置", "ANTHROPIC_AUTH_TOKEN=已设置"} {
		if !strings.Contains(hint, want) {
			t.Errorf("missing %s: %s", want, hint)
		}
	}
	if strings.Contains(hint, "inherited-secret") || strings.Contains(hint, "configured-secret") {
		t.Fatal("leaked secret value")
	}
}

// Fake ACP peer runs in the current Go test binary, requiring no external CLI,
// account, browser or network. The failure can occur before or during a turn.
func TestACPAuthFailureHelper(t *testing.T) {
	stage := os.Getenv("AGENTKIT_AUTH_TEST_STAGE")
	if stage == "" {
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(scan.Bytes(), &req) != nil || len(req.ID) == 0 {
			continue
		}
		reply := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "initialize":
			reply["result"] = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}, "authMethods": []any{}}
		case "session/new":
			if stage == "session" {
				reply["error"] = acp.NewAuthRequired(map[string]any{"details": "secret-from-server"})
			} else {
				reply["result"] = map[string]any{"sessionId": "fake-auth-session"}
			}
		case "session/prompt":
			reply["error"] = acp.NewAuthRequired(map[string]any{"details": "secret-from-server"})
		default:
			reply["result"] = map[string]any{}
		}
		if json.NewEncoder(os.Stdout).Encode(reply) != nil {
			os.Exit(1)
		}
	}
	os.Exit(0)
}

func TestRunTurnAuthGuidanceAtBothStages(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"session", "prompt"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			agent, err := New(Config{ID: "claude", Command: []string{exe, "-test.run=^TestACPAuthFailureHelper$"}, Cwd: dir, Env: map[string]string{"AGENTKIT_AUTH_TEST_STAGE": stage, "ANTHROPIC_AUTH_TOKEN": "secret-from-config"}, ReleaseSubprocessAfterTurn: true}, Deps{Workspace: &stubWorkspace{}, FS: bindTestFS(t, dir), Telemetry: mustToolkit(t)})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(agenttest.TurnContext("auth-test", "claude"), 8*time.Second)
			defer cancel()
			messages := 0
			err = agent.RunTurn(ctx, agentkit.TurnInput{Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}}}, Emit: func(_ context.Context, ev agentkit.OutboundEvent) error {
				if ev.Type == agentkit.EventAssistantMessage {
					messages++
				}
				return nil
			}})
			var protocol *acp.RequestError
			if !errors.As(err, &protocol) || protocol.Code != -32000 {
				t.Fatalf("protocol cause lost: %v", err)
			}
			for _, want := range []string{"认证", "export", "ANTHROPIC_AUTH_TOKEN=已设置", "错误详情", "Authentication required"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("missing %s: %v", want, err)
				}
			}
			if stage == "session" {
				if !strings.Contains(err.Error(), "session/new") {
					t.Fatalf("missing stage in detail: %v", err)
				}
			} else if !strings.Contains(err.Error(), "session/prompt") {
				t.Fatalf("missing stage in detail: %v", err)
			}
			if strings.Contains(err.Error(), "secret-from-config") || strings.Contains(err.Error(), "secret-from-server") {
				t.Fatalf("unsafe raw error: %v", err)
			}
			if messages != 0 {
				t.Fatalf("guidance emitted twice: %d extra assistant messages", messages)
			}
		})
	}
}
