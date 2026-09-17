package credentials

import "testing"

func TestFirstShellCommandToken(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"gh pr list":    "gh",
		"  npm install": "npm",
		"'my tool' run": "my tool",
		"echo hello":    "echo",
		"":              "",
	}
	for cmd, want := range cases {
		if got := FirstShellCommandToken(cmd); got != want {
			t.Fatalf("FirstShellCommandToken(%q)=%q want %q", cmd, got, want)
		}
	}
}

func TestShellBashScopeForCommand(t *testing.T) {
	t.Parallel()
	cmd, scope := ShellBashScopeForCommand("gh auth status")
	if cmd != "gh" || scope != "shell-bash.gh" {
		t.Fatalf("got cmd=%q scope=%q", cmd, scope)
	}
	cmd, scope = ShellBashScopeForCommand("/usr/bin/gh auth status")
	if cmd != "gh" || scope != "shell-bash.gh" {
		t.Fatalf("path cmd=%q scope=%q", cmd, scope)
	}
}
