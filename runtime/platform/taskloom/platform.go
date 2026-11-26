// Package taskloom is the platform/taskloom plugin: it connects an AgentKit
// runner to a Taskloom task system (github.com/lengzhao/taskloom) as one of
// its Agents. It follows the Agent's authorized change feed and wakes one AgentKit
// session per task when that task is waiting on the Agent. All reads use the
// shared business API with the Agent's own credential; all writes stay with
// the Agent's MCP tools, so business rules, fencing and authorization remain
// in Taskloom.
//
// What the adapter decides (Agent-layer concerns; Taskloom keeps the rest):
//   - wake: a task enters the Agent's inbox (first seen, or back after it
//     stopped waiting on the Agent, e.g. its required subtasks completed), or
//     someone else wrote to it while it is there; the Agent's own commits never
//     wake it (no self-reply);
//   - steer: a wake for a task whose turn is running is folded into that turn
//     by the AgentKit runner.
//
// A running turn is not stopped when its task is cancelled or reassigned:
// Taskloom has already fenced the execution, and this Agent has only Taskloom
// tools, so its remaining writes are rejected and the turn ends on its own.
package taskloom

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

const platformID = "taskloom"

// Config wires the platform to one Taskloom deployment and one Agent credential.
type Config struct {
	// BaseURL is the Taskloom API root, e.g. http://taskloom:8899/v1.
	BaseURL string `json:"baseUrl"`
	// AuthorizationEnv names the environment variable holding the full
	// Authorization header value ("Bearer <secret>"); the secret never sits in
	// config. Default TASKLOOM_AUTHORIZATION.
	AuthorizationEnv string `json:"authorizationEnv"`
	// PollSeconds is how often the change feed is read. Polling the feed costs
	// one small HTTP request; the model is only woken when a task needs it.
	PollSeconds int `json:"pollSeconds"`
	// RequestTimeoutSeconds bounds each HTTP request. Default 15.
	RequestTimeoutSeconds int `json:"requestTimeoutSeconds"`
	// MaxBackoffSeconds caps the retry delay while Taskloom is unreachable.
	MaxBackoffSeconds int `json:"maxBackoffSeconds"`
}

func (c *Config) SetDefaults() {
	if strings.TrimSpace(c.AuthorizationEnv) == "" {
		c.AuthorizationEnv = "TASKLOOM_AUTHORIZATION"
	}
	if c.PollSeconds == 0 {
		c.PollSeconds = 3
	}
	if c.RequestTimeoutSeconds == 0 {
		c.RequestTimeoutSeconds = 15
	}
	if c.MaxBackoffSeconds == 0 {
		c.MaxBackoffSeconds = 60
	}
}

func (c *Config) Validate() error {
	if !strings.HasPrefix(c.BaseURL, "http://") && !strings.HasPrefix(c.BaseURL, "https://") {
		return fmt.Errorf("platform/taskloom baseUrl must be an http(s) URL, got %q", c.BaseURL)
	}
	if c.PollSeconds < 1 || c.RequestTimeoutSeconds < 1 || c.MaxBackoffSeconds < c.PollSeconds {
		return fmt.Errorf("platform/taskloom needs pollSeconds >= 1, requestTimeoutSeconds >= 1 and maxBackoffSeconds >= pollSeconds")
	}
	return nil
}

// taskState is what the adapter remembers about one task in this process.
// Nothing is persisted: after a restart every task in the inbox is woken once
// and the Agent continues from read_task.
type taskState struct {
	seen  int64 // last timeline sequence already reported to the Agent
	woken bool  // woken at least once since this process started
	// waiting is whether the task waited on the Agent at the last review that
	// covered it. A task that stops waiting and later waits again re-enters the
	// inbox and is woken, even when nobody else wrote to it.
	waiting bool
}

type Platform struct {
	cfg    Config
	api    *client
	poll   time.Duration
	maxGap time.Duration
	sleep  func(context.Context, time.Duration) error

	// State touched only by the runner's Receive loop.
	me      string
	cursor  string
	started bool
	queue   []agentkit.MessageEvent
	backoff time.Duration
	tasks   map[string]*taskState
}

