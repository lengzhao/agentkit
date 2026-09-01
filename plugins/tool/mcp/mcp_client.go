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

	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/agentkit/cap/tenant"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
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
	Content string
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
			Meta:      callMetaFromBinds(ctx, server.Binds),
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
	bindEnv, err := envFromBinds(ctx, server.Binds)
	if err != nil {
		return nil, fmt.Errorf("mcp server %q: %w", server.Name, err)
	}
	env = append(env, bindEnv...)
	var client *mcpclient.Client
	switch {
	case server.URL != "":
		url, err := resolveEnvValue(ctx, server.URL, creds)
		if err != nil {
			return nil, fmt.Errorf("mcp server %q url: %w", server.Name, err)
		}
		headers, err := resolveStringMap(ctx, server.Headers, creds)
		if err != nil {
			return nil, fmt.Errorf("mcp server %q headers: %w", server.Name, err)
		}
		client, err = connectURL(url, server.Type, headers, server.Binds)
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

func connectURL(url, typ string, staticHeaders map[string]string, binds []bindConfig) (*mcpclient.Client, error) {
	headerFn := mergeHeaderFunc(staticHeaders, binds)
	switch strings.ToLower(typ) {
	case "sse":
		return mcpclient.NewSSEMCPClient(url, mcpclient.WithHeaderFunc(headerFn))
	case "http", "streamable", "streamable-http", "streamable_http":
		return mcpclient.NewStreamableHttpClient(url, transport.WithHTTPHeaderFunc(headerFn))
	case "":
		if client, err := mcpclient.NewStreamableHttpClient(url, transport.WithHTTPHeaderFunc(headerFn)); err == nil {
			return client, nil
		}
		return mcpclient.NewSSEMCPClient(url, mcpclient.WithHeaderFunc(headerFn))
	default:
		return nil, fmt.Errorf("unsupported mcp transport type %q", typ)
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

func resolveStringMap(ctx context.Context, values map[string]string, creds credentials.Store) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		resolved, err := resolveEnvValue(ctx, value, creds)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		out[key] = resolved
	}
	return out, nil
}

func mergeHeaderFunc(static map[string]string, binds []bindConfig) func(context.Context) map[string]string {
	bindFn := headerBindFunc(binds)
	return func(ctx context.Context) map[string]string {
		var out map[string]string
		if len(static) > 0 {
			out = make(map[string]string, len(static))
			for k, v := range static {
				out[k] = v
			}
		}
		if bound := bindFn(ctx); bound != nil {
			if out == nil {
				out = make(map[string]string, len(bound))
			}
			for k, v := range bound {
				out[k] = v
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
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
	var b strings.Builder
	appendMCPText := func(text string) {
		if text == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(text)
	}
	for _, content := range result.Content {
		switch c := content.(type) {
		case mcplib.TextContent:
			appendMCPText(c.Text)
		case mcplib.ImageContent:
			appendMCPText(fmt.Sprintf("[image %s]", c.MIMEType))
		case mcplib.AudioContent:
			appendMCPText(fmt.Sprintf("[audio %s]", c.MIMEType))
		case mcplib.EmbeddedResource:
			if encoded, err := json.Marshal(c); err == nil {
				appendMCPText(string(encoded))
			}
		default:
			if encoded, err := json.Marshal(c); err == nil {
				appendMCPText(string(encoded))
			}
		}
	}
	if result.StructuredContent != nil {
		if encoded, err := json.Marshal(result.StructuredContent); err == nil {
			appendMCPText(string(encoded))
		}
	}
	out.Content = b.String()
	return out
}

func slogWarnMCPFile(path string, err error) {
	slog.Warn("mcp config ignored", "path", path, "error", err)
}

func slogWarnMCPServer(name string, err error) {
	slog.Warn("mcp server tools unavailable", "server", name, "error", err)
}
