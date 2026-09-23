package credentials

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/config"
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
	// ScopedEnv preloads integration secrets per scope (L1 config); enc /env add overrides same key.
	ScopedEnv map[string]map[string]string `json:"scopedEnv,omitempty"`
}

type EnvDeps struct {
	// FS reads/writes dotenv and encrypted secrets files (scope prefixes allowed).
	FS filesystem.Service `json:"fs"`
}

type envStore struct {
	prefix          string
	configEnv       map[string]string
	masterKeyConfig string
	filePaths       []string
	encryptedRel    string
	fs              filesystem.Service
	processEnv      bool
	mu              sync.RWMutex
	files             map[string]string
	encrypted         map[string]string
	abnormalEncrypted []string
	configScoped      map[string]string
}

func init() {
	pluginkit.Register("credentials/env", NewStatic)
	pluginkit.Register("credentials/integrations", NewIntegrations)
	config.RegisterGraphEnvSource(EnvGraphSource)
}

func newEnvStore(cfg Config, deps EnvDeps, defaultEnc string, defaultProcessEnv bool) (*envStore, error) {
	if deps.FS == nil {
		return nil, fmt.Errorf("credentials requires fs dependency")
	}
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
		masterKeyConfig = strings.TrimSpace(cfg.Env[SecretsMasterKeyEnv])
	}
	s := &envStore{
		prefix:          cfg.Prefix,
		configEnv:       normalizeConfigEnv(cfg.Env, cfg.Prefix),
		masterKeyConfig: masterKeyConfig,
		filePaths:       append([]string(nil), files...),
		encryptedRel:    encRel,
		fs:              deps.FS,
		processEnv:      processEnv,
		files:           make(map[string]string),
		encrypted:       make(map[string]string),
	}
	_, _ = s.reload(context.Background())
	return s, nil
}

func (s *envStore) lookupValue(ctx context.Context, ref string) (string, error) {
	if secret, ok := secretFromContext(ctx, ref); ok {
		return secret.Value, nil
	}
	key := envKey(ref)
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

func (s *envStore) lookupStorageValue(storageKey string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if value := s.encrypted[storageKey]; value != "" {
		return value, true
	}
	if value := s.files[storageKey]; value != "" {
		return value, true
	}
	if value := s.configScoped[storageKey]; value != "" {
		return value, true
	}
	return "", false
}

func (s *envStore) resolvePaths(context.Context) ([]string, error) {
	var out []string
	for _, rel := range s.filePaths {
		rel = strings.TrimSpace(rel)
		if rel == "" {
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
	return workspace.FirstScoped(s.filePaths, workspace.ScopeLocal)
}

func (s *envStore) resolveEncryptedPath(context.Context) (string, error) {
	rel := strings.TrimSpace(s.encryptedRel)
	if rel == "" || rel == EncryptedFileDisabled {
		return "", fmt.Errorf("encrypted secrets file is not configured")
	}
	return rel, nil
}

func (s *envStore) masterKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(SecretsMasterKeyEnv))
	if raw == "" {
		raw = s.masterKeyConfig
	}
	return parseSecretsMasterKey(raw)
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
		data, err := s.fs.Read(ctx, path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
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
		s.abnormalEncrypted = nil
		s.mu.Unlock()
		return 0, nil
	}
	path, err := s.resolveEncryptedPath(ctx)
	if err != nil {
		return 0, err
	}
	data, err := s.fs.Read(ctx, path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.mu.Lock()
			s.encrypted = make(map[string]string)
			s.abnormalEncrypted = nil
			s.mu.Unlock()
			return 0, nil
		}
		return 0, fmt.Errorf("read secrets file %q: %w", path, err)
	}
	if len(data) == 0 {
		s.mu.Lock()
		s.encrypted = make(map[string]string)
		s.abnormalEncrypted = nil
		s.mu.Unlock()
		return 0, nil
	}
	key, err := s.masterKey()
	if err != nil {
		return 0, fmt.Errorf("load %s: %w", path, err)
	}
	values, abnormal, err := loadEncryptedSecretsFile(data, key)
	if err != nil {
		return 0, fmt.Errorf("decrypt secrets file %q: %w", path, err)
	}
	if len(abnormal) > 0 {
		slog.Warn("credentials: encrypted secrets file has abnormal entries", "count", len(abnormal), "path", path)
	}
	s.mu.Lock()
	s.encrypted = values
	s.abnormalEncrypted = abnormal
	s.mu.Unlock()
	return len(values) + len(abnormal), nil
}