// New registers platform/taskloom.
func New(cfg Config) (agentkit.Platform, error) {
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	auth := strings.TrimSpace(os.Getenv(cfg.AuthorizationEnv))
	if auth == "" {
		return nil, fmt.Errorf("platform/taskloom: environment variable %s is empty", cfg.AuthorizationEnv)
	}
	return newPlatform(cfg, newClient(cfg.BaseURL, auth, time.Duration(cfg.RequestTimeoutSeconds)*time.Second)), nil
}

func newPlatform(cfg Config, api *client) *Platform {
	return &Platform{
		cfg:    cfg,
		api:    api,
		poll:   time.Duration(cfg.PollSeconds) * time.Second,
		maxGap: time.Duration(cfg.MaxBackoffSeconds) * time.Second,
		sleep:  sleepContext,
		tasks:  map[string]*taskState{},
	}
}

func (p *Platform) PlatformID() string { return platformID }

// Receive blocks until a task needs the Agent. It owns no goroutine: polling
// happens inline, so cancelling ctx stops it and nothing outlives the runner.
func (p *Platform) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
	for {
		if err := ctx.Err(); err != nil {
			return agentkit.MessageEvent{}, err
		}
		if len(p.queue) > 0 {
			ev := p.queue[0]
			p.queue = p.queue[1:]
			return ev, nil
		}
		wait := p.poll
		if p.backoff > 0 {
			wait = p.backoff
		}
		if p.started {
			if err := p.sleep(ctx, wait); err != nil {
				return agentkit.MessageEvent{}, err
			}
		}
		if err := p.step(ctx); err != nil {
			if ctx.Err() != nil {
				return agentkit.MessageEvent{}, ctx.Err()
			}
			p.fail(err)
			if !p.started {
				if err := p.sleep(ctx, p.backoff); err != nil {
					return agentkit.MessageEvent{}, err
				}
			}
			continue
		}
		p.backoff = 0
	}
}

func (p *Platform) fail(err error) {
	if p.backoff == 0 {
		p.backoff = p.poll
	} else {
		p.backoff = min(2*p.backoff, p.maxGap)
	}
	level := slog.LevelWarn
	if errors.Is(err, errAuth) {
		// A rejected credential needs an operator; keep running so a restored
		// credential store or a restart with a new secret recovers.
		level = slog.LevelError
		p.backoff = p.maxGap
	}
	slog.Log(context.Background(), level, "taskloom poll failed", "err", err, "retry_in", p.backoff.String())
}

func (p *Platform) step(ctx context.Context) error {
	if !p.started {
		return p.start(ctx)
	}
	page, err := p.api.changes(ctx, p.cursor)
	if err != nil {
		if errors.Is(err, errBadCursor) {
			// The cursor does not belong to this database any more (restored
			// or replaced). Resynchronize from the inbox instead of guessing.
			slog.Warn("taskloom cursor rejected, resynchronizing", "err", err)
			p.started = false
			return nil
		}
		return err
	}
	if len(page.Changes) == 0 {
		p.cursor = page.Cursor
		return nil
	}
	ids := map[string]bool{}
	all := false
	for _, c := range page.Changes {
		if c.Kind == "access.changed" {
			all = true
		}
		if c.TaskID != "" {
			ids[c.TaskID] = true
		}
	}
	if err := p.review(ctx, ids, all); err != nil {
		return err // keep the cursor: the same changes are read again
	}
	p.cursor = page.Cursor
	return nil
}

// start takes the cursor before reading the inbox, so no change falls between
// the two reads; a change seen twice only re-evaluates a task.
func (p *Platform) start(ctx context.Context) error {
	v, err := p.api.me(ctx)
	if err != nil {
		return err
	}
	if v.Kind != "" && v.Kind != "agent" {
		return fmt.Errorf("%w: credential belongs to a %s, not an agent", errAuth, v.Kind)
	}
	page, err := p.api.changes(ctx, "")
	if err != nil {
		return err
	}
	p.me = v.Subject
	if err := p.review(ctx, nil, true); err != nil {
		return err
	}
	p.cursor = page.Cursor
	p.started = true
	slog.Info("taskloom platform started", "agent", p.me, "tenant", v.Tenant)
	return nil
}

