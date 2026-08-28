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
	sessionID     agentkit.SessionID
	pending       *permissionPrompt
	turnDone      chan struct{}
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
	if !waitingPermission {
		if err := p.waitTurnIdle(ctx); err != nil {
			return agentkit.MessageEvent{}, err
		}
	}
	text, err := p.readInput(waitingPermission)
	if err != nil {
		return agentkit.MessageEvent{}, err
	}
	if pending := p.takePending(); pending != nil {
		return p.permissionReplyEvent(text, pending), nil
	}
	if text == "" {
		return agentkit.MessageEvent{}, nil
	}
	if name, args, ok := parseSlashCommand(text); ok {
		if handled, err := p.handleSlash(ctx, name, args); handled || err != nil {
			return agentkit.MessageEvent{}, err
		}
	}

	if p.once {
		p.done = true
	}
	p.beginTurnWait()
	return agentkit.MessageEvent{
		SessionID:  p.sessionID,
		PlatformID: platformID,
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		},
	}, nil
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

	cmdCtx := context.WithValue(ctx, agentkit.KeySessionID, p.sessionID)
	result, err := p.commands.Dispatch(cmdCtx, name, splitArgs(args))
	if err != nil {
		fmt.Fprintf(os.Stderr, "command error: %v\n", err)
		return true, nil
	}
	if result == nil {
		fmt.Fprintf(os.Stderr, "unknown command /%s (try /help)\n", name)
		return true, nil
	}
	if result.Output != "" {
		fmt.Fprintln(os.Stderr, result.Output)
	}
	if result.NewSession != "" {
		p.sessionID = result.NewSession
		fmt.Fprintf(os.Stderr, "new session: %s\n", p.sessionID)
	}
	return true, nil
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
	cmdCtx := context.WithValue(context.Background(), agentkit.KeySessionID, p.sessionID)
	result, err := p.commands.Dispatch(cmdCtx, fields[0], fields[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "help error: %v\n", err)
		return
	}
	if result == nil {
		fmt.Fprintln(os.Stderr, "unknown help topic (try /help)")
		return
	}
	if result.Output != "" {
		fmt.Fprintln(os.Stderr, result.Output)
	}
}

func parseSlashCommand(line string) (name, args string, ok bool) {
	if !strings.HasPrefix(line, "/") {
		return "", "", false
	}
	body := strings.TrimSpace(strings.TrimPrefix(line, "/"))
	if body == "" {
		return "", "", true
	}
	fields := strings.Fields(body)
	name = strings.ToLower(fields[0])
	if len(fields) > 1 {
		args = strings.TrimSpace(body[len(fields[0]):])
	}
	return name, args, true
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
