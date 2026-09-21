package mcp

import "strings"

// credentialScope returns the scopedStore scope for an mcpServers entry name.
func credentialScope(serverName string) string {
	return "mcp." + strings.TrimSpace(serverName)
}
