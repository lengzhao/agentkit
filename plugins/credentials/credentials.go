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
	rtcredentials "github.com/lengzhao/agentkit/runtime/credentials"
	"github.com/lengzhao/agentkit/config"
	"github.com/lengzhao/agentkit/runtime/configfile"
	"github.com/lengzhao/pluginkit"
)

const (
	defaultEnvFile       = "local:.env"
	defaultEncryptedFile = "global:secrets.enc.json"
	// EncryptedFileDisabled turns off encrypted storage (tests or legacy-only .env).
	EncryptedFileDisabled = "-"
)

type Config struct {
	// Prefix is prepended to every lookup key.
	Prefix string `json:"prefix"`
	// Env holds in-memory KEY=VALUE pairs from config, used after process environment misses.
	Env map[string]string `json:"env"`
	// Files are dotenv-style KEY=VALUE files used after context, process environment, and config env misses.
	Files []string `json:"files"`
	// EncryptedFile is the workspace-relative AES-GCM secrets file (default global:secrets.enc.json).
	// Set to EncryptedFileDisabled ("-") to disable.
	EncryptedFile string `json:"encryptedFile"`
}

type EnvDeps struct {
	Workspace workspace.Service `json:"workspace,omitempty"`
}

type Store struct {
	prefix          string
	configEnv       map[string]string
	masterKeyConfig string
	filePaths       []string
	encryptedRel    string
	workspace       workspace.Service
	mu              sync.RWMutex
	files           map[string]string
	encrypted       map[string]string
}

func init() {
	pluginkit.Register("credentials/env", New)
	config.RegisterGraphEnvSource(EnvGraphSource)
}

// New registers credentials/env: Resolve secrets from environment variables.
//
// Best practices:
//   - Reference a secret as env:NAME from the consumer's apiKeyRef rather than inlining it in YAML.
//   - Use config.env for inline secrets in YAML; process environment still takes precedence.
//   - Use files for local development .env files; config env takes precedence over files and encrypted storage.
//   - Set AGENTKIT_SECRETS_KEY in config.env (or process env) to unlock global:secrets.enc.json.
//   - Dotenv and encrypted files are loaded into memory; run "/env -u" to reload from disk.
func New(cfg Config, deps EnvDeps) (credentials.Store, error) {
	encRel := strings.TrimSpace(cfg.EncryptedFile)
	if encRel == "" {
		encRel = defaultEncryptedFile
	}
	files := cfg.Files
	if len(files) == 0 && encRel == EncryptedFileDisabled {
		files = []string{defaultEnvFile}
	}
	masterKeyConfig := ""
	if cfg.Env != nil {
		masterKeyConfig = strings.TrimSpace(cfg.Env[rtcredentials.SecretsMasterKeyEnv])
	}
	s := &Store{
		prefix:          cfg.Prefix,
		configEnv:       normalizeConfigEnv(cfg.Env, cfg.Prefix),
		masterKeyConfig: masterKeyConfig,
		filePaths:       append([]string(nil), files...),
		encryptedRel:    encRel,
		workspace:       deps.Workspace,
		files:           make(map[string]string),
		encrypted:       make(map[string]string),
	}
	_, _ = s.reload(context.Background())
	return s, nil
}

func (s *Store) Resolve(ctx context.Context, ref string) (credentials.Secret, error) {
	if secret, ok := rtcredentials.SecretFromContext(ctx, ref); ok {
		return secret, nil
	}
	key := rtcredentials.EnvKey(ref)
	if s.prefix != "" {
		key = s.prefix + key
	}
	value := os.Getenv(key)
	if value == "" {
		value = s.configEnv[key]
	}
	if value == "" {
		s.mu.RLock()
		value = s.encrypted[key]
		if value == "" {
			value = s.files[key]
		}
		s.mu.RUnlock()
	}
	if value == "" {
		return credentials.Secret{}, fmt.Errorf("credential %q not found in environment, config env, or env files", key)
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
	if s.encryptedRel != "" && s.encryptedRel != EncryptedFileDisabled {
		return s.resolveEncryptedPath(ctx)
	}
	rel, err := configfile.WriteTarget(s.filePaths)
	if err != nil {
		return "", err
	}
	if s.workspace != nil {
		return s.workspace.Resolve(ctx, rel)
	}
	return rel, nil
}

func (s *Store) resolveEncryptedPath(ctx context.Context) (string, error) {
	rel := strings.TrimSpace(s.encryptedRel)
	if rel == "" || rel == EncryptedFileDisabled {
		return "", fmt.Errorf("encrypted secrets file is not configured")
	}
	if s.workspace != nil {
		return s.workspace.Resolve(ctx, rel)
	}
	return rel, nil
}

func (s *Store) masterKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(rtcredentials.SecretsMasterKeyEnv))
	if raw == "" {
		raw = s.masterKeyConfig
	}
	return rtcredentials.ParseSecretsMasterKey(raw)
}

