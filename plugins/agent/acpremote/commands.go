package acpremote

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessctx"
)

func (a *Runtime) Commands() []agentkit.Command { return []agentkit.Command{acpCommand{runtime: a}} }

type acpCommand struct{ runtime *Runtime }

func (c acpCommand) Name() string { return "acp-" + strings.ToLower(string(c.runtime.id)) }
func (acpCommand) Alias() string  { return "" }
func (acpCommand) Description() string {
	return "show or configure this ACP agent's session model and config (mode | model | effort | config)"
}
func (acpCommand) SanitizeArgsForLog(args string) string { return agentkit.RedactSlashArgsForLog(args) }

func (c acpCommand) navigation() string {
	p := "/" + c.Name()
	return fmt.Sprintf(`ACP configuration (session by default; -g saves defaults for new sessions).
%[1]s mode [-g] [value|reset]              show or change mode
%[1]s model [-g] [value|reset]             show or change model
%[1]s effort [-g] [value|reset]            show or change effort
%[1]s config [-g] show [id]          show all options or one option
%[1]s config [-g] <id> <value|reset>       change one option
Session reset restores the first observed value; -g reset clears the saved default.
Use config -g reset to clear all global defaults. Commands time out after 5 seconds.
Options depend on the remote agent; use a subcommand to see its values.`, p)
}

type acpCommandArgs struct {
	global bool
	action string // navigation, show, set or reset
	target string // resolve a subcommand by category instead of a literal ID
	id     string
	value  string
}

// parseACPCommand normalizes aliases before connecting or resolving a session.
func parseACPCommand(args string) (acpCommandArgs, error) {
	fields := strings.Fields(args)
	global := false
	for i, field := range fields {
		if field == "-g" || field == "--global" {
			if i != 1 || global {
				return acpCommandArgs{}, fmt.Errorf("place -g immediately after the subcommand")
			}
			global = true
		}
	}
	if global {
		fields = append(fields[:1], fields[2:]...)
	}
	parsed, err := parseACPCommandPayload(strings.Join(fields, " "))
	parsed.global = global
	if parsed.action == "reset" && !global && parsed.target == "" && parsed.id == "" {
		return acpCommandArgs{}, fmt.Errorf("session reset requires an option")
	}
	return parsed, err
}

func parseACPCommandPayload(args string) (acpCommandArgs, error) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return acpCommandArgs{action: "navigation"}, nil
	}
	switch fields[0] {
	case "show":
		if len(fields) == 1 {
			return acpCommandArgs{action: "show"}, nil
		}
	case "mode", "model", "effort":
		if len(fields) == 1 || (len(fields) == 2 && fields[1] == "show") {
			return acpCommandArgs{action: "show", target: fields[0]}, nil
		}
		if len(fields) == 2 {
			if fields[1] == "reset" {
				return acpCommandArgs{action: "reset", target: fields[0]}, nil
			}
			return acpCommandArgs{action: "set", target: fields[0], value: fields[1]}, nil
		}
	case "config":
		switch {
		case len(fields) == 1:
			return acpCommandArgs{action: "show"}, nil
		case fields[1] == "show" && len(fields) <= 3:
			parsed := acpCommandArgs{action: "show"}
			if len(fields) == 3 {
				parsed.id = fields[2]
			}
			return parsed, nil
		case len(fields) == 2:
			if fields[1] == "reset" {
				return acpCommandArgs{action: "reset"}, nil
			}
			return acpCommandArgs{action: "show", id: fields[1]}, nil
		case len(fields) == 3:
			if fields[2] == "reset" {
				return acpCommandArgs{action: "reset", id: fields[1]}, nil
			}
			return acpCommandArgs{action: "set", id: fields[1], value: fields[2]}, nil
		}
	}
	return acpCommandArgs{}, fmt.Errorf("invalid ACP command syntax")
}

