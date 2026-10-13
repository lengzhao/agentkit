package rpcplatform

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

const platformID = "rpc"

// Config is the Pi RPC session Centaur already knows about.
// Model and provider are echoed on get_state and checked on set_model.
type Config struct {
	// SessionFile is the --session path. Its basename (without .jsonl) is the Pi session id.
	SessionFile string `json:"sessionFile"`
	// Model is the initial model id.
	Model string `json:"model"`
	// ModelProvider is config.modelProvider. set_model rejects any other provider.
	ModelProvider string `json:"modelProvider"`
	// BaseURL is config.baseUrl, echoed on get_state.
	BaseURL string `json:"baseUrl"`
	// WireAPI is config.wireApi, echoed as model.api.
	WireAPI string `json:"wireApi"`
	// AllowedModels lists model ids set_model may select, besides Model.
	// An empty map allows only Model.
	AllowedModels map[string]json.RawMessage `json:"allowedModels,omitempty"`
}

// Deps holds optional capabilities. SessionStore is used when it implements
// SessionRuntimeStore, so set_model writes the same bind as /model.
type Deps struct {
	SessionStore agentkit.SessionStore `json:"sessionStore,omitempty"`
}

// Platform speaks Centaur Pi RPC on stdin/stdout.
// stdout is protocol only. Logs go to stderr.
type Platform struct {
	cfg      Config
	runtime  agentkit.SessionRuntimeStore
	in       io.Reader
	out      io.Writer
	inbox    *common.Inbox
	session  string
	delivery agentkit.SessionID

	mu      sync.Mutex
	started bool
	done    chan struct{}
	asm     *assembler
	modelID string
	effort  string
	cancel  func()
}

// New registers platform/rpc: stdio Pi RPC for a harness that already speaks Pi blocks.
func New(cfg Config, deps Deps) (agentkit.Platform, error) {
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.ModelProvider = strings.TrimSpace(cfg.ModelProvider)
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.WireAPI = strings.TrimSpace(cfg.WireAPI)
	cfg.SessionFile = strings.TrimSpace(cfg.SessionFile)
	if cfg.Model == "" || cfg.ModelProvider == "" || cfg.BaseURL == "" || cfg.WireAPI == "" || cfg.SessionFile == "" {
		return nil, fmt.Errorf("platform/rpc requires model, modelProvider, baseUrl, wireApi, and sessionFile")
	}
	abs, err := filepath.Abs(cfg.SessionFile)
	if err != nil {
		return nil, fmt.Errorf("platform/rpc sessionFile: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, fmt.Errorf("platform/rpc session dir: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("platform/rpc sessionFile: %w", err)
		}
		if err := os.WriteFile(abs, nil, 0o644); err != nil {
			return nil, fmt.Errorf("platform/rpc sessionFile: %w", err)
		}
	}
	cfg.SessionFile = abs
	var runtime agentkit.SessionRuntimeStore
	if deps.SessionStore != nil {
		runtime, _ = deps.SessionStore.(agentkit.SessionRuntimeStore)
	}
	return &Platform{
		cfg:      cfg,
		runtime:  runtime,
		in:       os.Stdin,
		out:      os.Stdout,
		inbox:    common.NewInbox(64),
		session:  sessionIDFromFile(abs),
		delivery: rctx.BuildDeliverySessionID(platformID, sessionIDFromFile(abs), "", ""),
		done:     make(chan struct{}),
		asm:      newAssembler(cfg.Model),
		modelID:  cfg.Model,
	}, nil
}

func sessionIDFromFile(path string) string {
	base := filepath.Base(path)
	id := strings.TrimSuffix(base, filepath.Ext(base))
	if id == "" {
		return base
	}
	return id
}

func (p *Platform) PlatformID() string { return platformID }

func (p *Platform) PermissionCapability() permission.Capability {
	return permission.Capability{Interactive: false}
}

func (p *Platform) start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return
	}
	p.started = true
	go p.readLoop()
	slog.Info("platform/rpc: listening on stdin")
}

func (p *Platform) readLoop() {
	sc := bufio.NewScanner(p.in)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if err := p.handleLine(line); err != nil {
			slog.Error("platform/rpc: command", "err", err)
		}
	}
	if err := sc.Err(); err != nil {
		slog.Error("platform/rpc: stdin", "err", err)
	}
	close(p.done)
}

func (p *Platform) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
	p.start()
	eventCh := make(chan agentkit.MessageEvent, 1)
	errCh := make(chan error, 1)
	go func() {
		event, err := p.inbox.Receive(ctx)
		if err != nil {
			errCh <- err
			return
		}
		eventCh <- event
	}()
	select {
	case <-ctx.Done():
		return agentkit.MessageEvent{}, ctx.Err()
	case <-p.done:
		select {
		case event := <-eventCh:
			return event, nil
		default:
			return agentkit.MessageEvent{}, io.EOF
		}
	case err := <-errCh:
		return agentkit.MessageEvent{}, err
	case event := <-eventCh:
		return event, nil
	}
}

