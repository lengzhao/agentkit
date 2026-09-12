package credentials

import (
	"context"
	"fmt"
	"os"
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
	if !s.scopeAllows(scope, key) {
		return credentials.Secret{}, fmt.Errorf("credential %q is not declared for scope %q", key, scope)
	}
	value, err := s.lookupValue(ctx, ref)
	if err != nil {
		return credentials.Secret{}, err
	}
	return credentials.Secret{Ref: ref, Value: value}, nil
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

func (s *integrationStore) resolveManifestPath(ctx context.Context, rel string) (string, error) {
	path := rel
	if s.workspace != nil {
		resolved, err := s.workspace.Resolve(ctx, rel)
		if err != nil {
			return "", fmt.Errorf("resolve manifest %q: %w", rel, err)
		}
		path = resolved
	}
	return path, nil
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
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, fmt.Errorf("stat manifest %q: %w", path, err)
		}
		if mod := info.ModTime().UnixNano(); mod > maxMod {
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
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
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
  /env                      show status and help
  /env add KEY=VALUE [...]  append or update secrets.enc.json, reload, and verify
  /env -u                   reload secrets, dotenv, and integration manifests

Notes:
  Lookup priority: context secret > config env > encrypted file > dotenv file (no process env)
  Declare env:NAME in mcp.json / api.json; Resolve(scope, ref) allows only refs listed for that entry (scope mcp.<server> or openapi.<api>)
  Manifest allowlists refresh automatically when mcp.json / api.json change on disk
  add writes to global:secrets.enc.json by default (AES-256-GCM)`
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
		if len(rest) < 2 {
			return "", fmt.Errorf("usage: /env add KEY=VALUE [KEY=VALUE ...]")
		}
		path, count, err := c.store.addPairs(ctx, rest[1:])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("env: wrote %d key(s) to %s, verified", count, path), nil
	case len(rest) == 0:
		return c.store.statusWithHelp(), nil
	default:
		return "", fmt.Errorf("usage: /env | /env add KEY=VALUE ... | /env -u")
	}
}

var (
	_ credentials.Store            = (*integrationStore)(nil)
	_ agentkit.CommandProvider     = (*integrationStore)(nil)
	_ agentkit.CommandLogSanitizer = (*integrationEnvCommand)(nil)
)
