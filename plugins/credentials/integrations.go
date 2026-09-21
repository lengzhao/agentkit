package credentials

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
	rtcredentials "github.com/lengzhao/agentkit/runtime/credentials"
)

const (
	defaultMCPManifestFile     = "global:mcp.json"
	defaultOpenAPIManifestFile = "global:api.json"
)

type integrationStore struct {
	*envStore
	manifestPaths  []string
	mu             sync.RWMutex
	allow          map[string]map[string]struct{}
	manifestMaxMod int64
}

// NewIntegrations registers credentials/integrations for MCP and OpenAPI with /env and scoped Resolve.
func NewIntegrations(cfg Config, deps EnvDeps) (credentials.Store, error) {
	backend, err := newEnvStore(cfg, deps, defaultEncryptedFile, false)
	if err != nil {
		return nil, err
	}
	manifestPaths := append([]string(nil), cfg.ManifestFiles...)
	if len(manifestPaths) == 0 {
		manifestPaths = []string{defaultMCPManifestFile, defaultOpenAPIManifestFile}
	}
	scoped, err := buildConfigScoped(cfg.Prefix, cfg.ScopedEnv)
	if err != nil {
		return nil, err
	}
	backend.setConfigScoped(scoped)
	s := &integrationStore{
		envStore:      backend,
		manifestPaths: manifestPaths,
		allow:         make(map[string]map[string]struct{}),
	}
	if err := s.reloadManifest(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *integrationStore) Resolve(ctx context.Context, scope string, ref string) (credentials.Secret, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return credentials.Secret{}, fmt.Errorf("credential scope is required")
	}
	if err := s.ensureManifestFresh(ctx); err != nil {
		return credentials.Secret{}, err
	}
	key := rtcredentials.EnvKey(ref)
	if key == "" {
		return credentials.Secret{}, fmt.Errorf("credential ref %q is invalid", ref)
	}
	if value, ok := s.lookupScopedValue(ctx, scope, ref); ok {
		return credentials.Secret{Ref: ref, Value: value}, nil
	}
	if !s.scopeAllows(scope, key) {
		if rtcredentials.IsIntegrationScope(scope) {
			return credentials.Secret{}, fmt.Errorf("credential %q is not set for scope %q (/env add %s %s=<value>)", key, scope, scope, key)
		}
		return credentials.Secret{}, fmt.Errorf("credential %q is not declared for scope %q", key, scope)
	}
	value, err := s.lookupValue(ctx, ref)
	if err != nil {
		return credentials.Secret{}, err
	}
	return credentials.Secret{Ref: ref, Value: value}, nil
}

func (s *integrationStore) lookupScopedValue(ctx context.Context, scope string, ref string) (string, bool) {
	if secret, ok := rtcredentials.SecretFromContext(ctx, ref); ok && secret.Value != "" {
		return secret.Value, true
	}
	key := rtcredentials.EnvKey(ref)
	if key == "" {
		return "", false
	}
	storageKey := key
	if s.prefix != "" {
		storageKey = s.prefix + key
	}
	scoped := rtcredentials.ScopedStorageKey(scope, storageKey)
	value, ok := s.lookupStorageValue(scoped)
	return value, ok
}

func (s *integrationStore) addScopedPairs(ctx context.Context, scope string, pairs []string) (string, int, error) {
	scope = strings.TrimSpace(scope)
	if err := rtcredentials.ValidateIntegrationScope(scope); err != nil {
		return "", 0, err
	}
	if err := s.ensureManifestFresh(ctx); err != nil {
		return "", 0, err
	}
	updates := make(map[string]string, len(pairs))
	refs := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		key, value, err := parseEnvPair(pair)
		if err != nil {
			return "", 0, err
		}
		if value == "" {
			return "", 0, fmt.Errorf("%s: value is required", key)
		}
		storageKey := key
		if s.prefix != "" {
			storageKey = s.prefix + key
		}
		updates[rtcredentials.ScopedStorageKey(scope, storageKey)] = value
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

func (s *integrationStore) scopeAllows(scope string, key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys, ok := s.allow[scope]
	if !ok {
		return false
	}
	_, ok = keys[key]
	return ok
}

func (s *integrationStore) allowlistedEnvKeys(scope string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys, ok := s.allow[scope]
	if !ok || len(keys) == 0 {
		return nil
	}
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// EnvPairs implements credentials.EnvPairResolver.
func (s *integrationStore) EnvPairs(ctx context.Context, scope string, opts credentials.EnvPairsOptions) ([]string, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return nil, fmt.Errorf("credential scope is required")
	}
	if err := s.ensureManifestFresh(ctx); err != nil {
		return nil, err
	}
	keys := append([]string(nil), opts.Keys...)
	if len(keys) == 0 {
		keys = s.allowlistedEnvKeys(scope)
	}
	if len(keys) == 0 {
		keys = s.listEnvKeysForScope(scope)
	}
	if len(keys) == 0 {
		return nil, nil
	}
	return rtcredentials.InjectScopedEnv(ctx, nil, scope, keys, s), nil
}

func (s *integrationStore) ensureManifestFresh(ctx context.Context) error {
	maxMod, err := s.manifestsMaxModTime(ctx)
	if err != nil {
		return err
	}
	s.mu.RLock()
	fresh := s.manifestMaxMod == maxMod
	s.mu.RUnlock()
	if fresh {
		return nil
	}
	return s.reloadManifest(ctx)
}

func (s *integrationStore) resolveManifestPath(_ context.Context, rel string) (string, error) {
	return rel, nil
}

func (s *integrationStore) manifestsMaxModTime(ctx context.Context) (int64, error) {
	var maxMod int64
	for _, rel := range s.manifestPaths {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		path, err := s.resolveManifestPath(ctx, rel)
		if err != nil {
			return 0, err
		}
		info, err := s.fs.Stat(ctx, path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return 0, fmt.Errorf("stat manifest %q: %w", path, err)
		}
		if mod := info.ModTime.UnixNano(); mod > maxMod {
			maxMod = mod
		}
	}
	return maxMod, nil
}

func (s *integrationStore) reloadManifest(ctx context.Context) error {
	maxMod, err := s.manifestsMaxModTime(ctx)
	if err != nil {
		return err
	}
	parts := make([]map[string]map[string]struct{}, 0, len(s.manifestPaths))
	for _, rel := range s.manifestPaths {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		path, err := s.resolveManifestPath(ctx, rel)
		if err != nil {
			return err
		}
		data, err := s.fs.Read(ctx, path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("read manifest %q: %w", path, err)
		}
		part, err := manifestFromFile(data)
		if err != nil {
			return fmt.Errorf("parse manifest %q: %w", rel, err)
		}
		if len(part) > 0 {
			parts = append(parts, part)
		}
	}
	merged := rtcredentials.MergeManifests(parts...)
	s.mu.Lock()
	s.allow = merged
	s.manifestMaxMod = maxMod
	s.mu.Unlock()
	return nil
}

func manifestFromFile(data []byte) (map[string]map[string]struct{}, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, nil
	}
	if strings.Contains(trimmed, `"mcpServers"`) {
		return rtcredentials.ManifestFromMCPFile(data)
	}
	if strings.Contains(trimmed, `"apis"`) {
		return rtcredentials.ManifestFromAPIIndex(data)
	}
	if strings.Contains(trimmed, `"commands"`) {
		return rtcredentials.ManifestFromShellBashFile(data)
	}
	return nil, fmt.Errorf("unsupported manifest shape")
}

