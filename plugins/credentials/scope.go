package credentials

import (
	"fmt"
	"strings"
)

const scopedStorageSep = "::"

const (
	mcpCredentialScopePrefix       = "mcp."
	openAPICredentialScopePrefix   = "openapi."
	shellBashCredentialScopePrefix = "shell-bash."
)

func mcpCredentialScope(serverName string) string {
	return mcpCredentialScopePrefix + strings.TrimSpace(serverName)
}

func openAPICredentialScope(apiName string) string {
	return openAPICredentialScopePrefix + strings.TrimSpace(apiName)
}

func shellBashCredentialScope(cmd string) string {
	return shellBashCredentialScopePrefix + strings.TrimSpace(cmd)
}

func isIntegrationScope(scope string) bool {
	return validateIntegrationScope(scope) == nil
}

func validateIntegrationScope(scope string) error {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return fmt.Errorf("credential scope is required (mcp.<server>, openapi.<api>, or shell-bash.<cmd>)")
	}
	if strings.HasPrefix(scope, mcpCredentialScopePrefix) && len(strings.TrimSpace(strings.TrimPrefix(scope, mcpCredentialScopePrefix))) > 0 {
		return nil
	}
	if strings.HasPrefix(scope, openAPICredentialScopePrefix) && len(strings.TrimSpace(strings.TrimPrefix(scope, openAPICredentialScopePrefix))) > 0 {
		return nil
	}
	if strings.HasPrefix(scope, shellBashCredentialScopePrefix) && len(strings.TrimSpace(strings.TrimPrefix(scope, shellBashCredentialScopePrefix))) > 0 {
		return nil
	}
	return fmt.Errorf("credential scope %q must be mcp.<server>, openapi.<api>, or shell-bash.<cmd>", scope)
}

func scopedStorageKey(scope, storageKey string) string {
	scope = strings.TrimSpace(scope)
	storageKey = strings.TrimSpace(storageKey)
	return scope + scopedStorageSep + storageKey
}
