package credentials

import (
	"encoding/json"
	"strings"
)

// CollectEnvKeys walks JSON-decoded values and records env: variable names.
func CollectEnvKeys(v any, out map[string]struct{}) {
	if out == nil {
		return
	}
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if strings.HasPrefix(s, "env:") {
			if key := EnvKey(s); key != "" {
				out[key] = struct{}{}
			}
		}
	case map[string]any:
		for _, child := range t {
			CollectEnvKeys(child, out)
		}
	case []any:
		for _, child := range t {
			CollectEnvKeys(child, out)
		}
	}
}

const mcpCredentialScopePrefix = "mcp."

// mcpCredentialScope maps an mcpServers key to integration credential scope.
// Must stay aligned with mcp.CredentialScope.
func mcpCredentialScope(serverName string) string {
	return mcpCredentialScopePrefix + strings.TrimSpace(serverName)
}

// ManifestFromMCPFile maps each mcpServers key to allowed env keys under scope mcp.<name>.
func ManifestFromMCPFile(data []byte) (map[string]map[string]struct{}, error) {
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
		CollectEnvKeys(v, keys)
		if len(keys) == 0 {
			continue
		}
		out[mcpCredentialScope(scope)] = keys
	}
	return out, nil
}

const apiIndexCredentialScopePrefix = "openapi."

// apiIndexCredentialScope maps an api.json apis entry name to integration credential scope.
// Must stay aligned with openapi.CredentialScope.
func apiIndexCredentialScope(apiName string) string {
	return apiIndexCredentialScopePrefix + strings.TrimSpace(apiName)
}

// ManifestFromAPIIndex maps each apis key (scope) to allowed env keys.
func ManifestFromAPIIndex(data []byte) (map[string]map[string]struct{}, error) {
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
		CollectEnvKeys(v, keys)
		if len(keys) == 0 {
			continue
		}
		out[apiIndexCredentialScope(scope)] = keys
	}
	return out, nil
}

// MergeManifests unions scope maps; later maps overwrite key sets for the same scope.
func MergeManifests(parts ...map[string]map[string]struct{}) map[string]map[string]struct{} {
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
