package credentials

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
)

type integrationStore struct {
	*envStore
}

// NewIntegrations registers credentials/integrations for MCP and OpenAPI with /env and scoped Resolve.
func NewIntegrations(cfg Config, deps EnvDeps) (credentials.Store, error) {
	backend, err := newEnvStore(cfg, deps, defaultEncryptedFile, false)
	if err != nil {
		return nil, err
	}
	scoped, err := buildConfigScoped(cfg.Prefix, cfg.ScopedEnv)
	if err != nil {
		return nil, err
	}
	backend.setConfigScoped(scoped)
	return &integrationStore{envStore: backend}, nil
}

func (s *integrationStore) Resolve(ctx context.Context, scope string, ref string) (credentials.Secret, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return credentials.Secret{}, fmt.Errorf("credential scope is required")
	}
	key := envKey(ref)
	if key == "" {
		return credentials.Secret{}, fmt.Errorf("credential ref %q is invalid", ref)
	}
	if value, ok := s.lookupScopedValue(ctx, scope, ref); ok {
		return credentials.Secret{Ref: ref, Value: value}, nil
	}
	if isIntegrationScope(scope) {
		return credentials.Secret{}, fmt.Errorf("credential %q is not set for scope %q (/env add %s %s=<value>)", key, scope, scope, key)
	}
	return credentials.Secret{}, fmt.Errorf("credential %q is not set for scope %q", key, scope)
}

func (s *integrationStore) lookupScopedValue(ctx context.Context, scope string, ref string) (string, bool) {
	if secret, ok := secretFromContext(ctx, ref); ok && secret.Value != "" {
		return secret.Value, true
	}
	key := envKey(ref)
	if key == "" {
		return "", false
	}
	storageKey := key
	if s.prefix != "" {
		storageKey = s.prefix + key
	}
	scoped := scopedStorageKey(scope, storageKey)
	value, ok := s.lookupStorageValue(scoped)
	return value, ok
}

func (s *integrationStore) addScopedPairs(ctx context.Context, scope string, pairs []string) (envAddOutcome, error) {
	scope = strings.TrimSpace(scope)
	if err := validateIntegrationScope(scope); err != nil {
		return envAddOutcome{}, err
	}
	updates := make(map[string]string, len(pairs))
	refs := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		key, value, err := parseEnvPair(pair)
		if err != nil {
			return envAddOutcome{}, err
		}
		if value == "" {
			return envAddOutcome{}, fmt.Errorf("%s: value is required", key)
		}
		storageKey := key
		if s.prefix != "" {
			storageKey = s.prefix + key
		}
		updates[scopedStorageKey(scope, storageKey)] = value
		refs = append(refs, "env:"+key)
	}
	verify := func(ctx context.Context, ref string) error {
		secret, err := s.Resolve(ctx, scope, ref)
		if err != nil {
			return err
		}
		if secret.Value == "" {
			return fmt.Errorf("value is empty")
		}
		return nil
	}
	return s.addUpdates(ctx, updates, refs, verify)
}

func formatEnvAddOutcome(outcome envAddOutcome, scope string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "env: wrote %d key(s) for scope %s to %s, reloaded %d key(s), verified",
		outcome.Wrote, strings.TrimSpace(scope), outcome.Path, outcome.Reloaded)
	if len(outcome.Overwrites) > 0 {
		b.WriteString("\noverwrote:")
		for _, key := range outcome.Overwrites {
			b.WriteString("\n  - ")
			b.WriteString(key)
		}
	}
	if len(outcome.Warnings) > 0 {
		b.WriteString("\nwarnings:")
		for _, w := range outcome.Warnings {
			b.WriteString("\n  - ")
			b.WriteString(w)
		}
	}
	return b.String()
}

// EnvPairs implements credentials.EnvPairResolver.
func (s *integrationStore) EnvPairs(ctx context.Context, scope string, opts credentials.EnvPairsOptions) ([]string, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return nil, fmt.Errorf("credential scope is required")
	}
	keys := append([]string(nil), opts.Keys...)
	if len(keys) == 0 {
		keys = s.listEnvKeysForScope(scope)
	}
	if len(keys) == 0 {
		return nil, nil
	}
	return injectScopedEnv(ctx, nil, scope, keys, s), nil
}

func (s *integrationStore) statusWithHelp() string {
	var b strings.Builder
	b.WriteString(s.formatStatus())
	b.WriteString(s.formatFlatEncryptedKeys())
	b.WriteString(s.formatScopedEnvKeyInventory())
	b.WriteString("\n\n")
	b.WriteString(integrationEnvHelp())
	return b.String()
}

func integrationEnvHelp() string {
	return `Usage:
  /env                                    show status, env key inventory, and help
  /env add SCOPE KEY=VALUE  write one scoped secret (VALUE is not split on spaces; use multiple /env add for more keys)
  /env -u                                 reload secrets and dotenv from disk

Notes:
  SCOPE is mcp.<server>, openapi.<api>, or shell-bash.<cmd> (command basename after the dot)
  Scoped values live in secrets.enc.json (or dotenv) as SCOPE::KEY; undecryptable enc entries are preserved with abnormal=true (see /env status)
  L1 credentials.integrations.config.scopedEnv preloads values at startup; same SCOPE::KEY in secrets.enc.json wins over scopedEnv
  Resolve: context > scoped store (encrypted/file, then L1 scopedEnv)`
}

func (s *integrationStore) Commands() []agentkit.Command {
	return []agentkit.Command{&integrationEnvCommand{store: s}}
}

type integrationEnvCommand struct {
	store *integrationStore
}

func (c *integrationEnvCommand) Name() string  { return "env" }
func (c *integrationEnvCommand) Alias() string { return "" }
func (c *integrationEnvCommand) Description() string {
	return "Show integration secrets cache, write KEY=VALUE pairs, or reload with -u"
}

func (c *integrationEnvCommand) SanitizeArgsForLog(args string) string {
	return redactEnvAddArgsForLog(args)
}

func (c *integrationEnvCommand) CommandExec(ctx context.Context, args string) (string, error) {
	update, sub, scope, pair, err := parseEnvSlashArgs(args)
	if err != nil {
		return "", err
	}
	if update && sub != "" {
		return "", fmt.Errorf("usage: /env | /env add SCOPE KEY=VALUE | /env -u")
	}
	switch {
	case update:
		count, err := c.store.reload(ctx)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("env: reloaded %d key(s) from disk", count), nil
	case sub == "add":
		outcome, err := c.store.addScopedPairs(ctx, scope, []string{pair})
		if err != nil {
			return "", err
		}
		return formatEnvAddOutcome(outcome, scope), nil
	case sub == "" && !update:
		return c.store.statusWithHelp(), nil
	default:
		return "", fmt.Errorf("usage: /env | /env add SCOPE KEY=VALUE | /env -u")
	}
}

var (
	_ credentials.Store            = (*integrationStore)(nil)
	_ credentials.EnvPairResolver  = (*integrationStore)(nil)
	_ agentkit.CommandProvider     = (*integrationStore)(nil)
	_ agentkit.CommandLogSanitizer = (*integrationEnvCommand)(nil)
)
