package agenttest

import (
	"context"

	"github.com/lengzhao/agentkit/cap/credentials"
)

// StubEnvPairResolver implements credentials.Store and EnvPairResolver for
// shell / shell-bwrap env injection tests.
type StubEnvPairResolver struct {
	Pairs map[string][]string
	Err   error
}

func (s StubEnvPairResolver) Resolve(context.Context, string, string) (credentials.Secret, error) {
	return credentials.Secret{}, nil
}

func (s StubEnvPairResolver) EnvPairs(_ context.Context, scope string, _ credentials.EnvPairsOptions) ([]string, error) {
	if s.Err != nil {
		return nil, s.Err
	}
	if s.Pairs == nil {
		return nil, nil
	}
	return s.Pairs[scope], nil
}