type verifyRefFunc func(ctx context.Context, ref string) error

type envAddOutcome struct {
	Path       string
	Wrote      int
	Reloaded   int
	Overwrites []string
	Warnings   []string
}

func (s *envStore) addUpdates(ctx context.Context, updates map[string]string, refs []string, verify verifyRefFunc) (envAddOutcome, error) {
	if verify == nil {
		verify = func(ctx context.Context, ref string) error {
			value, err := s.lookupValue(ctx, ref)
			if err != nil {
				return err
			}
			if value == "" {
				return fmt.Errorf("verify %s: value is empty", ref)
			}
			return nil
		}
	}
	target, err := s.writeTarget(ctx)
	if err != nil {
		return envAddOutcome{}, err
	}
	prev, err := s.fs.Read(ctx, target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return envAddOutcome{}, fmt.Errorf("read %s: %w", target, err)
	}
	var prevBytes []byte
	if err == nil {
		prevBytes = append([]byte(nil), prev...)
	}
	var warnings []string
	var overwrites []string
	var merged []byte
	if s.encryptedRel != "" && s.encryptedRel != EncryptedFileDisabled {
		master, err := s.masterKey()
		if err != nil {
			return envAddOutcome{}, fmt.Errorf("write encrypted secrets: %w", err)
		}
		var marked []encryptMergeWarning
		merged, marked, overwrites, err = mergeEncryptedSecretsFileForAdd(prevBytes, master, updates)
		if err != nil {
			return envAddOutcome{}, err
		}
		for _, item := range marked {
			msg := fmt.Sprintf("marked entry %q abnormal (decrypt failed): %v", item.name, item.err)
			warnings = append(warnings, msg)
			slog.Warn("credentials: encrypted secrets merge", "entry", item.name, "abnormal", true, "err", item.err)
		}
	} else {
		overwrites = detectEnvFileOverwrites(prevBytes, updates)
		merged, err = mergeEnvFile(prevBytes, updates)
		if err != nil {
			return envAddOutcome{}, err
		}
	}
	if err := s.fs.Write(ctx, target, merged, filesystem.WithPerm(0o600)); err != nil {
		return envAddOutcome{}, fmt.Errorf("write %s: %w", target, err)
	}
	reloaded, err := s.reload(ctx)
	if err != nil {
		_ = s.fs.Write(ctx, target, prevBytes, filesystem.WithPerm(0o600))
		_, _ = s.reload(ctx)
		return envAddOutcome{}, fmt.Errorf("reload after write: %w", err)
	}
	for _, ref := range refs {
		if err := verify(ctx, ref); err != nil {
			_ = s.fs.Write(ctx, target, prevBytes, filesystem.WithPerm(0o600))
			_, _ = s.reload(ctx)
			return envAddOutcome{}, fmt.Errorf("verify %s: %w", ref, err)
		}
	}
	return envAddOutcome{
		Path:       target,
		Wrote:      len(refs),
		Reloaded:   reloaded,
		Overwrites: overwrites,
		Warnings:   warnings,
	}, nil
}

func (s *envStore) formatStatus() string {
	s.mu.RLock()
	fileKeys := len(s.files)
	encKeys := len(s.encrypted)
	abnormal := append([]string(nil), s.abnormalEncrypted...)
	filePaths := append([]string(nil), s.filePaths...)
	s.mu.RUnlock()

	configKeys := len(s.configEnv)
	files := 0
	var b strings.Builder
	for _, path := range filePaths {
		if strings.TrimSpace(path) != "" {
			files++
		}
	}
	fmt.Fprintf(&b, "env: %d config key(s), %d encrypted key(s)", configKeys, encKeys)
	if len(abnormal) > 0 {
		fmt.Fprintf(&b, ", %d abnormal encrypted key(s)", len(abnormal))
	}
	fmt.Fprintf(&b, ", %d dotenv key(s), %d configured file(s)", fileKeys, files)
	s.appendKeyInventory(&b)
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
	rest := fields[1:]
	if len(rest) > 0 && !strings.Contains(rest[0], "=") {
		out = append(out, rest[0])
		rest = rest[1:]
	}
	for _, pair := range rest {
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
