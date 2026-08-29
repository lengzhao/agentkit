package openapi

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("tool/openapi", NewOpenAPI)
}
