package credentialsenv

import (
	"context"
	"fmt"
	"os"

	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	Prefix string `json:"prefix"`
}

type Store struct {
	prefix string
}

func init() {
	pluginkit.Register("credentials/env", New)
}

func New(cfg Config) (credentials.Store, error) {
	return &Store{prefix: cfg.Prefix}, nil
}

func (s *Store) Resolve(_ context.Context, ref string) (credentials.Secret, error) {
	key := ref
	if s.prefix != "" {
		key = s.prefix + ref
	}
	value := os.Getenv(key)
	if value == "" {
		return credentials.Secret{}, fmt.Errorf("credential %q not found in environment", key)
	}
	return credentials.Secret{Ref: ref, Value: value}, nil
}
