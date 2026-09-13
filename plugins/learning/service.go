package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	capmemory "github.com/lengzhao/agentkit/cap/memory"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/plugins/learning/dreaming"
	"github.com/lengzhao/agentkit/plugins/learning/workshop"
	"github.com/lengzhao/agentkit/runtime/session"
)

// SummarizeSessionUserMessages extracts recent user text from the current session.
func SummarizeSessionUserMessages(ctx context.Context, store agentkit.SessionStore, sessionID agentkit.SessionID, maxMessages int) (string, error) {
	if store == nil {
		return "", fmt.Errorf("session store is required")
	}
	if sessionID == "" {
		return "", fmt.Errorf("session id is required")
	}
	if maxMessages <= 0 {
		maxMessages = 8
	}
	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		return "", err
	}
	events, err := session.ReadAllEvents(ctx, sess)
	if err != nil {
		return "", err
	}
	var users []string
	for _, ev := range events {
		if ev.Type != agentkit.EventUserMessage {
			continue
		}
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			continue
		}
		text := strings.TrimSpace(session.FlattenTextParts(msg.Content, "\n"))
		if text == "" || strings.HasPrefix(text, "/") {
			continue
		}
		users = append(users, text)
	}
	if len(users) == 0 {
		return "", fmt.Errorf("no user messages in session")
	}
	if len(users) > maxMessages {
		users = users[len(users)-maxMessages:]
	}
	return strings.Join(users, " | "), nil
}

// Service owns dreaming, workshop, and /learn commands (memory.md is memory/default).
type Service struct {
	disabled    bool
	sessionsDir string
	dreaming    dreaming.Config
	workshop    workshop.Config
	workspace   workspace.Service
	sessions    agentkit.SessionStore
	memory      capmemory.Service
}

type Config struct {
	Disabled    bool            `json:"disabled"`
	SessionsDir string          `json:"sessionsDir"`
	Dreaming    dreaming.Config `json:"dreaming"`
	Workshop    workshop.Config `json:"workshop"`
}

type Deps struct {
	Workspace    workspace.Service     `json:"workspace"`
	SessionStore agentkit.SessionStore `json:"sessionStore"`
	Memory       capmemory.Service     `json:"memory"`
}

// New registers learning/default: dreaming, workshop, and /learn (memory via deps.Memory).
func New(cfg Config, deps Deps) (*Service, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("learning/default requires workspace")
	}
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("learning/default requires sessionStore")
	}
	if deps.Memory == nil {
		return nil, fmt.Errorf("learning/default requires memory")
	}
	dreamCfg := cfg.Dreaming.Normalized()
	wsCfg := cfg.Workshop.Normalized()
	sessionsDir := strings.TrimSpace(cfg.SessionsDir)
	if sessionsDir == "" {
		sessionsDir = "sessions"
	}
	svc := &Service{
		disabled:    cfg.Disabled,
		sessionsDir: sessionsDir,
		dreaming:    dreamCfg,
		workshop:    wsCfg,
		workspace:   deps.Workspace,
		sessions:    deps.SessionStore,
		memory:      deps.Memory,
	}
	if reg, ok := deps.Memory.(capmemory.CommitObserverRegistrar); ok {
		reg.RegisterCommitObserver(svc)
	}
	return svc, nil
}

func (s *Service) dreamingCfg() dreaming.Config {
	return s.dreaming.Normalized()
}

func (s *Service) workshopCfg() workshop.Config {
	return s.workshop.Normalized()
}

func (s *Service) dreamingStore(ctx context.Context) (*dreaming.Store, error) {
	path, err := s.memory.ResolveRel(ctx, "memory", "dreaming", "state.json")
	if err != nil {
		return nil, err
	}
	return &dreaming.Store{Path: path}, nil
}

func (s *Service) diaryPath(ctx context.Context) (string, error) {
	return s.memory.ResolveRel(ctx, DefaultDreamsFile)
}

func (s *Service) deepReportDir(ctx context.Context) (string, error) {
	return s.memory.ResolveRel(ctx, DefaultDreamingSubdir, "deep")
}

