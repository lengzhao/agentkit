package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/agentkit/cap/tenant"
	mcpclient "github.com/mark3labs/mcp-go/client"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

type clientPool struct {
	mu sync.Mutex
	// sessions is keyed by tenant and server name, not server name alone. Two
	// tenants routinely declare the same server ("filesystem") pointed at their
	// own workspace; sharing a slot made every alternating call evict the other
	// tenant's client and respawn its subprocess.
	sessions map[string]*serverSession
}

type serverSession struct {
	fingerprint string
	client      *mcpclient.Client
}

type mcpCallOutcome struct {
	Content []agentkit.ContentPart
	IsError bool
}

func newClientPool() *clientPool {
	return &clientPool{sessions: make(map[string]*serverSession)}
}

func (p *clientPool) tools(ctx context.Context, server serverConfig, creds credentials.Store) ([]toolDefinition, error) {
	client, err := p.ensure(ctx, server, creds)
	if err != nil {
		return nil, err
	}
	result, err := client.ListTools(ctx, mcplib.ListToolsRequest{})
	if err != nil {
		p.evict(poolKey(ctx, server))
		return nil, err
	}
	var out []toolDefinition
	prefix := server.toolPrefix()
	for _, tool := range result.Tools {
		if !server.allowsTool(tool.Name) {
			continue
		}
		out = append(out, toolDefinition{
			Server:       server.Name,
			ExposedName:  prefix + tool.Name,
			OriginalName: tool.Name,
			Description:  tool.Description,
			InputSchema:  toolInputSchema(tool),
		})
	}
	return out, nil
}

func (p *clientPool) call(ctx context.Context, server serverConfig, toolName string, input json.RawMessage, creds credentials.Store) (mcpCallOutcome, error) {
	client, err := p.ensure(ctx, server, creds)
	if err != nil {
		return mcpCallOutcome{}, err
	}
	if server.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(server.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	args, err := decodeToolArguments(input)
	if err != nil {
		return mcpCallOutcome{}, err
	}
	result, err := client.CallTool(ctx, mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{
			Name:      toolName,
			Arguments: args,
		},
	})
	if err != nil {
		p.evict(poolKey(ctx, server))
		return mcpCallOutcome{}, err
	}
	return convertCallResult(result), nil
}

// poolKey scopes a pooled client to the tenant that opened it. Within one tenant
// the fingerprint still decides replacement, so editing mcp.json reconnects
// rather than accumulating a second client.
func poolKey(ctx context.Context, server serverConfig) string {
	return tenant.FromContext(ctx) + "\x00" + server.Name
}

func (p *clientPool) ensure(ctx context.Context, server serverConfig, creds credentials.Store) (*mcpclient.Client, error) {
	fp := server.fingerprint()
	key := poolKey(ctx, server)
	p.mu.Lock()
	defer p.mu.Unlock()
	if sess, ok := p.sessions[key]; ok && sess.fingerprint == fp && sess.client != nil {
		return sess.client, nil
	}
	if sess, ok := p.sessions[key]; ok && sess.client != nil {
		sess.client.Close()
	}
	client, err := connectServer(ctx, server, creds)
	if err != nil {
		return nil, err
	}
	p.sessions[key] = &serverSession{fingerprint: fp, client: client}
	return client, nil
}

func (p *clientPool) evict(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if sess, ok := p.sessions[key]; ok && sess.client != nil {
		sess.client.Close()
	}
	delete(p.sessions, key)
}

func connectServer(ctx context.Context, server serverConfig, creds credentials.Store) (*mcpclient.Client, error) {
	env, err := resolveEnv(ctx, server.Env, creds)
	if err != nil {
		return nil, fmt.Errorf("mcp server %q: %w", server.Name, err)
	}
	var client *mcpclient.Client
	switch {
	case server.URL != "":
		client, err = connectURL(server.URL, server.Type)
	case server.Command != "":
		client, err = mcpclient.NewStdioMCPClient(server.Command, env, server.Args...)
	default:
		return nil, fmt.Errorf("mcp server %q has no command or url", server.Name)
	}
	if err != nil {
		return nil, fmt.Errorf("mcp server %q connect: %w", server.Name, err)
	}
	if err := initializeClient(ctx, client); err != nil {
		client.Close()
		return nil, fmt.Errorf("mcp server %q initialize: %w", server.Name, err)
	}
	return client, nil
}

