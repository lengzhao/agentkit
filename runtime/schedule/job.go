package schedule

import (
	"time"

	capschedule "github.com/lengzhao/agentkit/cap/schedule"
)

// NextFire reports when a job should next run, given its anchor.
func NextFire(job capschedule.Job, after time.Time) (time.Time, bool) {
	switch job.NormalizedKind() {
	case capschedule.KindDelay, capschedule.KindAt:
		if job.Fired || !job.InFlightAt.IsZero() || job.FireAt.IsZero() {
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
