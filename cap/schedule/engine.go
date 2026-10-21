package schedule

import "time"

// Cron is a parsed expression. Next returns the first matching minute strictly
// after t.
type Cron interface {
	Next(t time.Time) (time.Time, bool)
}

// Engine evaluates cron expressions and job fire times. The standard
// implementation lives in runtime/schedule (schedule/engine kind) and is
// injected via deps. runtime/loop and runtime/runner only read the schedule
// fire-metadata helpers in this package, so they do not import the engine
// implementation.
type Engine interface {
	ParseCron(expr string) (Cron, error)
	NextFire(job Job, after time.Time) (time.Time, bool)
}
