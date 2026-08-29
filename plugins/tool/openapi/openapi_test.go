package openapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

func TestOpenAPIToolEndToEnd(t *testing.T) {
	t.Setenv("TESTAPI_TOKEN", "s3cr3t")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/pets/42":
			if got := r.Header.Get("Authorization"); got != "Bearer s3cr3t" {
				t.Errorf("Authorization = %q", got)
			}
			if got := r.URL.Query().Get("verbose"); got != "true" {
				t.Errorf("verbose query = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":42,"name":"Rex"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/pets":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			if body["name"] != "Fido" {
				t.Errorf("body = %v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":43,"name":"Fido"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	apiJSON := `{
  "apis": {
    "petstore": {
      "baseUrl": "` + server.URL + `",
      "auth": {"type": "bearer", "token": "env:TESTAPI_TOKEN"},
      "denyOperations": ["deletePet"],
      "paths": {
        "/pets/{id}": {
          "get": {
            "operationId": "getPet",
            "summary": "Get a pet by id",
            "parameters": [
              {"name": "id", "in": "path", "required": true, "schema": {"type": "string"}},
              {"name": "verbose", "in": "query", "schema": {"type": "boolean"}}
            ]
          },
          "delete": {
            "operationId": "deletePet",
            "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}]
          }
        },
        "/pets": {
          "post": {
            "operationId": "createPet",
            "requestBody": {
              "required": true,
              "content": {"application/json": {"schema": {"type": "object", "properties": {"name": {"type": "string"}}}}}
            }
          }
        }
      }
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "api.json"), []byte(apiJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	provider, err := NewOpenAPI(OpenAPIConfig{}, OpenAPIDeps{Workspace: &testWorkspace{root: dir}})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx := context.Background()
	tools, err := provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	byName := map[string]agentkit.Tool{}
	for _, tl := range tools {
		byName[tl.Name()] = tl
	}
	if _, ok := byName["petstore__deletePet"]; ok {
		t.Fatal("deletePet should be filtered out by denyOperations")
	}

	getPet, ok := byName["petstore__getPet"]
	if !ok {
		t.Fatalf("getPet tool missing, got %v", keysOf(byName))
	}
	out := agenttest.CallTool(t, ctx, getPet, `{"id":"42","verbose":true}`)
	var getResult callResult
	if err := json.Unmarshal([]byte(out), &getResult); err != nil {
		t.Fatalf("unmarshal result: %v (%s)", err, out)
	}
	if getResult.Status != http.StatusOK {
		t.Fatalf("status = %d", getResult.Status)
	}
	var pet map[string]any
	if err := json.Unmarshal(getResult.Body, &pet); err != nil {
		t.Fatalf("unmarshal body: %v (%s)", err, getResult.Body)
	}
	if pet["name"] != "Rex" {
		t.Fatalf("pet = %v", pet)
	}

	createPet, ok := byName["petstore__createPet"]
	if !ok {
		t.Fatalf("createPet tool missing, got %v", keysOf(byName))
	}
	out = agenttest.CallTool(t, ctx, createPet, `{"body":{"name":"Fido"}}`)
	var createResult callResult
	if err := json.Unmarshal([]byte(out), &createResult); err != nil {
		t.Fatalf("unmarshal result: %v (%s)", err, out)
	}
	if createResult.Status != http.StatusCreated {
		t.Fatalf("status = %d", createResult.Status)
	}
}

func TestOpenAPIToolMissingRequiredPathParam(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	apiJSON := `{
  "apis": {
    "petstore": {
      "baseUrl": "http://example.invalid",
      "paths": {
        "/pets/{id}": {
          "get": {
            "operationId": "getPet",
            "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}]
          }
        }
      }
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "api.json"), []byte(apiJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	provider, err := NewOpenAPI(OpenAPIConfig{}, OpenAPIDeps{Workspace: &testWorkspace{root: dir}})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx := context.Background()
	tools, err := provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools = %d", len(tools))
	}
	out := agenttest.CallTool(t, ctx, tools[0], `{}`)
	if out == "" {
		t.Fatal("expected error text in tool output")
	}
}

func TestOpenAPIToolSpecFileEndToEnd(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "openapi"), 0o755); err != nil {
		t.Fatal(err)
	}
	specJSON := `{
  "servers": [{"url": "https://petstore.example.com"}],
  "paths": {
    "/pets/{id}": {
      "get": {
        "operationId": "getPet",
        "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}]
      }
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "openapi", "petstore.json"), []byte(specJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	apiJSON := `{"apis": {"petstore": {"specFile": "openapi/petstore.json"}}}`
	if err := os.WriteFile(filepath.Join(dir, "api.json"), []byte(apiJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	provider, err := NewOpenAPI(OpenAPIConfig{}, OpenAPIDeps{Workspace: &testWorkspace{root: dir}})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	tools, err := provider.ListTools(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "petstore__getPet" {
		t.Fatalf("tools = %v, want [petstore__getPet]", keysOf(toolMap(tools)))
	}
}

// TestOpenAPIToolCachingAndSyncCommand verifies that ListTools does not
// re-read api.json after the first load, and that the "openapi" command
// forces a reload picking up on-disk changes.
func TestOpenAPIToolCachingAndSyncCommand(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	apiPath := filepath.Join(dir, "api.json")
	writeAPI := func(names ...string) {
		var b strings.Builder
		b.WriteString(`{"apis":{`)
		for i, name := range names {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `%q:{"baseUrl":"https://example.com","paths":{"/ping":{"get":{"operationId":"ping"}}}}`, name)
		}
		b.WriteString(`}}`)
		if err := os.WriteFile(apiPath, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeAPI("a")

	provider, err := NewOpenAPI(OpenAPIConfig{}, OpenAPIDeps{Workspace: &testWorkspace{root: dir}})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx := context.Background()

	tools, err := provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "a__ping" {
		t.Fatalf("tools = %v, want [a__ping]", keysOf(toolMap(tools)))
	}

	// Change the file on disk; ListTools should keep serving the cached result.
	writeAPI("a", "b")
	tools, err = provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list after edit: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools after edit (pre-sync) = %d, want cache to still report 1", len(tools))
	}

	cp, ok := provider.(agentkit.CommandProvider)
	if !ok {
		t.Fatalf("provider does not implement agentkit.CommandProvider: %T", provider)
	}
	cmds := cp.Commands()
	if len(cmds) != 1 || cmds[0].Name() != "openapi" {
		t.Fatalf("commands = %v, want a single \"openapi\" command", cmds)
	}
	if _, err := cmds[0].CommandExec(ctx, "-u"); err != nil {
		t.Fatalf("sync command: %v", err)
	}

	tools, err = provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list after sync: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("tools after sync = %d, want 2", len(tools))
	}
}

func TestOpenAPIAddCommand(t *testing.T) {
	t.Parallel()

	provider, err := NewOpenAPI(OpenAPIConfig{}, OpenAPIDeps{Workspace: &testWorkspace{root: t.TempDir()}})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx := context.Background()
	cp := provider.(agentkit.CommandProvider)
	cmd := cp.Commands()[0]

	out, err := cmd.CommandExec(ctx, "add", "demo", `{"baseUrl":"https://example.com","paths":{"/ping":{"get":{"operationId":"ping"}}}}`)
	if err != nil {
		t.Fatalf("add command: %v", err)
	}
	if !strings.Contains(out, "verified") {
		t.Fatalf("output=%q, want verified", out)
	}

	tools, err := provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "demo__ping" {
		t.Fatalf("tools = %v, want [demo__ping]", keysOf(toolMap(tools)))
	}
}

func keysOf(m map[string]agentkit.Tool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func toolMap(tools []agentkit.Tool) map[string]agentkit.Tool {
	out := make(map[string]agentkit.Tool, len(tools))
	for _, tl := range tools {
		out[tl.Name()] = tl
	}
	return out
}
