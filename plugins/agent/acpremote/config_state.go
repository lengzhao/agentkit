package acpremote

import (
	"context"
	"strconv"
	"sync"

	acp "github.com/coder/acp-go-sdk"
)

// Options are immutable snapshots decoded by the SDK. Initial means first
// observed on this connection, not a vendor/global default (ACP has no such field).
type sessionConfigState struct {
	mu      sync.Mutex
	options []acp.SessionConfigOption
	initial map[string]string
}

func (p *subprocess) updateConfig(options []acp.SessionConfigOption) {
	s := &p.config
	s.mu.Lock()
	defer s.mu.Unlock()
	s.options = options
	if s.initial == nil {
		s.initial = make(map[string]string)
	}
	for _, option := range options {
		key, value := configOptionValue(option)
		if _, ok := s.initial[key]; !ok && key != "" {
			s.initial[key] = value
		}
	}
}

func (p *subprocess) configSnapshot() ([]acp.SessionConfigOption, map[string]string) {
	s := &p.config
	s.mu.Lock()
	defer s.mu.Unlock()
	initial := make(map[string]string, len(s.initial))
	for key, value := range s.initial {
		initial[key] = value
	}
	return s.options, initial
}

func configOptionValue(option acp.SessionConfigOption) (string, string) {
	if s := option.Select; s != nil {
		return string(s.Id), string(s.CurrentValue)
	}
	if b := option.Boolean; b != nil {
		return string(b.Id), strconv.FormatBool(b.CurrentValue)
	}
	return "", ""
}

// Capture load/resume responses without changing the injectable restore contract.
type configRestoreConn struct {
	conn *acp.ClientSideConnection
	proc *subprocess
}

func (c configRestoreConn) ResumeSession(ctx context.Context, req acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	resp, err := c.conn.ResumeSession(ctx, req)
	if err == nil {
		c.proc.updateConfig(resp.ConfigOptions)
	}
	return resp, err
}

func (c configRestoreConn) LoadSession(ctx context.Context, req acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	resp, err := c.conn.LoadSession(ctx, req)
	if err == nil {
		c.proc.updateConfig(resp.ConfigOptions)
	}
	return resp, err
}
