package telemetry

import (
	"context"
	"sync"
)

// RecordingExporter captures telemetry calls for tests.
type RecordingExporter struct {
	mu           sync.Mutex
	Turns        []RecordedTurn
	Observations []RecordedObservation
	Events       []RecordedEvent
	FlushCalls   int
	ShutdownCalls int
}

type RecordedTurn struct {
	Meta TurnMeta
	End  TurnEnd
}

type RecordedObservation struct {
	Meta ObservationMeta
	End  ObservationEnd
}

type RecordedEvent struct {
	Name  string
	Attrs map[string]string
}

func (r *RecordingExporter) BeginTurn(ctx context.Context, meta TurnMeta) (context.Context, func(TurnEnd)) {
	ctx = WithExporter(ctx, r)
	ctx = WithTurnID(ctx, meta.TurnID)
	return ctx, func(end TurnEnd) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.Turns = append(r.Turns, RecordedTurn{Meta: meta, End: end})
	}
}

func (r *RecordingExporter) BeginObservation(ctx context.Context, meta ObservationMeta) (context.Context, func(ObservationEnd)) {
	return ctx, func(end ObservationEnd) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.Observations = append(r.Observations, RecordedObservation{Meta: meta, End: end})
	}
}

func (r *RecordingExporter) RecordEvent(_ context.Context, name string, attrs map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := make(map[string]string, len(attrs))
	for k, v := range attrs {
		copied[k] = v
	}
	r.Events = append(r.Events, RecordedEvent{Name: name, Attrs: copied})
}

func (r *RecordingExporter) Flush(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.FlushCalls++
	return nil
}

func (r *RecordingExporter) Shutdown(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ShutdownCalls++
	return nil
}

func (r *RecordingExporter) Snapshot() (turns []RecordedTurn, observations []RecordedObservation, events []RecordedEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	turns = append([]RecordedTurn(nil), r.Turns...)
	observations = append([]RecordedObservation(nil), r.Observations...)
	events = append([]RecordedEvent(nil), r.Events...)
	return turns, observations, events
}
