package acpremote

import (
	"strings"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
)

type sessionState struct {
	ch chan func(*sessionStateData)
}

type sessionStateData struct {
	configOptions []acp.SessionConfigOption
	commands      []acp.AvailableCommand
	modes         *acp.SessionModeState
}

func newSessionState() *sessionState {
	s := &sessionState{ch: make(chan func(*sessionStateData))}
	go s.loop()
	return s
}

func (s *sessionState) loop() {
	var data sessionStateData
	for fn := range s.ch {
		fn(&data)
	}
}

// Stop retires the owner goroutine. Safe to call once; the bridge calls it
// when its connLoop exits so sessionState goroutines do not leak.
func (s *sessionState) Stop() {
	close(s.ch)
}

func (s *sessionState) sync(fn func(*sessionStateData)) {
	done := make(chan struct{})
	s.ch <- func(data *sessionStateData) {
		fn(data)
		close(done)
	}
	<-done
}

func (s *sessionState) applyBootstrap(configOptions []acp.SessionConfigOption, modes *acp.SessionModeState) {
	s.sync(func(data *sessionStateData) {
		if len(configOptions) > 0 {
			data.configOptions = append([]acp.SessionConfigOption(nil), configOptions...)
		}
		if modes != nil {
			copied := *modes
			data.modes = &copied
		}
	})
}

func (s *sessionState) applyUpdate(update acp.SessionUpdate) {
	s.sync(func(data *sessionStateData) {
		switch {
		case update.ConfigOptionUpdate != nil:
			data.configOptions = append([]acp.SessionConfigOption(nil), update.ConfigOptionUpdate.ConfigOptions...)
		case update.AvailableCommandsUpdate != nil:
			data.commands = append([]acp.AvailableCommand(nil), update.AvailableCommandsUpdate.AvailableCommands...)
		}
	})
}

func (s *sessionState) catalog() agentkit.ACPCommandCatalog {
	var out agentkit.ACPCommandCatalog
	s.sync(func(data *sessionStateData) {
		out = agentkit.ACPCommandCatalog{
			AvailableCommands: make([]agentkit.ACPCommandInfo, 0, len(data.commands)),
			ConfigOptions:     make([]agentkit.ACPConfigOptionInfo, 0, len(data.configOptions)),
		}
		for _, cmd := range data.commands {
			out.AvailableCommands = append(out.AvailableCommands, agentkit.ACPCommandInfo{
				Name:        cmd.Name,
				Description: cmd.Description,
			})
		}
		for _, opt := range data.configOptions {
			if info, ok := configOptionInfo(opt); ok {
				out.ConfigOptions = append(out.ConfigOptions, info)
			}
		}
	})
	return out
}

func (s *sessionState) findConfigOption(key string) (configOptionRef, bool) {
	key = strings.ToLower(strings.TrimSpace(key))
	var ref configOptionRef
	var ok bool
	s.sync(func(data *sessionStateData) {
		for _, opt := range data.configOptions {
			if matched, matchOK := matchConfigOption(opt, key); matchOK {
				ref = matched
				ok = true
				return
			}
		}
	})
	return ref, ok
}

type configOptionRef struct {
	id      acp.SessionConfigId
	boolean bool
	options map[string]struct{}
}

func matchConfigOption(opt acp.SessionConfigOption, key string) (configOptionRef, bool) {
	if opt.Select != nil {
		if !matchesConfigKey(key, string(opt.Select.Id), opt.Select.Name, opt.Select.Category) {
			return configOptionRef{}, false
		}
		ref := configOptionRef{
			id:      opt.Select.Id,
			options: make(map[string]struct{}),
		}
		for _, item := range selectConfigOptions(opt.Select.Options) {
			ref.options[string(item.Value)] = struct{}{}
		}
		return ref, true
	}
	if opt.Boolean != nil {
		if !matchesConfigKey(key, string(opt.Boolean.Id), opt.Boolean.Name, opt.Boolean.Category) {
			return configOptionRef{}, false
		}
		return configOptionRef{id: opt.Boolean.Id, boolean: true}, true
	}
	return configOptionRef{}, false
}

func matchesConfigKey(key, id, name string, category *acp.SessionConfigOptionCategory) bool {
	if strings.EqualFold(id, key) || strings.EqualFold(name, key) {
		return true
	}
	if category != nil && strings.EqualFold(string(*category), key) {
		return true
	}
	return false
}

func configOptionInfo(opt acp.SessionConfigOption) (agentkit.ACPConfigOptionInfo, bool) {
	if opt.Select != nil {
		info := agentkit.ACPConfigOptionInfo{
			ID:           string(opt.Select.Id),
			Name:         opt.Select.Name,
			Type:         "select",
			CurrentValue: string(opt.Select.CurrentValue),
			Options:      make([]agentkit.ACPConfigOptionValue, 0),
		}
		if opt.Select.Category != nil {
			info.Category = string(*opt.Select.Category)
		}
		if opt.Select.Description != nil {
			info.Description = *opt.Select.Description
		}
		for _, item := range selectConfigOptions(opt.Select.Options) {
			entry := agentkit.ACPConfigOptionValue{
				Value: string(item.Value),
				Name:  item.Name,
			}
			if item.Description != nil {
				entry.Description = *item.Description
			}
			info.Options = append(info.Options, entry)
		}
		return info, true
	}
	if opt.Boolean != nil {
		info := agentkit.ACPConfigOptionInfo{
			ID:           string(opt.Boolean.Id),
			Name:         opt.Boolean.Name,
			Type:         "boolean",
			CurrentValue: boolString(opt.Boolean.CurrentValue),
		}
		if opt.Boolean.Category != nil {
			info.Category = string(*opt.Boolean.Category)
		}
		if opt.Boolean.Description != nil {
			info.Description = *opt.Boolean.Description
		}
		return info, true
	}
	return agentkit.ACPConfigOptionInfo{}, false
}

func selectConfigOptions(options acp.SessionConfigSelectOptions) []acp.SessionConfigSelectOption {
	if options.Ungrouped != nil {
		return []acp.SessionConfigSelectOption(*options.Ungrouped)
	}
	if options.Grouped == nil {
		return nil
	}
	out := make([]acp.SessionConfigSelectOption, 0)
	for _, group := range *options.Grouped {
		out = append(out, group.Options...)
	}
	return out
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
