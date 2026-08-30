package openapi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func noSpecLoader(rel string) ([]byte, error) {
	return nil, fmt.Errorf("unexpected specFile load: %s", rel)
}

func TestParseIndexFileInline(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
  "apis": {
    "petstore": {
      "baseUrl": "https://api.example.com/",
      "prefix": "pet__",
      "headers": {"Accept": "application/json"},
      "auth": {"type": "bearer", "token": "env:PETSTORE_TOKEN"},
      "allowOperations": ["getPet", "createPet"],
      "timeoutSeconds": 15,
      "paths": {
        "/pets/{id}": {
          "get": {
            "operationId": "getPet",
            "summary": "Get a pet by id",
            "parameters": [
              {"name": "id", "in": "path", "required": true, "schema": {"type": "string"}},
              {"name": "verbose", "in": "query", "schema": {"type": "boolean"}}
            ]
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
}`)

	apis, err := parseIndexFile("/tmp/api.json", raw, noSpecLoader)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(apis) != 1 {
		t.Fatalf("apis = %d, want 1", len(apis))
	}
	api := apis[0]
	if api.Name != "petstore" {
		t.Fatalf("name = %q", api.Name)
	}
	if api.BaseURL != "https://api.example.com" {
		t.Fatalf("baseUrl = %q, want trailing slash trimmed", api.BaseURL)
	}
	if api.toolPrefix() != "pet__" {
		t.Fatalf("prefix = %q", api.toolPrefix())
	}
	if api.Auth == nil || api.Auth.Type != "bearer" || api.Auth.Token != "env:PETSTORE_TOKEN" {
		t.Fatalf("auth = %+v", api.Auth)
	}
	if !api.allowsOperation("getPet") || !api.allowsOperation("createPet") {
		t.Fatal("getPet/createPet should be allowed")
	}
	if api.allowsOperation("deletePet") {
		t.Fatal("deletePet should be denied by allowOperations")
	}
	if len(api.Operations) != 2 {
		t.Fatalf("operations = %d, want 2", len(api.Operations))
	}

	var get, create *operationConfig
	for i := range api.Operations {
		switch api.Operations[i].OperationID {
		case "getPet":
			get = &api.Operations[i]
		case "createPet":
			create = &api.Operations[i]
		}
	}
	if get == nil || create == nil {
		t.Fatalf("missing operations: %+v", api.Operations)
	}
	if get.Method != "GET" || get.Path != "/pets/{id}" {
		t.Fatalf("getPet = %+v", get)
	}
	if len(get.Parameters) != 2 {
		t.Fatalf("getPet parameters = %d, want 2", len(get.Parameters))
	}
	if create.RequestBody == nil || !create.RequestBody.Required {
		t.Fatalf("createPet requestBody = %+v", create.RequestBody)
	}
}

func TestParseIndexFilePath(t *testing.T) {
	t.Parallel()

	specs := map[string][]byte{
		"api/petstore.json": []byte(`{
  "openapi": "3.0.3",
  "info": {"title": "petstore", "version": "1.0.0"},
  "servers": [{"url": "https://fallback.example.com"}],
  "paths": {"/ping": {"get": {}}}
}`),
	}
	loadSpec := func(rel string) ([]byte, error) {
		raw, ok := specs[rel]
		if !ok {
			return nil, fmt.Errorf("no such spec: %s", rel)
		}
		return raw, nil
	}

	raw := []byte(`{
  "apis": {
    "petstore": {"path": "api/petstore.json"}
  }
}`)
	apis, err := parseIndexFile("/tmp/api.json", raw, loadSpec)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(apis) != 1 {
		t.Fatalf("apis = %d, want 1", len(apis))
	}
	api := apis[0]
	if api.BaseURL != "https://fallback.example.com" {
		t.Fatalf("baseUrl = %q, want document servers[0].url fallback", api.BaseURL)
	}
	if len(api.Operations) != 1 || api.Operations[0].OperationID != "get_ping" {
		t.Fatalf("operations = %+v, want auto-generated operationId", api.Operations)
	}
}

func TestParseIndexFileSpecFile(t *testing.T) {
	t.Parallel()

	specs := map[string][]byte{
		"specs/petstore.json": []byte(`{
  "servers": [{"url": "https://fallback.example.com"}],
  "paths": {"/ping": {"get": {}}}
}`),
	}
	loadSpec := func(rel string) ([]byte, error) {
		raw, ok := specs[rel]
		if !ok {
			return nil, fmt.Errorf("no such spec: %s", rel)
		}
		return raw, nil
	}

	raw := []byte(`{
  "apis": {
    "petstore": {"specFile": "specs/petstore.json"}
  }
}`)
	apis, err := parseIndexFile("/tmp/api.json", raw, loadSpec)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(apis) != 1 {
		t.Fatalf("apis = %d, want 1", len(apis))
	}
	api := apis[0]
	if api.BaseURL != "https://fallback.example.com" {
		t.Fatalf("baseUrl = %q, want spec servers[0].url fallback", api.BaseURL)
	}
	if len(api.Operations) != 1 || api.Operations[0].OperationID != "get_ping" {
		t.Fatalf("operations = %+v, want auto-generated operationId", api.Operations)
	}
}

func TestParseIndexFileBind(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
  "apis": {
    "internal": {
      "baseUrl": "https://api.example.com",
      "bind": {
        "uid": {"from": "ctx:user_id", "in": "header", "name": "X-User-Id"},
        "orgId": {"from": "ctx:metadata.org_id", "in": "query"}
      },
      "paths": {
        "/orders": {
          "get": {
            "operationId": "listOrders",
            "parameters": [
              {"name": "uid", "in": "header", "required": true, "schema": {"type": "string"}},
              {"name": "orgId", "in": "query", "required": true, "schema": {"type": "string"}},
              {"name": "page", "in": "query", "schema": {"type": "integer"}}
            ]
          }
        }
      }
    }
  }
}`)

	apis, err := parseIndexFile("/tmp/api.json", raw, noSpecLoader)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(apis) != 1 {
		t.Fatalf("apis = %d, want 1", len(apis))
	}
	api := apis[0]
	if len(api.Binds) != 2 {
		t.Fatalf("binds = %d, want 2", len(api.Binds))
	}
	if !api.isBoundParameter("header", "uid") {
		t.Fatal("uid header should be bound")
	}
	if !api.isBoundParameter("query", "orgId") {
		t.Fatal("orgId query should be bound")
	}
	if api.isBoundParameter("query", "page") {
		t.Fatal("page query should not be bound")
	}
}

func TestParseIndexFileBindValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "missing from",
			raw:  `{"apis":{"x":{"baseUrl":"https://a.example.com","bind":{"uid":{"in":"header"}},"paths":{"/p":{"get":{}}}}}}`,
		},
		{
			name: "bad from prefix",
			raw:  `{"apis":{"x":{"baseUrl":"https://a.example.com","bind":{"uid":{"from":"user_id","in":"header"}},"paths":{"/p":{"get":{}}}}}}`,
		},
		{
			name: "bad in",
			raw:  `{"apis":{"x":{"baseUrl":"https://a.example.com","bind":{"uid":{"from":"ctx:user_id","in":"cookie"}},"paths":{"/p":{"get":{}}}}}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseIndexFile("/tmp/api.json", []byte(tc.raw), noSpecLoader)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseIndexFileSpecFileResolvesSchemaRef(t *testing.T) {
	t.Parallel()

	specs := map[string][]byte{
		"specs/petstore.json": []byte(`{
  "openapi": "3.0.3",
  "info": {"title": "petstore", "version": "1.0.0"},
  "servers": [{"url": "https://api.example.com"}],
  "paths": {
    "/pets/{id}": {
      "get": {
        "operationId": "getPet",
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {"$ref": "#/components/schemas/PetId"}
          }
        ]
      }
    }
  },
  "components": {
    "schemas": {
      "PetId": {"type": "string", "description": "Pet identifier"}
    }
  }
}`),
	}
	loadSpec := func(rel string) ([]byte, error) {
		raw, ok := specs[rel]
		if !ok {
			return nil, fmt.Errorf("no such spec: %s", rel)
		}
		return raw, nil
	}

	raw := []byte(`{"apis": {"petstore": {"specFile": "specs/petstore.json"}}}`)
	apis, err := parseIndexFile("/tmp/api.json", raw, loadSpec)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(apis) != 1 || len(apis[0].Operations) != 1 {
		t.Fatalf("apis = %+v", apis)
	}
	schema := string(apis[0].Operations[0].Parameters[0].Schema)
	if !strings.Contains(schema, `"type":"string"`) {
		t.Fatalf("schema = %s, want resolved PetId schema", schema)
	}
	if strings.Contains(schema, "$ref") {
		t.Fatalf("schema still contains $ref: %s", schema)
	}
}

func TestParseIndexFileSpecFileBaseURLOverride(t *testing.T) {
	t.Parallel()

	loadSpec := func(rel string) ([]byte, error) {
		return []byte(`{"servers": [{"url": "https://spec.example.com"}], "paths": {"/ping": {"get": {}}}}`), nil
	}
	raw := []byte(`{
  "apis": {
    "petstore": {"specFile": "specs/petstore.json", "baseUrl": "https://override.example.com"}
  }
}`)
	apis, err := parseIndexFile("/tmp/api.json", raw, loadSpec)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if apis[0].BaseURL != "https://override.example.com" {
		t.Fatalf("baseUrl = %q, want entry override to win over spec", apis[0].BaseURL)
	}
}

func TestParseIndexFilePathAndInlinePathsConflict(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
  "apis": {
    "petstore": {
      "path": "api/petstore.json",
      "baseUrl": "https://api.example.com",
      "paths": {"/ping": {"get": {}}}
    }
  }
}`)
	_, err := parseIndexFile("/tmp/api.json", raw, noSpecLoader)
	if err == nil {
		t.Fatal("expected error for path + inline paths conflict")
	}
}

