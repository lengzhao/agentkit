package acpclient

import (
	"sort"

	acpsdk "github.com/coder/acp-go-sdk"
	capacp "github.com/lengzhao/agentkit/cap/acp"
)

// ToMCPServers converts harness MCP specs into ACP SDK values for session/new.
func ToMCPServers(specs []capacp.SessionMCPServer) []acpsdk.McpServer {
	if len(specs) == 0 {
		return []acpsdk.McpServer{}
	}
	out := make([]acpsdk.McpServer, 0, len(specs))
	for _, spec := range specs {
		if srv, ok := toMCPServer(spec); ok {
			out = append(out, srv)
		}
	}
	return out
}

func toMCPServer(spec capacp.SessionMCPServer) (acpsdk.McpServer, bool) {
	switch {
	case spec.Stdio != nil:
		s := spec.Stdio
		env := envVariables(s.Env)
		return acpsdk.McpServer{
			Stdio: &acpsdk.McpServerStdio{
				Name:    s.Name,
				Command: s.Command,
				Args:    append([]string(nil), s.Args...),
				Env:     env,
			},
		}, true
	case spec.HTTP != nil:
		s := spec.HTTP
		return acpsdk.McpServer{
			Http: &acpsdk.McpServerHttpInline{
				Name:    s.Name,
				Url:     s.URL,
				Type:    s.Type,
				Headers: httpHeaders(s.Headers),
			},
		}, true
	case spec.SSE != nil:
		s := spec.SSE
		return acpsdk.McpServer{
			Sse: &acpsdk.McpServerSseInline{
				Name:    s.Name,
				Url:     s.URL,
				Type:    s.Type,
				Headers: httpHeaders(s.Headers),
			},
		}, true
	default:
		return acpsdk.McpServer{}, false
	}
}

func envVariables(env map[string]string) []acpsdk.EnvVariable {
	if len(env) == 0 {
		return nil
	}
	out := make([]acpsdk.EnvVariable, 0, len(env))
	for k, v := range env {
		out = append(out, acpsdk.EnvVariable{Name: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func httpHeaders(headers map[string]string) []acpsdk.HttpHeader {
	if len(headers) == 0 {
		return nil
	}
	out := make([]acpsdk.HttpHeader, 0, len(headers))
	for k, v := range headers {
		out = append(out, acpsdk.HttpHeader{Name: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
