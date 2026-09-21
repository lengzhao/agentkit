package credentials

import (
	"testing"
)

func TestManifestFromMCPFile(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
  "mcpServers": {
    "github": {
      "command": "npx",
      "env": {"TOKEN": "env:GITHUB_TOKEN"},
      "headers": {"X-Key": "env:SHARED_KEY"}
    },
    "plain": {"command": "echo", "args": ["hi"]}
  }
}`)
	got, err := manifestFromMCPFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	keys := got["mcp.github"]
	if len(keys) != 2 {
		t.Fatalf("keys=%v, want GITHUB_TOKEN and SHARED_KEY", keys)
	}
	if _, ok := keys["GITHUB_TOKEN"]; !ok {
		t.Fatal("missing GITHUB_TOKEN")
	}
	if _, ok := keys["SHARED_KEY"]; !ok {
		t.Fatal("missing SHARED_KEY")
	}
	if len(got["plain"]) != 0 {
		t.Fatal("plain server should not appear")
	}
}

func TestManifestFromAPIIndex(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
  "apis": {
    "petstore": {
      "path": "api/pet.json",
      "baseUrl": "https://example.com",
      "auth": {"type": "bearer", "token": "env:PETSTORE_TOKEN"},
      "headers": {"X-Tenant": "env:TENANT_ID"}
    }
  }
}`)
	got, err := manifestFromAPIIndex(raw)
	if err != nil {
		t.Fatal(err)
	}
	keys := got["openapi.petstore"]
	if len(keys) != 2 {
		t.Fatalf("keys=%v", keys)
	}
}

func TestManifestFromShellBashFile(t *testing.T) {
	t.Parallel()
	data := []byte(`{
  "commands": {
    "gh": {"env": ["GH_TOKEN", "GITHUB_TOKEN"]},
    "npm": {"env": ["NPM_TOKEN"]}
  }
}`)
	got, err := manifestFromShellBashFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got["shell-bash.gh"]) != 2 {
		t.Fatalf("gh keys=%v", got["shell-bash.gh"])
	}
	if _, ok := got["shell-bash.gh"]["GH_TOKEN"]; !ok {
		t.Fatal("missing GH_TOKEN")
	}
}
