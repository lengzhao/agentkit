package openapi

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit/cap/credentials"
	rtcredentials "github.com/lengzhao/agentkit/runtime/credentials"
)

// envSecretRefsFromAPI returns env: credential refs from auth and static headers.
func envSecretRefsFromAPI(api apiConfig) []string {
	var refs []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, "env:") {
			refs = append(refs, v)
		}
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
	return refs
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
		seen := make(map[string]struct{})
		for _, ref := range envSecretRefsFromAPI(api) {
			k := rtcredentials.EnvKey(ref)
			if k == "" {
				continue
			}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			lines = append(lines, line{apiName: api.Name, key: k})
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
		scope := CredentialScope(ln.apiName)
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
		ref     string
	}
	var missing []pair
	seen := make(map[string]struct{})
	for _, api := range apis {
		for _, ref := range envSecretRefsFromAPI(api) {
			key := api.Name + "\x00" + ref
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			if _, err := resolveSecret(ctx, CredentialScope(api.Name), ref, p.credentials); err != nil {
				missing = append(missing, pair{apiName: api.Name, ref: ref})
			}
		}
	}
	if len(missing) == 0 {
		return ""
	}
	parts := make([]string, 0, len(missing))
	hintParts := make([]string, 0, len(missing))
	for _, m := range missing {
		parts = append(parts, fmt.Sprintf("%s %s", m.apiName, m.ref))
		if k := rtcredentials.EnvKey(m.ref); k != "" {
			hintParts = append(hintParts, fmt.Sprintf("/env add %s %s=<value>", CredentialScope(m.apiName), k))
		}
	}
	sort.Strings(hintParts)
	return "\nwarning: missing credentials: " + strings.Join(parts, ", ") +
		"\nhint: " + strings.Join(hintParts, "; ")
}

func (p *openapiProvider) summarizeReload(ctx context.Context, apis []apiConfig) string {
	return summarizeAPIs(apis) + p.credentialWarningSuffix(ctx, apis)
}
