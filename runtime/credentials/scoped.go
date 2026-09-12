package credentials

import (
	"fmt"
	"strings"
)

const scopedStorageSep = "::"

// ValidateIntegrationScope reports whether scope is a non-empty mcp.* or openapi.* credential scope.
func ValidateIntegrationScope(scope string) error {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return fmt.Errorf("credential scope is required (mcp.<server> or openapi.<api>)")
	}
	if strings.HasPrefix(scope, "mcp.") && len(strings.TrimSpace(strings.TrimPrefix(scope, "mcp."))) > 0 {
		return nil
	}
	if strings.HasPrefix(scope, "openapi.") && len(strings.TrimSpace(strings.TrimPrefix(scope, "openapi."))) > 0 {
		return nil
	}
	return fmt.Errorf("credential scope %q must be mcp.<server> or openapi.<api>", scope)
}

// ScopedStorageKey names an integration secret entry in secrets.enc.json or dotenv.
func ScopedStorageKey(scope, storageKey string) string {
	scope = strings.TrimSpace(scope)
	storageKey = strings.TrimSpace(storageKey)
	return scope + scopedStorageSep + storageKey
}
