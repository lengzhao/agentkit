package policy_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/policy"
)

func evaluate(t *testing.T, p agentkit.Policy, name, input string) agentkit.Decision {
	t.Helper()
	decision, err := p.Evaluate(context.Background(), agentkit.PolicyInput{
		ToolCall: &agentkit.ToolCall{ID: "call", Name: name, Input: []byte(input)},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	return decision
}

func TestShellAllowlistStrictDeniesUnlisted(t *testing.T) {
	t.Parallel()

	p, err := policy.NewShellAllowlist(policy.ShellAllowlistConfig{
		Allow:  []string{"go test", "git status"},
		Strict: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		command string
		want    agentkit.DecisionKind
	}{
		{`go test ./...`, agentkit.DecisionAllow},
		{`git status`, agentkit.DecisionAllow},
		{`git status && go test ./...`, agentkit.DecisionAllow},
		{`curl http://example.com`, agentkit.DecisionDeny},
		// A chained command must not ride in on an allowed prefix.
		{`git status && rm -rf /tmp/x`, agentkit.DecisionDeny},
		{`go test ./... | sh`, agentkit.DecisionDeny},
		{`gopher --wat`, agentkit.DecisionDeny},
	}
	for _, tc := range cases {
		got := evaluate(t, p, "bash", `{"command":"`+tc.command+`"}`).Kind
		if got != tc.want {
			t.Errorf("command %q = %q, want %q", tc.command, got, tc.want)
		}
	}
}

func TestShellAllowlistNonStrictAsksForUnlisted(t *testing.T) {
	t.Parallel()

	p, err := policy.NewShellAllowlist(policy.ShellAllowlistConfig{Allow: []string{"go build"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := evaluate(t, p, "bash", `{"command":"npm install"}`).Kind; got != agentkit.DecisionAsk {
		t.Fatalf("unlisted command = %q, want ask", got)
	}
	if got := evaluate(t, p, "bash", `{"command":"go build ./..."}`).Kind; got != agentkit.DecisionAllow {
		t.Fatalf("allowed command = %q, want allow", got)
	}
}

func TestShellAllowlistDenyBeatsAllow(t *testing.T) {
	t.Parallel()

	p, err := policy.NewShellAllowlist(policy.ShellAllowlistConfig{
		Allow: []string{"git"},
		Deny:  []string{"git push"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := evaluate(t, p, "bash", `{"command":"git push origin main"}`).Kind; got != agentkit.DecisionDeny {
		t.Fatalf("git push = %q, want deny", got)
	}
	if got := evaluate(t, p, "bash", `{"command":"git log"}`).Kind; got != agentkit.DecisionAllow {
		t.Fatalf("git log = %q, want allow", got)
	}
}

func TestShellAllowlistIgnoresOtherTools(t *testing.T) {
	t.Parallel()

	p, err := policy.NewShellAllowlist(policy.ShellAllowlistConfig{Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := evaluate(t, p, "read", `{"path":"README.md"}`).Kind; got != agentkit.DecisionAllow {
		t.Fatalf("read tool = %q, want allow", got)
	}
}

func TestPathDenylistDefaultsProtectSensitivePaths(t *testing.T) {
	t.Parallel()

	p, err := policy.NewPathDenylist(policy.PathDenylistConfig{})
	if err != nil {
		t.Fatal(err)
	}

	denied := []string{
		".git/config",
		"./.git/hooks/pre-commit",
		"nested/repo/.git/HEAD",
		".env",
		"config/.env.production",
		"home/user/.ssh/id_rsa",
		"certs/server.pem",
	}
	for _, path := range denied {
		if got := evaluate(t, p, "write", `{"path":"`+path+`"}`).Kind; got != agentkit.DecisionDeny {
			t.Errorf("path %q = %q, want deny", path, got)
		}
	}

	allowed := []string{
		"README.md",
		"internal/git/client.go",
		"docs/environment.md",
		"gitignore.txt",
	}
	for _, path := range allowed {
		if got := evaluate(t, p, "write", `{"path":"`+path+`"}`).Kind; got != agentkit.DecisionAllow {
			t.Errorf("path %q = %q, want allow", path, got)
		}
	}
}

func TestPathDenylistScopesToConfiguredTools(t *testing.T) {
	t.Parallel()

	p, err := policy.NewPathDenylist(policy.PathDenylistConfig{
		Deny:  []string{"secrets/**"},
		Tools: []string{"write"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := evaluate(t, p, "write", `{"path":"secrets/key.txt"}`).Kind; got != agentkit.DecisionDeny {
		t.Fatalf("write to secrets = %q, want deny", got)
	}
	// read is outside the configured tool list, so this policy stays out of it.
	if got := evaluate(t, p, "read", `{"path":"secrets/key.txt"}`).Kind; got != agentkit.DecisionAllow {
		t.Fatalf("read from secrets = %q, want allow", got)
	}
}

func TestPathDenylistAllowsCallsWithoutPath(t *testing.T) {
	t.Parallel()

	p, err := policy.NewPathDenylist(policy.PathDenylistConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if got := evaluate(t, p, "bash", `{"command":"ls"}`).Kind; got != agentkit.DecisionAllow {
		t.Fatalf("call without a path = %q, want allow", got)
	}
}
