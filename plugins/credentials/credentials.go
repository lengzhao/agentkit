package credentials

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/plugins/configfile"
	"github.com/lengzhao/pluginkit"
)

const defaultEnvFile = "local:.env"

type Config struct {
	// Prefix is prepended to every lookup key.
	Prefix string `json:"prefix"`
	// Files are dotenv-style KEY=VALUE files used after context and process environment misses.
	Files []string `json:"files"`
}

type EnvDeps struct {
	Workspace workspace.Service `json:"workspace,omitempty"`
}

type Store struct {
	prefix    string
	filePaths []string
	workspace workspace.Service
	mu        sync.RWMutex
	files     map[string]string
}

func init() {
	pluginkit.Register("credentials/env", New)
}

// New registers credentials/env: Resolve secrets from environment variables.
//
// Best practices:
//   - Reference a secret as env:NAME from the consumer's apiKeyRef rather than inlining it in YAML.
//   - Use files for local development .env files; real environment variables still take precedence.
//   - Dotenv files are loaded once into memory; run "env -u" to reload config.files after editing them.
func New(cfg Config, deps EnvDeps) (credentials.Store, error) {
	files := cfg.Files
	if len(files) == 0 {
		files = []string{defaultEnvFile}
	}
	s := &Store{
		prefix:    cfg.Prefix,
		filePaths: append([]string(nil), files...),
		workspace: deps.Workspace,
		files:     make(map[string]string),
	}
	_, _ = s.reload(context.Background())
	return s, nil
}

func (s *Store) Resolve(ctx context.Context, ref string) (credentials.Secret, error) {
	if secret, ok := credentials.SecretFromContext(ctx, ref); ok {
		return secret, nil
	}
	key := credentials.EnvKey(ref)
	if s.prefix != "" {
		key = s.prefix + key
	}
	value := os.Getenv(key)
	if value == "" {
		s.mu.RLock()
		value = s.files[key]
		s.mu.RUnlock()
	}
	if value == "" {
		return credentials.Secret{}, fmt.Errorf("credential %q not found in environment or env files", key)
	}
	return credentials.Secret{Ref: ref, Value: value}, nil
}

func (s *Store) resolvePaths(ctx context.Context) ([]string, error) {
	var out []string
	for _, rel := range s.filePaths {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		if s.workspace != nil {
			path, err := s.workspace.Resolve(ctx, rel)
			if err != nil {
				return nil, err
			}
			out = append(out, path)
			continue
		}
		out = append(out, rel)
	}
	return out, nil
}

func (s *Store) writeTarget(ctx context.Context) (string, error) {
	rel, err := configfile.WriteTarget(s.filePaths)
	if err != nil {
		return "", err
	}
	if s.workspace != nil {
		return s.workspace.Resolve(ctx, rel)
	}
	return rel, nil
}

func (s *Store) reload(ctx context.Context) (int, error) {
	paths, err := s.resolvePaths(ctx)
	if err != nil {
		return 0, err
	}
	values := make(map[string]string)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, fmt.Errorf("read env file %q: %w", path, err)
		}
		if err := parseEnvFile(path, data, values); err != nil {
			return 0, err
		}
	}
	s.mu.Lock()
	s.files = values
	s.mu.Unlock()
	return len(values), nil
}

func (s *Store) addPairs(ctx context.Context, pairs []string) (string, int, error) {
	updates, refs, err := parseEnvUpdates(pairs, s.prefix)
	if err != nil {
		return "", 0, err
	}
	target, err := s.writeTarget(ctx)
	if err != nil {
		return "", 0, err
	}
	prev, err := os.ReadFile(target)
	if err != nil && !os.IsNotExist(err) {
		return "", 0, fmt.Errorf("read %s: %w", target, err)
	}
	var prevBytes []byte
	if err == nil {
		prevBytes = append([]byte(nil), prev...)
	}
	merged, err := mergeEnvFile(prevBytes, updates)
	if err != nil {
		return "", 0, err
	}
	if err := configfile.WriteAtomic(target, merged, 0o600); err != nil {
		return "", 0, fmt.Errorf("write %s: %w", target, err)
	}
	if _, err := s.reload(ctx); err != nil {
		_ = configfile.Restore(target, prevBytes, 0o600)
		_, _ = s.reload(ctx)
		return "", 0, err
	}
	for _, ref := range refs {
		secret, err := s.Resolve(ctx, ref)
		if err != nil {
			_ = configfile.Restore(target, prevBytes, 0o600)
			_, _ = s.reload(ctx)
			return "", 0, fmt.Errorf("verify %s: %w", ref, err)
		}
		if secret.Value == "" {
			_ = configfile.Restore(target, prevBytes, 0o600)
			_, _ = s.reload(ctx)
			return "", 0, fmt.Errorf("verify %s: value is empty", ref)
		}
	}
	return target, len(refs), nil
}

