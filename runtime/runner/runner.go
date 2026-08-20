package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/lengzhao/agentkit"
)

type Config struct {
	ShutdownTimeoutSeconds int `json:"shutdownTimeoutSeconds"`
}

type Deps struct {
	Platform agentkit.Platform `json:"platform"`
	Loop     agentkit.Loop     `json:"loop"`
}

type Root struct {
	platform agentkit.Platform
	loop     agentkit.Loop
}

func New(cfg Config, deps Deps) (*Root, error) {
	if deps.Platform == nil {
		return nil, fmt.Errorf("runner requires platform")
	}
	if deps.Loop == nil {
		return nil, fmt.Errorf("runner requires loop")
	}
	_ = cfg
	return &Root{platform: deps.Platform, loop: deps.Loop}, nil
}

func (r *Root) Run(ctx context.Context) error {
	for {
		event, err := r.platform.Receive(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if event.Message.Role == "" {
			continue
		}
		fmt.Fprintln(os.Stderr)
		emit := func(ctx context.Context, out agentkit.OutboundEvent) error {
			if out.SessionID == "" {
				out.SessionID = event.SessionID
			}
			if out.AgentID == "" {
				out.AgentID = event.AgentID
			}
			return r.platform.Send(ctx, out)
		}
		_, err = r.loop.Dispatch(ctx, agentkit.LoopRequest{Event: event, Emit: emit})
		if err != nil {
			slog.Error("loop dispatch failed", "err", err)
			if sendErr := r.platform.Send(ctx, agentkit.OutboundEvent{
				SessionID: event.SessionID,
				AgentID:   event.AgentID,
				Type:      "error",
				Data:      json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error())),
			}); sendErr != nil {
				return sendErr
			}
			continue
		}
	}
}

func (r *Root) Stop(context.Context) error { return nil }

type CLIConfig struct {
	Prompt string `json:"prompt"`
	Once   bool   `json:"once"`
}

type CLI struct {
	initialPrompt string
	once          bool
	done          bool
	welcomed      bool
	reader        *bufio.Reader
}

func NewCLI(cfg CLIConfig) (*CLI, error) {
	initial := cfg.Prompt
	if initial == "" {
		initial = initialPromptFromArgs(os.Args[1:])
	}
	return &CLI{
		initialPrompt: initial,
		once:          cfg.Once,
		reader:        bufio.NewReader(os.Stdin),
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

func (p *CLI) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
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
	if cmd, ok := parseSlashCommand(text); ok {
		switch cmd {
		case "exit", "quit", "q":
			fmt.Fprintln(os.Stderr, "bye")
			return agentkit.MessageEvent{}, io.EOF
		case "help", "h", "?":
			p.printHelp()
			return agentkit.MessageEvent{}, nil
		default:
			fmt.Fprintf(os.Stderr, "unknown command /%s (try /help)\n", cmd)
			return agentkit.MessageEvent{}, nil
		}
	}

	if p.once {
		p.done = true
	}
	return agentkit.MessageEvent{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		},
	}, nil
}

func (p *CLI) readInput() (string, error) {
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

func (p *CLI) Send(_ context.Context, event agentkit.OutboundEvent) error {
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

func (p *CLI) printWelcome() {
	fmt.Fprintln(os.Stderr, "AgentKit interactive mode. Type /help for commands, /exit to quit.")
}

func (p *CLI) printHelp() {
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  /help, /h, /?   show this help")
	fmt.Fprintln(os.Stderr, "  /exit, /quit   exit the session")
	fmt.Fprintln(os.Stderr, "  Ctrl+D         exit when the input line is empty")
}

func parseSlashCommand(line string) (string, bool) {
	if !strings.HasPrefix(line, "/") {
		return "", false
	}
	cmd := strings.TrimSpace(strings.TrimPrefix(line, "/"))
	if cmd == "" {
		return "", true
	}
	if i := strings.IndexAny(cmd, " \t"); i >= 0 {
		cmd = cmd[:i]
	}
	return strings.ToLower(cmd), true
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
