package chatapi

import (
	"net/http/httptest"
	"testing"
)

func TestResolveMetadataHeadersDefaultsTaskID(t *testing.T) {
	got := resolveMetadataHeaders(nil)
	if len(got) != 1 || got[0] != defaultMetadataHeaderTaskID {
		t.Fatalf("got %v want [%s]", got, defaultMetadataHeaderTaskID)
	}
	got = resolveMetadataHeaders([]string{"X-Org-Id", "x-task-id"})
	if len(got) != 2 {
		t.Fatalf("got %v want 2 entries", got)
	}
}

func TestNormalizeMetadataHeaders(t *testing.T) {
	got := normalizeMetadataHeaders([]string{" X-Org-Id ", "", "X-Org-Id", "X-Tenant"})
	want := []string{"X-Org-Id", "X-Tenant"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestMetadataFromRequestWhitelist(t *testing.T) {
	p, err := New(Config{
		MetadataHeaders: []string{"X-Org-Id", "X-Chat-API-User-Name"},
	}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	req := httptest.NewRequest("POST", "/v1/chat-messages", nil)
	req.Header.Set("X-Org-Id", "org-7")
	req.Header.Set("X-Chat-API-User-Name", "Alice")
	req.Header.Set("X-Secret", "ignored")

	md := plat.metadataFromRequest(req)
	if md == nil {
		t.Fatal("metadata is nil")
	}
	if md["X-Org-Id"] != "org-7" {
		t.Fatalf("org = %v", md["X-Org-Id"])
	}
	if md["X-Chat-API-User-Name"] != "Alice" {
		t.Fatalf("name = %v", md["X-Chat-API-User-Name"])
	}
	if _, ok := md["X-Secret"]; ok {
		t.Fatalf("unexpected secret metadata: %v", md)
	}
}

func TestMetadataFromRequestSkipsEmpty(t *testing.T) {
	p, err := New(Config{MetadataHeaders: []string{"X-Org-Id"}}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	req := httptest.NewRequest("POST", "/v1/chat-messages", nil)
	if md := plat.metadataFromRequest(req); md != nil {
		t.Fatalf("expected nil metadata, got %v", md)
	}
}

func TestRequestMetadataIncludesUserNameByDefault(t *testing.T) {
	p, err := New(Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	req := httptest.NewRequest("POST", "/v1/chat-messages", nil)
	req.Header.Set("X-Chat-API-User-Name", "Alice")
	md := plat.requestMetadata(req)
	if md == nil {
		t.Fatal("metadata is nil")
	}
	if md["X-Chat-API-User-Name"] != "Alice" {
		t.Fatalf("name = %v", md["X-Chat-API-User-Name"])
	}
}

func TestRequestMetadataIncludesTaskIDAndUserName(t *testing.T) {
	p, err := New(Config{MetadataHeaders: []string{"x-task-id"}}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	req := httptest.NewRequest("POST", "/v1/chat-messages", nil)
	req.Header.Set("x-task-id", "task-42")
	req.Header.Set("X-Chat-API-User-Name", "Bob")
	md := plat.requestMetadata(req)
	if md == nil {
		t.Fatal("metadata is nil")
	}
	if md["x-task-id"] != "task-42" {
		t.Fatalf("task-id = %v", md["x-task-id"])
	}
	if md["X-Chat-API-User-Name"] != "Bob" {
		t.Fatalf("name = %v", md["X-Chat-API-User-Name"])
	}
}

func TestCORSAllowedHeadersIncludesMetadataHeaders(t *testing.T) {
	p, err := New(Config{
		MetadataHeaders: []string{"X-Org-Id", "X-Org-Id"},
	}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	headers := plat.corsAllowedHeaders()
	seen := make(map[string]bool, len(headers))
	for _, h := range headers {
		seen[h] = true
	}
	for _, want := range []string{"Authorization", "X-Chat-API-User", "X-Org-Id"} {
		if !seen[want] {
			t.Fatalf("missing %q in %v", want, headers)
		}
	}
}
