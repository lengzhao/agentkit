package openapi

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

type rawServer struct {
	URL string `json:"url"`
}

type rawAuth struct {
	Type     string `json:"type"`
	Token    string `json:"token"`
	Name     string `json:"name"`
	Value    string `json:"value"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type rawBind struct {
	From string `json:"from"`
	In   string `json:"in"`
	Name string `json:"name,omitempty"`
}

// rawAPIEntry is one named entry inside api.json's index. Wiring fields (baseUrl,
// auth, bind, prefix, …) live here; OpenAPI paths live in the document file
// referenced by path (or legacy specFile). Inline paths in the index entry is
// deprecated but still supported for tiny hand-written APIs.
type rawAPIEntry struct {
	Path            string             `json:"path"`
	SpecFile        string             `json:"specFile"`
	BaseURL         string             `json:"baseUrl"`
	Servers         []rawServer        `json:"servers"`
	Prefix          string             `json:"prefix"`
	Headers         map[string]string  `json:"headers"`
	Auth            *rawAuth           `json:"auth"`
	AllowOperations []string           `json:"allowOperations"`
	DenyOperations  []string           `json:"denyOperations"`
	TimeoutSeconds  int                `json:"timeoutSeconds"`
	Bind            map[string]rawBind `json:"bind,omitempty"`
	Paths           json.RawMessage    `json:"paths"`
}

// rawIndexDocument is api.json's top-level shape: an index naming APIs and
// wiring each one to a separate OpenAPI document plus runtime overrides.
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
	DocPath         string
	BaseURL         string
	Prefix          string
	Headers         map[string]string
	Auth            *authConfig
	Binds           []bindConfig
	AllowOperations []string
	DenyOperations  []string
	TimeoutSeconds  int
	Operations      []operationConfig
}

var validParamLocations = map[string]bool{"path": true, "query": true, "header": true}

// specLoader resolves and reads the OpenAPI document an index entry's path points at.
// The provider implements it over workspace.Service; tests can supply a simpler stub.
type specLoader func(relPath string) ([]byte, error)

// entryDocumentPath returns the workspace-relative OpenAPI document path for an index entry.
// path is preferred; specFile is a legacy alias.
func entryDocumentPath(entry rawAPIEntry) (string, error) {
	docPath := strings.TrimSpace(entry.Path)
	legacy := strings.TrimSpace(entry.SpecFile)
	switch {
	case docPath != "" && legacy != "" && docPath != legacy:
		return "", fmt.Errorf("path %q and specFile %q disagree", docPath, legacy)
	case docPath != "":
		return docPath, nil
	case legacy != "":
		return legacy, nil
	default:
		return "", nil
	}
}

// parseIndexFile parses one api.json index document into its named apiConfigs.
// Each entry under "apis" references an OpenAPI document via path (preferred) or
// legacy specFile, or inlines paths directly.
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
	docPath, err := entryDocumentPath(entry)
	if err != nil {
		return apiConfig{}, fmt.Errorf("api %q in %s: %w", name, path, err)
	}
	if docPath != "" && len(entry.Paths) > 0 {
		return apiConfig{}, fmt.Errorf("api %q in %s: path and inline paths are mutually exclusive", name, path)
	}

	doc, err := loadOpenAPIDocument(name, docPath, entry, loadSpec)
	if err != nil {
		return apiConfig{}, fmt.Errorf("api %q in %s: %w", name, path, err)
	}

	servers := entry.Servers
	if len(servers) == 0 && doc.Servers != nil {
		for _, srv := range doc.Servers {
			if srv == nil {
				continue
			}
			servers = append(servers, rawServer{URL: srv.URL})
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
		DocPath:         docPath,
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
	binds, err := parseBinds(entry.Bind)
	if err != nil {
		return apiConfig{}, fmt.Errorf("api %q in %s: %w", name, path, err)
	}
	cfg.Binds = binds

	ops, err := operationsFromDocument(doc)
	if err != nil {
		return apiConfig{}, err
	}
	cfg.Operations = ops
	return cfg, nil
}

func loadOpenAPIDocument(name, docPath string, entry rawAPIEntry, loadSpec specLoader) (*openapi3.T, error) {
	loader := &openapi3.Loader{IsExternalRefsAllowed: true}
	if loadSpec != nil && docPath != "" {
		loader.ReadFromURIFunc = readURIFunc(docPath, loadSpec)
	}

	if docPath != "" {
		specRaw, err := loadSpec(docPath)
		if err != nil {
			return nil, fmt.Errorf("load path %q: %w", docPath, err)
		}
		location := &url.URL{Path: docPath}
		return loader.LoadFromDataWithPath(specRaw, location)
	}

	if len(entry.Paths) == 0 {
		return nil, fmt.Errorf("needs path to an OpenAPI document (or legacy inline paths)")
	}
	var pathsObj any
	if err := json.Unmarshal(entry.Paths, &pathsObj); err != nil {
		return nil, fmt.Errorf("parse inline paths: %w", err)
	}
	docObj := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":   name,
			"version": "1.0.0",
		},
		"paths": pathsObj,
	}
	if len(entry.Servers) > 0 {
		docObj["servers"] = entry.Servers
	}
	data, err := json.Marshal(docObj)
	if err != nil {
		return nil, err
	}
	return loader.LoadFromData(data)
}

func readURIFunc(docPath string, loadSpec specLoader) openapi3.ReadFromURIFunc {
	specDir := path.Dir(docPath)
	return func(_ *openapi3.Loader, uri *url.URL) ([]byte, error) {
		ref := uri.String()
		switch uri.Scheme {
		case "":
			// Relative ref from the spec file directory.
		case "file":
			ref = uri.Path
		default:
			return nil, fmt.Errorf("unsupported ref scheme %q", uri.Scheme)
		}
		if !strings.HasPrefix(ref, "/") && !strings.Contains(ref, "://") {
			ref = path.Join(specDir, ref)
		}
		return loadSpec(ref)
	}
}

func operationsFromDocument(doc *openapi3.T) ([]operationConfig, error) {
	if doc == nil || doc.Paths == nil || doc.Paths.Len() == 0 {
		return nil, fmt.Errorf("declares no operations")
	}

	seen := make(map[string]string)
	var out []operationConfig
	for _, p := range doc.Paths.Keys() {
		pathItem := doc.Paths.Value(p)
		if pathItem == nil {
			continue
		}
		for method, op := range pathItem.Operations() {
			if op == nil {
				continue
			}
			opCfg, err := buildOperation(strings.ToUpper(method), p, op)
			if err != nil {
				return nil, err
			}
			if prior, ok := seen[opCfg.OperationID]; ok {
				return nil, fmt.Errorf("duplicate operationId %q (%s and %s %s)",
					opCfg.OperationID, prior, opCfg.Method, opCfg.Path)
			}
			seen[opCfg.OperationID] = opCfg.Method + " " + opCfg.Path
			out = append(out, opCfg)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("declares no operations")
	}
	return out, nil
}

func buildOperation(method, path string, op *openapi3.Operation) (operationConfig, error) {
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
	for _, paramRef := range op.Parameters {
		if paramRef == nil || paramRef.Value == nil {
			continue
		}
		param := paramRef.Value
		pname := strings.TrimSpace(param.Name)
		if pname == "" {
			continue
		}
		in := strings.ToLower(strings.TrimSpace(param.In))
		if !validParamLocations[in] {
			return operationConfig{}, fmt.Errorf("operation %q: parameter %q has unsupported in %q", opID, pname, param.In)
		}
		cfg.Parameters = append(cfg.Parameters, paramConfig{
			Name:        pname,
			In:          in,
			Required:    param.Required,
			Description: strings.TrimSpace(param.Description),
			Schema:      marshalSchemaRef(param.Schema),
		})
	}
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		if mt := op.RequestBody.Value.Content["application/json"]; mt != nil && mt.Schema != nil {
			cfg.RequestBody = &requestBodyConfig{
				Required: op.RequestBody.Value.Required,
				Schema:   marshalSchemaRef(mt.Schema),
			}
		}
	}
	return cfg, nil
}

func marshalSchemaRef(ref *openapi3.SchemaRef) json.RawMessage {
	if ref == nil || ref.Value == nil {
		return nil
	}
	raw, err := json.Marshal(ref.Value)
	if err != nil {
		return nil
	}
	return json.RawMessage(raw)
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
