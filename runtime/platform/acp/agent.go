package acpplatform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

type agentBridge struct {
	platform *Platform
	conn     *acp.AgentSideConnection
}

var (
	_ acp.Agent       = (*agentBridge)(nil)
	_ acp.AgentLoader = (*agentBridge)(nil)
)

func (a *agentBridge) setConn(conn *acp.AgentSideConnection) {
	a.conn = conn
}

func (a *agentBridge) Initialize(_ context.Context, _ acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersionNumber,
		AgentCapabilities: acp.AgentCapabilities{
			LoadSession: false,
		},
		AgentInfo: &acp.Implementation{
			Name:    a.platform.cfg.AgentName,
			Version: a.platform.cfg.AgentVersion,
		},
	}, nil
}

func (a *agentBridge) Authenticate(_ context.Context, _ acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}

func (a *agentBridge) Logout(_ context.Context, _ acp.LogoutRequest) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, acp.NewMethodNotFound(acp.AgentMethodLogout)
}

func (a *agentBridge) NewSession(_ context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	sid := randomACPSessionID()
	sess := a.platform.getOrCreateSession(acp.SessionId(sid))
	sess.cwd = params.Cwd
	return acp.NewSessionResponse{SessionId: acp.SessionId(sid)}, nil
}

func (a *agentBridge) LoadSession(_ context.Context, _ acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	return acp.LoadSessionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionLoad)
}

func (a *agentBridge) ListSessions(_ context.Context, _ acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	return acp.ListSessionsResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionList)
}

func (a *agentBridge) ResumeSession(_ context.Context, _ acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	return acp.ResumeSessionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionResume)
}

func (a *agentBridge) CloseSession(_ context.Context, _ acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	return acp.CloseSessionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionClose)
}

func (a *agentBridge) SetSessionMode(_ context.Context, _ acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, nil
}

func (a *agentBridge) SetSessionConfigOption(_ context.Context, _ acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	return acp.SetSessionConfigOptionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionSetConfigOption)
}

func (a *agentBridge) Cancel(_ context.Context, params acp.CancelNotification) error {
	a.platform.mu.Lock()
	sess, ok := a.platform.sessions[params.SessionId]
	a.platform.mu.Unlock()
	if !ok {
		return nil
	}
	sess.invokeCancelTurn()
	sess.promptMu.Lock()
	if sess.promptCancel != nil {
		sess.promptCancel()
	}
	sess.promptMu.Unlock()
	sess.completeTurn(turnResult{stopReason: acp.StopReasonCancelled})
	return nil
}

func (a *agentBridge) Prompt(ctx context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	a.platform.mu.Lock()
	sess, ok := a.platform.sessions[params.SessionId]
	a.platform.mu.Unlock()
	if !ok {
		return acp.PromptResponse{}, fmt.Errorf("session %s not found", params.SessionId)
	}

	promptCtx, promptCancel := context.WithCancel(ctx)
	sess.setPromptCancel(promptCancel)
	defer sess.clearPromptCancel()

	turnCh := make(chan turnResult, 1)
	sess.setTurnWait(turnCh)

	msg := promptToModelMessage(params.Prompt)
	event := common.WithDeliverySession(agentkit.MessageEvent{
		PlatformID: platformID,
		Message:    msg,
	}, platformID, sess.deliveryID)
	if err := a.platform.inbox.Push(promptCtx, event); err != nil {
		return acp.PromptResponse{}, err
	}

	select {
	case <-promptCtx.Done():
		return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
	case result := <-turnCh:
		if result.err != nil {
			slog.Error("platform/acp: turn failed", "session", params.SessionId, "err", result.err)
			return acp.PromptResponse{}, result.err
		}
		if result.stopReason == "" {
			result.stopReason = acp.StopReasonEndTurn
		}
		return acp.PromptResponse{StopReason: result.stopReason}, nil
	}
}

func randomACPSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "sess_fallback"
	}
	return "sess_" + hex.EncodeToString(b)
}
