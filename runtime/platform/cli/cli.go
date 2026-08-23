package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/command"
	"github.com/lengzhao/agentkit/runtime/session"
)

type Config struct {
	Prompt           string `json:"prompt"`
	Once             bool   `json:"once"`
	DefaultSessionID string `json:"defaultSessionId"`
}

type Deps struct {
	Commands command.Registry `json:"commands,omitempty"`
}

type Platform struct {
	initialPrompt string
	once          bool
	done          bool
	welcomed      bool
	reader        *bufio.Reader
	commands      command.Registry
	sessionID     agentkit.SessionID
}

func New(cfg Config, deps Deps) (agentkit.Platform, error) {
	initial := cfg.Prompt
	if initial == "" {
		initial = initialPromptFromArgs(os.Args[1:])
	}
	sessionID := agentkit.SessionID(cfg.DefaultSessionID)
	if sessionID == "" {
		sessionID = session.DefaultCLISessionID
	}
	return &Platform{
		initialPrompt: initial,
		once:          cfg.Once,
		reader:        bufio.NewReader(os.Stdin),
		commands:      deps.Commands,
		sessionID:     sessionID,
	}, nil
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

	text, err := p.readInput()
	if err != nil {
		return agentkit.MessageEvent{}, err
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
	return agentkit.MessageEvent{
		SessionID:  p.sessionID,
		PlatformID: "cli",
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
		p.printHelp()
		return true, nil
	}

	if p.commands == nil {
		fmt.Fprintf(os.Stderr, "unknown command /%s (try /help)\n", name)
		return true, nil
	}

	result, err := p.commands.Dispatch(ctx, command.Request{
		Name:       name,
		Args:       args,
		SessionID:  p.sessionID,
		PlatformID: "cli",
		ErrOut:     os.Stderr,
		Out:        os.Stdout,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "command error: %v\n", err)
		return true, nil
	}
	if !result.Handled {
		fmt.Fprintf(os.Stderr, "unknown command /%s (try /help)\n", name)
		return true, nil
	}
	if result.NewSession != "" {
		p.sessionID = result.NewSession
	}
	return true, nil
}

func (p *Platform) readInput() (string, error) {
	if p.initialPrompt != "" {
		text := p.initialPrompt
		p.initialPrompt = ""
		if !p.once {
			fmt.Fprintf(os.Stderr, "> %s\n", text)
		}
		return text, nil
	}
	fmt.Fprint(os.Stderr, "> ")
	line, err := p.reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			if len(line) > 0 {
				return strings.TrimSpace(line), nil
			}
			fmt.Fprintln(os.Stderr)
			return "", io.EOF
		}
		return "", err
	}
	return strings.TrimSpace(line), nil
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
	case "error":
		var payload struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(event.Data, &payload); err == nil && payload.Error != "" {
			fmt.Fprintf(os.Stderr, "error: %s\n", payload.Error)
			return nil
		}
		fallthrough
	default:
		if len(event.Data) > 0 {
			fmt.Println(string(event.Data))
		}
	}
	return nil
}

func (p *Platform) printWelcome() {
	fmt.Fprintln(os.Stderr, "AgentKit interactive mode. Type /help for commands, /exit to quit.")
}

func (p *Platform) printHelp() {
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  /help, /h, /?     show this help")
	fmt.Fprintln(os.Stderr, "  /exit, /quit      exit the session")
	if p.commands != nil {
		for _, desc := range p.commands.List() {
			line := fmt.Sprintf("  /%-14s %s", desc.Name, desc.Description)
			if len(desc.Aliases) > 0 {
				line += fmt.Sprintf(" (aliases: %s)", strings.Join(desc.Aliases, ", "))
			}
			fmt.Fprintln(os.Stderr, line)
		}
	}
	fmt.Fprintln(os.Stderr, "  Ctrl+D            exit when the input line is empty")
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
