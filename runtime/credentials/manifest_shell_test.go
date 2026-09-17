package credentials

import (
	"testing"
)

func TestValidateIntegrationScope_shellBash(t *testing.T) {
	t.Parallel()
	if err := ValidateIntegrationScope("shell-bash.gh"); err != nil {
		t.Fatalf("shell-bash.gh: %v", err)
	}
	if err := ValidateIntegrationScope("shell-bash."); err == nil {
		t.Fatal("shell-bash. should be rejected")
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
	got, err := ManifestFromShellBashFile(data)
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
