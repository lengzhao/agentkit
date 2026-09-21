package openapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	rw "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestOpenAPIAddGlobalCopiesLocalSpec(t *testing.T) {
	t.Parallel()

	globalRoot := t.TempDir()
	localBase := filepath.Join(globalRoot, "tenants")
	ws, err := rw.NewTenant(rw.TenantConfig{
		Global:    globalRoot,
		LocalBase: localBase,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: "slack:C001",
		Workspace:    "slack:C001",
	})

	globalAPI := filepath.Join(globalRoot, "api.json")
	if err := os.WriteFile(globalAPI, []byte(`{"apis":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	specAbs, err := ws.Resolve(ctx, "local:api/petstore.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(specAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	specJSON := `{
  "openapi": "3.0.3",
  "info": {"title": "pet", "version": "1"},
  "paths": {
    "/ping": {"get": {"operationId": "ping"}}
  }
}`
	if err := os.WriteFile(specAbs, []byte(specJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	provider, err := NewOpenAPI(OpenAPIConfig{
		Files: []string{"global:api.json"},
	}, OpenAPIDeps{FS: testFSOver(t, ws), Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	cmd := provider.(agentkit.CommandProvider).Commands()[0]

	entry := `{"path":"local:api/petstore.json","baseUrl":"https://example.com"}`
	out, err := cmd.CommandExec(ctx, "add -g petstore "+entry)
	if err != nil {
		t.Fatalf("add -g: %v", err)
	}
	if !strings.Contains(out, "verified") {
		t.Fatalf("output=%q, want verified", out)
	}

	raw, err := os.ReadFile(globalAPI)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Apis map[string]struct {
			Path string `json:"path"`
		} `json:"apis"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(globalRoot, "api", "petstore.json")
	if doc.Apis["petstore"].Path != wantPath {
		t.Fatalf("written path = %q, want %q", doc.Apis["petstore"].Path, wantPath)
	}

	globalSpec := wantPath
	if _, err := os.Stat(globalSpec); err != nil {
		t.Fatalf("global spec not copied: %v", err)
	}
}
