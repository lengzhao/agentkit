package chatapi

import (
	"net/http/httptest"
	"testing"
)

func TestFileDownloadURLFromRequest(t *testing.T) {
	p, err := New(Config{Path: "/v1/"}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	req := httptest.NewRequest("GET", "http://internal/v1/files", nil)
	req.Host = "api.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")

	base := plat.apiBaseFromRequest(req)
	if base != "https://api.example.com/v1" {
		t.Fatalf("apiBase = %q", base)
	}
	if got := plat.fileDownloadURL(base, "default_channel", "file_abc"); got != "https://api.example.com/v1/files/file_abc?channel=default_channel" {
		t.Fatalf("url = %q", got)
	}
	if got := plat.fileDownloadPath("file_abc"); got != "/v1/files/file_abc" {
		t.Fatalf("path = %q", got)
	}
}

func TestPublicBaseURLOverridesRequest(t *testing.T) {
	p, err := New(Config{
		Path:          "/v1/",
		PublicBaseURL: "https://public.example.com/api/",
	}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	req := httptest.NewRequest("GET", "http://localhost/v1/files", nil)
	base := plat.apiBaseFromRequest(req)
	if base != "https://public.example.com/api" {
		t.Fatalf("apiBase = %q", base)
	}
}
