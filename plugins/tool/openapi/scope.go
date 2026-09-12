package openapi

import "strings"

const credentialScopePrefix = "openapi."

// CredentialScope returns the scopedStore scope for an api.json apis entry name.
func CredentialScope(apiName string) string {
	return credentialScopePrefix + strings.TrimSpace(apiName)
}
