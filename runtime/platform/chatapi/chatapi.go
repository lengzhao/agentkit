package chatapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/session"
)

const (
	defaultListenAddr = ":8030"
	defaultPath       = "/v1/"
	defaultTimeout    = 30 * time.Minute
	busyPolicyQueue   = "queue"
	busyPolicyReject  = "reject"
)

type Config struct {
	common.AgentRoutingConfig
	ListenAddr         string   `json:"listenAddr"`
	// RegisterOnly mounts routes on http.DefaultServeMux and does not listen.
	// Use with platform/http (or any plugin that serves DefaultServeMux).
	// listenAddr "-" is an alias for registerOnly.
	RegisterOnly       bool     `json:"registerOnly"`
	Path               string   `json:"path"`
	APIToken           string   `json:"apiToken"`
	UserHeader         string   `json:"userHeader"`
	UserNameHeader     string   `json:"userNameHeader"`
	ChannelHeader      string   `json:"channelHeader"`
	// MetadataHeaders lists HTTP headers copied into MessageEvent.Metadata.
	MetadataHeaders    []string `json:"metadataHeaders"`
	CORSOrigins        []string `json:"corsOrigins"`
	RequestTimeout     string   `json:"requestTimeout"`
	InteractionTimeout string   `json:"interactionTimeout"`
	BusyPolicy         string   `json:"busyPolicy"`
	MaxRuns            int      `json:"maxRuns"`
	MaxUploadSize      int64    `json:"maxUploadSize"`
	PublicBaseURL      string   `json:"publicBaseUrl"`
	DebugUI            bool     `json:"debugUi"`
	SessionsDir        string   `json:"sessionsDir"`
	// Agents lists selectable agent ids for debug UI and request validation.
	Agents []string `json:"agents"`
}

type Deps struct {
	Commands     agentkit.Commands     `json:"commands,omitempty"`
	SessionStore agentkit.SessionStore `json:"sessionStore,omitempty"`
	Workspace    workspace.Service     `json:"workspace,omitempty"`
	Agents       []agentkit.Agent      `json:"agents,omitempty"`
}

type Platform struct {
	listenAddr          string
	registerOnly        bool
	path                string
	apiToken            string
	agentID             agentkit.AgentID
	availableAgents     []string
	userHeader          string
	userNameHeader      string
	channelHeader       string
	metadataHeaders     []string
	corsOrigins         []string
	requestTimeout      time.Duration
	interactionTimeout  time.Duration
	busyPolicy          string
	maxRuns             int
	maxUploadSize       int64
	publicBaseURL       string
	debugUI             bool
	sessionStore        agentkit.SessionStore
	workspace           workspace.Service
	commands            agentkit.Commands
	sessionScope        session.SessionScope
	sessionsDirRel        string

	inbox        *common.Inbox
	conversations *conversationStore
	pending      *pendingStore
	activeByConv map[string]string
	activeMu     sync.RWMutex

	resolvedAddr string
	server       *http.Server
	cancel   context.CancelFunc
	startOnce sync.Once
}

// New registers platform/chat-api: Dify-like HTTP + SSE API for custom apps and BFFs.
func New(cfg Config, deps Deps) (agentkit.Platform, error) {
	registerOnly := cfgRegisterOnly(cfg)
	listen := strings.TrimSpace(cfg.ListenAddr)
	if registerOnly {
		listen = ""
	} else if listen == "" {
		listen = defaultListenAddr
	}
	path := normalizePath(cfg.Path)
	if path == "" {
		path = defaultPath
	}
	timeout, err := parseDuration(cfg.RequestTimeout, defaultTimeout)
	if err != nil {
		return nil, fmt.Errorf("platform/chat-api requestTimeout: %w", err)
	}
	interactionTimeout, err := parseDuration(cfg.InteractionTimeout, defaultInteractionTimeout)
	if err != nil {
		return nil, fmt.Errorf("platform/chat-api interactionTimeout: %w", err)
	}
	busy := strings.ToLower(strings.TrimSpace(cfg.BusyPolicy))
	if busy == "" {
		busy = busyPolicyQueue
	}
	if busy != busyPolicyQueue && busy != busyPolicyReject {
		return nil, fmt.Errorf("platform/chat-api busyPolicy must be queue or reject")
	}
	userHeader := strings.TrimSpace(cfg.UserHeader)
	if userHeader == "" {
		userHeader = defaultUserHeader
	}
	channelHeader := strings.TrimSpace(cfg.ChannelHeader)
	if channelHeader == "" {
		channelHeader = defaultChannelHeader
	}
	userNameHeader := strings.TrimSpace(cfg.UserNameHeader)
	if userNameHeader == "" {
		userNameHeader = defaultUserNameHeader
	}
	maxRuns := cfg.MaxRuns
	if maxRuns <= 0 {
		maxRuns = defaultMaxRuns
	}
	maxUpload := cfg.MaxUploadSize
	if maxUpload <= 0 {
		maxUpload = defaultMaxUploadSize
	}

	sessionsDir := strings.TrimSpace(cfg.SessionsDir)
	if sessionsDir == "" {
		sessionsDir = defaultSessionsDir
	}

	p := &Platform{
		listenAddr:         listen,
		registerOnly:       registerOnly,
		path:               path,
		apiToken:           strings.TrimSpace(cfg.APIToken),
		agentID:            cfg.ResolveAgentID(),
		availableAgents:    collectAgentIDs(cfg.Agents, deps.Agents),
		userHeader:         userHeader,
		userNameHeader:     userNameHeader,
		channelHeader:      channelHeader,
		metadataHeaders:    resolveMetadataHeaders(cfg.MetadataHeaders),
		corsOrigins:        cfg.CORSOrigins,
		requestTimeout:     timeout,
		interactionTimeout: interactionTimeout,
		busyPolicy:         busy,
		maxRuns:            maxRuns,
		maxUploadSize:      maxUpload,
		publicBaseURL:      strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/"),
		inbox:              common.NewInbox(128),
		conversations:      newConversationStore(),
		pending:            newPendingStore(maxRuns),
		activeByConv:       make(map[string]string),
		debugUI:            cfg.DebugUI,
		sessionStore:       deps.SessionStore,
		workspace:          deps.Workspace,
		commands:           deps.Commands,
		sessionScope:       session.ParseScope(cfg.SessionScope),
		sessionsDirRel:       sessionsDir,
	}
	if registerOnly {
		p.registerDefaultHTTP()
	}
	return p, nil
}

