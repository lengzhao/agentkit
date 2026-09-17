package credentials

import "context"

type Secret struct {
	Value string
	Ref   string
}

// GlobalScope is used for YAML-level refs (apiKeyRef, botTokenRef, telemetry keys).
// Integration tools pass a non-empty scope (mcp.<server>, openapi.<api>, shell-bash.<cmd>, etc.).
const GlobalScope = ""

// Store resolves secret refs. When scope is GlobalScope, plugins use the global
// lookup chain (context override, process env, config env, encrypted file, dotenv).
// When scope is non-empty, integrations require each env: ref to be allowlisted for
// fallback unless a scoped value exists (L1 scopedEnv, /env add, or encrypted SCOPE::KEY).
type Store interface {
	Resolve(ctx context.Context, scope string, ref string) (Secret, error)
}

// EnvPairsOptions selects env var names for scoped subprocess injection.
// When Keys is non-empty it overrides manifest allowlist and scoped key enumeration.
type EnvPairsOptions struct {
	Keys []string
}

// EnvPairResolver is implemented by credentials/integrations. It resolves scoped
// secrets to KEY=value strings (opts.Keys, manifest allowlist, or stored keys for scope).
type EnvPairResolver interface {
	EnvPairs(ctx context.Context, scope string, opts EnvPairsOptions) ([]string, error)
}