func connectURL(url, transportType string) (*mcpclient.Client, error) {
	switch strings.ToLower(transportType) {
	case "sse":
		return mcpclient.NewSSEMCPClient(url)
	case "http", "streamable", "streamable-http", "streamable_http":
		return mcpclient.NewStreamableHttpClient(url)
	case "":
		if client, err := mcpclient.NewStreamableHttpClient(url); err == nil {
			return client, nil
		}
		return mcpclient.NewSSEMCPClient(url)
	default:
		return nil, fmt.Errorf("unsupported mcp transport type %q", transportType)
	}
}

func initializeClient(ctx context.Context, client *mcpclient.Client) error {
	if err := client.Start(ctx); err != nil {
		return err
	}
	_, err := client.Initialize(ctx, mcplib.InitializeRequest{
		Params: mcplib.InitializeParams{
			ProtocolVersion: mcplib.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcplib.Implementation{
				Name:    "agentkit",
				Version: "dev",
			},
		},
	})
	return err
}

func resolveEnv(ctx context.Context, env map[string]string, creds credentials.Store) ([]string, error) {
	if len(env) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(env))
	for key, value := range env {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		resolved, err := resolveEnvValue(ctx, value, creds)
		if err != nil {
			return nil, fmt.Errorf("env %s: %w", key, err)
		}
		out = append(out, key+"="+resolved)
	}
	return out, nil
}

func resolveEnvValue(ctx context.Context, value string, creds credentials.Store) (string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "env:") {
		if creds != nil {
			secret, err := creds.Resolve(ctx, value)
			if err == nil {
				return secret.Value, nil
			}
		}
		key := credentials.EnvKey(value)
		if v := os.Getenv(key); v != "" {
			return v, nil
		}
		return "", fmt.Errorf("credential %q not found", value)
	}
	return value, nil
}

func decodeToolArguments(input json.RawMessage) (map[string]any, error) {
	if len(input) == 0 {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, fmt.Errorf("invalid tool input: %w", err)
	}
	if args == nil {
		return map[string]any{}, nil
	}
	return args, nil
}

func toolInputSchema(tool mcplib.Tool) json.RawMessage {
	if len(tool.RawInputSchema) > 0 {
		return append(json.RawMessage(nil), tool.RawInputSchema...)
	}
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return raw
}

func convertCallResult(result *mcplib.CallToolResult) mcpCallOutcome {
	out := mcpCallOutcome{IsError: result.IsError}
	for _, content := range result.Content {
		switch c := content.(type) {
		case mcplib.TextContent:
			out.Content = append(out.Content, agentkit.ContentPart{Type: "text", Text: c.Text})
		case mcplib.ImageContent:
			out.Content = append(out.Content, agentkit.ContentPart{
				Type: "text",
				Text: fmt.Sprintf("[image %s]", c.MIMEType),
			})
		case mcplib.AudioContent:
			out.Content = append(out.Content, agentkit.ContentPart{
				Type: "text",
				Text: fmt.Sprintf("[audio %s]", c.MIMEType),
			})
		case mcplib.EmbeddedResource:
			if encoded, err := json.Marshal(c); err == nil {
				out.Content = append(out.Content, agentkit.ContentPart{
					Type: "text",
					Text: string(encoded),
				})
			}
		default:
			if encoded, err := json.Marshal(c); err == nil {
				out.Content = append(out.Content, agentkit.ContentPart{Type: "text", Text: string(encoded)})
			}
		}
	}
	if result.StructuredContent != nil {
		if encoded, err := json.Marshal(result.StructuredContent); err == nil {
			out.Content = append(out.Content, agentkit.ContentPart{Type: "text", Text: string(encoded)})
		}
	}
	if len(out.Content) == 0 {
		out.Content = []agentkit.ContentPart{{Type: "text", Text: ""}}
	}
	return out
}

func slogWarnMCPFile(path string, err error) {
	slog.Warn("mcp config ignored", "path", path, "error", err)
}

func slogWarnMCPServer(name string, err error) {
	slog.Warn("mcp server tools unavailable", "server", name, "error", err)
}
