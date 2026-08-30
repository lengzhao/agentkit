package httphost

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestHTTPHostServesDefaultServeMux(t *testing.T) {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})

	p, err := New(Config{ListenAddr: ":0"}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	plat := p.(*Platform)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_, _ = plat.Receive(ctx)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if plat.resolvedAddr != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if plat.resolvedAddr == "" {
		t.Fatal("server did not start")
	}

	resp, err := http.Get("http://" + plat.resolvedAddr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q, want ok", string(body))
	}
}
