package telemetry

import captelemetry "github.com/lengzhao/agentkit/cap/telemetry"

// Noop is the default exporter that discards all telemetry.
var Noop captelemetry.Exporter = captelemetry.Noop
