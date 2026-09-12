package openapi

import "testing"

func TestCredentialScope(t *testing.T) {
	t.Parallel()
	if got := CredentialScope("example1"); got != "openapi.example1" {
		t.Fatalf("got %q", got)
	}
	if CredentialScope("  pet  ") != "openapi.pet" {
		t.Fatal("should trim api name")
	}
}
