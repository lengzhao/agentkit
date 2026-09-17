package mcp

import rtcredentials "github.com/lengzhao/agentkit/runtime/credentials"

// CredentialScope returns the scopedStore scope for an mcpServers entry name.
func CredentialScope(serverName string) string {
	return rtcredentials.MCPCredentialScope(serverName)
}
