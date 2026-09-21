package openapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/configfile"
	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/agentkit/cap/filesystem"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/telemetry"
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
	// FS reads/writes api.json and spec files (scope prefixes allowed).
	FS          filesystem.Service `json:"fs"`
	Credentials credentials.Store `json:"credentials,omitempty"`
	// ConfigFile picks the /openapi add target file; without it the add command fails fast.
	ConfigFile configfile.Writer `json:"configfile,omitempty"`
}

type openapiProvider struct {
	files       []string
	enableLocal bool
	fs          filesystem.Service
	credentials credentials.Store
	configFile  configfile.Writer
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
	if deps.FS == nil {
		return nil, fmt.Errorf("tool/openapi requires fs")
	}
	files := resolveAPIFiles(cfg)
	return &openapiProvider{
		files:       files,
		enableLocal: cfg.EnableLocal,
		fs:          deps.FS,
		credentials: deps.Credentials,
		configFile:  deps.ConfigFile,
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
		scope, _, scoped := workspace.ParseScoped(rel)
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
		_, endObservation := telemetry.BeginObservation(ctx, captelemetry.ObservationMeta{
			Name: "openapi.init",
			Kind: captelemetry.KindSpan,
		})
		endObservation(captelemetry.ObservationEnd{Err: err})
		return nil, err
	}
	if len(apis) == 0 {
		p.mu.Lock()
		p.apis = nil
		p.loaded = true
		p.mu.Unlock()
		return nil, nil
	}

	_, endObservation := telemetry.BeginObservation(ctx, captelemetry.ObservationMeta{
		Name: "openapi.init",
		Kind: captelemetry.KindSpan,
	})
	totalOps := 0
	for _, api := range apis {
		totalOps += len(api.Operations)
	}
	endObservation(captelemetry.ObservationEnd{
		Output: fmt.Sprintf("%d API(s), %d operation(s)", len(apis), totalOps),
	})

	p.mu.Lock()
	p.apis = apis
	p.loaded = true
	p.mu.Unlock()
	return apis, nil
}

func (p *openapiProvider) writeTarget(ctx context.Context, global bool) (string, error) {
	if p.configFile == nil {
		return "", fmt.Errorf("tool/openapi requires configFile dependency for /openapi add")
	}
	return p.configFile.WriteTargetForAdd(p.files, global)
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
	if global {
		rewritten, err := rewriteAPIEntryPathsForGlobalAdd(ctx, p.fs, raw)
		if err != nil {
			return "", err
		}
		raw = rewritten
	}

	target, err := p.writeTarget(ctx, global)
	if err != nil {
		return "", err
	}
	prev, err := p.fs.Read(ctx, target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
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
	if err := p.fs.Write(ctx, target, merged); err != nil {
		return "", fmt.Errorf("write %s: %w", target, err)
	}
	apis, err := p.reload(ctx)
	if err != nil {
		_ = p.fs.Write(ctx, target, prevBytes)
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
		_ = p.fs.Write(ctx, target, prevBytes)
		_, _ = p.reload(ctx)
		return "", fmt.Errorf("api %q failed validation after reload", name)
	}
	msg := fmt.Sprintf("openapi: wrote %s to %s (%d ops, %d tools), verified", name, target, len(loaded.Operations), len(tools))
	return msg + p.credentialWarningSuffix(ctx, apis), nil
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
	return formatOpenAPIStatus(apis) + formatOpenAPICredentialStatus(ctx, apis, p.credentials) + "\n\n" + openapiHelp(), nil
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
  /openapi                         show status, env scopes/keys, and help
  /openapi add [-g] <name> <json>  write API to api.json, validate, reload, and verify
  /openapi -u                      reload api.json and referenced spec files
                                   (warns when env: auth/header refs are unset)

JSON format matches one apis entry in api.json, e.g.:
  {"baseUrl":"https://api.example.com","paths":{"/ping":{"get":{"operationId":"ping"}}}}
  {"path":"api/petstore.json","baseUrl":"https://api.example.com","auth":{"type":"bearer","token":"env:TOKEN"}}

Notes:
  add writes to local api.json by default; -g writes to global:api.json
  -g copies local spec files to global: (same relative path) and updates path in the index
  when enableLocal is off, only -g is allowed
  See docs/guides/tools.zh.md for full api.json format`
}

func (p *openapiProvider) specLoader(ctx context.Context) specLoader {
	return func(rel string) ([]byte, error) {
		return p.fs.Read(ctx, rel)
	}
}

func (p *openapiProvider) loadAPIs(ctx context.Context) ([]apiConfig, error) {
	loadSpec := p.specLoader(ctx)

	seen := make(map[string]struct{})
	var out []apiConfig
	for _, rel := range p.files {
		raw, err := p.fs.Read(ctx, rel)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			slog.Warn("api.json ignored", "path", rel, "error", err)
			continue
		}
		apis, err := parseIndexFile(rel, raw, loadSpec)
		if err != nil {
			slog.Warn("api.json ignored", "path", rel, "error", err)
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

func (c *openapiSyncCommand) SanitizeArgsForLog(args string) string {
	return agentkit.RedactSlashAddNamePayload(args)
}

func (c *openapiSyncCommand) CommandExec(ctx context.Context, args string) (string, error) {
	update, rest := peelUpdateFlag(strings.Fields(strings.TrimSpace(args)))
	switch {
	case update:
		apis, err := c.provider.reload(ctx)
		if err != nil {
			return "", err
		}
		return c.provider.summarizeReload(ctx, apis), nil
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
