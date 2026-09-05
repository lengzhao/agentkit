package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/session"
)

const platformID = "cli"

type Config struct {
	// Prompt is first message; falls back to the positional command-line arguments.
	Prompt string `json:"prompt"`
	// Once runs a single turn and exit instead of looping on stdin.
	Once bool `json:"once"`
	// DefaultSessionID overrides the session to attach to. When empty, CLI reads
	// sessions/cli_current.jsonl (falls back to cli:default when the link is missing).
	DefaultSessionID string `json:"defaultSessionId"`
}

type Deps struct {
	Commands     agentkit.Commands     `json:"commands,omitempty"`
	SessionStore agentkit.SessionStore `json:"sessionStore,omitempty"`
}

type Platform struct {
	mu    sync.Mutex
	turnMu sync.Mutex

	initialPrompt string
	once          bool
	done          bool
	welcomed      bool
	input         *Input
	commands      agentkit.Commands
	sessionStore  agentkit.SessionStore
	sessionID     agentkit.SessionID
	pending       *permissionPrompt
	turnDone      chan struct{}
	heldLine      string
}

// New registers platform/cli: Interactive terminal platform with slash commands.
func New(cfg Config, deps Deps) (agentkit.Platform, error) {
	initial := cfg.Prompt
	if initial == "" {
		initial = initialPromptFromArgs(promptArgs())
	}
	sessionID := agentkit.SessionID(cfg.DefaultSessionID)
	if sessionID == "" {
		sessionID = resolveCLISessionID(deps.SessionStore)
	}
	return &Platform{
		initialPrompt: initial,
		once:          cfg.Once,
		input:         NewInput(os.Stdin),
		sessionID:     sessionID,
		commands:      deps.Commands,
		sessionStore:  deps.SessionStore,
	}, nil
}

func (p *Platform) PlatformID() string { return platformID }

// promptArgs returns the positional arguments the first message may come from.
// Once the host has parsed its flags, flag.Args() holds exactly the positional
// tail, so `agent -config preset.yaml "task"` still yields the task.
func promptArgs() []string {
	if flag.Parsed() {
		return flag.Args()
	}
	return os.Args[1:]
}

func initialPromptFromArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	if stringsHasSuffix(args[0], ".yaml") || stringsHasSuffix(args[0], ".yml") {
		return ""
	}
	if strings.HasPrefix(args[0], "-") {
		return ""
	}
	return strings.Join(args, " ")
}

func (p *Platform) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
	if p.done {
		return agentkit.MessageEvent{}, io.EOF
	}
	if err := ctx.Err(); err != nil {
		return agentkit.MessageEvent{}, err
	}
	if !p.once && !p.welcomed {
		p.printWelcome()
		p.welcomed = true
	}

	waitingPermission := p.hasPending()
	turnBusy := p.isTurnBusy()

	var text string
	if p.heldLine != "" {
		text = p.heldLine
		p.heldLine = ""
	} else {
		if !waitingPermission && !turnBusy {
			if err := p.waitTurnIdle(ctx); err != nil {
				return agentkit.MessageEvent{}, err
			}
		}
		var err error
		text, err = p.readInput(waitingPermission)
		if err != nil {
			return agentkit.MessageEvent{}, err
		}
	}
	if pending := p.takePending(); pending != nil {
		return p.permissionReplyEvent(text, pending), nil
	}
	if text == "" {
		return agentkit.MessageEvent{}, nil
	}
	if turnBusy {
		if name, args, ok := common.ParseSlashCommand(text); ok && isTurnControlSlash(name) {
			if handled, err := p.handleSlash(ctx, name, args); handled || err != nil {
				return agentkit.MessageEvent{}, err
			}
		}
		p.heldLine = text
		if err := p.waitTurnIdle(ctx); err != nil {
			return agentkit.MessageEvent{}, err
		}
		text = p.heldLine
		p.heldLine = ""
		if text == "" {
			return agentkit.MessageEvent{}, nil
		}
	}
	if name, args, ok := common.ParseSlashCommand(text); ok {
		if handled, err := p.handleSlash(ctx, name, args); handled || err != nil {
			return agentkit.MessageEvent{}, err
		}
	}

	if p.once {
		p.done = true
	}
	p.beginTurnWait()
	return common.WithInboundRoute(agentkit.MessageEvent{
		PlatformID: platformID,
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		},
	}, session.SessionRouteInput{
		Platform:   platformID,
		DeliveryID: p.sessionID,
	}), nil
}

