package credentials

import (
	"fmt"
	"strings"
)

const scopedStorageSep = "::"

const (
	MCPCredentialScopePrefix       = "mcp."
	OpenAPICredentialScopePrefix   = "openapi."
	ShellBashCredentialScopePrefix = "shell-bash."
)

// MCPCredentialScope maps an mcpServers key to integration credential scope.
func MCPCredentialScope(serverName string) string {
	return MCPCredentialScopePrefix + strings.TrimSpace(serverName)
}

// OpenAPICredentialScope maps an api.json apis entry name to integration credential scope.
func OpenAPICredentialScope(apiName string) string {
	return OpenAPICredentialScopePrefix + strings.TrimSpace(apiName)
}

// ShellBashCredentialScope maps a shell command basename to integration credential scope.
func ShellBashCredentialScope(cmd string) string {
	return ShellBashCredentialScopePrefix + strings.TrimSpace(cmd)
}

// IsIntegrationScope reports whether scope is mcp.*, openapi.*, or shell-bash.*.
func IsIntegrationScope(scope string) bool {
	return ValidateIntegrationScope(scope) == nil
}

// ValidateIntegrationScope reports whether scope is a non-empty integration credential scope.
func ValidateIntegrationScope(scope string) error {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return fmt.Errorf("credential scope is required (mcp.<server>, openapi.<api>, or shell-bash.<cmd>)")
	}
	if strings.HasPrefix(scope, MCPCredentialScopePrefix) && len(strings.TrimSpace(strings.TrimPrefix(scope, MCPCredentialScopePrefix))) > 0 {
		return nil
	}
	if strings.HasPrefix(scope, OpenAPICredentialScopePrefix) && len(strings.TrimSpace(strings.TrimPrefix(scope, OpenAPICredentialScopePrefix))) > 0 {
		return nil
	}
	if strings.HasPrefix(scope, ShellBashCredentialScopePrefix) && len(strings.TrimSpace(strings.TrimPrefix(scope, ShellBashCredentialScopePrefix))) > 0 {
		return nil
	}
	return fmt.Errorf("credential scope %q must be mcp.<server>, openapi.<api>, or shell-bash.<cmd>", scope)
}

// ScopedStorageKey names an integration secret entry in secrets.enc.json or dotenv.
func ScopedStorageKey(scope, storageKey string) string {
	scope = strings.TrimSpace(scope)
	storageKey = strings.TrimSpace(storageKey)
	return scope + scopedStorageSep + storageKey
}
