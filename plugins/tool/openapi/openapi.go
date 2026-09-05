package openapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/cap/workspace"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/plugins/configfile"
)

const defaultGlobalAPIFile = "global:api.json"
const defaultLocalAPIFile = "local:api.json"

type OpenAPIConfig struct {
	// Files are api.json paths in precedence order; first file wins for duplicate API names.
	// When omitted, defaults to global:api.json only; set EnableLocal to also load local:api.json.
	Files []string `json:"files,omitempty"`
	// EnableLocal allows per-tenant/project local:api.json and /openapi add writes. Off by default.
	EnableLocal bool `json:"enableLocal,omitempty"`
}

type OpenAPIDeps struct {
	Workspace   workspace.Service `json:"workspace"`
	Credentials credentials.Store `json:"credentials,omitempty"`
}

type openapiProvider struct {
	files       []string
	enableLocal bool
	workspace   workspace.Service
	credentials credentials.Store
	client      *http.Client

	mu     sync.RWMutex
	loaded bool
	apis   []apiConfig
}

type openapiTool struct {
	api      apiConfig
	op       operationConfig
	provider *openapiProvider
}

// NewOpenAPI registers tool/openapi: Load api.json (an index of REST APIs
// described OpenAPI-compatibly) and expose each operation as a dynamic
// model-visible tool.
//
// Best practices:
//   - Mount via tools/runtime deps.dynamicTools, not deps.tools, because definitions are discovered at runtime.
//   - api.json is an index: each entry under "apis" points at an OpenAPI document via path (specFile is a legacy alias).
//   - Definitions are loaded once and cached; run the "openapi" command to reload api.json (and any specFile) from disk after editing it.
//   - Secrets in auth (token/value/password) accept "env:NAME" and resolve through credentials, same as tool/mcp.
func NewOpenAPI(cfg OpenAPIConfig, deps OpenAPIDeps) (agentkit.ToolProvider, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("tool/openapi requires workspace")
	}
	files := resolveAPIFiles(cfg)
	return &openapiProvider{
		files:       files,
		enableLocal: cfg.EnableLocal,
		workspace:   deps.Workspace,
		credentials: deps.Credentials,
		client:      &http.Client{},
	}, nil
}

func resolveAPIFiles(cfg OpenAPIConfig) []string {
	files := cfg.Files
	if len(files) == 0 {
		if cfg.EnableLocal {
			return []string{defaultLocalAPIFile, defaultGlobalAPIFile}
		}
		return []string{defaultGlobalAPIFile}
	}
	if !cfg.EnableLocal {
		return filterGlobalAPIFiles(files)
	}
	return files
}

func filterGlobalAPIFiles(files []string) []string {
	out := make([]string, 0, len(files))
	for _, rel := range files {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		scope, _, scoped := rtworkspace.ParseScoped(rel)
		if scoped && scope == workspace.ScopeGlobal {
			out = append(out, rel)
		}
	}
	return out
}

func (p *openapiProvider) ListTools(ctx context.Context) ([]agentkit.Tool, error) {
	apis, err := p.cachedAPIs(ctx)
	if err != nil {
		return nil, err
	}
	var out []agentkit.Tool
	for _, api := range apis {
		for _, op := range api.Operations {
			if !api.allowsOperation(op.OperationID) {
				continue
			}
			out = append(out, &openapiTool{api: api, op: op, provider: p})
		}
	}
	return out, nil
}

// cachedAPIs returns the last loaded API definitions, loading them once on
// first use. They are not re-read from disk again until reload runs (via the
// "openapi" command), so repeated ListTools calls don't pay for file IO and
// spec parsing on every turn.
func (p *openapiProvider) cachedAPIs(ctx context.Context) ([]apiConfig, error) {
	p.mu.RLock()
	loaded := p.loaded
	apis := p.apis
	p.mu.RUnlock()
	if loaded {
		return apis, nil
	}
	return p.reload(ctx)
}

// reload re-reads every api.json file (and any specFile it references) from
// disk and replaces the cache. It backs the "openapi -u" command.
func (p *openapiProvider) reload(ctx context.Context) ([]apiConfig, error) {
	apis, err := p.loadAPIs(ctx)
	if err != nil {
		_, endObservation := telemetry.BeginObservation(ctx, telemetry.ObservationMeta{
			Name: "openapi.init",
			Kind: telemetry.KindSpan,
		})
		endObservation(telemetry.ObservationEnd{Err: err})
		return nil, err
	}
	if len(apis) == 0 {
		p.mu.Lock()
		p.apis = nil
		p.loaded = true
		p.mu.Unlock()
		return nil, nil
	}

	_, endObservation := telemetry.BeginObservation(ctx, telemetry.ObservationMeta{
		Name: "openapi.init",
		Kind: telemetry.KindSpan,
	})
	totalOps := 0
	for _, api := range apis {
		totalOps += len(api.Operations)
	}
	endObservation(telemetry.ObservationEnd{
		Output: fmt.Sprintf("%d API(s), %d operation(s)", len(apis), totalOps),
	})

	p.mu.Lock()
	p.apis = apis
	p.loaded = true
	p.mu.Unlock()
	return apis, nil
}

