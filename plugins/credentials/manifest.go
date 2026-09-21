package credentials

import (
	"encoding/json"
	"strings"
)

func manifestFromMCPFile(data []byte) (map[string]map[string]struct{}, error) {
	var doc struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	out := make(map[string]map[string]struct{})
	for scope, raw := range doc.MCPServers {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		keys := make(map[string]struct{})
		collectEnvKeys(v, keys)
		if len(keys) == 0 {
			continue
		}
		out[mcpCredentialScope(scope)] = keys
	}
	return out, nil
}

func manifestFromAPIIndex(data []byte) (map[string]map[string]struct{}, error) {
	var doc struct {
		Apis map[string]json.RawMessage `json:"apis"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	out := make(map[string]map[string]struct{})
	for scope, raw := range doc.Apis {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		keys := make(map[string]struct{})
		collectEnvKeys(v, keys)
		if len(keys) == 0 {
			continue
		}
		out[openAPICredentialScope(scope)] = keys
	}
	return out, nil
}

func manifestFromShellBashFile(data []byte) (map[string]map[string]struct{}, error) {
	var doc struct {
		Commands map[string]struct {
			Env []string `json:"env"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	out := make(map[string]map[string]struct{})
	for name, entry := range doc.Commands {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		keys := make(map[string]struct{})
		for _, key := range entry.Env {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			keys[key] = struct{}{}
		}
		if len(keys) == 0 {
			continue
		}
		out[shellBashCredentialScope(name)] = keys
	}
	return out, nil
}

func mergeManifests(parts ...map[string]map[string]struct{}) map[string]map[string]struct{} {
	out := make(map[string]map[string]struct{})
	for _, part := range parts {
		for scope, keys := range part {
			if len(keys) == 0 {
				continue
			}
			dst := out[scope]
			if dst == nil {
				dst = make(map[string]struct{}, len(keys))
				out[scope] = dst
			}
			for k := range keys {
				dst[k] = struct{}{}
			}
		}
	}
	return out
}
