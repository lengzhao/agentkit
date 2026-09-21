package openapi

import "testing"

func TestCredentialScope(t *testing.T) {
	t.Parallel()
	if got := credentialScope("example1"); got != "openapi.example1" {
		t.Fatalf("got %q", got)
	}
	if credentialScope("  pet  ") != "openapi.pet" {
		t.Fatal("should trim api name")
	}
}