func (p *openapiProvider) writeTarget(ctx context.Context, global bool) (string, error) {
	rel, err := configfile.WriteTargetForAdd(p.files, global)
	if err != nil {
		return "", err
	}
	return p.workspace.Resolve(ctx, rel)
}

func (p *openapiProvider) addAPI(ctx context.Context, name string, raw []byte, global bool) (string, error) {
	if !global && !p.enableLocal {
		return "", fmt.Errorf("local openapi is disabled; use /openapi add -g or set enableLocal")
	}
	loadSpec := p.specLoader(ctx)
	cfg, err := parseAPIEntryJSON(name, "command:add", raw, loadSpec)
	if err != nil {
		return "", err
	}
	tools, err := p.toolsForAPI(cfg)
	if err != nil {
		return "", err
	}

	target, err := p.writeTarget(ctx, global)
	if err != nil {
		return "", err
	}
	prev, err := os.ReadFile(target)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s: %w", target, err)
	}
	var prevBytes []byte
	if err == nil {
		prevBytes = append([]byte(nil), prev...)
	}
	merged, err := upsertAPIJSON(prevBytes, name, raw)
	if err != nil {
		return "", err
	}
	if err := configfile.WriteAtomic(target, merged, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", target, err)
	}
	apis, err := p.reload(ctx)
	if err != nil {
		_ = configfile.Restore(target, prevBytes, 0o644)
		_, _ = p.reload(ctx)
		return "", err
	}
	var loaded *apiConfig
	for i := range apis {
		if apis[i].Name == name {
			loaded = &apis[i]
			break
		}
	}
	if loaded == nil || len(loaded.Operations) != len(cfg.Operations) {
		_ = configfile.Restore(target, prevBytes, 0o644)
		_, _ = p.reload(ctx)
		return "", fmt.Errorf("api %q failed validation after reload", name)
	}
	return fmt.Sprintf("openapi: wrote %s to %s (%d ops, %d tools), verified", name, target, len(loaded.Operations), len(tools)), nil
}

func (p *openapiProvider) toolsForAPI(api apiConfig) ([]agentkit.Tool, error) {
	var out []agentkit.Tool
	for _, op := range api.Operations {
		if !api.allowsOperation(op.OperationID) {
			continue
		}
		out = append(out, &openapiTool{api: api, op: op, provider: p})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("api %q exposes no tools after allow/deny filters", api.Name)
	}
	return out, nil
}

func (p *openapiProvider) statusWithHelp(ctx context.Context) (string, error) {
	apis, err := p.cachedAPIs(ctx)
	if err != nil {
		return "", err
	}
	return formatOpenAPIStatus(apis) + "\n\n" + openapiHelp(), nil
}

func formatOpenAPIStatus(apis []apiConfig) string {
	if len(apis) == 0 {
		return "openapi: 0 APIs loaded"
	}
	total := 0
	parts := make([]string, 0, len(apis))
	for _, api := range apis {
		total += len(api.Operations)
		parts = append(parts, fmt.Sprintf("%s (%d ops)", api.Name, len(api.Operations)))
	}
	return fmt.Sprintf("openapi: %d API(s), %d operation(s): %s", len(apis), total, strings.Join(parts, ", "))
}

func openapiHelp() string {
	return `Usage:
  /openapi                         show status and help
  /openapi add [-g] <name> <json>  write API to api.json, validate, reload, and verify
  /openapi -u                      reload api.json and referenced spec files

JSON format matches one apis entry in api.json, e.g.:
  {"baseUrl":"https://api.example.com","paths":{"/ping":{"get":{"operationId":"ping"}}}}
  {"path":"api/petstore.json","baseUrl":"https://api.example.com","auth":{"type":"bearer","token":"env:TOKEN"}}

Notes:
  add writes to local api.json by default; -g writes to global:api.json
  when enableLocal is off, only -g is allowed
  See docs/guides/tools.zh.md for full api.json format`
}

func (p *openapiProvider) specLoader(ctx context.Context) specLoader {
	return func(rel string) ([]byte, error) {
		path, err := p.workspace.Resolve(ctx, rel)
		if err != nil {
			return nil, err
		}
		return os.ReadFile(path)
	}
}

func (p *openapiProvider) loadAPIs(ctx context.Context) ([]apiConfig, error) {
	loadSpec := p.specLoader(ctx)

	seen := make(map[string]struct{})
	var out []apiConfig
	for _, rel := range p.files {
		path, err := p.workspace.Resolve(ctx, rel)
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			slog.Warn("api.json ignored", "path", path, "error", err)
			continue
		}
		apis, err := parseIndexFile(path, raw, loadSpec)
		if err != nil {
			slog.Warn("api.json ignored", "path", path, "error", err)
			continue
		}
		for _, api := range apis {
			if _, ok := seen[api.Name]; ok {
				continue
			}
			seen[api.Name] = struct{}{}
			out = append(out, api)
		}
	}
	return out, nil
}