func (p *Platform) PlatformID() string { return "chat-api" }

func (p *Platform) PermissionCapability() permission.Capability {
	return permission.Capability{
		Interactive:    true,
		DefaultTimeout: p.interactionTimeout,
		AnswerScope:    permission.ScopeAsker,
	}
}

func (p *Platform) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
	p.startOnce.Do(func() {
		if p.registerOnly {
			return
		}
		runCtx, cancel := context.WithCancel(ctx)
		p.cancel = cancel
		go p.serve(runCtx)
	})
	event, err := p.inbox.Receive(ctx)
	if err != nil {
		return agentkit.MessageEvent{}, err
	}
	if event.Message.Role == "" && len(event.Reply) == 0 {
		return agentkit.MessageEvent{}, nil
	}
	return event, nil
}

func (p *Platform) Send(ctx context.Context, event agentkit.OutboundEvent) error {
	return p.handleOutbound(ctx, event)
}

func (p *Platform) serve(ctx context.Context) {
	p.server = &http.Server{
		Addr:              p.listenAddr,
		Handler:           p.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ln, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		slog.Error("chat-api: listen failed", "addr", p.listenAddr, "err", err)
		return
	}
	p.resolvedAddr = ln.Addr().String()
	slog.Info("chat-api: server started", "addr", p.resolvedAddr, "path", p.path)
	if p.debugUI {
		slog.Info("chat-api: debug UI enabled", "url", "http://"+p.resolvedAddr+"/debug/")
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.server.Shutdown(shutdownCtx)
	}()
	if err := p.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("chat-api: serve", "err", err)
	}
}

func (p *Platform) routes() http.Handler {
	mux := http.NewServeMux()
	p.mountRoutes(mux)
	return mux
}

func (p *Platform) mountRoutes(mux *http.ServeMux) {
	wrap := func(h http.HandlerFunc) http.HandlerFunc {
		return p.corsHTTP(p.authHTTP(h))
	}
	mux.HandleFunc(p.path+"conversations", wrap(p.handleConversations))
	mux.HandleFunc(p.path+"conversations/", wrap(p.handleConversationSub))
	mux.HandleFunc(p.path+"chat-messages", wrap(p.handleChatMessages))
	mux.HandleFunc(p.path+"files", wrap(p.handleFiles))
	mux.HandleFunc(p.path+"files/", wrap(p.handleFileRoutes))
	mux.HandleFunc(p.path+"runs/", wrap(p.handleRunRoutes))
	p.registerDebugUI(mux)
}

func (p *Platform) registerDefaultHTTP() {
	http.Handle("/", p.routes())
	slog.Info("chat-api: routes registered on http.DefaultServeMux", "path", p.path)
	if p.debugUI {
		slog.Info("chat-api: debug UI enabled", "url", "/debug/")
	}
}

func cfgRegisterOnly(cfg Config) bool {
	return cfg.RegisterOnly || strings.TrimSpace(cfg.ListenAddr) == "-"
}

func normalizePath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultPath
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	if !strings.HasSuffix(raw, "/") {
		raw += "/"
	}
	return raw
}

func parseDuration(raw string, fallback time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	return d, nil
}

var (
	_ agentkit.Platform          = (*Platform)(nil)
	_ permission.Capable         = (*Platform)(nil)
)
