package schedule

import (
	"time"

	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/pluginkit"
)

// Engine is the standard capschedule.Engine: cron parsing and job fire-time
// evaluation. Stateless; safe to share across instances.
type Engine struct{}

var _ capschedule.Engine = Engine{}

// New is the schedule/engine kind constructor.
func New(_ struct{}, _ struct{}) (capschedule.Engine, error) {
	return Engine{}, nil
}

func init() {
	pluginkit.Register("schedule/engine", New)
}

func (Engine) ParseCron(expr string) (capschedule.Cron, error) {
	return ParseCron(expr)
}

func (Engine) NextFire(job capschedule.Job, after time.Time) (time.Time, bool) {
	return NextFire(job, after)
}
