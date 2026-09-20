package workspace_test

import (
	"testing"

	"github.com/lengzhao/agentkit/cap/workspace"
)

func TestParseScoped(t *testing.T) {
	t.Parallel()
	scope, path, ok := workspace.ParseScoped("global:skills")
	if !ok || scope != workspace.ScopeGlobal || path != "skills" {
		t.Fatalf("ParseScoped(global:skills)=%q %q %v", scope, path, ok)
	}
	scope, path, ok = workspace.ParseScoped("local:")
	if !ok || scope != workspace.ScopeLocal || path != "." {
		t.Fatalf("ParseScoped(local:)=%q %q %v", scope, path, ok)
	}
	_, _, ok = workspace.ParseScoped("/abs/path")
	if ok {
		t.Fatal("expected absolute path to be unscoped")
	}
}
