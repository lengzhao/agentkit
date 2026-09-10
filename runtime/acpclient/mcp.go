package acpclient

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	acpsdk "github.com/coder/acp-go-sdk"
	capacp "github.com/lengzhao/agentkit/cap/acp"
)

// ToMCPServers converts harness MCP specs into ACP SDK values for session/new.
func ToMCPServers(specs []capacp.SessionMCPServer) []acpsdk.McpServer {
	if len(specs) == 0 {
		return nil
	}
	out := make([]acpsdk.McpServer, 0, len(specs))
	for _, spec := range specs {
		if spec.Stdio == nil {
			continue
		}
		s := spec.Stdio
		env := make([]acpsdk.EnvVariable, 0, len(s.Env))
		for k, v := range s.Env {
			env = append(env, acpsdk.EnvVariable{Name: k, Value: v})
		}
		sort.Slice(env, func(i, j int) bool { return env[i].Name < env[j].Name })
		out = append(out, acpsdk.McpServer{
			Stdio: &acpsdk.McpServerStdio{
				Name:    s.Name,
				Command: s.Command,
				Args:    append([]string(nil), s.Args...),
				Env:     env,
			},
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Fingerprint returns a stable hash of the server list for session bind/resume.
func Fingerprint(specs []capacp.SessionMCPServer) string {
	if len(specs) == 0 {
		return ""
	}
	canonical := make([]capacp.SessionMCPServer, 0, len(specs))
	for _, spec := range specs {
		if spec.Stdio == nil {
			continue
		}
		s := spec.Stdio
		envKeys := make([]string, 0, len(s.Env))
		for k := range s.Env {
			envKeys = append(envKeys, k)
		}
		sort.Strings(envKeys)
		env := make(map[string]string, len(envKeys))
		for _, k := range envKeys {
			env[k] = s.Env[k]
		}
		canonical = append(canonical, capacp.SessionMCPServer{
			Stdio: &capacp.StdioMCPServer{
				Name:    s.Name,
				Command: s.Command,
				Args:    append([]string(nil), s.Args...),
				Env:     env,
			},
		})
	}
	if len(canonical) == 0 {
		return ""
	}
	sort.Slice(canonical, func(i, j int) bool {
		ai, aj := canonical[i].Stdio, canonical[j].Stdio
		if ai.Name != aj.Name {
			return ai.Name < aj.Name
		}
		return ai.Command < aj.Command
	})
	raw, err := json.Marshal(canonical)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
