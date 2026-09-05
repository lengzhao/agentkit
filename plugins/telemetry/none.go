package telemetry

import (
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	rttelemetry "github.com/lengzhao/agentkit/runtime/telemetry"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("telemetry/none", NewNone)
}

// NewNone registers telemetry/none: No-op telemetry exporter for default builds.
func NewNone() (captelemetry.Exporter, error) {
	return rttelemetry.Noop, nil
}
