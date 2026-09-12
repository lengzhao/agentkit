package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit/cap/credentials"
	rtcredentials "github.com/lengzhao/agentkit/runtime/credentials"
)

func envKeysFromServer(server serverConfig) []string {
	keys := make(map[string]struct{})
	rtcredentials.CollectEnvKeys(server.URL, keys)
	for _, v := range server.Env {
		rtcredentials.CollectEnvKeys(v, keys)
	}
	for _, v := range server.Headers {
		rtcredentials.CollectEnvKeys(v, keys)
	}
	for _, arg := range server.Args {
		rtcredentials.CollectEnvKeys(arg, keys)
	}
	for _, v := range server.Command {
		rtcredentials.CollectEnvKeys(v, keys)
	}
	out := make([]string, 0, len(keys))
	for k := range keys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func formatMCPCredentialStatus(ctx context.Context, servers []serverConfig, creds credentials.Store) string {
	if len(servers) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\ncredentials (declare env:KEY in mcp.json; set with /env add SCOPE KEY=VALUE):")
	any := false
	names := make([]string, 0, len(servers))
	for _, s := range servers {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	byName := make(map[string]serverConfig, len(servers))
	for _, s := range servers {
		byName[s.Name] = s
	}
	for _, name := range names {
		server := byName[name]
		envKeys := envKeysFromServer(server)
		if len(envKeys) == 0 {
			continue
		}
		any = true
		scope := CredentialScope(name)
		b.WriteString("\n  ")
		b.WriteString(scope)
		b.WriteString(":")
		for _, key := range envKeys {
			b.WriteString("\n    ")
			b.WriteString(key)
			b.WriteString(" — ")
			b.WriteString(credentialKeyStatus(ctx, creds, scope, key))
		}
	}
	if !any {
		b.WriteString("\n  (none declared)")
	}
	return b.String()
}

func credentialKeyStatus(ctx context.Context, creds credentials.Store, scope, key string) string {
	if creds == nil {
		return "credentials store not configured"
	}
	ref := "env:" + key
	if _, err := creds.Resolve(ctx, scope, ref); err != nil {
		return fmt.Sprintf("missing (/env add %s %s=<value>)", scope, key)
	}
	return "ok"
}
