package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
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
		text := strings.TrimSpace(flattenMessageContent(msg.Content))
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

func flattenMessageContent(parts []agentkit.ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		if part.Type != "text" {
			continue
		}
		text := strings.TrimSpace(part.Text)
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(text)
	}
	return b.String()
}

// Service owns tenant personal memory, dreaming, workshop, and /learn commands.
type Service struct {
	disabled    bool
	charLimit   int
	memoryRoot  string
	memoryFile  string
	sessionsDir string
	dreaming    dreaming.Config
	workshop    workshop.Config
	workspace   workspace.Service
	sessions    agentkit.SessionStore
}

type Config struct {
	Disabled    bool            `json:"disabled"`
	CharLimit   int             `json:"charLimit"`
	MemoryRoot  string          `json:"memoryRoot"`
	MemoryFile  string          `json:"memoryFile"`
	SessionsDir string          `json:"sessionsDir"`
	Dreaming    dreaming.Config `json:"dreaming"`
	Workshop    workshop.Config `json:"workshop"`
}

type Deps struct {
	Workspace    workspace.Service     `json:"workspace"`
	SessionStore agentkit.SessionStore `json:"sessionStore"`
}

// New registers learning/default: personal memory, dreaming, workshop, and /learn.
func New(cfg Config, deps Deps) (*Service, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("learning/default requires workspace")
	}
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("learning/default requires sessionStore")
	}
	dreamCfg := cfg.Dreaming.Normalized()
	wsCfg := cfg.Workshop.Normalized()
	sessionsDir := strings.TrimSpace(cfg.SessionsDir)
	if sessionsDir == "" {
		sessionsDir = "sessions"
	}
	return &Service{
		disabled:    cfg.Disabled,
		charLimit:   cfg.CharLimit,
		memoryRoot:  cfg.MemoryRoot,
		memoryFile:  cfg.MemoryFile,
		sessionsDir: sessionsDir,
		dreaming:    dreamCfg,
		workshop:    wsCfg,
		workspace:   deps.Workspace,
		sessions:    deps.SessionStore,
	}, nil
}

func (s *Service) dreamingCfg() dreaming.Config {
	return s.dreaming.Normalized()
}

func (s *Service) workshopCfg() workshop.Config {
	return s.workshop.Normalized()
}

func (s *Service) dreamingEnabled() bool {
	if s.disabled {
		return false
	}
	st, err := s.loadDreamingState(context.Background())
	if err != nil {
		return s.dreamingCfg().IsEnabled()
	}
	return st.Enabled
}

func (s *Service) memoryStore(ctx context.Context) (*MemoryStore, error) {
	path, err := s.workspace.Resolve(ctx, MemoryRelPath(s.memoryRoot, s.memoryFile))
	if err != nil {
		return nil, err
	}
	limit := s.charLimit
	if limit <= 0 {
		limit = DefaultCharLimit
	}
	return NewMemoryStore(path, limit), nil
}

func (s *Service) dreamingStore(ctx context.Context) (*dreaming.Store, error) {
	path, err := s.workspace.Resolve(ctx, dreamingStateRelPath(s.memoryRoot))
	if err != nil {
		return nil, err
	}
	return &dreaming.Store{Path: path}, nil
}

func (s *Service) diaryPath(ctx context.Context) (string, error) {
	return s.workspace.Resolve(ctx, dreamsRelPath(s.memoryRoot))
}

