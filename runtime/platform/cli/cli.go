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
	// DefaultSessionID overrides the stable delivery session key. When empty,
	// CLI uses cli:default and resolves the active conversation via session/store.
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
	deliveryID    agentkit.SessionID
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
	deliveryID := agentkit.SessionID(cfg.DefaultSessionID)
	if deliveryID == "" {
		deliveryID = session.DefaultCLISessionID
	}
	return &Platform{
		initialPrompt: initial,
		once:          cfg.Once,
		input:         NewInput(os.Stdin),
		deliveryID:    deliveryID,
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
		UserID:     cliUserID(),
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		},
	}, session.SessionRouteInput{
		Platform:   platformID,
		DeliveryID: p.deliveryID,
	}), nil
}

func (p *Platform) slashContext() common.SlashContext {
	return common.SlashContext{
		Route:        session.SessionRouteFromDelivery(platformID, p.deliveryID, ""),
		SessionScope: session.ScopeChannel,
		UserID:       cliUserID(),
	}
}

func (p *Platform) handleSlash(ctx context.Context, name, args string) (bool, error) {
	switch name {
	case "exit", "quit", "q":
		fmt.Fprintln(os.Stderr, "bye")
		return true, io.EOF
	}

	line := "/" + name
	if strings.TrimSpace(args) != "" {
		line += " " + args
	}
	outcome, err := common.ProcessSlash(ctx, p.commands, p.slashContext(), line)
	if err != nil {
		fmt.Fprintf(os.Stderr, "command error: %v\n", err)
		return true, nil
	}
	switch outcome.Kind {
	case common.SlashHandled:
		if outcome.Reply != "" {
			fmt.Fprintln(os.Stderr, outcome.Reply)
		}
		if name == "new" {
			p.notifyActiveSession(ctx)
		}
		return true, nil
	case common.SlashForward:
		if outcome.Reply != "" {
			fmt.Fprintln(os.Stderr, outcome.Reply)
		}
		return false, nil
	default:
		return false, nil
	}
}

func (p *Platform) notifyActiveSession(ctx context.Context) {
	if p.sessionStore == nil {
		return
	}
	activeStore, ok := p.sessionStore.(agentkit.ActiveSessionStore)
	if !ok {
		return
	}
	entry := session.ActiveEntryKey(p.slashContext().Route, session.DefaultRoutePolicy(session.ScopeChannel), cliUserID())
	active, err := activeStore.ActiveSession(ctx, entry)
	if err != nil || active == "" || active == entry {
		return
	}
	fmt.Fprintf(os.Stderr, "new session: %s\n", active)
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
