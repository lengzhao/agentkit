package credentials

import (
	"context"

	"github.com/lengzhao/agentkit/cap/credentials"
)

type contextKey string

const keySecrets contextKey = "credentials.secrets"

func withSecrets(ctx context.Context, secrets map[string]string) context.Context {
	if len(secrets) == 0 {
		return ctx
	}
	return context.WithValue(ctx, keySecrets, secrets)
}

func secretFromContext(ctx context.Context, ref string) (credentials.Secret, bool) {
	bag, ok := ctx.Value(keySecrets).(map[string]string)
	if !ok || len(bag) == 0 {
		return credentials.Secret{}, false
	}
	if value := bag[ref]; value != "" {
		return credentials.Secret{Ref: ref, Value: value}, true
	}
	if key := envKey(ref); key != ref {
		if value := bag[key]; value != "" {
			return credentials.Secret{Ref: ref, Value: value}, true
		}
	}
	return credentials.Secret{}, false
}