func (s *Service) sessionsPath(ctx context.Context) (string, error) {
	return s.workspace.Resolve(ctx, s.sessionsDir)
}

func (s *Service) workshopStore(ctx context.Context) (*workshop.Store, string, error) {
	skillsDir, err := s.workspace.Resolve(ctx, s.workshopCfg().SkillsDir)
	if err != nil {
		return nil, "", err
	}
	root := filepath.Join(skillsDir, ".workshop")
	return &workshop.Store{Root: root}, skillsDir, nil
}

func (s *Service) loadDreamingState(ctx context.Context) (*dreaming.State, error) {
	store, err := s.dreamingStore(ctx)
	if err != nil {
		return nil, err
	}
	return store.Load()
}

func (s *Service) Commands() []agentkit.Command {
	return []agentkit.Command{learnCommand{svc: s}}
}

type learnCommand struct {
	svc *Service
}

func (learnCommand) Name() string  { return "learn" }
func (learnCommand) Alias() string { return "" }
func (learnCommand) Description() string {
	return "dreaming consolidation and skill workshop (/memory for memory.md)"
}

func (c learnCommand) CommandExec(ctx context.Context, args string) (string, error) {
	if c.svc.disabled {
		return "", fmt.Errorf("learning is disabled in preset config")
	}
	fields := strings.Fields(strings.TrimSpace(args))
	if len(fields) == 0 {
		return FormatHelp(), nil
	}
	switch strings.ToLower(fields[0]) {
	case "help", "-h", "--help":
		return FormatHelp(), nil
	case "session":
		return c.svc.learnSession(ctx)
	case "dream":
		return c.svc.handleDream(ctx, fields[1:])
	case "skill":
		focus := strings.TrimSpace(strings.Join(fields[1:], " "))
		return c.svc.learnSkill(ctx, focus)
	case "workshop":
		return c.svc.handleWorkshop(ctx, fields[1:])
	case "policy":
		return c.svc.handlePolicy(ctx, fields[1:])
	default:
		return "", fmt.Errorf("unknown /learn command %q (try /learn help)", fields[0])
	}
}

func (s *Service) recordMemorySignal(ctx context.Context, text, source string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	stateStore, err := s.dreamingStore(ctx)
	if err != nil {
		return err
	}
	st, err := stateStore.Load()
	if err != nil {
		return err
	}
	if !st.Enabled || !s.dreamingCfg().IsEnabled() {
		return nil
	}
	st.UpsertSignal(dreaming.Signal{Text: text, Source: source}, time.Now().UTC())
	return stateStore.Save(st)
}

func (s *Service) learnSession(ctx context.Context) (string, error) {
	if !s.dreamingCfg().IsEnabled() {
		return "dreaming is off; use /learn dream on to queue session notes for background review", nil
	}
	stateStore, err := s.dreamingStore(ctx)
	if err != nil {
		return "", err
	}
	st, err := stateStore.Load()
	if err != nil {
		return "", err
	}
	if st != nil && !st.Enabled {
		return "dreaming is off; use /learn dream on to queue session notes for background review", nil
	}
	sessionID := session.SessionIDFromContext(ctx)
	summary, err := SummarizeSessionUserMessages(ctx, s.sessions, sessionID, 8)
	if err != nil {
		return "", err
	}
	source := "session:" + string(sessionID)
	if err := s.recordMemorySignal(ctx, summary, source); err != nil {
		return "", err
	}
	return "session notes recorded as dreaming signals (memory.md: /memory add or background review)", nil
}

func (s *Service) handleDream(ctx context.Context, args []string) (string, error) {
	if len(args) == 0 {
		return s.dreamStatus(ctx)
	}
	switch strings.ToLower(args[0]) {
	case "status":
		return s.dreamStatus(ctx)
	case "run", "sweep":
		res, err := s.runDreamSweep(ctx)
		if err != nil {
			return "", err
		}
		return formatSweepResult(res), nil
	case "on":
		return s.setDreaming(ctx, true)
	case "off":
		return s.setDreaming(ctx, false)
	case "help":
		return "usage: /learn dream status|run|on|off", nil
	default:
		return "", fmt.Errorf("usage: /learn dream status|run|on|off")
	}
}

