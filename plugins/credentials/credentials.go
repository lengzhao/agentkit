package credentials

import (
	"context"
	"fmt"
	"os"

	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	// Prefix is prepended to every lookup key.
	Prefix string `json:"prefix"`
}

type Store struct {
	prefix string
}

func init() {
	pluginkit.Register("credentials/env", New)
}

// New registers credentials/env: Resolve secrets from environment variables.
//
// Best practices:
//   - Reference a secret as env:NAME from the consumer's apiKeyRef rather than inlining it in YAML.
func New(cfg Config) (credentials.Store, error) {
	return &Store{prefix: cfg.Prefix}, nil
}

func (s *Store) Resolve(_ context.Context, ref string) (credentials.Secret, error) {
	key := credentials.EnvKey(ref)
	if s.prefix != "" {
		key = s.prefix + key
	}
	value := os.Getenv(key)
	if value == "" {
		return credentials.Secret{}, fmt.Errorf("credential %q not found in environment", key)
	}
	return credentials.Secret{Ref: ref, Value: value}, nil
}
