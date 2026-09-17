package credentials

import "testing"

func TestValidateIntegrationScope(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"mcp.github", "openapi.petstore", "mcp.x", "openapi.a", "shell-bash.gh"} {
		if err := ValidateIntegrationScope(scope); err != nil {
			t.Fatalf("scope %q: %v", scope, err)
		}
	}
	for _, scope := range []string{"", "mcp.", "openapi.", "global", "mcp"} {
		if err := ValidateIntegrationScope(scope); err == nil {
			t.Fatalf("scope %q should be rejected", scope)
		}
	}
}

func TestScopedStorageKey(t *testing.T) {
	t.Parallel()
	got := ScopedStorageKey("mcp.tool", "GITHUB_TOKEN")
	want := "mcp.tool::GITHUB_TOKEN"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
