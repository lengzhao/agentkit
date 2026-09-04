package acpremote

import (
	"strings"
	"sync"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
)

type sessionState struct {
	mu            sync.RWMutex
	configOptions []acp.SessionConfigOption
	commands      []acp.AvailableCommand
	modes         *acp.SessionModeState
}

func (s *sessionState) applyNewSession(resp acp.NewSessionResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(resp.ConfigOptions) > 0 {
		s.configOptions = append([]acp.SessionConfigOption(nil), resp.ConfigOptions...)
	}
	if resp.Modes != nil {
		modes := *resp.Modes
		s.modes = &modes
	}
}

func (s *sessionState) applyUpdate(update acp.SessionUpdate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case update.ConfigOptionUpdate != nil:
		s.configOptions = append([]acp.SessionConfigOption(nil), update.ConfigOptionUpdate.ConfigOptions...)
	case update.AvailableCommandsUpdate != nil:
		s.commands = append([]acp.AvailableCommand(nil), update.AvailableCommandsUpdate.AvailableCommands...)
	}
}

func (s *sessionState) catalog() agentkit.ACPCommandCatalog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := agentkit.ACPCommandCatalog{
		AvailableCommands: make([]agentkit.ACPCommandInfo, 0, len(s.commands)),
		ConfigOptions:     make([]agentkit.ACPConfigOptionInfo, 0, len(s.configOptions)),
	}
	for _, cmd := range s.commands {
		out.AvailableCommands = append(out.AvailableCommands, agentkit.ACPCommandInfo{
			Name:        cmd.Name,
			Description: cmd.Description,
		})
	}
	for _, opt := range s.configOptions {
		if info, ok := configOptionInfo(opt); ok {
			out.ConfigOptions = append(out.ConfigOptions, info)
		}
	}
	return out
}

func (s *sessionState) findConfigOption(key string) (configOptionRef, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key = strings.ToLower(strings.TrimSpace(key))
	for _, opt := range s.configOptions {
		if ref, ok := matchConfigOption(opt, key); ok {
			return ref, true
		}
	}
	return configOptionRef{}, false
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
