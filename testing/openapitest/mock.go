package openapitest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/plugins/tool/openapi"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

const (
	// DefaultToken is the bearer token expected by the mock API and fixture auth.
	DefaultToken = "openapitest-token"
	// BaseURLPlaceholder is substituted when materializing api.json from fixtures.
	BaseURLPlaceholder = "{{BASE_URL}}"
)

// Mock serves the petstore-like HTTP API used in OpenAPI smoke tests.
type Mock struct {
	Server *httptest.Server
	URL    string
}

// StartMock starts an httptest server for the OpenAPI fixture API.
func StartMock(t *testing.T) *Mock {
	t.Helper()
	m := &Mock{}
	m.Server = httptest.NewServer(http.HandlerFunc(m.serveHTTP))
	t.Cleanup(m.Server.Close)
	m.URL = m.Server.URL
	return m
}

func (m *Mock) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if auth := r.Header.Get("Authorization"); auth != "" && auth != "Bearer "+DefaultToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/pets/42":
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":42,"name":"Rex"}`))
	case r.Method == http.MethodPost && r.URL.Path == "/pets":
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if body["name"] == nil {
			http.Error(w, "missing name", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":43,"name":"Fido"}`))
	case r.Method == http.MethodGet && r.URL.Path == "/orders":
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "0"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"page":` + page + `}`))
	case r.Method == http.MethodGet && r.URL.Path == "/ping":
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

// FixturesDir returns the repo-relative fixtures/openapi directory.
func FixturesDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(agenttest.RepoRoot(t), "testing", "fixtures", "openapi")
}

// Materialize copies fixture files into a temp workspace and substitutes baseURL
// into the chosen api.json variant (default api.json).
func Materialize(t *testing.T, baseURL string, apiFile ...string) string {
	t.Helper()
	baseURL = strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		t.Fatal("baseURL is required")
	}
	name := "api.json"
	if len(apiFile) > 0 && strings.TrimSpace(apiFile[0]) != "" {
		name = strings.TrimSpace(apiFile[0])
	}

	srcRoot := FixturesDir(t)
	dstRoot := t.TempDir()
	if err := copyTree(srcRoot, dstRoot); err != nil {
		t.Fatalf("copy fixtures: %v", err)
	}
	path := filepath.Join(dstRoot, name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	replaced := strings.ReplaceAll(string(raw), BaseURLPlaceholder, baseURL)
	if strings.Contains(replaced, BaseURLPlaceholder) {
		t.Fatalf("%s still contains %q after substitution", name, BaseURLPlaceholder)
	}
	if err := os.WriteFile(path, []byte(replaced), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	// tools/runtime resolves local:api.json; keep a stable copy at api.json too.
	if name != "api.json" {
		if err := os.WriteFile(filepath.Join(dstRoot, "api.json"), []byte(replaced), 0o644); err != nil {
			t.Fatalf("write api.json: %v", err)
		}
	}
	return dstRoot
}

// Workspace returns a workspace.Service rooted at the materialized fixture dir.
func Workspace(root string) workspace.Service {
	return rootWorkspace{root: root}
}

type rootWorkspace struct {
	root string
}

func (w rootWorkspace) Resolve(_ context.Context, rel string) (string, error) {
	scope, path, scoped := parseScoped(rel)
	if scoped {
		switch scope {
		case "local":
			// fall through
		case "global":
			return "", os.ErrNotExist
		default:
			return "", os.ErrNotExist
		}
	} else {
		path = rel
	}
	path = strings.TrimPrefix(path, "/")
	if path == "" || strings.Contains(path, "..") {
		return "", os.ErrNotExist
	}
	return filepath.Join(w.root, filepath.FromSlash(path)), nil
}

func parseScoped(rel string) (scope, path string, ok bool) {
	for _, prefix := range []string{"local:", "global:"} {
		if strings.HasPrefix(rel, prefix) {
			return strings.TrimSuffix(prefix, ":"), strings.TrimPrefix(rel, prefix), true
		}
	}
	return "", rel, false
}

// NewProvider builds tool/openapi against a materialized workspace.
func NewProvider(t *testing.T, root string) agentkit.ToolProvider {
	t.Helper()
	t.Setenv("OPENAPITEST_TOKEN", DefaultToken)
	provider, err := openapi.NewOpenAPI(openapi.OpenAPIConfig{
		Files: []string{"api.json", "local:api.json"},
	}, openapi.OpenAPIDeps{
		Workspace: Workspace(root),
	})
	if err != nil {
		t.Fatalf("new openapi provider: %v", err)
	}
	return provider
}

// TurnContext returns a turn context seeded for bind tests.
func TurnContext(sessionID agentkit.SessionID, agentID agentkit.AgentID, userID string, metadata map[string]any) context.Context {
	ctx := agenttest.TurnContext(sessionID, agentID)
	if userID != "" {
		ctx = context.WithValue(ctx, agentkit.KeyUserID, userID)
	}
	if len(metadata) > 0 {
		ctx = context.WithValue(ctx, agentkit.KeyMessageMetadata, metadata)
	}
	return ctx
}

// ToolByName lists tools from the provider and returns the named tool.
func ToolByName(t *testing.T, ctx context.Context, provider agentkit.ToolProvider, name string) agentkit.Tool {
	t.Helper()
	tools, err := provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tl := range tools {
		if tl.Name() == name {
			return tl
		}
	}
	names := make([]string, 0, len(tools))
	for _, tl := range tools {
		names = append(names, tl.Name())
	}
	t.Fatalf("tool %q not found, have %v", name, names)
	return nil
}

func copyTree(srcRoot, dstRoot string) error {
	return filepath.Walk(srcRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(dstRoot, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		return copyFile(path, dst, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
