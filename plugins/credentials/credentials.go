package credentials

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	// Prefix is prepended to every lookup key.
	Prefix string `json:"prefix"`
	// Files are dotenv-style KEY=VALUE files used after context and process environment misses.
	Files []string `json:"files"`
}

type Store struct {
	prefix string
	files  map[string]string
}

func init() {
	pluginkit.Register("credentials/env", New)
}

// New registers credentials/env: Resolve secrets from environment variables.
//
// Best practices:
//   - Reference a secret as env:NAME from the consumer's apiKeyRef rather than inlining it in YAML.
//   - Use files for local development .env files; real environment variables still take precedence.
func New(cfg Config) (credentials.Store, error) {
	files, err := loadEnvFiles(cfg.Files)
	if err != nil {
		return nil, err
	}
	return &Store{prefix: cfg.Prefix, files: files}, nil
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
		value = s.files[key]
	}
	if value == "" {
		return credentials.Secret{}, fmt.Errorf("credential %q not found in environment or env files", key)
	}
	return credentials.Secret{Ref: ref, Value: value}, nil
}

func loadEnvFiles(files []string) (map[string]string, error) {
	values := make(map[string]string)
	for _, file := range files {
		path := strings.TrimSpace(file)
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
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
