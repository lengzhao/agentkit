package chatapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegisterOnlyMountsDefaultServeMux(t *testing.T) {
	const path = "/v1/"
	p, err := New(Config{RegisterOnly: true, Path: path}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)
	if !plat.registerOnly {
		t.Fatal("expected registerOnly")
	}
	if plat.listenAddr != "" {
		t.Fatalf("listenAddr = %q, want empty", plat.listenAddr)
	}
	if !cfgRegisterOnly(Config{ListenAddr: "-"}) {
		t.Fatal("listenAddr '-' should set registerOnly")
	}

	server := httptest.NewUnstartedServer(http.DefaultServeMux)
	server.Start()
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+path+"conversations", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Chat-API-Channel", "default_channel")
	req.Header.Set("X-Chat-API-User", "u1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
