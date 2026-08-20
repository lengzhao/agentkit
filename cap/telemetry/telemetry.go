package telemetry

import "context"

type Exporter interface {
	Record(context.Context, Event) error
}

type Event struct{}
