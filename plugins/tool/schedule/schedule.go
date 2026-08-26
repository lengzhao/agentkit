package schedule

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/schedule"
)

type ScheduleConfig struct {
	// MaxJobs is cap on agent-created jobs, default 32; jobs declared in config do not count against it.
	MaxJobs int `json:"maxJobs"`
}

type ScheduleDeps struct {
	Schedule schedule.Registry `json:"schedule"`
}

type ScheduleInput struct {
	Op     string `json:"op" jsonschema:"required,description=list to see the schedule; add to create a job; remove to delete one"`
	Cron   string `json:"cron" jsonschema:"description=5-field cron expression (minute hour day-of-month month day-of-week) or @daily/@hourly/@weekly/@monthly; required for add"`
	Prompt string `json:"prompt" jsonschema:"description=The task to run when the schedule fires; required for add"`
	ID     string `json:"id" jsonschema:"description=Job id; required for remove"`
	Note   string `json:"note" jsonschema:"description=Optional context for your future self"`
}

// ScheduleEntry is one job as reported to the model, with the next fire time
// resolved so the agent can reason about it without parsing cron itself.
type ScheduleEntry struct {
	ID       string `json:"id"`
	Cron     string `json:"cron"`
	Prompt   string `json:"prompt"`
	Source   string `json:"source"`
	Note     string `json:"note,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	NextRun  string `json:"nextRun,omitempty"`
}

type ScheduleOutput struct {
	Jobs        []ScheduleEntry `json:"jobs"`
	Instruction string          `json:"instruction,omitempty"`
}

// Schedule operations.
const (
	scheduleOpList   = "list"
	scheduleOpAdd    = "add"
	scheduleOpRemove = "remove"
)

const defaultMaxAgentJobs = 32

// NewSchedule registers tool/schedule: Let the agent list, add and remove its own cron jobs.
//
// Best practices:
//   - Ids are assigned by the registry (agent-1, agent-2, ...); read them back with op=list before op=remove.
//   - Needs a schedule registry that platform/worker also uses, or nothing will fire the jobs.
func NewSchedule(cfg ScheduleConfig, deps ScheduleDeps) (agentkit.ToolPack, error) {
	if deps.Schedule == nil {
		return nil, fmt.Errorf("tool/schedule requires schedule dependency")
	}
	maxJobs := cfg.MaxJobs
	if maxJobs <= 0 {
		maxJobs = defaultMaxAgentJobs
	}
	registry := deps.Schedule

	tool, err := agentkit.NewTool[ScheduleInput, ScheduleOutput]("schedule", func(ctx context.Context, input ScheduleInput) (ScheduleOutput, error) {
		switch strings.ToLower(strings.TrimSpace(input.Op)) {
		case scheduleOpList, "":
			jobs, err := registry.List(ctx)
			if err != nil {
				return ScheduleOutput{}, err
			}
			return scheduleOutput(jobs, ""), nil

		case scheduleOpAdd:
			if strings.TrimSpace(input.Cron) == "" {
				return ScheduleOutput{}, fmt.Errorf("add requires a cron expression")
			}
			if strings.TrimSpace(input.Prompt) == "" {
				return ScheduleOutput{}, fmt.Errorf("add requires a prompt")
			}
			// Validate before counting, so a typo reports the cron error rather
			// than a quota error.
			if _, err := schedule.ParseCron(input.Cron); err != nil {
				return ScheduleOutput{}, err
			}
			existing, err := registry.List(ctx)
			if err != nil {
				return ScheduleOutput{}, err
			}
			if countSource(existing, schedule.SourceAgent) >= maxJobs {
				return ScheduleOutput{}, fmt.Errorf("cannot add: already at the limit of %d scheduled jobs; remove one first", maxJobs)
			}
			added, err := registry.Add(ctx, schedule.Job{
				Cron:   strings.TrimSpace(input.Cron),
				Prompt: strings.TrimSpace(input.Prompt),
				Note:   strings.TrimSpace(input.Note),
				Source: schedule.SourceAgent,
			})
			if err != nil {
				return ScheduleOutput{}, err
			}
			jobs, err := registry.List(ctx)
			if err != nil {
				return ScheduleOutput{}, err
			}
			return scheduleOutput(jobs, fmt.Sprintf("scheduled %s", added.ID)), nil

		case scheduleOpRemove:
			id := strings.TrimSpace(input.ID)
			if id == "" {
				return ScheduleOutput{}, fmt.Errorf("remove requires an id")
			}
			removed, err := registry.Remove(ctx, id)
			if err != nil {
				return ScheduleOutput{}, err
			}
			if !removed {
				return ScheduleOutput{}, fmt.Errorf("no job with id %q", id)
			}
			jobs, err := registry.List(ctx)
			if err != nil {
				return ScheduleOutput{}, err
			}
			return scheduleOutput(jobs, fmt.Sprintf("removed %s", id)), nil

		default:
			return ScheduleOutput{}, fmt.Errorf("unknown op %q: use list, add or remove", input.Op)
		}
	}).
		Description("Put work on the calendar. op=add with a cron expression to run a task later or repeatedly (e.g. cron \"0 9 * * 1-5\" for weekday mornings), op=list to review, op=remove to cancel. Scheduled jobs survive restarts and run as their own session, so state the task in full.").
		Build()
	if err != nil {
		return nil, err
	}
	return agentkit.Pack(tool), nil
}

func countSource(jobs []schedule.Job, source string) int {
	n := 0
	for _, job := range jobs {
		if job.Source == source {
			n++
		}
	}
	return n
}

func scheduleOutput(jobs []schedule.Job, instruction string) ScheduleOutput {
	out := ScheduleOutput{Instruction: instruction, Jobs: make([]ScheduleEntry, 0, len(jobs))}
	for _, job := range jobs {
		entry := ScheduleEntry{
			ID:       job.ID,
			Cron:     job.Cron,
			Prompt:   job.Prompt,
			Source:   job.Source,
			Note:     job.Note,
			Disabled: job.Disabled,
		}
		if next, ok := schedule.NextFire(job, job.LastRun); ok {
			entry.NextRun = next.Format(time.RFC3339)
		}
		out.Jobs = append(out.Jobs, entry)
	}
	return out
}
