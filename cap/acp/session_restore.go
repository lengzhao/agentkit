package acp

import (
	"context"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
)

// SessionRestoreParams describes a persisted ACP session bind before re-attach.
type SessionRestoreParams struct {
	HarnessSessionID  agentkit.SessionID
	AgentID           agentkit.AgentID
	ACPSessionID      acpsdk.SessionId
	Cwd               string
	McpServers        []acpsdk.McpServer
	AgentCapabilities acpsdk.AgentCapabilities
}

// SessionRestoreConn is the subset of the ACP client used to re-attach sessions.
type SessionRestoreConn interface {
	ResumeSession(context.Context, acpsdk.ResumeSessionRequest) (acpsdk.ResumeSessionResponse, error)
	LoadSession(context.Context, acpsdk.LoadSessionRequest) (acpsdk.LoadSessionResponse, error)
}

// SessionRestorer customizes how agent/acp-remote re-attaches to a saved ACP session id
// (bind file) before falling back to session/new and harness history replay.
type SessionRestorer interface {
	Restore(ctx context.Context, conn SessionRestoreConn, params SessionRestoreParams) (restored bool, err error)
}
