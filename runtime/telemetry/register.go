package telemetry

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("telemetry/toolkit", NewToolkit)
}
