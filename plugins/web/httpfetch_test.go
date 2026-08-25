package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	capweb "github.com/lengzhao/agentkit/cap/web"
)

// httptest always listens on loopback, which the default guard refuses. Tests
// that need a live server opt in explicitly — which is also the assertion that
// the guard is on by default.
func testFetcher(t *testing.T, cfg FetchConfig) capweb.Fetcher {
	t.Helper()
	cfg.AllowPrivateHosts = true
	f, err := NewFetcher(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestFetchExtractsTextAndTitle(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><head><title>Doc</title></head><body><h1>Hi</h1><p>body text</p><script>junk()</script></body></html>`)
	}))
	defer srv.Close()

	got, err := testFetcher(t, FetchConfig{}).Fetch(context.Background(), capweb.FetchRequest{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Doc" {
		t.Errorf("title = %q", got.Title)
	}
	if !strings.Contains(got.Content, "body text") || strings.Contains(got.Content, "junk()") {
		t.Errorf("content = %q", got.Content)
	}
	if got.Status != 200 || got.Bytes == 0 {
		t.Errorf("result = %+v", got)
	}
}

func TestFetchRawSkipsExtraction(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<p>hi</p>")
	}))
	defer srv.Close()

	got, err := testFetcher(t, FetchConfig{}).Fetch(context.Background(), capweb.FetchRequest{URL: srv.URL, Raw: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "<p>hi</p>" {
		t.Errorf("raw content = %q, want the markup as served", got.Content)
	}
}

func TestFetchTruncatesAtMaxBytes(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, strings.Repeat("x", 5000))
	}))
	defer srv.Close()

	got, err := testFetcher(t, FetchConfig{MaxBytes: 100}).Fetch(context.Background(), capweb.FetchRequest{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || got.Bytes != 100 {
		t.Errorf("result = %+v, want truncated at 100 bytes", got)
	}

	// A per-call limit may tighten the provider default, never loosen it.
	got, err = testFetcher(t, FetchConfig{MaxBytes: 100}).Fetch(context.Background(), capweb.FetchRequest{URL: srv.URL, MaxBytes: 4000})
	if err != nil {
		t.Fatal(err)
	}
	if got.Bytes != 100 {
		t.Errorf("bytes = %d, want the provider limit to win", got.Bytes)
	}
}

func TestFetchReportsNonTextInsteadOfReturningBytes(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte{0x89, 'P', 'N', 'G'})
	}))
	defer srv.Close()

	got, err := testFetcher(t, FetchConfig{}).Fetch(context.Background(), capweb.FetchRequest{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Content, "[non-text content:") {
		t.Errorf("content = %q, want a placeholder", got.Content)
	}
}

func TestFetchStopsRedirectLoop(t *testing.T) {
	t.Parallel()

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/next", http.StatusFound)
	}))
	defer srv.Close()

	_, err := testFetcher(t, FetchConfig{MaxRedirects: 2}).Fetch(context.Background(), capweb.FetchRequest{URL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("err = %v, want a redirect limit error", err)
	}
}

func TestFetchRefusesPrivateAddressesByDefault(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "should never be read")
	}))
	defer srv.Close()

	f, err := NewFetcher(FetchConfig{})
	if err != nil {
		t.Fatal(err)
	}
	// The URL check catches the literal 127.0.0.1 that httptest hands back.
	if _, err := f.Fetch(context.Background(), capweb.FetchRequest{URL: srv.URL}); err == nil {
		t.Fatal("fetched a loopback address with the default config")
	}
	for _, target := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/admin",
		"http://[::1]:8080/",
		"http://0.0.0.0/",
	} {
		if _, err := f.Fetch(context.Background(), capweb.FetchRequest{URL: target}); err == nil {
			t.Errorf("fetched %s with the default config", target)
		}
	}
}

func TestFetchRejectsSchemeAndHostRules(t *testing.T) {
	t.Parallel()

	f, err := NewFetcher(FetchConfig{
		AllowPrivateHosts: true,
		AllowHosts:        []string{"example.com"},
		DenyHosts:         []string{"secret.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"file:///etc/passwd":           "scheme",
		"ftp://example.com/x":          "scheme",
		"https://elsewhere.test/":      "allowHosts",
		"https://secret.example.com/x": "denyHosts",
	}
	for target, want := range cases {
		_, err := f.Fetch(context.Background(), capweb.FetchRequest{URL: target})
		if err == nil {
			t.Errorf("%s was allowed", target)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want mention of %q", target, err, want)
		}
	}
	// Subdomains of an allowHosts entry pass the rules. Asserted on checkURL so
	// the test never makes a real request.
	if err := f.(*Fetcher).checkURL(mustParseURL(t, "https://api.example.com/")); err != nil {
		t.Errorf("subdomain of an allowHosts entry was refused: %v", err)
	}
}

func TestFetchRequiresURL(t *testing.T) {
	t.Parallel()

	if _, err := testFetcher(t, FetchConfig{}).Fetch(context.Background(), capweb.FetchRequest{}); err == nil {
		t.Fatal("empty url was accepted")
	}
}

func TestIsPrivateIPClassification(t *testing.T) {
	t.Parallel()

	private := []string{"127.0.0.1", "10.1.2.3", "192.168.1.1", "172.16.0.1", "169.254.169.254",
		"::1", "fd00::1", "fc00::1", "0.0.0.0", "100.64.0.1", "224.0.0.1", "::ffff:127.0.0.1"}
	public := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700::1111"}

	for _, s := range private {
		if !isPrivateIP(parseIP(t, s)) {
			t.Errorf("%s classified as public", s)
		}
	}
	for _, s := range public {
		if isPrivateIP(parseIP(t, s)) {
			t.Errorf("%s classified as private", s)
		}
	}
}

func parseIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("cannot parse %q", s)
	}
	return ip
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}
