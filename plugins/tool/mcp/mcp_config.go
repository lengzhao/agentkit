package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

type configDocument struct {
	MCPServers map[string]rawServerConfig `json:"mcpServers"`
}

type rawServerConfig struct {
	Command        string            `json:"command"`
	Args           []string          `json:"args"`
	Env            map[string]string `json:"env"`
	URL            string            `json:"url"`
	Type           string            `json:"type"`
	Transport      string            `json:"transport"`
	Headers        map[string]string `json:"headers"`
	Prefix         string            `json:"prefix"`
	AllowTools     []string          `json:"allowTools"`
	DenyTools      []string          `json:"denyTools"`
	TimeoutSeconds int               `json:"timeoutSeconds"`
	Bind           map[string]rawBind `json:"bind,omitempty"`
}

type serverConfig struct {
	Name           string
	Source         string
	Command        string
	Args           []string
	Env            map[string]string
	URL            string
	Type           string
	Headers        map[string]string
	Prefix         string
	AllowTools     []string
	DenyTools      []string
	TimeoutSeconds int
	Binds          []bindConfig
}

type toolDefinition struct {
	Server       string
	ExposedName  string
	OriginalName string
	Description  string
	InputSchema  json.RawMessage
}

func parseConfigFile(path string, raw []byte) ([]serverConfig, error) {
	var doc configDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if len(doc.MCPServers) == 0 {
		return nil, nil
	}
	out := make([]serverConfig, 0, len(doc.MCPServers))
	for name, raw := range doc.MCPServers {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		binds, err := parseBinds(raw.Bind)
		if err != nil {
			return nil, fmt.Errorf("mcp server %q in %s: %w", name, path, err)
		}
		cfg := serverConfig{
			Name:           name,
			Source:         path,
			Command:        strings.TrimSpace(raw.Command),
			Args:           append([]string(nil), raw.Args...),
			Env:            cloneStringMap(raw.Env),
			URL:            strings.TrimSpace(raw.URL),
			Type:           serverType(raw),
			Headers:        cloneStringMap(raw.Headers),
			Prefix:         strings.TrimSpace(raw.Prefix),
			AllowTools:     trimAll(raw.AllowTools),
			DenyTools:      trimAll(raw.DenyTools),
			TimeoutSeconds: raw.TimeoutSeconds,
			Binds:          binds,
		}
		if cfg.Command == "" && cfg.URL == "" {
			return nil, fmt.Errorf("mcp server %q in %s needs command or url", name, path)
		}
		out = append(out, cfg)
	}
	return out, nil
}

func parseServerJSON(name, source string, raw []byte) (serverConfig, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return serverConfig{}, fmt.Errorf("server name is required")
	}
	var rawCfg rawServerConfig
	if err := json.Unmarshal(raw, &rawCfg); err != nil {
		return serverConfig{}, fmt.Errorf("parse server json: %w", err)
	}
	binds, err := parseBinds(rawCfg.Bind)
	if err != nil {
		return serverConfig{}, fmt.Errorf("mcp server %q: %w", name, err)
	}
	cfg := serverConfig{
		Name:           name,
		Source:         source,
		Command:        strings.TrimSpace(rawCfg.Command),
		Args:           append([]string(nil), rawCfg.Args...),
		Env:            cloneStringMap(rawCfg.Env),
		URL:            strings.TrimSpace(rawCfg.URL),
		Type:           serverType(rawCfg),
		Headers:        cloneStringMap(rawCfg.Headers),
		Prefix:         strings.TrimSpace(rawCfg.Prefix),
		AllowTools:     trimAll(rawCfg.AllowTools),
		DenyTools:      trimAll(rawCfg.DenyTools),
		TimeoutSeconds: rawCfg.TimeoutSeconds,
		Binds:          binds,
	}
	if cfg.Command == "" && cfg.URL == "" {
		return serverConfig{}, fmt.Errorf("mcp server %q needs command or url", name)
	}
	return cfg, nil
}

func (s serverConfig) toolPrefix() string {
	if s.Prefix != "" {
		return s.Prefix
	}
	return s.Name + "__"
}

func (s serverConfig) allowsTool(name string) bool {
	if len(s.AllowTools) > 0 {
		for _, allowed := range s.AllowTools {
			if allowed == name {
				return true
			}
		}
		return false
	}
	for _, denied := range s.DenyTools {
		if denied == name {
			return false
		}
	}
	return true
}

func (s serverConfig) fingerprint() string {
	b, _ := json.Marshal(s)
	return string(b)
}

func upsertMCPJSON(existing []byte, name string, raw json.RawMessage) ([]byte, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("server name is required")
	}
	var server rawServerConfig
	if err := json.Unmarshal(raw, &server); err != nil {
		return nil, fmt.Errorf("parse server json: %w", err)
	}
	if strings.TrimSpace(server.Command) == "" && strings.TrimSpace(server.URL) == "" {
		return nil, fmt.Errorf("mcp server %q needs command or url", name)
	}

	var doc configDocument
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &doc); err != nil {
			return nil, fmt.Errorf("parse mcp.json: %w", err)
		}
	}
	if doc.MCPServers == nil {
		doc.MCPServers = make(map[string]rawServerConfig)
	}
	doc.MCPServers[name] = server
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func trimAll(in []string) []string {
	var out []string
	for _, item := range in {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func serverType(raw rawServerConfig) string {
	if typ := strings.TrimSpace(raw.Type); typ != "" {
		return typ
	}
	return strings.TrimSpace(raw.Transport)
}