func TestParseIndexFileSpecFileAndInlinePathsConflict(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
  "apis": {
    "petstore": {
      "specFile": "specs/petstore.json",
      "baseUrl": "https://api.example.com",
      "paths": {"/ping": {"get": {}}}
    }
  }
}`)
	_, err := parseIndexFile("/tmp/api.json", raw, noSpecLoader)
	if err == nil {
		t.Fatal("expected error for specFile + inline paths conflict")
	}
}

func TestParseIndexFilePathAndSpecFileConflict(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
  "apis": {
    "petstore": {
      "path": "api/a.json",
      "specFile": "api/b.json",
      "baseUrl": "https://api.example.com"
    }
  }
}`)
	_, err := parseIndexFile("/tmp/api.json", raw, noSpecLoader)
	if err == nil {
		t.Fatal("expected error for mismatched path and specFile")
	}
}

func TestParseIndexFileMissingBaseURL(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"apis": {"petstore": {"paths": {"/ping": {"get": {}}}}}}`)
	_, err := parseIndexFile("/tmp/api.json", raw, noSpecLoader)
	if err == nil {
		t.Fatal("expected error for missing baseUrl")
	}
}

func TestParseIndexFileDuplicateOperationID(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
  "apis": {
    "petstore": {
      "baseUrl": "https://api.example.com",
      "paths": {
        "/a": {"get": {"operationId": "dup"}},
        "/b": {"get": {"operationId": "dup"}}
      }
    }
  }
}`)
	_, err := parseIndexFile("/tmp/api.json", raw, noSpecLoader)
	if err == nil {
		t.Fatal("expected duplicate operationId error")
	}
}

