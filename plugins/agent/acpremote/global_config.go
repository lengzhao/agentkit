package acpremote

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
)

// Writers to the same defaults file are serialized across runtime instances.
// Readers use atomic filesystem snapshots and never wait for remote validation.
var globalConfigLocks sync.Map // resolved defaults path -> *requestMutex

type globalConfigStore struct {
	fs      filesystem.Service
	agentID agentkit.AgentID
}

type globalConfigOptions map[string]acp.SessionConfigOption

func (s *globalConfigStore) path() string {
	return "global:acp/defaults." + base64.RawURLEncoding.EncodeToString([]byte(s.agentID)) + ".json"
}

func (s *globalConfigStore) load(ctx context.Context) (globalConfigOptions, error) {
	options := make(globalConfigOptions)
	if s == nil {
		return options, nil
	}
	raw, err := s.fs.Read(ctx, s.path())
	if errors.Is(err, os.ErrNotExist) {
		return options, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read global ACP defaults: %w", err)
	}
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, fmt.Errorf("decode global ACP defaults: %w", err)
	}
	if options == nil {
		options = make(globalConfigOptions)
	}
	return options, nil
}

func (s *globalConfigStore) save(ctx context.Context, options globalConfigOptions) error {
	raw, err := json.Marshal(options)
	if err != nil {
		return err
	}
	return s.fs.Write(ctx, s.path(), raw, filesystem.WithPerm(0600))
}

func configPriority(option acp.SessionConfigOption) int {
	if s := option.Select; s != nil && s.Category != nil {
		switch string(*s.Category) {
		case "mode":
			return 0
		case "model":
			return 1
		}
	}
	id, _ := configOptionValue(option)
	switch id {
	case "mode":
		return 0
	case "model":
		return 1
	}
	return 2
}

func sortedGlobalConfig(options globalConfigOptions) []acp.SessionConfigOption {
	sorted := make([]acp.SessionConfigOption, 0, len(options))
	for _, option := range options {
		sorted = append(sorted, option)
	}
	sort.Slice(sorted, func(i, j int) bool {
		a, b := configPriority(sorted[i]), configPriority(sorted[j])
		if a != b {
			return a < b
		}
		ai, _ := configOptionValue(sorted[i])
		bi, _ := configOptionValue(sorted[j])
		return ai < bi
	})
	return sorted
}

// Resolve semantic options again after every RPC: changing model or mode may
// replace IDs, available choices or dependent current values.
func resolveSavedConfig(options []acp.SessionConfigOption, saved acp.SessionConfigOption) (acp.SessionConfigOption, bool) {
	if s := saved.Select; s != nil && s.Category != nil {
		target := ""
		switch string(*s.Category) {
		case "mode":
			target = "mode"
		case "model":
			target = "model"
		case "thought_level":
			target = "effort"
		}
		if target != "" {
			if option := subcommandConfigOption(options, target); option != nil {
				return acp.SessionConfigOption{Select: option}, true
			}
		}
	}
	id, _ := configOptionValue(saved)
	return findConfigOption(options, id)
}

func applyGlobalConfig(ctx context.Context, proc *subprocess, remote acp.SessionId, options []acp.SessionConfigOption, defaults globalConfigOptions) ([]acp.SessionConfigOption, error) {
	for _, saved := range sortedGlobalConfig(defaults) {
		id, value := configOptionValue(saved)
		option, ok := resolveSavedConfig(options, saved)
		if !ok {
			return nil, fmt.Errorf("global ACP default %q is not exposed by this agent", id)
		}
		req, err := configSetRequest(remote, option, value)
		if err != nil {
			return nil, fmt.Errorf("global ACP default %q: %w", id, err)
		}
		resp, err := proc.conn.SetSessionConfigOption(ctx, req)
		if err != nil {
			return nil, wrapACPStageError("session/set_config_option (global default "+id+")", err)
		}
		options = resp.ConfigOptions
		proc.updateConfig(options)
	}
	return options, nil
}

func (a *Runtime) globalConfigBridge() *bridge {
	// Guarded by configBridgeMu, which some callers already hold when calling
	// this (a requestMutex is not reentrant): never lock here.
	if a.configBridge == nil {
		// Configuration discovery never prompts, replays history or applies defaults
		// automatically. It must not alter an existing conversation's remote session.
		// Created outside the pool: this connection belongs to config discovery,
		// not to any session.
		a.configBridge = newBridge(a.cfg, a.workspace, a.fs, nil, nil, a.telemetry, a.sandbox, nil)
	}
	return a.configBridge
}