// review wakes the given tasks (every inbox task when all is set) that wait on
// the Agent. The inbox is derived by Taskloom from the same facts as every
// snapshot, so the adapter keeps no copy of business rules.
func (p *Platform) review(ctx context.Context, ids map[string]bool, all bool) error {
	inbox, err := p.api.inbox(ctx)
	if err != nil {
		return err
	}
	// Rows not waiting on the Agent (e.g. blocked on subtasks) have nothing to do yet.
	waiting := map[string]bool{}
	for _, it := range inbox {
		if it.ForMe {
			waiting[it.TaskID] = true
		}
	}
	for id, st := range p.tasks {
		if (all || ids[id]) && !waiting[id] {
			st.waiting = false
		}
	}
	for _, it := range inbox {
		if !it.ForMe || !all && !ids[it.TaskID] {
			continue
		}
		if err := p.wakeIfNew(ctx, it); err != nil {
			return err
		}
	}
	return nil
}

func (p *Platform) state(id string) *taskState {
	st := p.tasks[id]
	if st == nil {
		st = &taskState{}
		p.tasks[id] = st
	}
	return st
}

// wakeIfNew wakes the task's session when it enters the inbox, and while it
// stays there only when someone other than the Agent wrote to it.
func (p *Platform) wakeIfNew(ctx context.Context, it pending) error {
	st := p.state(it.TaskID)
	// Before the first wake the history is not reported, only its position.
	title, commits, err := p.api.commitsAfter(ctx, it.TaskID, st.seen, !st.woken)
	if errors.Is(err, errGone) {
		return nil // lost between the inbox read and this one; the next change says more
	}
	if err != nil {
		return err
	}
	var others []commitView
	for _, c := range commits {
		st.seen = max(st.seen, c.Sequence)
		if c.Author != p.me {
			others = append(others, c)
		}
	}
	first, entered := !st.woken, !st.waiting
	st.waiting = true
	if !entered && len(others) == 0 {
		return nil
	}
	st.woken = true
	slog.Info("taskloom wake", "task", it.TaskID, "next_actor", it.NextActor, "first", first, "entered", entered, "new_commits", len(others))
	p.queue = append(p.queue, p.event(it.TaskID, wakeText(it, title, others, first)))
	return nil
}

// event addresses one task's session. Each task is its own channel, so its
// history and steering stay apart from every other task.
func (p *Platform) event(taskID, text string) agentkit.MessageEvent {
	ev := agentkit.MessageEvent{Message: agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: text}},
	}}
	return rctx.WithInboundRoute(ev, agentkit.SessionRouteInput{Platform: platformID, Instance: platformID, ChannelID: taskID})
}

// Send only logs turn outcomes: the Agent reports to people through Taskloom
// tools, so nothing is posted.
func (p *Platform) Send(_ context.Context, out agentkit.OutboundEvent) error {
	switch out.Type {
	case agentkit.EventTurnEnd:
		slog.Info("taskloom turn end", "session", rctx.OutboundRouteID(out), "data", truncate(string(out.Data), 300))
	case "error":
		slog.Warn("taskloom turn error", "session", rctx.OutboundRouteID(out), "data", truncate(string(out.Data), 500))
	}
	return nil
}

func wakeText(it pending, title string, others []commitView, first bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Taskloom] 任务 %s「%s」现在等你处理（你的角色：%s）。\n", it.TaskID, title, it.NextActor)
	if !first && len(others) > 0 {
		b.WriteString("自你上次被唤醒后，其他人的新动态：\n")
		for _, c := range others {
			fmt.Fprintf(&b, "- %s（%s）：%s\n", c.Author, c.Kind, truncate(strings.TrimSpace(c.Body), 800))
		}
	}
	fmt.Fprintf(&b, "只处理这个任务：先 read_task(task_id=%q) 读取完整情况再按规则推进；处理完或需要等待别人时调用 finish。", it.TaskID)
	return b.String()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
