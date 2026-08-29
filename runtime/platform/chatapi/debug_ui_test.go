package chatapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDebugUIDisabledByDefault(t *testing.T) {
	p, err := New(Config{APIToken: "secret"}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)
	req := httptest.NewRequest(http.MethodGet, "/debug/", nil)
	rec := httptest.NewRecorder()
	plat.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when debug UI disabled", rec.Code)
	}
}

func TestDebugUIServesPage(t *testing.T) {
	p, err := New(Config{
		APIToken: "secret",
		Path:     "/v1/",
		DebugUI:  true,
	}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	req := httptest.NewRequest(http.MethodGet, "/debug/", nil)
	rec := httptest.NewRecorder()
	plat.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "AgentKit") {
		t.Fatalf("missing AgentKit branding in HTML shell")
	}
	if !strings.Contains(body, `window.__CHAT_API_PATH__=`) || !strings.Contains(body, `"/v1/"`) {
		t.Fatalf("missing injected api path")
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}

	req2 := httptest.NewRequest(http.MethodGet, "/debug/config.json", nil)
	rec2 := httptest.NewRecorder()
	plat.routes().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("config status = %d", rec2.Code)
	}
	if !strings.Contains(rec2.Body.String(), `"project":"platformPlugin"`) {
		t.Fatalf("config body = %s", rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), `"agents"`) {
		t.Fatalf("config body missing agents: %s", rec2.Body.String())
	}
}

func TestDebugRedirect(t *testing.T) {
	p, err := New(Config{DebugUI: true}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)
	req := httptest.NewRequest(http.MethodGet, "/debug", nil)
	rec := httptest.NewRecorder()
	plat.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Location") != "/debug/" {
		t.Fatalf("location = %q", rec.Header().Get("Location"))
	}
}