func (c acpCommand) globalConfig(ctx context.Context, args acpCommandArgs) (string, error) {
	store := &globalConfigStore{fs: c.runtime.fs, agentID: c.runtime.id}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if args.action == "show" {
		defaults, err := store.load(ctx)
		if err != nil {
			return "", err
		}
		return c.showGlobalConfig(defaults, args), nil
	}
	key, err := c.runtime.workspace.Resolve(ctx, store.path())
	if err != nil {
		return "", err
	}
	value, _ := globalConfigLocks.LoadOrStore(key, &requestMutex{})
	lock := value.(*requestMutex)
	if err := lock.Lock(ctx); err != nil {
		return "", err
	}
	defer lock.Unlock()
	defaults, err := store.load(ctx)
	if err != nil {
		return "", err
	}
	if args.action == "reset" {
		if args.target == "" && args.id == "" {
			defaults = make(globalConfigOptions)
		} else {
			id := args.id
			if args.target != "" {
				if option := subcommandConfigOption(sortedGlobalConfig(defaults), args.target); option != nil {
					id = string(option.Id)
				} else {
					id = args.target
				}
			}
			delete(defaults, id)
		}
		if err := store.save(ctx, defaults); err != nil {
			return "", err
		}
		return c.showGlobalConfig(defaults, args), nil
	}
	if err := c.runtime.configBridgeMu.Lock(ctx); err != nil {
		return "", err
	}
	defer c.runtime.configBridgeMu.Unlock()
	b := c.runtime.globalConfigBridge()
	// Never reuse a subprocess across requests: cwd, credentials and the sandbox
	// view belong to the caller's workspace. Reuse only the idle connection loop.
	defer b.releaseSubprocess()
	ctx = context.WithValue(ctx, subprocessContextKey{}, c.runtime.processCtx)
	proc, err := b.ensureConn(ctx)
	if err != nil {
		return "", err
	}
	cwd, err := b.resolveCwd(ctx)
	if err != nil {
		return "", err
	}
	resp, err := proc.conn.NewSession(ctx, acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
	if err != nil {
		return "", wrapACPStageError("session/new (global config)", err)
	}
	// Apply only upstream defaults before resolving/validating this option.
	// A stale value being replaced must not prevent its own repair.
	priority := 2
	switch args.target {
	case "mode":
		priority = 0
	case "model":
		priority = 1
	}
	if args.target == "" {
		if selected, ok := findConfigOption(resp.ConfigOptions, args.id); ok {
			priority = configPriority(selected)
		}
	}
	upstream := make(globalConfigOptions)
	for key, saved := range defaults {
		if configPriority(saved) < priority {
			upstream[key] = saved
		}
	}
	options, err := applyGlobalConfig(ctx, proc, resp.SessionId, resp.ConfigOptions, upstream)
	if err != nil {
		return "", err
	}
	var option acp.SessionConfigOption
	var ok bool
	if args.target != "" {
		if selected := subcommandConfigOption(options, args.target); selected != nil {
			option = acp.SessionConfigOption{Select: selected}
			ok = true
		}
	} else {
		option, ok = findConfigOption(options, args.id)
	}
	if !ok {
		return "", fmt.Errorf("this ACP agent does not expose configuration %q", args.target+args.id)
	}
	if _, err := configSetRequest(resp.SessionId, option, args.value); err != nil {
		return "", err
	}
	// Copy the selected union member: snapshots from the SDK remain immutable.
	if option.Select != nil {
		copy := *option.Select
		copy.CurrentValue = acp.SessionConfigValueId(args.value)
		option.Select = &copy
	}
	if option.Boolean != nil {
		copy := *option.Boolean
		copy.CurrentValue = args.value == "true"
		option.Boolean = &copy
	}
	id, _ := configOptionValue(option)
	for key, saved := range defaults {
		if resolved, ok := resolveSavedConfig(options, saved); ok {
			if resolvedID, _ := configOptionValue(resolved); resolvedID == id {
				delete(defaults, key)
			}
		}
	}
	defaults[id] = option
	options, err = applyGlobalConfig(ctx, proc, resp.SessionId, options, defaults)
	if err != nil {
		return "", err
	}
	// Save authoritative values and refreshed metadata only for explicit defaults.
	refreshed := make(globalConfigOptions, len(defaults))
	for _, saved := range defaults {
		selected, ok := resolveSavedConfig(options, saved)
		if !ok {
			return "", fmt.Errorf("remote removed a configured global option")
		}
		key, _ := configOptionValue(selected)
		refreshed[key] = selected
	}
	if err := store.save(ctx, refreshed); err != nil {
		return "", err
	}
	return c.showGlobalConfig(refreshed, args), nil
}

func (c acpCommand) showGlobalConfig(defaults globalConfigOptions, args acpCommandArgs) string {
	var out strings.Builder
	fmt.Fprintf(&out, "ACP agent: %s (global defaults; new sessions only)\n", c.runtime.id)
	options := sortedGlobalConfig(defaults)
	filter := args.id
	if args.target != "" {
		if option := subcommandConfigOption(options, args.target); option != nil {
			filter = string(option.Id)
		} else {
			filter = args.target
		}
	}
	found := false
	for _, option := range options {
		id, value := configOptionValue(option)
		if filter != "" && id != filter {
			continue
		}
		fmt.Fprintf(&out, "\n%s\n  global default: %s\n", id, value)
		found = true
	}
	if !found {
		if filter == "" {
			out.WriteString("\nglobal defaults: (not set)\n")
		} else {
			fmt.Fprintf(&out, "\n%s\n  global default: (not set)\n", filter)
		}
	}
	fmt.Fprintf(&out, "\n/%s\n", c.Name())
	return out.String()
}
