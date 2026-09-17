package openapi

import rtcredentials "github.com/lengzhao/agentkit/runtime/credentials"

// CredentialScope returns the scopedStore scope for an api.json apis entry name.
func CredentialScope(apiName string) string {
	return rtcredentials.OpenAPICredentialScope(apiName)
}