func (s *Service) dreamStatus(ctx context.Context) (string, error) {
	st, err := s.loadDreamingState(ctx)
	if err != nil {
		return "", err
	}
	return dreaming.FormatStatus(st, s.dreamingCfg()), nil
}

func (s *Service) setDreaming(ctx context.Context, enabled bool) (string, error) {
	store, err := s.dreamingStore(ctx)
	if err != nil {
		return "", err
	}
	st, err := store.Load()
	if err != nil {
		return "", err
	}
	st.Enabled = enabled
	if err := store.Save(st); err != nil {
		return "", err
	}
	if enabled {
		return "dreaming enabled", nil
	}
	return "dreaming disabled", nil
}

func (s *Service) runDreamSweep(ctx context.Context) (*dreaming.SweepResult, error) {
	stateStore, err := s.dreamingStore(ctx)
	if err != nil {
		return nil, err
	}
	diaryPath, err := s.diaryPath(ctx)
	if err != nil {
		return nil, err
	}
	deepDir, err := s.deepReportDir(ctx)
	if err != nil {
		return nil, err
	}
	sessionsDir, err := s.sessionsPath(ctx)
	if err != nil {
		return nil, err
	}
	return dreaming.Run(s.dreamingCfg(), stateStore, &dreaming.Diary{Path: diaryPath}, deepDir, sessionsDir, time.Now().UTC())
}

func (s *Service) learnSkill(ctx context.Context, focus string) (string, error) {
	if !s.skillsWorkshopEnabled(ctx) {
		return "", fmt.Errorf("skill workshop is disabled (use /learn policy skills propose|auto)")
	}
	sessionID := session.SessionIDFromContext(ctx)
	summary, err := SummarizeSessionUserMessages(ctx, s.sessions, sessionID, 12)
	if err != nil {
		return "", err
	}
	name := workshop.SuggestSkillName(focus, summary)
	desc := "Learned workflow from session."
	if focus != "" {
		desc = "Focus: " + focus
	}
	body := workshop.DraftSkillBody(name, desc, summary)
	return s.createSkillProposal(ctx, skillProposeParams{
		Name:       name,
		Body:       body,
		Source:     "learn-skill",
		SessionID:  string(sessionID),
		Focus:      focus,
		Autonomous: false,
	})
}

func (s *Service) handleWorkshop(ctx context.Context, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: /learn workshop list|show <id>|apply <id>|reject <id>")
	}
	wsStore, skillsDir, err := s.workshopStore(ctx)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(args[0]) {
	case "list":
		all, err := wsStore.List()
		if err != nil {
			return "", err
		}
		return workshop.FormatList(all), nil
	case "show":
		if len(args) < 2 {
			return "", fmt.Errorf("usage: /learn workshop show <id>")
		}
		p, err := wsStore.Load(args[1])
		if err != nil {
			return "", err
		}
		return workshop.FormatProposal(p), nil
	case "apply":
		if len(args) < 2 {
			return "", fmt.Errorf("usage: /learn workshop apply <id>")
		}
		p, err := wsStore.Load(args[1])
		if err != nil {
			return "", err
		}
		if err := p.Apply(skillsDir); err != nil {
			return "", err
		}
		return fmt.Sprintf("applied proposal %s to skill %q", p.Meta.ID, p.Meta.SkillName), nil
	case "reject":
		if len(args) < 2 {
			return "", fmt.Errorf("usage: /learn workshop reject <id>")
		}
		p, err := wsStore.Load(args[1])
		if err != nil {
			return "", err
		}
		if err := p.Reject(); err != nil {
			return "", err
		}
		return fmt.Sprintf("rejected proposal %s", p.Meta.ID), nil
	default:
		return "", fmt.Errorf("usage: /learn workshop list|show <id>|apply <id>|reject <id>")
	}
}