// Commands contributes the "openapi" slash command that forces a reload.
func (p *openapiProvider) Commands() []agentkit.Command {
	return []agentkit.Command{&openapiSyncCommand{provider: p}}
}

type openapiSyncCommand struct {
	provider *openapiProvider
}

func (c *openapiSyncCommand) Name() string  { return "openapi" }
func (c *openapiSyncCommand) Alias() string { return "" }
func (c *openapiSyncCommand) Description() string {
	return "Show OpenAPI tools, add an API from JSON, or reload api.json with -u"
}

func (c *openapiSyncCommand) CommandExec(ctx context.Context, args string) (string, error) {
	update, rest := peelUpdateFlag(strings.Fields(strings.TrimSpace(args)))
	switch {
	case update:
		apis, err := c.provider.reload(ctx)
		if err != nil {
			return "", err
		}
		return summarizeAPIs(apis), nil
	case len(rest) >= 1 && rest[0] == "add":
		global, addRest := configfile.PeelGlobalFlag(rest[1:])
		if len(addRest) < 2 {
			return "", fmt.Errorf("usage: /openapi add [-g] <name> <json>")
		}
		name := strings.TrimSpace(addRest[0])
		if name == "" {
			return "", fmt.Errorf("api name is required")
		}
		return c.provider.addAPI(ctx, name, []byte(strings.Join(addRest[1:], " ")), global)
	case len(rest) == 0:
		return c.provider.statusWithHelp(ctx)
	default:
		return "", fmt.Errorf("usage: /openapi | /openapi add [-g] <name> <json> | /openapi -u")
	}
}

func peelUpdateFlag(args []string) (update bool, rest []string) {
	for _, arg := range args {
		switch arg {
		case "-u", "--update":
			update = true
		default:
			rest = append(rest, arg)
		}
	}
	return update, rest
}

func summarizeAPIs(apis []apiConfig) string {
	if len(apis) == 0 {
		return "openapi: reloaded, 0 APIs discovered"
	}
	total := 0
	parts := make([]string, 0, len(apis))
	for _, api := range apis {
		total += len(api.Operations)
		parts = append(parts, fmt.Sprintf("%s (%d ops)", api.Name, len(api.Operations)))
	}
	return fmt.Sprintf("openapi: reloaded %d API(s), %d operation(s): %s", len(apis), total, strings.Join(parts, ", "))
}

var _ agentkit.CommandProvider = (*openapiProvider)(nil)

func (t *openapiTool) Name() string { return t.api.toolPrefix() + t.op.OperationID }

func (t *openapiTool) Description() string {
	if t.op.Summary != "" {
		return t.op.Summary
	}
	if t.op.Description != "" {
		return t.op.Description
	}
	return fmt.Sprintf("%s %s via %s API", t.op.Method, t.op.Path, t.api.Name)
}

func (t *openapiTool) InputSchema() agentkit.JSONSchema {
	properties := make(map[string]any, len(t.op.Parameters)+1)
	var required []string
	for _, p := range t.op.Parameters {
		if t.api.isBoundParameter(p.In, p.Name) {
			continue
		}
		properties[p.Name] = paramSchema(p)
		if p.Required {
			required = append(required, p.Name)
		}
	}
	if t.op.RequestBody != nil {
		properties["body"] = requestBodySchema(t.op.RequestBody)
		if t.op.RequestBody.Required {
			required = append(required, "body")
		}
	}
	raw := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		raw["required"] = required
	}
	return agentkit.JSONSchema{Raw: raw}
}

func paramSchema(p paramConfig) map[string]any {
	schema := decodeRawSchema(p.Schema)
	desc, _ := schema["description"].(string)
	if desc == "" {
		desc = p.Description
	}
	if desc == "" {
		schema["description"] = "in: " + p.In
	} else {
		schema["description"] = desc + " (in: " + p.In + ")"
	}
	return schema
}

func requestBodySchema(body *requestBodyConfig) map[string]any {
	return decodeRawSchema(body.Schema)
}

func decodeRawSchema(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{"type": "string"}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return map[string]any{"type": "string"}
	}
	return m
}

func (t *openapiTool) Call(ctx context.Context, input json.RawMessage) (string, error) {
	result, err := t.provider.call(ctx, t.api, t.op, input)
	if err != nil {
		return err.Error(), nil
	}
	return result, nil
}

func timeout(api apiConfig) time.Duration {
	if api.TimeoutSeconds > 0 {
		return time.Duration(api.TimeoutSeconds) * time.Second
	}
	return defaultCallTimeout
}