func (p *Platform) Send(ctx context.Context, event agentkit.OutboundEvent) error {
	if event.Type == agentkit.EventTurnStart {
		p.captureCancel(ctx)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.writeAll(p.asm.consume(event))
}

func (p *Platform) captureCancel(ctx context.Context) {
	ctrl, _ := ctx.Value(agentkit.KeySessionControl).(interface {
		Cancel(context.Context, string) error
	})
	if ctrl == nil {
		return
	}
	p.mu.Lock()
	p.cancel = func() { _ = ctrl.Cancel(context.Background(), "aborted") }
	abort := p.asm.abort
	p.mu.Unlock()
	if abort {
		p.cancel()
	}
}

func (p *Platform) handleLine(line string) error {
	var cmd inboundCommand
	if err := json.Unmarshal([]byte(line), &cmd); err != nil {
		slog.Error("platform/rpc: invalid json", "err", err)
		return nil
	}
	p.mu.Lock()
	var cancel func()
	var err error
	switch cmd.Type {
	case "get_state":
		err = p.writeAll([]any{p.getState(cmd)})
	case "set_model":
		err = p.writeAll([]any{p.setModel(cmd)})
	case "set_thinking_level":
		err = p.writeAll([]any{p.setThinking(cmd)})
	case "prompt":
		err = p.prompt(cmd)
	case "steer":
		err = p.steer(cmd)
	case "abort":
		cancel, err = p.abort(cmd)
	default:
		err = p.writeAll([]any{wireResponse{
			Type:    "response",
			ID:      cmd.ID,
			Command: cmd.Type,
			Success: false,
		}})
	}
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return err
}

func (p *Platform) getState(cmd inboundCommand) wireResponse {
	resp := wireResponse{Type: "response", ID: cmd.ID, Command: "get_state", Success: false}
	if p.asm.busy() {
		return resp
	}
	resp.Success = true
	resp.Data = stateData{
		SessionFile:         p.cfg.SessionFile,
		IsStreaming:         false,
		PendingMessageCount: 0,
		SessionID:           p.session,
		Model: stateModel{
			ID:       p.modelID,
			Provider: p.cfg.ModelProvider,
			API:      p.cfg.WireAPI,
			BaseURL:  p.cfg.BaseURL,
		},
	}
	return resp
}

func (p *Platform) setModel(cmd inboundCommand) wireResponse {
	resp := wireResponse{Type: "response", ID: cmd.ID, Command: "set_model", Success: false}
	if cmd.Provider != p.cfg.ModelProvider || !p.allows(cmd.ModelID) {
		return resp
	}
	if p.runtime != nil {
		if err := p.runtime.SetModelBind(context.Background(), p.delivery, cmd.ModelID); err != nil {
			slog.Error("platform/rpc: set model bind", "err", err)
			return resp
		}
	}
	p.modelID = cmd.ModelID
	p.asm.setModel(cmd.ModelID)
	resp.Success = true
	return resp
}

func (p *Platform) allows(modelID string) bool {
	if modelID == "" {
		return false
	}
	if modelID == p.cfg.Model {
		return true
	}
	_, ok := p.cfg.AllowedModels[modelID]
	return ok
}

func (p *Platform) setThinking(cmd inboundCommand) wireResponse {
	resp := wireResponse{Type: "response", ID: cmd.ID, Command: "set_thinking_level", Success: false}
	switch cmd.Level {
	case "off", "minimal", "low", "medium", "high", "xhigh":
	default:
		return resp
	}
	p.effort = cmd.Level
	resp.Success = true
	return resp
}

func (p *Platform) prompt(cmd inboundCommand) error {
	resp := wireResponse{Type: "response", ID: cmd.ID, Command: "prompt", Success: false}
	if p.asm.busy() {
		return p.writeAll([]any{resp})
	}
	resp.Success = true
	if err := p.writeAll([]any{resp}); err != nil {
		return err
	}
	p.asm.accept(cmd.ID)
	return p.enqueue(cmd.Message)
}

func (p *Platform) steer(cmd inboundCommand) error {
	resp := wireResponse{Type: "response", ID: cmd.ID, Command: "steer", Success: false}
	if p.asm.phase == "idle" || p.asm.suppress {
		return p.writeAll([]any{resp})
	}
	resp.Success = true
	if err := p.writeAll([]any{resp}); err != nil {
		return err
	}
	return p.enqueue(cmd.Message)
}

func (p *Platform) abort(cmd inboundCommand) (func(), error) {
	resp := wireResponse{Type: "response", ID: cmd.ID, Command: "abort", Success: false}
	if !p.asm.busy() {
		return nil, p.writeAll([]any{resp})
	}
	resp.Success = true
	if err := p.writeAll([]any{resp}); err != nil {
		return nil, err
	}
	p.asm.abort = true
	extra := p.asm.finishAborted()
	if err := p.writeAll(extra); err != nil {
		return nil, err
	}
	return p.cancel, nil
}

func (p *Platform) enqueue(text string) error {
	event := agentkit.MessageEvent{
		PlatformID: platformID,
		Message: agentkit.ModelMessage{
			Role: "user",
			Content: []agentkit.ContentPart{{
				Type: "text",
				Text: text,
			}},
		},
	}
	event = rctx.WithDeliverySession(event, platformID, p.delivery)
	return p.inbox.Push(context.Background(), event)
}

func (p *Platform) writeAll(events []any) error {
	if len(events) == 0 {
		return nil
	}
	enc := json.NewEncoder(p.out)
	enc.SetEscapeHTML(false)
	for _, event := range events {
		if err := enc.Encode(event); err != nil {
			return err
		}
	}
	return nil
}
