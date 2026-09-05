package openapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/lengzhao/agentkit/cap/credentials"
	rtcredentials "github.com/lengzhao/agentkit/runtime/credentials"
)

const (
	defaultCallTimeout  = 30 * time.Second
	defaultCallMaxBytes = 1 << 20
)

type callResult struct {
	Status    int                 `json:"status"`
	Headers   map[string][]string `json:"headers,omitempty"`
	Body      json.RawMessage     `json:"body,omitempty"`
	Truncated bool                `json:"truncated,omitempty"`
}

func (p *openapiProvider) call(ctx context.Context, api apiConfig, op operationConfig, input json.RawMessage) (string, error) {
	args, err := decodeArguments(input)
	if err != nil {
		return "", err
	}
	if err := applyBinds(ctx, api.Binds, args); err != nil {
		return "", err
	}

	path, err := substitutePathParams(op, args)
	if err != nil {
		return "", err
	}

	query := url.Values{}
	for _, param := range op.Parameters {
		if param.In != "query" {
			continue
		}
		v, ok := args[param.Name]
		if !ok {
			if param.Required && !api.isBoundParameter(param.In, param.Name) {
				return "", fmt.Errorf("operation %q: missing required query parameter %q", op.OperationID, param.Name)
			}
			continue
		}
		query.Set(api.wireParamName(param.In, param.Name), stringifyValue(v))
	}

	headers := http.Header{}
	for k, v := range api.Headers {
		headers.Set(k, v)
	}
	for _, param := range op.Parameters {
		if param.In != "header" {
			continue
		}
		v, ok := args[param.Name]
		if !ok {
			if param.Required && !api.isBoundParameter(param.In, param.Name) {
				return "", fmt.Errorf("operation %q: missing required header parameter %q", op.OperationID, param.Name)
			}
			continue
		}
		headers.Set(api.wireParamName(param.In, param.Name), stringifyValue(v))
	}
	if err := applyBindOnlyParams(op, api.Binds, args, &path, query, headers); err != nil {
		return "", err
	}
	if strings.Contains(path, "{") {
		return "", fmt.Errorf("operation %q: path %q has unresolved placeholders", op.OperationID, op.Path)
	}

	if err := applyAuth(ctx, api.Auth, p.credentials, headers, query); err != nil {
		return "", fmt.Errorf("operation %q: %w", op.OperationID, err)
	}

	target := api.BaseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if op.RequestBody != nil {
		if raw, ok := args["body"]; ok {
			encoded, err := json.Marshal(raw)
			if err != nil {
				return "", fmt.Errorf("operation %q: encode body: %w", op.OperationID, err)
			}
			bodyReader = strings.NewReader(string(encoded))
			headers.Set("Content-Type", "application/json")
		} else if op.RequestBody.Required {
			return "", fmt.Errorf("operation %q: missing required body", op.OperationID)
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout(api))
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, op.Method, target, bodyReader)
	if err != nil {
		return "", fmt.Errorf("operation %q: build request: %w", op.OperationID, err)
	}
	req.Header = headers

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("operation %q: %w", op.OperationID, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(defaultCallMaxBytes)+1))
	if err != nil {
		return "", fmt.Errorf("operation %q: read response: %w", op.OperationID, err)
	}
	truncated := len(raw) > defaultCallMaxBytes
	if truncated {
		raw = raw[:defaultCallMaxBytes]
	}

	result := callResult{
		Status:    resp.StatusCode,
		Headers:   map[string][]string(resp.Header),
		Truncated: truncated,
		Body:      encodeResponseBody(resp.Header.Get("Content-Type"), raw),
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("operation %q: encode result: %w", op.OperationID, err)
	}
	return string(encoded), nil
}

func substitutePathParams(op operationConfig, args map[string]any) (string, error) {
	path := op.Path
	for _, param := range op.Parameters {
		if param.In != "path" {
			continue
		}
		v, ok := args[param.Name]
		if !ok || stringifyValue(v) == "" {
			return "", fmt.Errorf("operation %q: missing required path parameter %q", op.OperationID, param.Name)
		}
		path = strings.ReplaceAll(path, "{"+param.Name+"}", url.PathEscape(stringifyValue(v)))
	}
	return path, nil
}

func applyAuth(ctx context.Context, auth *authConfig, creds credentials.Store, headers http.Header, query url.Values) error {
	if auth == nil {
		return nil
	}
	switch strings.ToLower(auth.Type) {
	case "bearer":
		token, err := resolveSecret(ctx, auth.Token, creds)
		if err != nil {
			return fmt.Errorf("auth token: %w", err)
		}
		headers.Set("Authorization", "Bearer "+token)
	case "header":
		if auth.Name == "" {
			return fmt.Errorf("auth type header needs name")
		}
		value, err := resolveSecret(ctx, auth.Value, creds)
		if err != nil {
			return fmt.Errorf("auth value: %w", err)
		}
		headers.Set(auth.Name, value)
	case "query":
		if auth.Name == "" {
			return fmt.Errorf("auth type query needs name")
		}
		value, err := resolveSecret(ctx, auth.Value, creds)
		if err != nil {
			return fmt.Errorf("auth value: %w", err)
		}
		query.Set(auth.Name, value)
	case "basic":
		password, err := resolveSecret(ctx, auth.Password, creds)
		if err != nil {
			return fmt.Errorf("auth password: %w", err)
		}
		token := base64.StdEncoding.EncodeToString([]byte(auth.Username + ":" + password))
		headers.Set("Authorization", "Basic "+token)
	case "":
		return nil
	default:
		return fmt.Errorf("unsupported auth type %q", auth.Type)
	}
	return nil
}

// resolveSecret mirrors tool/mcp's env resolution: values prefixed "env:" go
// through credentials first, falling back to the process environment.
func resolveSecret(ctx context.Context, value string, creds credentials.Store) (string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "env:") {
		return value, nil
	}
	if creds != nil {
		secret, err := creds.Resolve(ctx, value)
		if err == nil {
			return secret.Value, nil
		}
	}
	key := rtcredentials.EnvKey(value)
	if v := os.Getenv(key); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("credential %q not found", value)
}

func decodeArguments(input json.RawMessage) (map[string]any, error) {
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

func stringifyValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		s := string(b)
		return strings.Trim(s, `"`)
	}
}

func encodeResponseBody(contentType string, raw []byte) json.RawMessage {
	mime := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mime == "application/json" || strings.HasSuffix(mime, "+json") {
		trimmed := strings.TrimSpace(string(raw))
		if trimmed != "" && json.Valid([]byte(trimmed)) {
			return json.RawMessage(trimmed)
		}
	}
	encoded, err := json.Marshal(string(raw))
	if err != nil {
		return json.RawMessage(`""`)
	}
	return json.RawMessage(encoded)
}
