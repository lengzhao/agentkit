package openapi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestOpenAPICredentialWarningOnReload(t *testing.T) {
	dir := t.TempDir()
	const missingKey = "OPENAPI_WARN_TEST_TOKEN"
	t.Setenv(missingKey, "")
	apiJSON := `{
  "apis": {
    "petstore": {
      "baseUrl": "https://example.com",
      "auth": {"type": "bearer", "token": "env:` + missingKey + `"},
      "paths": {"/ping": {"get": {"operationId": "ping"}}}
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "api.json"), []byte(apiJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	provider, err := NewOpenAPI(localOpenAPIConfig(), OpenAPIDeps{Workspace: &testWorkspace{root: dir}, Credentials: scopedCredsForAPI("petstore", nil)})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx := context.Background()
	cp, ok := provider.(agentkit.CommandProvider)
	if !ok {
		t.Fatal("provider is not CommandProvider")
	}
	out, err := cp.Commands()[0].CommandExec(ctx, "-u")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !strings.Contains(out, "warning: missing credentials") {
		t.Fatalf("output=%q, want credential warning", out)
	}
	if !strings.Contains(out, "hint: /env add "+missingKey+"=<value>") {
		t.Fatalf("output=%q, want /env add hint", out)
	}
}

func TestOpenAPICredentialWarningAbsentWhenEnvSet(t *testing.T) {
	dir := t.TempDir()
	const key = "OPENAPI_WARN_PRESENT_TOKEN"
	apiJSON := `{
  "apis": {
    "petstore": {
      "baseUrl": "https://example.com",
      "auth": {"type": "bearer", "token": "env:` + key + `"},
      "paths": {"/ping": {"get": {"operationId": "ping"}}}
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "api.json"), []byte(apiJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	provider, err := NewOpenAPI(localOpenAPIConfig(), OpenAPIDeps{
		Workspace:   &testWorkspace{root: dir},
		Credentials: scopedCredsForAPI("petstore", map[string]string{key: "secret"}),
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx := context.Background()
	cp := provider.(agentkit.CommandProvider)
	out, err := cp.Commands()[0].CommandExec(ctx, "-u")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if strings.Contains(out, "warning: missing credentials") {
		t.Fatalf("output=%q, want no credential warning", out)
	}
}
