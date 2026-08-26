package credentials

import "context"

type contextKey string

const keySecrets contextKey = "credentials.secrets"

// WithSecrets returns a ctx whose refs resolve from secrets before a Store's
// default backend (for example environment variables).
func WithSecrets(ctx context.Context, secrets map[string]string) context.Context {
	if len(secrets) == 0 {
		return ctx
	}
	return context.WithValue(ctx, keySecrets, secrets)
}

// SecretFromContext returns a secret when ctx carries an override for ref.
func SecretFromContext(ctx context.Context, ref string) (Secret, bool) {
	bag, ok := ctx.Value(keySecrets).(map[string]string)
	if !ok || len(bag) == 0 {
		return Secret{}, false
	}
	if value := bag[ref]; value != "" {
		return Secret{Ref: ref, Value: value}, true
	}
	if key := EnvKey(ref); key != ref {
		if value := bag[key]; value != "" {
			return Secret{Ref: ref, Value: value}, true
		}
	}
	return Secret{}, false
}
