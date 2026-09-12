package credentials

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit/cap/credentials"
)

type staticStore struct {
	*envStore
}

// NewStatic registers credentials/env for LLM, platform, and other YAML-static plugins.
func NewStatic(cfg Config, deps EnvDeps) (credentials.Store, error) {
	backend, err := newEnvStore(cfg, deps, EncryptedFileDisabled, true)
	if err != nil {
		return nil, err
	}
	return &staticStore{envStore: backend}, nil
}

func (s *staticStore) Resolve(ctx context.Context, scope string, ref string) (credentials.Secret, error) {
	if strings.TrimSpace(scope) != "" {
		return credentials.Secret{}, fmt.Errorf("scoped credential resolution requires credentials/integrations")
	}
	value, err := s.lookupValue(ctx, ref)
	if err != nil {
		return credentials.Secret{}, err
	}
	return credentials.Secret{Ref: ref, Value: value}, nil
}
