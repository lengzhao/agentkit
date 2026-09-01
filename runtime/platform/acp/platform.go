package acpplatform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/session"
)

const platformID = "acp"

// Config configures the ACP agent platform (stdio JSON-RPC).
type Config struct {
	// AgentName is reported in initialize (default "agentkit").
	AgentName string `json:"agentName"`
	// AgentVersion is reported in initialize (default "0.1.0").
	AgentVersion string `json:"agentVersion"`
}

type Deps struct {
	Workspace workspace.Service `json:"workspace,omitempty"`
}

// Platform exposes AgentKit as an ACP agent over stdin/stdout.
type Platform struct {
	cfg       Config
	workspace workspace.Service
	inbox     *common.Inbox

	mu       sync.Mutex
	conn     *acp.AgentSideConnection
	agent    *agentBridge
	sessions map[acp.SessionId]*sessionState
	started  bool
}

// New registers platform/acp: stdio ACP agent for editors and ACP clients.
func New(cfg Config, deps Deps) (agentkit.Platform, error) {
	name := strings.TrimSpace(cfg.AgentName)
	if name == "" {
		name = "agentkit"
	}
	version := strings.TrimSpace(cfg.AgentVersion)
	if version == "" {
		version = "0.1.0"
	}
	return &Platform{
		cfg: Config{
			AgentName:    name,
			AgentVersion: version,
		},
		workspace: deps.Workspace,
		inbox:     common.NewInbox(64),
		sessions:  make(map[acp.SessionId]*sessionState),
	}, nil
}

func (p *Platform) PlatformID() string { return platformID }

func (p *Platform) PermissionCapability() permission.Capability {
	return permission.Capability{
		Interactive:    true,
		DefaultTimeout: permission.DefaultTimeout,
		AnswerScope:    permission.ScopeAnyone,
	}
}

func (p *Platform) ensureStarted() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return
	}
	bridge := &agentBridge{platform: p}
	p.agent = bridge
	p.conn = acp.NewAgentSideConnection(bridge, os.Stdout, os.Stdin)
	p.conn.SetLogger(slog.Default())
	bridge.setConn(p.conn)
	p.started = true
	slog.Info("platform/acp: listening on stdin/stdout")
}

func (p *Platform) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
	p.ensureStarted()
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
	case <-p.conn.Done():
		return agentkit.MessageEvent{}, io.EOF
	case err := <-errCh:
		return agentkit.MessageEvent{}, err
	case event := <-eventCh:
		return event, nil
	}
}

func (p *Platform) Send(ctx context.Context, event agentkit.OutboundEvent) error {
	if p.conn == nil {
		return nil
	}
	sess := p.sessionByDeliveryID(event.SessionID)
	if sess == nil {
		return nil
	}
	emit := &turnEmitter{
		platform:     p,
		acpSessionID: sess.acpSessionID,
		conn:         p.conn,
	}
	switch event.Type {
	case agentkit.EventTurnStart:
		if ctrl, ok := ctx.Value(agentkit.KeySessionControl).(turnCanceller); ok && ctrl != nil {
			sess.setCancelTurn(func() {
				_ = ctrl.Cancel(ctx, "acp session/cancel")
			})
		}
		return nil
	case agentkit.EventMessageUpdate:
		var payload agentkit.MessageUpdatePayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		return emit.handleUpdate(ctx, payload)
	case agentkit.EventMessageEnd:
		return nil
	case agentkit.EventTurnEnd:
		sess.completeTurn(turnResult{stopReason: acp.StopReasonEndTurn})
		return nil
	case agentkit.EventPermissionRequest:
		return p.handlePermission(ctx, sess, event.Data)
	case agentkit.EventPermissionResolved:
		return nil
	case "error":
		var payload struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(event.Data, &payload); err == nil && payload.Error != "" {
			sess.completeTurn(turnResult{err: fmt.Errorf("%s", payload.Error)})
		}
		return nil
	default:
		return nil
	}
}

func (p *Platform) sessionByDeliveryID(id agentkit.SessionID) *sessionState {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, sess := range p.sessions {
		if sess.deliveryID == id {
			return sess
		}
	}
	return nil
}

func (p *Platform) getOrCreateSession(acpID acp.SessionId) *sessionState {
	p.mu.Lock()
	defer p.mu.Unlock()
	if sess, ok := p.sessions[acpID]; ok {
		return sess
	}
	deliveryID := session.BuildDeliverySessionID(platformID, string(acpID), "", "")
	sess := &sessionState{
		acpSessionID: acpID,
		deliveryID:   deliveryID,
	}
	p.sessions[acpID] = sess
	return sess
}

type turnCanceller interface {
	Cancel(context.Context, string) error
}

type permissionDeliverer interface {
	DeliverPermissionReply(agentkit.SessionID, permission.Reply) bool
}
