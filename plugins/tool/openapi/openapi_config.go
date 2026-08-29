package openapi

import (
	"encoding/json"
	"fmt"
	"strings"
)

type rawServer struct {
	URL string `json:"url"`
}

type rawParameter struct {
	Name        string          `json:"name"`
	In          string          `json:"in"`
	Required    bool            `json:"required"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

type rawMediaType struct {
	Schema json.RawMessage `json:"schema"`
}

type rawRequestBody struct {
	Required bool                    `json:"required"`
	Content  map[string]rawMediaType `json:"content"`
}

type rawOperation struct {
	OperationID string          `json:"operationId"`
	Summary     string          `json:"summary"`
	Description string          `json:"description"`
	Parameters  []rawParameter  `json:"parameters"`
	RequestBody *rawRequestBody `json:"requestBody"`
}

type rawAuth struct {
	Type     string `json:"type"`
	Token    string `json:"token"`
	Name     string `json:"name"`
	Value    string `json:"value"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type rawPaths map[string]map[string]rawOperation

// rawSpecDocument is the shape of a file referenced by an index entry's
// specFile: a plain OpenAPI document with no wiring fields, so a downloaded
// spec works unmodified.
type rawSpecDocument struct {
	Servers []rawServer `json:"servers"`
	Paths   rawPaths    `json:"paths"`
}

// rawAPIEntry is one named entry inside api.json's "apis" index. It either
// points at an external OpenAPI document via specFile, or inlines paths
// directly for small hand-written APIs — not both.
type rawAPIEntry struct {
	SpecFile        string            `json:"specFile"`
	BaseURL         string            `json:"baseUrl"`
	Servers         []rawServer       `json:"servers"`
	Prefix          string            `json:"prefix"`
	Headers         map[string]string `json:"headers"`
	Auth            *rawAuth          `json:"auth"`
	AllowOperations []string          `json:"allowOperations"`
	DenyOperations  []string          `json:"denyOperations"`
	TimeoutSeconds  int               `json:"timeoutSeconds"`
	Paths           rawPaths          `json:"paths"`
}

// rawIndexDocument is api.json's top-level shape: an index naming APIs and
// wiring each one (baseUrl override, auth, prefix, allow/denyOperations) to
// either an external OpenAPI spec file or inline paths.
type rawIndexDocument struct {
	Apis map[string]rawAPIEntry `json:"apis"`
}

type paramConfig struct {
	Name        string
	In          string
	Required    bool
	Description string
	Schema      json.RawMessage
}

type requestBodyConfig struct {
	Required bool
	Schema   json.RawMessage
}

type operationConfig struct {
	OperationID string
	Method      string
	Path        string
	Summary     string
	Description string
	Parameters  []paramConfig
	RequestBody *requestBodyConfig
}

type authConfig struct {
	Type     string
	Token    string
	Name     string
	Value    string
	Username string
	Password string
}

type apiConfig struct {
	Name            string
	Source          string
	BaseURL         string
	Prefix          string
	Headers         map[string]string
	Auth            *authConfig
	AllowOperations []string
	DenyOperations  []string
	TimeoutSeconds  int
	Operations      []operationConfig
}

var validParamLocations = map[string]bool{"path": true, "query": true, "header": true}

// specLoader resolves and reads the file an index entry's specFile points at.
// The provider implements it over workspace.Service; tests can supply a
// simpler stub.
type specLoader func(relPath string) ([]byte, error)

// parseIndexFile parses one api.json index document into its named
// apiConfigs. Each entry under "apis" either points at an external OpenAPI
// document via specFile (loaded through loadSpec) or inlines paths directly.
func parseIndexFile(path string, raw []byte, loadSpec specLoader) ([]apiConfig, error) {
	var doc rawIndexDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(doc.Apis) == 0 {
		return nil, fmt.Errorf("%s declares no apis", path)
	}

	out := make([]apiConfig, 0, len(doc.Apis))
	for name, entry := range doc.Apis {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		cfg, err := buildAPIConfig(name, path, entry, loadSpec)
		if err != nil {
			return nil, err
		}
		out = append(out, cfg)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s declares no apis", path)
	}
	return out, nil
}

func upsertAPIJSON(existing []byte, name string, raw json.RawMessage) ([]byte, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("api name is required")
	}
	var entry rawAPIEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, fmt.Errorf("parse api json: %w", err)
	}

	var doc rawIndexDocument
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &doc); err != nil {
			return nil, fmt.Errorf("parse api.json: %w", err)
		}
	}
	if doc.Apis == nil {
		doc.Apis = make(map[string]rawAPIEntry)
	}
	doc.Apis[name] = entry
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func parseAPIEntryJSON(name, source string, raw []byte, loadSpec specLoader) (apiConfig, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return apiConfig{}, fmt.Errorf("api name is required")
	}
	var entry rawAPIEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return apiConfig{}, fmt.Errorf("parse api json: %w", err)
	}
	return buildAPIConfig(name, source, entry, loadSpec)
}