func (c acpCommand) CommandExec(ctx context.Context, args string) (out string, commandErr error) {
	defer func() { commandErr = c.runtime.withAuthGuidance(ctx, commandErr) }()
	parsed, err := parseACPCommand(args)
	if err != nil {
		return "", fmt.Errorf("%s", c.navigation())
	}
	if parsed.action == "navigation" {
		return c.navigation(), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	defer func() {
		if commandErr != nil && ctx.Err() != nil {
			commandErr = fmt.Errorf("ACP config command: %w", ctx.Err())
		}
	}()
	if parsed.global {
		return c.globalConfig(ctx, parsed)
	}
	a := c.runtime
	entry := rctx.ActiveEntryKeyFromContext(ctx)
	if entry == "" {
		entry = rctx.SessionIDFromContext(ctx)
	}
	sessionID := entry
	if a.sessionStore != nil {
		var err error
		sessionID, err = capsession.ResolveActiveSessionID(ctx, a.sessionStore, entry)
		if err != nil {
			return "", err
		}
	}
	if sessionID == "" {
		return "", fmt.Errorf("session id is required")
	}
	lease, err := a.acquireLease(ctx, sessionID)
	if err != nil {
		return "", err
	}
	// A slash command holds the connection like a turn does, so the pool will
	// not reclaim it mid-command.
	defer lease.Release()
	b := lease.Bridge()
	if err := b.configMu.Lock(ctx); err != nil {
		return "", err
	}
	defer b.configMu.Unlock()
	if err := b.contextMu.Lock(ctx); err != nil {
		return "", err
	}
	defer b.contextMu.Unlock()
	// The request still bounds initialization/RPCs, while the subprocess must
	// survive the slash HTTP response so its session settings remain effective.
	ctx = context.WithValue(ctx, subprocessContextKey{}, a.processCtx)
	ctx = rctx.WithConversation(ctx, string(sessionID))
	execution, err := sessctx.Resolve(ctx, a.sessionStore, a.workspace)
	if err != nil {
		return "", err
	}
	pending := rctx.WithExecution(ctx, execution)
	cwd, err := b.resolveCwd(pending)
	if err != nil {
		return "", err
	}
	if proc := b.proc.Load(); b.currentTurn() != nil && (proc == nil || proc.cwd != cwd || proc.epoch != execution.Epoch) {
		return "", fmt.Errorf("execution context change applies after the active turn completes")
	}
	ctx = pending
	remote, err := b.ensureSession(ctx, sessionID, a.id, a.sessionStore, false)
	if err != nil {
		return "", err
	}
	proc := b.proc.Load()
	if proc == nil {
		return "", fmt.Errorf("ACP connection closed; retry the command")
	}
	options, initial := proc.configSnapshot()
	id := parsed.id
	unsupported := ""
	if parsed.target != "" {
		if option := subcommandConfigOption(options, parsed.target); option != nil && option.Id != "" {
			id = string(option.Id)
		} else {
			unsupported = fmt.Sprintf("This ACP agent does not expose %s configuration; protocol query/switch is unavailable. Configure its startup command explicitly.", parsed.target)
		}
	} else if len(options) == 0 {
		unsupported = "This ACP agent does not expose configOptions; protocol configuration is unavailable. Configure its startup command explicitly."
	}
	if unsupported != "" {
		if parsed.action != "show" {
			return "", fmt.Errorf("%s", unsupported)
		}
		return unsupported, nil
	}
	var option acp.SessionConfigOption
	if id != "" {
		var ok bool
		option, ok = findConfigOption(options, id)
		if !ok {
			return "", fmt.Errorf("unknown ACP config option %q; use /%s config show", id, c.Name())
		}
	}
	if parsed.action == "show" {
		return c.show(options, initial, id), nil
	}
	if parsed.action == "reset" {
		value, ok := initial[id]
		if !ok {
			return "", fmt.Errorf("initial value for ACP config %q is unavailable", id)
		}
		parsed.value = value
	}
	req, err := configSetRequest(remote, option, parsed.value)
	if err != nil {
		return "", err
	}
	resp, err := proc.conn.SetSessionConfigOption(ctx, req)
	if err != nil {
		return "", wrapACPStageError("session/set_config_option", err)
	}
	// The full response is authoritative, including dependent options.
	proc.updateConfig(resp.ConfigOptions)
	options, initial = proc.configSnapshot()
	if parsed.target != "" {
		if option := subcommandConfigOption(options, parsed.target); option != nil {
			id = string(option.Id)
		}
	}
	if _, ok := findConfigOption(options, id); !ok {
		return fmt.Sprintf("ACP config %s updated; the remote agent no longer exposes this option.", id), nil
	}
	return c.show(options, initial, id), nil
}

// Semantic categories let subcommands work with vendor-specific option IDs.
func subcommandConfigOption(options []acp.SessionConfigOption, target string) *acp.SessionConfigOptionSelect {
	if target == "model" {
		return modelConfigOption(options)
	}
	category := target
	if target == "effort" {
		category = "thought_level"
	}
	for _, option := range options {
		s := option.Select
		if s != nil && ((s.Category != nil && string(*s.Category) == category) || (s.Category == nil && string(s.Id) == target)) {
			return s
		}
	}
	return nil
}

func findConfigOption(options []acp.SessionConfigOption, id string) (acp.SessionConfigOption, bool) {
	for _, option := range options {
		key, _ := configOptionValue(option)
		if key == id {
			return option, true
		}
	}
	return acp.SessionConfigOption{}, false
}

func configChoices(s *acp.SessionConfigOptionSelect) []acp.SessionConfigSelectOption {
	if s.Options.Ungrouped != nil {
		return []acp.SessionConfigSelectOption(*s.Options.Ungrouped)
	}
	var choices []acp.SessionConfigSelectOption
	if s.Options.Grouped != nil {
		for _, group := range *s.Options.Grouped {
			choices = append(choices, group.Options...)
		}
	}
	return choices
}

func configSetRequest(session acp.SessionId, option acp.SessionConfigOption, value string) (acp.SetSessionConfigOptionRequest, error) {
	if s := option.Select; s != nil {
		for _, choice := range configChoices(s) {
			if string(choice.Value) == value {
				return acp.SetSessionConfigOptionRequest{ValueId: &acp.SetSessionConfigOptionValueId{SessionId: session, ConfigId: s.Id, Value: choice.Value}}, nil
			}
		}
		return acp.SetSessionConfigOptionRequest{}, fmt.Errorf("invalid value %q for %s; use config show %s for available values", value, s.Id, s.Id)
	}
	if b := option.Boolean; b != nil {
		if value != "true" && value != "false" {
			return acp.SetSessionConfigOptionRequest{}, fmt.Errorf("%s requires true or false", b.Id)
		}
		return acp.SetSessionConfigOptionRequest{Boolean: &acp.SetSessionConfigOptionBoolean{SessionId: session, ConfigId: b.Id, Type: "boolean", Value: value == "true"}}, nil
	}
	return acp.SetSessionConfigOptionRequest{}, fmt.Errorf("unsupported ACP config option type")
}

func (c acpCommand) show(options []acp.SessionConfigOption, initial map[string]string, filter string) string {
	var out strings.Builder
	fmt.Fprintf(&out, "ACP agent: %s (session scope)\n", c.runtime.id)
	if model := modelFromConfig(options); filter == "" && model != "" {
		fmt.Fprintf(&out, "model: %s\n", model)
	}
	modelOption := modelConfigOption(options)
	for _, option := range options {
		id, _ := configOptionValue(option)
		if id == "" || (filter != "" && filter != id) {
			continue
		}
		c.writeConfigOption(&out, option, initial[id], option.Select != nil && option.Select == modelOption)
	}
	out.WriteString("\nInitial = first observed on this connection; ACP does not provide default values.\n\n")
	fmt.Fprintf(&out, "/%s\n", c.Name())
	return out.String()
}

func (c acpCommand) writeConfigOption(out *strings.Builder, option acp.SessionConfigOption, initial string, isModel bool) {
	id, current := configOptionValue(option)
	fmt.Fprintf(out, "\n%s\n  current: %s\n  initial: %s\n  default: (not provided)\n", id, current, initial)
	if s := option.Select; s != nil {
		if s.Description != nil {
			fmt.Fprintf(out, "  %s\n", *s.Description)
		}
		out.WriteString("  available:\n")
		choices := configChoices(s)
		for _, choice := range choices {
			fmt.Fprintf(out, "    %s (%s)", choice.Value, choice.Name)
			if choice.Description != nil {
				fmt.Fprintf(out, " — %s", *choice.Description)
			}
			out.WriteByte('\n')
		}
		if len(choices) > 0 {
			example := choices[0].Value
			for _, choice := range choices {
				if string(choice.Value) != current {
					example = choice.Value
					break
				}
			}
			fmt.Fprintf(out, "  example: /%s config %s %s\n", c.Name(), id, example)
			if isModel {
				fmt.Fprintf(out, "  example: /%s model %s\n", c.Name(), example)
			}
		}
	}
	if b := option.Boolean; b != nil {
		if b.Description != nil {
			fmt.Fprintf(out, "  %s\n", *b.Description)
		}
		fmt.Fprintf(out, "  available: true | false\n  example: /%s config %s %s\n", c.Name(), id, strconv.FormatBool(!b.CurrentValue))
	}
}

var _ agentkit.CommandProvider = (*Runtime)(nil)
var _ agentkit.CommandLogSanitizer = acpCommand{}