func (s *integrationStore) statusWithHelp() string {
	s.mu.RLock()
	scopes := len(s.allow)
	s.mu.RUnlock()
	var b strings.Builder
	b.WriteString(s.formatStatus())
	fmt.Fprintf(&b, ", %d scoped manifest entr(y/ies)", scopes)
	if len(s.manifestPaths) > 0 {
		b.WriteString("\nmanifest files:")
		for _, path := range s.manifestPaths {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			b.WriteString("\n  - ")
			b.WriteString(path)
		}
	}
	b.WriteString("\n\n")
	b.WriteString(integrationEnvHelp())
	return b.String()
}

func integrationEnvHelp() string {
	return `Usage:
  /env                                    show status and help
  /env add SCOPE KEY=VALUE [KEY=VALUE ...]  write scoped secrets, reload, and verify
  /env -u                                 reload secrets, dotenv, and integration manifests

Notes:
  SCOPE is mcp.<server>, openapi.<api>, or shell-bash.<cmd> (command basename after the dot)
  /env add does not require a manifest entry; mcp.json / api.json / shell-bash.json env entries declare keys for lookup after values are set
  Scoped values live in secrets.enc.json (or dotenv) as SCOPE::KEY
  L1 credentials.integrations.config.scopedEnv preloads values at startup; same SCOPE::KEY in secrets.enc.json wins over scopedEnv
  Resolve: context > scoped store (encrypted/file, then L1 scopedEnv) > manifest allowlist > flat fallback
  Manifest allowlists refresh automatically when mcp.json / api.json change on disk`
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
	update, rest := peelUpdateFlag(strings.Fields(strings.TrimSpace(args)))
	switch {
	case update:
		count, err := c.store.reload(ctx)
		if err != nil {
			return "", err
		}
		if err := c.store.reloadManifest(ctx); err != nil {
			return "", err
		}
		return fmt.Sprintf("env: reloaded %d key(s) from disk and refreshed manifests", count), nil
	case len(rest) >= 1 && rest[0] == "add":
		if len(rest) < 3 {
			return "", fmt.Errorf("usage: /env add SCOPE KEY=VALUE [KEY=VALUE ...] (SCOPE: mcp.<server>, openapi.<api>, or shell-bash.<cmd>)")
		}
		scope := rest[1]
		if strings.Contains(scope, "=") {
			return "", fmt.Errorf("usage: /env add SCOPE KEY=VALUE ...; SCOPE must be mcp.<server>, openapi.<api>, or shell-bash.<cmd>")
		}
		path, count, err := c.store.addScopedPairs(ctx, scope, rest[2:])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("env: wrote %d key(s) for scope %s to %s, verified", count, strings.TrimSpace(scope), path), nil
	case len(rest) == 0:
		return c.store.statusWithHelp(), nil
	default:
		return "", fmt.Errorf("usage: /env | /env add SCOPE KEY=VALUE ... | /env -u")
	}
}

var (
	_ credentials.Store         = (*integrationStore)(nil)
	_ credentials.EnvPairResolver = (*integrationStore)(nil)
	_ agentkit.CommandProvider         = (*integrationStore)(nil)
	_ agentkit.CommandLogSanitizer     = (*integrationEnvCommand)(nil)
)
