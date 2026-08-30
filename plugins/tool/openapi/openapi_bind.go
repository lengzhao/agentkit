package openapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lengzhao/agentkit"
)

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
		if !validParamLocations[in] {
			return nil, fmt.Errorf("bind %q: unsupported in %q", key, entry.In)
		}
		slot := in + "\x00" + key
		if prior, ok := seen[slot]; ok {
			return nil, fmt.Errorf("bind %q conflicts with bind %q on %s parameter %q", key, prior, in, key)
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

func (a apiConfig) isBoundParameter(in, name string) bool {
	in = strings.ToLower(strings.TrimSpace(in))
	name = strings.TrimSpace(name)
	for _, bind := range a.Binds {
		if bind.In == in && bind.Key == name {
			return true
		}
	}
	return false
}

func (a apiConfig) wireParamName(in, key string) string {
	in = strings.ToLower(strings.TrimSpace(in))
	key = strings.TrimSpace(key)
	for _, bind := range a.Binds {
		if bind.In == in && bind.Key == key {
			return bind.paramName()
		}
	}
	return key
}

func operationHasParameter(op operationConfig, in, key string) bool {
	for _, p := range op.Parameters {
		if p.In == in && p.Name == key {
			return true
		}
	}
	return false
}

func applyBinds(ctx context.Context, binds []bindConfig, args map[string]any) error {
	for _, bind := range binds {
		value, err := resolveCtxValue(ctx, bind.From)
		if err != nil {
			return fmt.Errorf("bind %q: %w", bind.Key, err)
		}
		if value == "" {
			return fmt.Errorf("bind %q: %s is empty", bind.Key, bind.From)
		}
		args[bind.Key] = value
	}
	return nil
}

func applyBindOnlyParams(op operationConfig, binds []bindConfig, args map[string]any, path *string, query url.Values, headers http.Header) error {
	for _, bind := range binds {
		if operationHasParameter(op, bind.In, bind.Key) {
			continue
		}
		value, ok := args[bind.Key]
		if !ok {
			return fmt.Errorf("bind %q: internal parameter %q missing", bind.Key, bind.Key)
		}
		text := stringifyValue(value)
		switch bind.In {
		case "path":
			if strings.Contains(*path, "{"+bind.Key+"}") {
				*path = strings.ReplaceAll(*path, "{"+bind.Key+"}", url.PathEscape(text))
			} else {
				return fmt.Errorf("bind %q: path %q has no {%s} placeholder", bind.Key, op.Path, bind.Key)
			}
		case "query":
			query.Set(bind.paramName(), text)
		case "header":
			headers.Set(bind.paramName(), text)
		}
	}
	return nil
}

func resolveCtxValue(ctx context.Context, from string) (string, error) {
	from = strings.TrimSpace(from)
	if !strings.HasPrefix(from, "ctx:") {
		return "", fmt.Errorf("from %q must use ctx: prefix", from)
	}
	key := strings.TrimPrefix(from, "ctx:")
	switch {
	case key == "user_id":
		if v, ok := ctx.Value(agentkit.KeyUserID).(string); ok {
			return v, nil
		}
		return "", nil
	case key == "session_id":
		if v, ok := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID); ok && v != "" {
			return string(v), nil
		}
		return "", nil
	case key == "store_session_id":
		if v, ok := ctx.Value(agentkit.KeyStoreSessionID).(agentkit.SessionID); ok && v != "" {
			return string(v), nil
		}
		return "", nil
	case key == "delivery_session_id":
		if v, ok := ctx.Value(agentkit.KeyDeliverySessionID).(agentkit.SessionID); ok && v != "" {
			return string(v), nil
		}
		return "", nil
	case key == "agent_id":
		if v, ok := ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID); ok && v != "" {
			return string(v), nil
		}
		return "", nil
	case key == "platform_id":
		if v, ok := ctx.Value(agentkit.KeyPlatformID).(string); ok {
			return v, nil
		}
		return "", nil
	case strings.HasPrefix(key, "metadata."):
		mdKey := strings.TrimPrefix(key, "metadata.")
		if mdKey == "" {
			return "", fmt.Errorf("metadata key is required")
		}
		md, ok := ctx.Value(agentkit.KeyMessageMetadata).(map[string]any)
		if !ok || md == nil {
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