func (p *Platform) handleSlash(ctx context.Context, name, args string) (bool, error) {
	switch name {
	case "exit", "quit", "q":
		fmt.Fprintln(os.Stderr, "bye")
		return true, io.EOF
	case "help", "h", "?":
		p.printHelp(args)
		return true, nil
	}

	if p.commands == nil {
		fmt.Fprintf(os.Stderr, "unknown command /%s (try /help)\n", name)
		return true, nil
	}

	env := agentkit.TurnEnvelope{
		Conversation: string(p.sessionID),
		Route:        session.SessionRouteFromDelivery(platformID, p.sessionID, ""),
	}
	if userID := cliUserID(); userID != "" {
		env.Actor.UserID = userID
	}
	cmdCtx := session.ApplyEnvelopeToContext(ctx, env)
	if enricher, ok := p.commands.(agentkit.SlashAdminContext); ok {
		cmdCtx = enricher.EnrichSlashContext(cmdCtx)
	}
	out, err := p.commands.Dispatch(cmdCtx, name, args)
	if errors.Is(err, agentkit.ErrCommandForbidden) {
		fmt.Fprintln(os.Stderr, common.UnauthorizedMessage)
		return true, nil
	}
	if errors.Is(err, agentkit.ErrCommandNotHandled) {
		fmt.Fprintf(os.Stderr, "unknown command /%s (try /help)\n", name)
		return true, nil
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "command error: %v\n", err)
		return true, nil
	}
	if out != "" {
		fmt.Fprintln(os.Stderr, out)
	}
	p.refreshSessionID(ctx)
	return true, nil
}

func (p *Platform) refreshSessionID(ctx context.Context) {
	if p.sessionStore == nil {
		return
	}
	current, ok := p.sessionStore.(session.CLICurrentStore)
	if !ok {
		return
	}
	id, err := current.ResolveCLICurrent(ctx)
	if err != nil || id == "" || id == p.sessionID {
		return
	}
	p.sessionID = id
	fmt.Fprintf(os.Stderr, "new session: %s\n", p.sessionID)
}

func (p *Platform) readInput(skipPrompt bool) (string, error) {
	if p.initialPrompt != "" {
		text := p.initialPrompt
		p.initialPrompt = ""
		if !p.once {
			fmt.Fprintf(os.Stderr, "> %s\n", text)
		}
		return text, nil
	}
	if !skipPrompt {
		fmt.Fprint(os.Stderr, "> ")
	}
	line, err := p.input.ReadPrompt()
	if err != nil {
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(os.Stderr)
			return "", io.EOF
		}
		return "", err
	}
	return line, nil
}

func (p *Platform) Send(_ context.Context, event agentkit.OutboundEvent) error {
	switch event.Type {
	case agentkit.EventMessageStart:
		return nil
	case agentkit.EventMessageUpdate:
		var payload agentkit.MessageUpdatePayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		switch payload.AssistantMessageEvent.Type {
		case agentkit.AssistantEventTextDelta, agentkit.AssistantEventThinkingDelta:
			fmt.Print(payload.AssistantMessageEvent.Delta)
		case agentkit.AssistantEventToolCallStart:
			if payload.AssistantMessageEvent.ToolName != "" {
				fmt.Fprintf(os.Stderr, "\n[tool:%s]\n", payload.AssistantMessageEvent.ToolName)
			}
		}
		return nil
	case agentkit.EventMessageEnd:
		fmt.Println()
		return nil
	case agentkit.EventAssistantMessage:
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(event.Data, &msg); err != nil {
			return err
		}
		text := textOf(msg)
		if text != "" {
			fmt.Println(text)
		}
	case agentkit.EventPermissionRequest:
		payload, err := decodePermissionRequest(event.Data)
		if err != nil {
			return err
		}
		if payload.ID == "" {
			return fmt.Errorf("permission/request missing id")
		}
		p.mu.Lock()
		p.pending = &permissionPrompt{
			requestID: payload.ID,
		}
		p.mu.Unlock()
		p.renderPermissionRequest(payload)
		return nil
	case agentkit.EventPermissionResolved:
		p.mu.Lock()
		p.pending = nil
		p.mu.Unlock()
		return nil
	case agentkit.EventTurnStart:
		p.beginTurnWait()
		fmt.Fprint(os.Stderr, formatTurnStart())
		return nil
	case agentkit.EventTurnEnd:
		var payload struct {
			Steps int `json:"steps"`
		}
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		fmt.Fprint(os.Stderr, formatTurnEnd(payload.Steps))
		p.endTurnWait()
		return nil
	case agentkit.EventTurnContinue:
		var payload struct {
			Segment int    `json:"segment"`
			Reason  string `json:"reason"`
			Steps   int    `json:"steps"`
		}
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "\n[continue #%d after %d step(s): %s]\n",
			payload.Segment, payload.Steps, payload.Reason)
		return nil
	case agentkit.EventSessionRecovery:
		fmt.Fprintf(os.Stderr, "\n[recovered interrupted turn: %s]\n", string(event.Data))
		return nil
	case "error":
		var payload struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(event.Data, &payload); err == nil && payload.Error != "" {
			fmt.Fprintf(os.Stderr, "error: %s\n", payload.Error)
			p.endTurnWait()
			return nil
		}
		fallthrough
	default:
		return nil
	}
	return nil
}

