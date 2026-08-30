package schedule

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/runtime/session"
)

type ScheduleConfig struct {
	// MaxJobs is cap on agent-created jobs, default 32; jobs declared in config do not count against it.
	MaxJobs int `json:"maxJobs"`
}

type ScheduleDeps struct {
	Schedule schedule.Registry `json:"schedule"`
}

type ScheduleInput struct {
	Op           string `json:"op" jsonschema:"list to see the schedule; add to create a job; remove to delete one"`
	Kind         string `json:"kind,omitempty" jsonschema:"cron for recurring jobs, delay for relative one-shot reminders, at for absolute one-shot reminders"`
	Cron         string `json:"cron,omitempty" jsonschema:"5-field cron (minute hour day-of-month month day-of-week) or @daily/@hourly; required for kind=cron"`
	In           string `json:"in,omitempty" jsonschema:"Relative delay for kind=delay, e.g. 30s, 1m, 2h, 1h30m"`
	At           string `json:"at,omitempty" jsonschema:"Absolute fire time for kind=at, e.g. RFC3339 or local 2006-01-02T15:04:05"`
	Prompt       string `json:"prompt,omitempty" jsonschema:"The task to run when the schedule fires; required for add"`
	ID           string `json:"id,omitempty" jsonschema:"Job id; required for remove"`
	Note         string `json:"note,omitempty" jsonschema:"Optional context for your future self"`
	IncludeFired bool   `json:"includeFired,omitempty" jsonschema:"Include fired one-shot jobs in list output"`
}

