package mcp

import (
	"context"
	"fmt"
	"strings"

	ctxbind "github.com/lengzhao/agentkit/runtime/bind"
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

func headerBindFunc(binds []bindConfig) func(context.Context) map[string]string {
	return func(ctx context.Context) map[string]string {
		out := make(map[string]string)
		for _, b := range binds {
			if b.In != "header" {
				continue
			}
			value, err := ctxbind.ResolveCtxValue(ctx, b.From)
			if err != nil || value == "" {
				continue
			}
			out[b.paramName()] = value
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
}

func envFromBinds(ctx context.Context, binds []bindConfig) ([]string, error) {
	var out []string
	for _, b := range binds {
		if b.In != "env" {
			continue
		}
		value, err := ctxbind.ResolveCtxValue(ctx, b.From)
		if err != nil {
			return nil, fmt.Errorf("bind %q: %w", b.Key, err)
		}
		if value == "" {
			continue
		}
		out = append(out, b.paramName()+"="+value)
	}
	return out, nil
}

func callMetaFromBinds(ctx context.Context, binds []bindConfig) *mcplib.Meta {
	fields := make(map[string]any)
	for _, b := range binds {
		if b.In != "meta" {
			continue
		}
		value, err := ctxbind.ResolveCtxValue(ctx, b.From)
		if err != nil || value == "" {
			continue
		}
		fields[b.paramName()] = value
	}
	if len(fields) == 0 {
		return nil
	}
	return &mcplib.Meta{AdditionalFields: fields}
}