func formatTurnStart() string {
	return "\n[⏳ turn started]\n"
}

func formatTurnEnd(steps int) string {
	return fmt.Sprintf("\n[✓ turn done · %d step(s)]\n", steps)
}

func (p *Platform) printWelcome() {
	fmt.Fprintln(os.Stderr, "AgentKit interactive mode. Type /help for commands, /exit to quit.")
}

func (p *Platform) printHelp(args string) {
	args = strings.TrimSpace(args)
	if args != "" {
		p.dispatchHelpTopic(args)
		return
	}
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  /help, /h, /?              show this help (or /help <command> for details)")
	fmt.Fprintln(os.Stderr, "  /exit, /quit               exit the session")
	if p.commands != nil {
		for _, cmd := range p.commands.List() {
			line := fmt.Sprintf("  /%-14s %s", cmd.Name(), cmd.Description())
			if alias := cmd.Alias(); alias != "" {
				line += fmt.Sprintf(" (alias: %s)", alias)
			}
			fmt.Fprintln(os.Stderr, line)
		}
	}
	fmt.Fprintln(os.Stderr, "  Ctrl+D                     exit when the input line is empty")
}

func (p *Platform) dispatchHelpTopic(args string) {
	if p.commands == nil {
		fmt.Fprintln(os.Stderr, "unknown help topic (try /help)")
		return
	}
	fields := splitArgs(args)
	if len(fields) == 0 {
		fmt.Fprintln(os.Stderr, "unknown help topic (try /help)")
		return
	}
	cmdCtx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: string(p.sessionID),
		Route:        session.SessionRouteFromDelivery(platformID, p.sessionID, ""),
	})
	rest := ""
	if len(fields) > 1 {
		rest = strings.TrimSpace(args[len(fields[0]):])
	}
	out, err := p.commands.Dispatch(cmdCtx, fields[0], rest)
	if errors.Is(err, agentkit.ErrCommandNotHandled) {
		fmt.Fprintln(os.Stderr, "unknown help topic (try /help)")
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "help error: %v\n", err)
		return
	}
	if out != "" {
		fmt.Fprintln(os.Stderr, out)
	}
}

func splitArgs(args string) []string {
	args = strings.TrimSpace(args)
	if args == "" {
		return nil
	}
	return strings.Fields(args)
}

func textOf(msg agentkit.ModelMessage) string {
	var b strings.Builder
	for _, part := range msg.Content {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

func stringsHasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func cliUserID() string {
	if user := strings.TrimSpace(os.Getenv("USER")); user != "" {
		return user
	}
	return "cli"
}

func isTurnControlSlash(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "stop", "exit", "quit", "q":
		return true
	default:
		return false
	}
}

func resolveCLISessionID(store agentkit.SessionStore) agentkit.SessionID {
	if store == nil {
		return session.DefaultCLISessionID
	}
	current, ok := store.(session.CLICurrentStore)
	if !ok {
		return session.DefaultCLISessionID
	}
	id, err := current.ResolveCLICurrent(context.Background())
	if err != nil || id == "" {
		return session.DefaultCLISessionID
	}
	return id
}
