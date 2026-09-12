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
	"github.com/lengzhao/agentkit/config"
	"github.com/lengzhao/agentkit/runtime/configfile"
	rtcredentials "github.com/lengzhao/agentkit/runtime/credentials"
	"github.com/lengzhao/pluginkit"
)

const (
	defaultEnvFile        = "local:.env"
	defaultEncryptedFile  = "global:secrets.enc.json"
	EncryptedFileDisabled = "-"
)

type Config struct {
	Prefix        string            `json:"prefix"`
	Env           map[string]string `json:"env"`
	Files         []string          `json:"files"`
	EncryptedFile string            `json:"encryptedFile"`
	// ProcessEnv enables os.Getenv lookup (static default true; integrations default false).
	ProcessEnv *bool `json:"processEnv,omitempty"`
	// ManifestFiles lists workspace paths scanned for per-scope env: allowlists (integrations only).
	ManifestFiles []string `json:"manifestFiles,omitempty"`
}

type EnvDeps struct {
	Workspace workspace.Service `json:"workspace,omitempty"`
}

type envStore struct {
	prefix          string
	configEnv       map[string]string
	masterKeyConfig string
	filePaths       []string
	encryptedRel    string
	workspace       workspace.Service
	processEnv      bool
	mu              sync.RWMutex
	files           map[string]string
	encrypted       map[string]string
}

func init() {
	pluginkit.Register("credentials/env", NewStatic)
	pluginkit.Register("credentials/integrations", NewIntegrations)
	config.RegisterGraphEnvSource(EnvGraphSource)
}

func newEnvStore(cfg Config, deps EnvDeps, defaultEnc string, defaultProcessEnv bool) (*envStore, error) {
	encRel := strings.TrimSpace(cfg.EncryptedFile)
	if encRel == "" {
		encRel = defaultEnc
	}
	processEnv := defaultProcessEnv
	if cfg.ProcessEnv != nil {
		processEnv = *cfg.ProcessEnv
	}
	files := cfg.Files
	if len(files) == 0 && encRel == EncryptedFileDisabled && processEnv {
		files = []string{defaultEnvFile}
	}
	masterKeyConfig := ""
	if cfg.Env != nil {
		masterKeyConfig = strings.TrimSpace(cfg.Env[rtcredentials.SecretsMasterKeyEnv])
	}
	s := &envStore{
		prefix:          cfg.Prefix,
		configEnv:       normalizeConfigEnv(cfg.Env, cfg.Prefix),
		masterKeyConfig: masterKeyConfig,
		filePaths:       append([]string(nil), files...),
		encryptedRel:    encRel,
		workspace:       deps.Workspace,
		processEnv:      processEnv,
		files:           make(map[string]string),
		encrypted:       make(map[string]string),
	}
	_, _ = s.reload(context.Background())
	return s, nil
}

func (s *envStore) lookupValue(ctx context.Context, ref string) (string, error) {
	if secret, ok := rtcredentials.SecretFromContext(ctx, ref); ok {
		return secret.Value, nil
	}
	key := rtcredentials.EnvKey(ref)
	if s.prefix != "" {
		key = s.prefix + key
	}
	var value string
	if s.processEnv {
		value = os.Getenv(key)
	}
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
		return "", fmt.Errorf("credential %q not found", key)
	}
	return value, nil
}

func (s *envStore) resolvePaths(ctx context.Context) ([]string, error) {
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

func (s *envStore) writeTarget(ctx context.Context) (string, error) {
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

func (s *envStore) resolveEncryptedPath(ctx context.Context) (string, error) {
	rel := strings.TrimSpace(s.encryptedRel)
	if rel == "" || rel == EncryptedFileDisabled {
		return "", fmt.Errorf("encrypted secrets file is not configured")
	}
	if s.workspace != nil {
		return s.workspace.Resolve(ctx, rel)
	}
	return rel, nil
}

func (s *envStore) masterKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(rtcredentials.SecretsMasterKeyEnv))
	if raw == "" {
		raw = s.masterKeyConfig
	}
	return rtcredentials.ParseSecretsMasterKey(raw)
}

func (s *envStore) reload(ctx context.Context) (int, error) {
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

func (s *envStore) reloadEncrypted(ctx context.Context) (int, error) {
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

func (s *envStore) addPairs(ctx context.Context, pairs []string) (string, int, error) {
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
		value, err := s.lookupValue(ctx, ref)
		if err != nil {
			_ = configfile.Restore(target, prevBytes, 0o600)
			_, _ = s.reload(ctx)
			return "", 0, fmt.Errorf("verify %s: %w", ref, err)
		}
		if value == "" {
			_ = configfile.Restore(target, prevBytes, 0o600)
			_, _ = s.reload(ctx)
			return "", 0, fmt.Errorf("verify %s: value is empty", ref)
		}
	}
	return target, len(refs), nil
}

func (s *envStore) formatStatus() string {
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

var _ credentials.Store = (*staticStore)(nil)

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

func redactEnvAddArgsForLog(args string) string {
	fields := strings.Fields(args)
	if len(fields) == 0 || fields[0] != "add" {
		return args
	}
	out := []string{"add"}
	for _, pair := range fields[1:] {
		key, _, ok := strings.Cut(pair, "=")
		if !ok {
			out = append(out, pair)
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			out = append(out, "="+agentkit.SlashLogRedacted)
			continue
		}
		out = append(out, key+"="+agentkit.SlashLogRedacted)
	}
	return strings.Join(out, " ")
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
