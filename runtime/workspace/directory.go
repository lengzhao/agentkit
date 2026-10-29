package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/lengzhao/agentkit/runtime/rctx"
)

// Directory scope controls execution sharing, not local:/global: storage roots.
func validateDirectoryPolicy(scope string, overrides map[string]string) error {
	valid := func(s string) bool {
		return s == "" || s == "global" || s == "channel" || s == "thread" || s == "session"
	}
	if !valid(scope) {
		return fmt.Errorf("workspaceScope must be global, channel, thread, or session, got %q", scope)
	}
	for instance, s := range overrides {
		if instance == "" || !valid(s) {
			return fmt.Errorf("invalid workspaceScopes entry %q: %q", instance, s)
		}
	}
	return nil
}

func copyDirectoryPolicies(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func scopedDirectory(ctx context.Context, scope string, overrides map[string]string, global, local, work string) (string, error) {
	env := rctx.EnvelopeFromContext(ctx)
	if override, ok := overrides[env.Route.DestinationID()]; ok {
		scope = override
	}
	if scope == "" {
		return "", nil
	}
	if scope == "global" {
		return ResolveRelStrict(global, work)
	}
	target, hasRoute := rctx.RouteTargetFromRoute(env.Route)
	instance := env.Route.DestinationID()
	channel := target.ChannelID
	// Opaque/headless routes can supply a stable workspace namespace themselves.
	if channel == "" {
		channel = env.Workspace
	}
	if scope == "session" && env.Conversation == "" {
		return "", fmt.Errorf("session workspace requires resolved logical session")
	}
	if channel == "" || instance == "" {
		return "", fmt.Errorf("%s workspace requires platform instance and channel/workspace identity", scope)
	}
	base, err := ResolveRelStrict(local, work)
	if err != nil {
		return "", err
	}
	base = filepath.Join(base, "channels", directoryIdentity(instance, channel))
	switch scope {
	case "channel":
		return base, nil
	case "thread":
		if !hasRoute || target.ThreadID == "" {
			slog.Info("thread workspace falls back to channel", "instance", instance, "channel", channel)
			return base, nil
		}
		return filepath.Join(base, "threads", directoryIdentity(instance, channel, target.ThreadID)), nil
	case "session":
		return filepath.Join(base, "sessions", directoryIdentity(instance, channel, env.Conversation)), nil
	default:
		return "", fmt.Errorf("unsupported workspace scope %q", scope)
	}
}

// Length-delimited identity encoding avoids punctuation aliases and path traversal.
func directoryIdentity(parts ...string) string {
	raw, _ := json.Marshal(parts)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func (s *TenantService) DefaultWorkingDirectory(ctx context.Context) (string, error) {
	return scopedDirectory(ctx, s.workspaceScope, s.workspaceScopes, s.globalRoot, s.TenantRoot(ctx), s.workDir)
}
func (s *Service) DefaultWorkingDirectory(ctx context.Context) (string, error) {
	return scopedDirectory(ctx, s.workspaceScope, s.workspaceScopes, s.globalRoot, s.localRoot, s.workDir)
}
