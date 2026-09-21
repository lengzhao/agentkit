package openapi

import "strings"

// credentialScope returns the scopedStore scope for an api.json apis entry name.
func credentialScope(apiName string) string {
	return "openapi." + strings.TrimSpace(apiName)
}
