package openapi

import (
	"context"
	"fmt"
	"sort"
	"strings"

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
			if _, err := resolveSecret(ctx, ref, p.credentials); err != nil {
				missing = append(missing, pair{apiName: api.Name, ref: ref})
			}
		}
	}
	if len(missing) == 0 {
		return ""
	}
	parts := make([]string, 0, len(missing))
	envKeys := make(map[string]struct{})
	for _, m := range missing {
		parts = append(parts, fmt.Sprintf("%s %s", m.apiName, m.ref))
		if k := rtcredentials.EnvKey(m.ref); k != "" {
			envKeys[k] = struct{}{}
		}
	}
	hintParts := make([]string, 0, len(envKeys))
	for k := range envKeys {
		hintParts = append(hintParts, k+"=<value>")
	}
	sort.Strings(hintParts)
	return "\nwarning: missing credentials: " + strings.Join(parts, ", ") +
		"\nhint: /env add " + strings.Join(hintParts, " ")
}

func (p *openapiProvider) summarizeReload(ctx context.Context, apis []apiConfig) string {
	return summarizeAPIs(apis) + p.credentialWarningSuffix(ctx, apis)
}
