package openapi

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit/cap/credentials"
)

// envSecretKeysFromAPI returns deduplicated env var names declared as env: refs in api config.
func envSecretKeysFromAPI(api apiConfig) []string {
	seen := make(map[string]struct{})
	var keys []string
	add := func(v string) {
		s := strings.TrimSpace(v)
		if !strings.HasPrefix(s, "env:") {
			return
		}
		k := envKey(s)
		if k == "" {
			return
		}
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	add(api.BaseURL)
	if api.Auth != nil {
		add(api.Auth.Token)
		add(api.Auth.Value)
		add(api.Auth.Password)
	}
	for _, v := range api.Headers {
		add(v)
	}
	return keys
}

func formatOpenAPICredentialStatus(ctx context.Context, apis []apiConfig, creds credentials.Store) string {
	if len(apis) == 0 {
		return ""
	}
	type line struct {
		apiName string
		key     string
	}
	var lines []line
	for _, api := range apis {
		for _, key := range envSecretKeysFromAPI(api) {
			lines = append(lines, line{apiName: api.Name, key: key})
		}
	}
	var b strings.Builder
	b.WriteString("\ncredentials (declare env:KEY in api.json; set with /env add SCOPE KEY=VALUE):")
	if len(lines) == 0 {
		b.WriteString("\n  (none declared)")
		return b.String()
	}
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].apiName != lines[j].apiName {
			return lines[i].apiName < lines[j].apiName
		}
		return lines[i].key < lines[j].key
	})
	lastAPI := ""
	for _, ln := range lines {
		scope := credentialScope(ln.apiName)
		if ln.apiName != lastAPI {
			b.WriteString("\n  ")
			b.WriteString(scope)
			b.WriteString(":")
			lastAPI = ln.apiName
		}
		b.WriteString("\n    ")
		b.WriteString(ln.key)
		b.WriteString(" — ")
		b.WriteString(openapiCredentialKeyStatus(ctx, creds, scope, ln.key))
	}
	return b.String()
}

func openapiCredentialKeyStatus(ctx context.Context, creds credentials.Store, scope, key string) string {
	if creds == nil {
		return "credentials store not configured"
	}
	ref := "env:" + key
	if _, err := resolveSecret(ctx, scope, ref, creds); err != nil {
		return fmt.Sprintf("missing (/env add %s %s=<value>)", scope, key)
	}
	return "ok"
}

func (p *openapiProvider) credentialWarningSuffix(ctx context.Context, apis []apiConfig) string {
	type pair struct {
		apiName string
		key     string
	}
	var missing []pair
	for _, api := range apis {
		scope := credentialScope(api.Name)
		for _, key := range envSecretKeysFromAPI(api) {
			if _, err := resolveSecret(ctx, scope, "env:"+key, p.credentials); err != nil {
				missing = append(missing, pair{apiName: api.Name, key: key})
			}
		}
	}
	if len(missing) == 0 {
		return ""
	}
	parts := make([]string, 0, len(missing))
	hintParts := make([]string, 0, len(missing))
	for _, m := range missing {
		parts = append(parts, fmt.Sprintf("%s env:%s", m.apiName, m.key))
		hintParts = append(hintParts, fmt.Sprintf("/env add %s %s=<value>", credentialScope(m.apiName), m.key))
	}
	sort.Strings(hintParts)
	return "\nwarning: missing credentials: " + strings.Join(parts, ", ") +
		"\nhint: " + strings.Join(hintParts, "; ")
}

func (p *openapiProvider) summarizeReload(ctx context.Context, apis []apiConfig) string {
	return summarizeAPIs(apis) + p.credentialWarningSuffix(ctx, apis)
}
