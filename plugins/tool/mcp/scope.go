package mcp

import "strings"

const credentialScopePrefix = "mcp."

// CredentialScope returns the scopedStore scope for an mcpServers entry name.
func CredentialScope(serverName string) string {
	return credentialScopePrefix + strings.TrimSpace(serverName)
}
