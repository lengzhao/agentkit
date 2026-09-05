package telemetry

import (
	"context"
	"fmt"
	"sync"

	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
)

// RecordingExporter captures telemetry calls for tests.
type RecordingExporter struct {
	mu            sync.Mutex
	nextObsID     int
	Turns         []RecordedTurn
	Observations  []RecordedObservation
	Events        []RecordedEvent
	FlushCalls    int
	ShutdownCalls int
}

type RecordedTurn struct {
	Meta captelemetry.TurnMeta
	End  captelemetry.TurnEnd
}

type RecordedObservation struct {
	ID       string
	Meta     captelemetry.ObservationMeta
	End      captelemetry.ObservationEnd
	ParentID string
}

type RecordedEvent struct {
	Name  string
	Attrs map[string]string
}

func (r *RecordingExporter) BeginTurn(ctx context.Context, meta captelemetry.TurnMeta) (context.Context, func(captelemetry.TurnEnd)) {
	ctx = WithExporter(ctx, r)
	ctx = WithTurnID(ctx, meta.TurnID)
	return ctx, func(end captelemetry.TurnEnd) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.Turns = append(r.Turns, RecordedTurn{Meta: meta, End: end})
	}
}

func (r *RecordingExporter) BeginObservation(ctx context.Context, meta captelemetry.ObservationMeta) (context.Context, func(captelemetry.ObservationEnd)) {
	parentID := ToolParentFrom(ctx)
	if meta.Kind == captelemetry.KindGeneration {
		parentID = ScopeParentFrom(ctx)
	}
	r.mu.Lock()
	r.nextObsID++
	obsID := fmt.Sprintf("obs-%d", r.nextObsID)
	r.mu.Unlock()
	ctx = WithToolParent(ctx, obsID)
	if meta.Scope {
		ctx = WithScopeParent(ctx, obsID)
	}
	return ctx, func(end captelemetry.ObservationEnd) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.Observations = append(r.Observations, RecordedObservation{
			ID:       obsID,
			Meta:     meta,
			End:      end,
			ParentID: parentID,
		})
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
