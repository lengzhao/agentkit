package credentials

import "context"

type Secret struct {
	Value string
	Ref   string
}

// GlobalScope is used for YAML-level refs (apiKeyRef, botTokenRef, telemetry keys).
// Integration tools pass a non-empty scope (mcp.<server>, openapi.<api>, etc.).
const GlobalScope = ""

// Store resolves secret refs. When scope is GlobalScope, plugins use the global
// lookup chain (context override, config env, encrypted file, dotenv, process env).
// When scope is non-empty, integrations require each env: ref to be declared under
// that scope in the integration manifest (mcp.<server> or openapi.<api>).
type Store interface {
	Resolve(ctx context.Context, scope string, ref string) (Secret, error)
}