// ScheduleEntry is one job as reported to the model, with the next fire time
// resolved so the agent can reason about it without parsing cron itself.
type ScheduleEntry struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Cron     string `json:"cron,omitempty"`
	In       string `json:"in,omitempty"`
	FireAt   string `json:"fireAt,omitempty"`
	Prompt   string `json:"prompt"`
	Source   string `json:"source"`
	Note     string `json:"note,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	Fired    bool   `json:"fired,omitempty"`
	FiredAt  string `json:"firedAt,omitempty"`
	LastError string `json:"lastError,omitempty"`
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
//   - Needs a schedule registry that schedule/cron also uses, or nothing will fire the jobs.
//   - Slash: /cron lists jobs for the current channel; /cron remove <id> cancels one.
func NewSchedule(cfg ScheduleConfig, deps ScheduleDeps) (agentkit.Tool, error) {
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
			return scheduleOutput(jobs, "", input.IncludeFired), nil

		case scheduleOpAdd:
			if strings.TrimSpace(input.Prompt) == "" {
				return ScheduleOutput{}, fmt.Errorf("add requires a prompt")
			}
			job, err := jobFromInput(time.Now(), input)
			if err != nil {
				return ScheduleOutput{}, err
			}
			existing, err := registry.List(ctx)
			if err != nil {
				return ScheduleOutput{}, err
			}
			if countSource(existing, schedule.SourceAgent) >= maxJobs {
				return ScheduleOutput{}, fmt.Errorf("cannot add: already at the limit of %d scheduled jobs; remove one first", maxJobs)
			}
			added, err := registry.Add(ctx, agentJobFromContext(ctx, job))
			if err != nil {
				return ScheduleOutput{}, err
			}
			jobs, err := registry.List(ctx)
			if err != nil {
				return ScheduleOutput{}, err
			}
			return scheduleOutput(jobs, fmt.Sprintf("scheduled %s", added.ID), input.IncludeFired), nil

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
			return scheduleOutput(jobs, fmt.Sprintf("removed %s", id), input.IncludeFired), nil

		default:
			return ScheduleOutput{}, fmt.Errorf("unknown op %q: use list, add or remove", input.Op)
		}
	}).
		Description("Put work on the calendar. op=add with kind=delay and in (e.g. in=\"1m\") for one-shot reminders, kind=at with at for an absolute one-shot time, or kind=cron with cron for recurring jobs. op=list reviews pending jobs; includeFired=true also shows fired one-shot history. op=remove cancels by id. The firing turn should state the task in full, e.g. use send with the reminder text.").
		Build()
	if err != nil {
		return nil, err
	}
	return &scheduleBundle{tool: tool, registry: registry}, nil
}

func countSource(jobs []schedule.Job, source string) int {
	n := 0
	for _, job := range jobs {
		if job.Source == source && !job.Fired {
			n++
		}
	}
	return n
}

func scheduleOutput(jobs []schedule.Job, instruction string, includeFired bool) ScheduleOutput {
	out := ScheduleOutput{Instruction: instruction, Jobs: make([]ScheduleEntry, 0, len(jobs))}
	for _, job := range jobs {
		if job.Fired && !includeFired {
			continue
		}
		entry := ScheduleEntry{
			ID:       job.ID,
			Kind:     schedule.JobKind(job),
			Cron:     job.Cron,
			In:       job.In,
			Prompt:   job.Prompt,
			Source:   job.Source,
			Note:     job.Note,
			Disabled: job.Disabled,
			Fired:    job.Fired,
			LastError: job.LastError,
		}
		if !job.FireAt.IsZero() {
			entry.FireAt = job.FireAt.Format(time.RFC3339)
		}
		if !job.FiredAt.IsZero() {
			entry.FiredAt = job.FiredAt.Format(time.RFC3339)
		}
		if next, ok := schedule.NextFire(job, job.LastRun); ok {
			entry.NextRun = next.Format(time.RFC3339)
		}
		out.Jobs = append(out.Jobs, entry)
	}
	return out
}

func jobFromInput(now time.Time, input ScheduleInput) (schedule.Job, error) {
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	if kind == "" {
		return schedule.Job{}, fmt.Errorf("add requires kind=cron, kind=delay, or kind=at")
	}
	job := schedule.Job{
		Kind:   kind,
		Prompt: strings.TrimSpace(input.Prompt),
		Note:   strings.TrimSpace(input.Note),
		Source: schedule.SourceAgent,
	}
	switch kind {
	case schedule.KindCron:
		if strings.TrimSpace(input.In) != "" || strings.TrimSpace(input.At) != "" {
			return schedule.Job{}, fmt.Errorf("kind=cron only accepts cron")
		}
		job.Cron = strings.TrimSpace(input.Cron)
		if job.Cron == "" {
			return schedule.Job{}, fmt.Errorf("kind=cron requires cron")
		}
		if _, err := schedule.ParseCron(job.Cron); err != nil {
			return schedule.Job{}, err
		}
	case schedule.KindDelay:
		if strings.TrimSpace(input.Cron) != "" || strings.TrimSpace(input.At) != "" {
			return schedule.Job{}, fmt.Errorf("kind=delay only accepts in")
		}
		delayText := strings.TrimSpace(input.In)
		if delayText == "" {
			return schedule.Job{}, fmt.Errorf("kind=delay requires in")
		}
		delay, err := time.ParseDuration(delayText)
		if err != nil || delay <= 0 {
			return schedule.Job{}, fmt.Errorf("invalid delay %q", delayText)
		}
		job.In = delayText
		job.FireAt = now.Add(delay)
	case schedule.KindAt:
		if strings.TrimSpace(input.Cron) != "" || strings.TrimSpace(input.In) != "" {
			return schedule.Job{}, fmt.Errorf("kind=at only accepts at")
		}
		at, err := parseAt(strings.TrimSpace(input.At), now.Location())
		if err != nil {
			return schedule.Job{}, err
		}
		job.FireAt = at
	default:
		return schedule.Job{}, fmt.Errorf("unknown schedule kind %q", input.Kind)
	}
	return job, nil
}

func parseAt(raw string, loc *time.Location) (time.Time, error) {
	if raw == "" {
		return time.Time{}, fmt.Errorf("kind=at requires at")
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, nil
		}
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(layout, raw, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid at time %q", raw)
}

func agentJobFromContext(ctx context.Context, job schedule.Job) schedule.Job {
	if delivery, ok := ctx.Value(agentkit.KeyDeliverySessionID).(agentkit.SessionID); ok && delivery != "" {
		job.DeliverySessionID = string(delivery)
	} else if session, ok := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID); ok && session != "" {
		job.DeliverySessionID = string(session)
	}
	if platform, ok := ctx.Value(agentkit.KeyPlatformID).(string); ok {
		job.PlatformID = strings.TrimSpace(platform)
	}
	if user, ok := ctx.Value(agentkit.KeyUserID).(string); ok {
		job.UserID = strings.TrimSpace(user)
	}
	if agent, ok := ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID); ok && agent != "" {
		job.AgentID = string(agent)
	}
	job.ChannelKey = session.ChannelKeyFromContext(ctx)
	return job
}