func (s *Service) deepReportDir(ctx context.Context) (string, error) {
	return s.workspace.Resolve(ctx, dreamingDeepRelPath(s.memoryRoot))
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

// LoadEntries reads personal memory for the current tenant workspace.
func (s *Service) LoadEntries(ctx context.Context) ([]MemoryEntry, int, int, error) {
	store, err := s.memoryStore(ctx)
	if err != nil {
		return nil, 0, 0, err
	}
	entries, err := store.Load()
	if err != nil {
		return nil, 0, 0, err
	}
	limit := store.CharLimit
	return entries, store.TotalChars(entries), limit, nil
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
	return "manage memory, dreaming, and skill workshop proposals"
}

func (c learnCommand) CommandExec(ctx context.Context, args string) (string, error) {
	if c.svc.disabled {
		return "", fmt.Errorf("learning is disabled in preset config")
	}
	fields := strings.Fields(strings.TrimSpace(args))
	if len(fields) == 0 {
		return c.svc.show(ctx)
	}
	switch strings.ToLower(fields[0]) {
	case "help", "-h", "--help":
		return FormatHelp(), nil
	case "memory":
		text := strings.TrimSpace(strings.Join(fields[1:], " "))
		if text == "" {
			return "", fmt.Errorf("usage: /learn memory <text>")
		}
		return c.svc.addMemory(ctx, text, "learn-memory")
	case "remove", "rm":
		text := strings.TrimSpace(strings.Join(fields[1:], " "))
		if text == "" {
			return "", fmt.Errorf("usage: /learn remove <text>")
		}
		return c.svc.removeMemory(ctx, text)
	case "session":
		return c.svc.learnSession(ctx)
	case "dream":
		return c.svc.handleDream(ctx, fields[1:])
	case "skill":
		focus := strings.TrimSpace(strings.Join(fields[1:], " "))
		return c.svc.learnSkill(ctx, focus)
	case "workshop":
		return c.svc.handleWorkshop(ctx, fields[1:])
	case "show":
		return c.svc.show(ctx)
	default:
		text := strings.TrimSpace(args)
		return c.svc.addMemory(ctx, text, "learn")
	}
}

func (s *Service) show(ctx context.Context) (string, error) {
	entries, used, limit, err := s.LoadEntries(ctx)
	if err != nil {
		return "", err
	}
	return FormatMemory(entries, used, limit), nil
}

func (s *Service) addMemory(ctx context.Context, text, source string) (string, error) {
	store, err := s.memoryStore(ctx)
	if err != nil {
		return "", err
	}
	if err := store.Add(text, source); err != nil {
		return "", err
	}
	warning := ""
	if err := s.recordMemorySignal(ctx, text, source); err != nil {
		warning = fmt.Sprintf("\nwarning: dreaming signal not recorded: %v", err)
	}
	entries, err := store.Load()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("personal memory updated [%d/%d chars]%s", store.TotalChars(entries), store.CharLimit, warning), nil
}

func (s *Service) recordMemorySignal(ctx context.Context, text, source string) error {
	stateStore, err := s.dreamingStore(ctx)
	if err != nil {
		return err
	}
	st, err := stateStore.Load()
	if err != nil {
		return err
	}
	st.UpsertSignal(dreaming.Signal{Text: text, Source: source}, time.Now().UTC())
	return stateStore.Save(st)
}

func (s *Service) removeMemory(ctx context.Context, text string) (string, error) {
	store, err := s.memoryStore(ctx)
	if err != nil {
		return "", err
	}
	if err := store.Remove(text); err != nil {
		return "", err
	}
	return "personal memory entry removed", nil
}

func (s *Service) learnSession(ctx context.Context) (string, error) {
	sessionID := session.SessionIDFromContext(ctx)
	summary, err := SummarizeSessionUserMessages(ctx, s.sessions, sessionID, 8)
	if err != nil {
		return "", err
	}
	content := "Session notes: " + summary
	out, err := s.addMemory(ctx, content, "learn-session")
	if err != nil {
		return "", err
	}
	if s.workshopCfg().Enabled() && looksLikeWorkflow(summary) {
		if proposal, err := s.maybeAutoSkillProposal(ctx, summary, string(sessionID), ""); err == nil && proposal != "" {
			out += "\n" + proposal
		}
	}
	return out, nil
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
	memStore, err := s.memoryStore(ctx)
	if err != nil {
		return nil, err
	}
	promote := func(text, meta string) error {
		return memStore.Add(text, meta)
	}
	return dreaming.Run(s.dreamingCfg(), stateStore, &dreaming.Diary{Path: diaryPath}, deepDir, promote, sessionsDir, time.Now().UTC())
}

func (s *Service) learnSkill(ctx context.Context, focus string) (string, error) {
	if !s.workshopCfg().Enabled() {
		return "", fmt.Errorf("skill workshop is disabled (workshop.mode=off)")
	}
	sessionID := session.SessionIDFromContext(ctx)
	summary, err := SummarizeSessionUserMessages(ctx, s.sessions, sessionID, 12)
	if err != nil {
		return "", err
	}
	wsStore, skillsDir, err := s.workshopStore(ctx)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(wsStore.Root, 0o755); err != nil {
		return "", err
	}
	pending, err := wsStore.PendingCount()
	if err != nil {
		return "", err
	}
	if pending >= s.workshopCfg().MaxPending {
		return "", fmt.Errorf("workshop has %d pending proposals (max %d); apply or reject first",
			pending, s.workshopCfg().MaxPending)
	}
	name := workshop.SuggestSkillName(focus, summary)
	desc := "Learned workflow from session."
	if focus != "" {
		desc = "Focus: " + focus
	}
	body := workshop.DraftSkillBody(name, desc, summary)
	proposal, err := wsStore.Create(name, body, "learn-skill", string(sessionID), focus, false)
	if err != nil {
		return "", err
	}
	if s.workshopCfg().AutoApply() {
		if err := proposal.Apply(skillsDir); err != nil {
			return fmt.Sprintf("proposal %s created (auto-apply failed: %v)", proposal.Meta.ID, err), nil
		}
		return fmt.Sprintf("skill %q applied from proposal %s", name, proposal.Meta.ID), nil
	}
	return fmt.Sprintf("skill proposal %s created for %q (pending apply)", proposal.Meta.ID, name), nil
}

func (s *Service) maybeAutoSkillProposal(ctx context.Context, summary, sessionID, focus string) (string, error) {
	if !s.workshopCfg().AutoApply() {
		return "", nil
	}
	wsStore, skillsDir, err := s.workshopStore(ctx)
	if err != nil {
		return "", err
	}
	pending, err := wsStore.PendingCount()
	if err != nil || pending >= s.workshopCfg().MaxPending {
		return "", err
	}
	name := workshop.SuggestSkillName(focus, summary)
	body := workshop.DraftSkillBody(name, "Autonomous capture from session.", summary)
	proposal, err := wsStore.Create(name, body, "learn-session-auto", sessionID, focus, true)
	if err != nil {
		return "", err
	}
	if err := proposal.Apply(skillsDir); err != nil {
		return "", err
	}
	return fmt.Sprintf("auto-applied skill proposal %s as %q", proposal.Meta.ID, name), nil
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

func looksLikeWorkflow(text string) bool {
	lower := strings.ToLower(text)
	needles := []string{"step ", "步骤", "workflow", "流程", "from now on", "每次", "routine"}
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return strings.Count(text, "|") >= 3
}