func buildAPIConfig(name, path string, entry rawAPIEntry, loadSpec specLoader) (apiConfig, error) {
	specFile := strings.TrimSpace(entry.SpecFile)
	if specFile != "" && len(entry.Paths) > 0 {
		return apiConfig{}, fmt.Errorf("api %q in %s: specFile and inline paths are mutually exclusive", name, path)
	}

	paths := entry.Paths
	servers := entry.Servers
	if specFile != "" {
		specRaw, err := loadSpec(specFile)
		if err != nil {
			return apiConfig{}, fmt.Errorf("api %q in %s: load specFile %q: %w", name, path, specFile, err)
		}
		var spec rawSpecDocument
		if err := json.Unmarshal(specRaw, &spec); err != nil {
			return apiConfig{}, fmt.Errorf("api %q in %s: parse specFile %q: %w", name, path, specFile, err)
		}
		paths = spec.Paths
		if len(servers) == 0 {
			servers = spec.Servers
		}
	}

	baseURL := strings.TrimSpace(entry.BaseURL)
	if baseURL == "" && len(servers) > 0 {
		baseURL = strings.TrimSpace(servers[0].URL)
	}
	if baseURL == "" {
		return apiConfig{}, fmt.Errorf("api %q in %s needs baseUrl (or servers[0].url)", name, path)
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	cfg := apiConfig{
		Name:            name,
		Source:          path,
		BaseURL:         baseURL,
		Prefix:          strings.TrimSpace(entry.Prefix),
		Headers:         cloneStringMap(entry.Headers),
		AllowOperations: trimAll(entry.AllowOperations),
		DenyOperations:  trimAll(entry.DenyOperations),
		TimeoutSeconds:  entry.TimeoutSeconds,
	}
	if entry.Auth != nil {
		cfg.Auth = &authConfig{
			Type:     strings.TrimSpace(entry.Auth.Type),
			Token:    entry.Auth.Token,
			Name:     strings.TrimSpace(entry.Auth.Name),
			Value:    entry.Auth.Value,
			Username: entry.Auth.Username,
			Password: entry.Auth.Password,
		}
	}

	seen := make(map[string]string)
	for p, methods := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		for method, op := range methods {
			method = strings.ToUpper(strings.TrimSpace(method))
			if method == "" {
				continue
			}
			opCfg, err := buildOperation(method, p, op)
			if err != nil {
				return apiConfig{}, fmt.Errorf("api %q in %s: %w", name, path, err)
			}
			if prior, ok := seen[opCfg.OperationID]; ok {
				return apiConfig{}, fmt.Errorf("api %q in %s: duplicate operationId %q (%s and %s %s)",
					name, path, opCfg.OperationID, prior, method, p)
			}
			seen[opCfg.OperationID] = method + " " + p
			cfg.Operations = append(cfg.Operations, opCfg)
		}
	}
	if len(cfg.Operations) == 0 {
		return apiConfig{}, fmt.Errorf("api %q in %s declares no operations", name, path)
	}
	return cfg, nil
}

func buildOperation(method, path string, op rawOperation) (operationConfig, error) {
	opID := strings.TrimSpace(op.OperationID)
	if opID == "" {
		opID = defaultOperationID(method, path)
	}
	cfg := operationConfig{
		OperationID: opID,
		Method:      method,
		Path:        path,
		Summary:     strings.TrimSpace(op.Summary),
		Description: strings.TrimSpace(op.Description),
	}
	for _, p := range op.Parameters {
		pname := strings.TrimSpace(p.Name)
		if pname == "" {
			continue
		}
		in := strings.ToLower(strings.TrimSpace(p.In))
		if !validParamLocations[in] {
			return operationConfig{}, fmt.Errorf("operation %q: parameter %q has unsupported in %q", opID, pname, p.In)
		}
		cfg.Parameters = append(cfg.Parameters, paramConfig{
			Name:        pname,
			In:          in,
			Required:    p.Required,
			Description: strings.TrimSpace(p.Description),
			Schema:      p.Schema,
		})
	}
	if op.RequestBody != nil {
		if mt, ok := op.RequestBody.Content["application/json"]; ok && len(mt.Schema) > 0 {
			cfg.RequestBody = &requestBodyConfig{
				Required: op.RequestBody.Required,
				Schema:   mt.Schema,
			}
		}
	}
	return cfg, nil
}

// defaultOperationID mirrors the common OpenAPI-generator fallback: lowercase
// method plus the path with separators collapsed and braces stripped, e.g.
// "GET /pets/{id}" -> "get_pets_id".
func defaultOperationID(method, path string) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(method))
	for _, r := range path {
		switch {
		case r == '{' || r == '}':
			continue
		case r == '/' || r == '-' || r == '.':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (a apiConfig) toolPrefix() string {
	if a.Prefix != "" {
		return a.Prefix
	}
	return a.Name + "__"
}

func (a apiConfig) allowsOperation(id string) bool {
	if len(a.AllowOperations) > 0 {
		for _, allowed := range a.AllowOperations {
			if allowed == id {
				return true
			}
		}
		return false
	}
	for _, denied := range a.DenyOperations {
		if denied == id {
			return false
		}
	}
	return true
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