func (s *Store) reload(ctx context.Context) (int, error) {
	encCount, err := s.reloadEncrypted(ctx)
	if err != nil {
		return 0, err
	}
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
	return encCount + len(values), nil
}

func (s *Store) reloadEncrypted(ctx context.Context) (int, error) {
	if s.encryptedRel == "" || s.encryptedRel == EncryptedFileDisabled {
		s.mu.Lock()
		s.encrypted = make(map[string]string)
		s.mu.Unlock()
		return 0, nil
	}
	path, err := s.resolveEncryptedPath(ctx)
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.mu.Lock()
			s.encrypted = make(map[string]string)
			s.mu.Unlock()
			return 0, nil
		}
		return 0, fmt.Errorf("read secrets file %q: %w", path, err)
	}
	if len(data) == 0 {
		s.mu.Lock()
		s.encrypted = make(map[string]string)
		s.mu.Unlock()
		return 0, nil
	}
	key, err := s.masterKey()
	if err != nil {
		return 0, fmt.Errorf("load %s: %w", path, err)
	}
	values, err := rtcredentials.DecryptSecretsFile(data, key)
	if err != nil {
		return 0, fmt.Errorf("decrypt secrets file %q: %w", path, err)
	}
	s.mu.Lock()
	s.encrypted = values
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
	var merged []byte
	if s.encryptedRel != "" && s.encryptedRel != EncryptedFileDisabled {
		master, err := s.masterKey()
		if err != nil {
			return "", 0, fmt.Errorf("write encrypted secrets: %w", err)
		}
		merged, err = rtcredentials.MergeEncryptedSecretsFile(prevBytes, master, updates)
		if err != nil {
			return "", 0, err
		}
	} else {
		merged, err = mergeEnvFile(prevBytes, updates)
		if err != nil {
			return "", 0, err
		}
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

func normalizeConfigEnv(env map[string]string, prefix string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for key, value := range env {
		storageKey := key
		if prefix != "" {
			storageKey = prefix + key
		}
		out[storageKey] = value
	}
	return out
}

func (s *Store) formatStatus() string {
	s.mu.RLock()
	fileKeys := len(s.files)
	encKeys := len(s.encrypted)
	filePaths := append([]string(nil), s.filePaths...)
	encRel := s.encryptedRel
	s.mu.RUnlock()

	configKeys := len(s.configEnv)
	files := 0
	var b strings.Builder
	for _, path := range filePaths {
		if strings.TrimSpace(path) != "" {
			files++
		}
	}
	fmt.Fprintf(&b, "env: %d config key(s), %d encrypted key(s), %d dotenv key(s), %d configured file(s)", configKeys, encKeys, fileKeys, files)
	if encRel != "" && encRel != EncryptedFileDisabled {
		b.WriteString("\nencrypted file:")
		b.WriteString("\n  - ")
		b.WriteString(encRel)
	}
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
  /env add KEY=VALUE [...]  append or update secrets.enc.json, reload, and verify
  /env -u                   reload secrets and dotenv files from disk into memory

Notes:
  Lookup priority: context secret > process env > config env > encrypted file > dotenv file
  config env comes from credentials config.env in YAML (include AGENTKIT_SECRETS_KEY for encryption)
  add writes to global:secrets.enc.json by default (AES-256-GCM)
  Reference secrets as env:NAME in apiKeyRef / mcp.json / api.json`
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

func (c *envSyncCommand) CommandExec(ctx context.Context, args string) (string, error) {
	update, rest := peelUpdateFlag(strings.Fields(strings.TrimSpace(args)))
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
