package acpremote

import (
	"context"
	"fmt"
	"strings"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
)

var _ agentkit.ACPCommandCapable = (*Runtime)(nil)

func (a *Runtime) ACPCommandCatalog(ctx context.Context, sessionID agentkit.SessionID) (agentkit.ACPCommandCatalog, error) {
	if _, err := a.ensureACPSessionWithAuth(ctx, nil, sessionID); err != nil {
		return agentkit.ACPCommandCatalog{}, err
	}
	state, ok := a.bridge.sessionState(sessionID)
	if !ok {
		return agentkit.ACPCommandCatalog{}, nil
	}
	return state.catalog(), nil
}

func (a *Runtime) SetACPConfigOption(ctx context.Context, sessionID agentkit.SessionID, key, value string) (string, error) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return "", fmt.Errorf("config key and value are required")
	}
	acpSessionID, err := a.ensureACPSessionWithAuth(ctx, nil, sessionID)
	if err != nil {
		return "", err
	}
	state, ok := a.bridge.sessionState(sessionID)
	if !ok {
		return "", fmt.Errorf("acp session state is not available")
	}
	opt, ok := state.findConfigOption(key)
	if !ok {
		return "", fmt.Errorf("unknown config option %q (try /acp <agent> config)", key)
	}
	resp, err := a.bridge.setConfigOption(ctx, acpSessionID, opt, value)
	if err != nil {
		return "", err
	}
	state.applyUpdate(acp.SessionUpdate{
		ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{ConfigOptions: resp.ConfigOptions},
	})
	return formatConfigOptions(resp.ConfigOptions), nil
}

func formatConfigOptions(options []acp.SessionConfigOption) string {
	var b strings.Builder
	b.WriteString("session config updated:\n")
	for _, opt := range options {
		info, ok := configOptionInfo(opt)
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "  %s (%s) = %s\n", info.Name, info.ID, info.CurrentValue)
	}
	return strings.TrimRight(b.String(), "\n")
}
