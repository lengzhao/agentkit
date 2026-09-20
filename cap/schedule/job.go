package schedule

import (
	"strings"
	"time"
)

// NextFire reports when a job should next run, given its anchor.
func NextFire(job Job, after time.Time) (time.Time, bool) {
	switch JobKind(job) {
	case KindDelay, KindAt:
		if job.Fired || job.InFlight || job.FireAt.IsZero() {
			return time.Time{}, false
		}
		return job.FireAt, true
	}
	sched, err := ParseCron(job.Cron)
	if err != nil {
		return time.Time{}, false
	}
	return sched.Next(after)
}

// JobKind returns the normalized job kind.
func JobKind(job Job) string {
	kind := strings.TrimSpace(job.Kind)
	if kind != "" {
		return kind
	}
	if !job.FireAt.IsZero() || strings.TrimSpace(job.In) != "" {
		return KindDelay
	}
	if strings.TrimSpace(job.Cron) != "" {
		return KindCron
	}
	return ""
}

// IsOneShot reports whether a job fires once at an absolute time.
func IsOneShot(job Job) bool {
	switch JobKind(job) {
	case KindDelay, KindAt:
		return true
	default:
		return false
	}
}

// InFlightExpired reports whether a claimed one-shot should be reclaimed.
func InFlightExpired(job Job, now time.Time) bool {
	if !job.InFlight || job.InFlightAt.IsZero() {
		return false
	}
	return now.Sub(job.InFlightAt) >= InFlightTimeout
}