func TestParseIndexFileNoApis(t *testing.T) {
	t.Parallel()

	_, err := parseIndexFile("/tmp/api.json", []byte(`{"apis": {}}`), noSpecLoader)
	if err == nil {
		t.Fatal("expected error for empty apis index")
	}
}

func TestParseIndexFileMultipleEntries(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
  "apis": {
    "a": {"baseUrl": "https://a.example.com", "paths": {"/ping": {"get": {"operationId": "ping"}}}},
    "b": {"baseUrl": "https://b.example.com", "paths": {"/ping": {"get": {"operationId": "ping"}}}}
  }
}`)
	apis, err := parseIndexFile("/tmp/api.json", raw, noSpecLoader)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(apis) != 2 {
		t.Fatalf("apis = %d, want 2", len(apis))
	}
}

func TestLoadAPIsPrecedence(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	project := filepath.Join(dir, "api.json")
	global := filepath.Join(dir, "global-api.json")
	sharedDoc := `{"apis":{"shared":{"baseUrl":"%s","paths":{"/ping":{"get":{"operationId":"ping"}}}}}}`
	if err := os.WriteFile(project, fmt.Appendf(nil, sharedDoc, "https://a.example.com"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(global, fmt.Appendf(nil, sharedDoc, "https://b.example.com"), 0o644); err != nil {
		t.Fatal(err)
	}
	extraGlobal := filepath.Join(dir, "global-extra-api.json")
	extraDoc := `{"apis":{"extra":{"baseUrl":"https://c.example.com","paths":{"/ping":{"get":{"operationId":"ping"}}}}}}`
	if err := os.WriteFile(extraGlobal, []byte(extraDoc), 0o644); err != nil {
		t.Fatal(err)
	}

	provider := &openapiProvider{
		files:     []string{"api.json", "global:api.json", "global:extra-api.json"},
		workspace: &testWorkspace{root: dir},
	}
	apis, err := provider.loadAPIs(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(apis) != 2 {
		t.Fatalf("apis = %d, want 2", len(apis))
	}
	byName := map[string]string{}
	for _, a := range apis {
		byName[a.Name] = a.BaseURL
	}
	if byName["shared"] != "https://a.example.com" {
		t.Fatalf("shared baseUrl = %q, want project override", byName["shared"])
	}
	if byName["extra"] != "https://c.example.com" {
		t.Fatalf("extra baseUrl = %q", byName["extra"])
	}
}

type testWorkspace struct {
	root string
}

func (w *testWorkspace) Resolve(_ context.Context, rel string) (string, error) {
	switch rel {
	case "api.json":
		return filepath.Join(w.root, "api.json"), nil
	case "global:api.json":
		return filepath.Join(w.root, "global-api.json"), nil
	case "global:extra-api.json":
		return filepath.Join(w.root, "global-extra-api.json"), nil
	default:
		return filepath.Join(w.root, rel), nil
	}
}