func (s *Store) statusWithHelp() string {
	var b strings.Builder
	b.WriteString(s.formatStatus())
	b.WriteString("\n\n")
	b.WriteString(envHelp())
	return b.String()
}

func (s *Store) formatStatus() string {
	s.mu.RLock()
	fileKeys := len(s.files)
	filePaths := append([]string(nil), s.filePaths...)
	s.mu.RUnlock()

	files := 0
	var b strings.Builder
	for _, path := range filePaths {
		if strings.TrimSpace(path) != "" {
			files++
		}
	}
	fmt.Fprintf(&b, "env: %d file key(s), %d configured file(s)", fileKeys, files)
	if files > 0 {
		b.WriteString("\nconfigured files:")
		for _, path := range filePaths {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			b.WriteString("\n  - ")
			b.WriteString(path)
		}
	}
	return b.String()
}

func envHelp() string {
	return `Usage:
  /env                      show status and help
  /env add KEY=VALUE [...]  append or update .env, reload, and verify
  /env -u                   reload dotenv files from disk into memory

Notes:
  Lookup priority: context secret > process env > dotenv file
  add writes to the local .env file (config.files local: entry)
  Reference secrets as env:NAME in apiKeyRef`
}

func (s *Store) Commands() []agentkit.Command {
	return []agentkit.Command{&envSyncCommand{store: s}}
}

type envSyncCommand struct {
	store *Store
}

func (c *envSyncCommand) Name() string  { return "env" }
func (c *envSyncCommand) Alias() string { return "" }
func (c *envSyncCommand) Description() string {
	return "Show env cache, write KEY=VALUE pairs to .env, or reload with -u"
}

func (c *envSyncCommand) CommandExec(ctx context.Context, args ...string) (string, error) {
	update, rest := peelUpdateFlag(args)
	switch {
	case update:
		count, err := c.store.reload(ctx)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("env: reloaded %d key(s) from disk", count), nil
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

func peelUpdateFlag(args []string) (update bool, rest []string) {
	for _, arg := range args {
		switch arg {
		case "-u", "--update":
			update = true
		default:
			rest = append(rest, arg)
		}
	}
	return update, rest
}

func parseEnvPair(pair string) (string, string, error) {
	key, rawValue, ok := strings.Cut(pair, "=")
	if !ok {
		return "", "", fmt.Errorf("expected KEY=VALUE, got %q", pair)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", fmt.Errorf("key is required in %q", pair)
	}
	value, err := parseEnvValue(strings.TrimSpace(rawValue))
	if err != nil {
		return "", "", fmt.Errorf("parse %q: %w", pair, err)
	}
	return key, value, nil
}

var _ agentkit.CommandProvider = (*Store)(nil)

func loadEnvFiles(files []string) (map[string]string, error) {
	values := make(map[string]string)
	for _, file := range files {
		path := strings.TrimSpace(file)
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read env file %q: %w", path, err)
		}
		if err := parseEnvFile(path, data, values); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func parseEnvFile(path string, data []byte, values map[string]string) error {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, rawValue, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("parse env file %q line %d: expected KEY=VALUE", path, lineNo)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("parse env file %q line %d: key is required", path, lineNo)
		}
		value, err := parseEnvValue(strings.TrimSpace(rawValue))
		if err != nil {
			return fmt.Errorf("parse env file %q line %d: %w", path, lineNo, err)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("parse env file %q: %w", path, err)
	}
	return nil
}

func parseEnvValue(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, `"`) {
		if !strings.HasSuffix(value, `"`) {
			return "", fmt.Errorf("unterminated double-quoted value")
		}
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return "", err
		}
		return unquoted, nil
	}
	if strings.HasPrefix(value, `'`) {
		if !strings.HasSuffix(value, `'`) {
			return "", fmt.Errorf("unterminated single-quoted value")
		}
		return strings.TrimSuffix(strings.TrimPrefix(value, `'`), `'`), nil
	}
	return value, nil
}
