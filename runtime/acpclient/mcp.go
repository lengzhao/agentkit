package acpclient

import (
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
