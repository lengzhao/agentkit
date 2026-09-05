package mcp

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/runtime/session"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

type rawBind struct {
	From string `json:"from"`
	In   string `json:"in"`
	Name string `json:"name,omitempty"`
}

type bindConfig struct {
	Key  string
	From string
	In   string
	Name string
}

func (b bindConfig) paramName() string {
	if b.Name != "" {
		return b.Name
	}
	return b.Key
}

var validMCPBindLocations = map[string]bool{
	"header": true,
	"meta":   true,
	"env":    true,
}

func parseBinds(raw map[string]rawBind) ([]bindConfig, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[string]string, len(raw))
	out := make([]bindConfig, 0, len(raw))
	for key, entry := range raw {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		from := strings.TrimSpace(entry.From)
		if from == "" {
			return nil, fmt.Errorf("bind %q: from is required", key)
		}
		if !strings.HasPrefix(from, "ctx:") {
			return nil, fmt.Errorf("bind %q: from %q must use ctx: prefix", key, from)
		}
		in := strings.ToLower(strings.TrimSpace(entry.In))
		if !validMCPBindLocations[in] {
			return nil, fmt.Errorf("bind %q: unsupported in %q (want header, meta, or env)", key, entry.In)
		}
		slot := in + "\x00" + key
		if prior, ok := seen[slot]; ok {
			return nil, fmt.Errorf("bind %q conflicts with bind %q on %s field %q", key, prior, in, key)
		}
		seen[slot] = key
		out = append(out, bindConfig{
			Key:  key,
			From: from,
			In:   in,
			Name: strings.TrimSpace(entry.Name),
		})
	}
	return out, nil
}

func resolveCtxValue(ctx context.Context, from string) (string, error) {
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

func headerBindFunc(binds []bindConfig) func(context.Context) map[string]string {
	return func(ctx context.Context) map[string]string {
		out := make(map[string]string)
		for _, bind := range binds {
			if bind.In != "header" {
				continue
			}
			value, err := resolveCtxValue(ctx, bind.From)
			if err != nil || value == "" {
				continue
			}
			out[bind.paramName()] = value
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
}

func envFromBinds(ctx context.Context, binds []bindConfig) ([]string, error) {
	var out []string
	for _, bind := range binds {
		if bind.In != "env" {
			continue
		}
		value, err := resolveCtxValue(ctx, bind.From)
		if err != nil {
			return nil, fmt.Errorf("bind %q: %w", bind.Key, err)
		}
		if value == "" {
			continue
		}
		out = append(out, bind.paramName()+"="+value)
	}
	return out, nil
}

func callMetaFromBinds(ctx context.Context, binds []bindConfig) *mcplib.Meta {
	fields := make(map[string]any)
	for _, bind := range binds {
		if bind.In != "meta" {
			continue
		}
		value, err := resolveCtxValue(ctx, bind.From)
		if err != nil || value == "" {
			continue
		}
		fields[bind.paramName()] = value
	}
	if len(fields) == 0 {
		return nil
	}
	return &mcplib.Meta{AdditionalFields: fields}
}
