package bind

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/telemetry"
	"github.com/lengzhao/agentkit/runtime/session"
)

// Resolver resolves ctx:-prefixed bind sources from the current turn context.
type Resolver interface {
	ResolveCtxValue(ctx context.Context, from string) (string, error)
}

// Default is the process-wide ctx bind resolver.
var Default Resolver = &defaultResolver{}

// ResolveCtxValue resolves a ctx: source string using Default.
func ResolveCtxValue(ctx context.Context, from string) (string, error) {
	return Default.ResolveCtxValue(ctx, from)
}

type defaultResolver struct{}

func (defaultResolver) ResolveCtxValue(ctx context.Context, from string) (string, error) {
	from = strings.TrimSpace(from)
	if !strings.HasPrefix(from, "ctx:") {
		return "", fmt.Errorf("from %q must use ctx: prefix", from)
	}
	key := strings.TrimPrefix(from, "ctx:")
	switch {
	case key == "user_id":
		if v := session.UserIDFromContext(ctx); v != "" {
			return v, nil
		}
		return "", nil
	case key == "session_id":
		if v := session.SessionIDFromContext(ctx); v != "" {
			return string(v), nil
		}
		return "", nil
	case key == "delivery_session_id":
		if v := session.DeliveryRouteFromContext(ctx); v != "" {
			return string(v), nil
		}
		return "", nil
	case key == "agent_id":
		if v := session.AgentIDFromContext(ctx); v != "" {
			return string(v), nil
		}
		return "", nil
	case key == "platform_id":
		if v := session.PlatformFromContext(ctx); v != "" {
			return v, nil
		}
		return "", nil
	case key == "turn_id":
		if v, ok := ctx.Value(agentkit.KeyTurnID).(string); ok && v != "" {
			return v, nil
		}
		if v := telemetry.TurnIDFrom(ctx); v != "" {
			return v, nil
		}
		return "", nil
	case key == "tool_call_id":
		if v, ok := ctx.Value(agentkit.KeyToolCallID).(agentkit.ToolCallID); ok && v != "" {
			return string(v), nil
		}
		return "", nil
	case key == "tenant":
		if v := session.WorkspaceFromContext(ctx); v != "" {
			return v, nil
		}
		return "", nil
	case strings.HasPrefix(key, "metadata."):
		mdKey := strings.TrimPrefix(key, "metadata.")
		if mdKey == "" {
			return "", fmt.Errorf("metadata key is required")
		}
		md := session.MetadataFromContext(ctx)
		if md == nil {
			return "", nil
		}
		v, ok := md[mdKey]
		if !ok || v == nil {
			return "", nil
		}
		return stringifyValue(v), nil
	default:
		return "", fmt.Errorf("unsupported ctx source %q", key)
	}
}

func stringifyValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}
