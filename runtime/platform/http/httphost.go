package httphost

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
)

const defaultListenAddr = ":8080"

type Config struct {
	ListenAddr string `json:"listenAddr"`
}

type Deps struct{}

// Platform serves http.DefaultServeMux. Other plugins (e.g. platform/chat-api with
// registerOnly) mount routes via http.Handle before this platform starts listening.
type Platform struct {
	listenAddr   string
	resolvedAddr string
	server       *http.Server
	cancel       context.CancelFunc
	startOnce    sync.Once
}

// New registers platform/http: HTTP host that serves http.DefaultServeMux.
func New(cfg Config, deps Deps) (agentkit.Platform, error) {
	listen := strings.TrimSpace(cfg.ListenAddr)
	if listen == "" {
		listen = defaultListenAddr
	}
	return &Platform{listenAddr: listen}, nil
}

func (p *Platform) PlatformID() string { return "http" }

func (p *Platform) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
	p.startOnce.Do(func() {
		runCtx, cancel := context.WithCancel(ctx)
		p.cancel = cancel
		go p.serve(runCtx)
	})
	<-ctx.Done()
	return agentkit.MessageEvent{}, ctx.Err()
}

func (p *Platform) Send(context.Context, agentkit.OutboundEvent) error {
	return nil
}

func (p *Platform) serve(ctx context.Context) {
	p.server = &http.Server{
		Addr:              p.listenAddr,
		ReadHeaderTimeout: 5 * time.Second,
	}
	ln, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		slog.Error("http: listen failed", "addr", p.listenAddr, "err", err)
		return
	}
	p.resolvedAddr = ln.Addr().String()
	slog.Info("http: server started", "addr", p.resolvedAddr)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.server.Shutdown(shutdownCtx)
	}()
	if err := p.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("http: serve", "err", err)
	}
}

var _ agentkit.Platform = (*Platform)(nil)
