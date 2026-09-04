package agentkit

import "context"

// ACPCommandInfo describes one native slash command advertised by an ACP agent.
type ACPCommandInfo struct {
	Name        string
	Description string
}

// ACPConfigOptionValue is one selectable value for a select-type config option.
type ACPConfigOptionValue struct {
	Value       string
	Name        string
	Description string
}

// ACPConfigOptionInfo is a display snapshot of a session config option from ACP.
type ACPConfigOptionInfo struct {
	ID           string
	Name         string
	Category     string
	Type         string
	CurrentValue string
	Description  string
	Options      []ACPConfigOptionValue
}

// ACPCommandCatalog is the cached native command surface of an ACP remote session.
type ACPCommandCatalog struct {
	AvailableCommands []ACPCommandInfo
	ConfigOptions     []ACPConfigOptionInfo
}

// ACPCommandCapable is implemented by agent/acp-remote instances.
// It exposes ACP session config control via session/set_config_option, without
// going through the AgentKit LLM loop or session/prompt.
type ACPCommandCapable interface {
	Agent
	ACPCommandCatalog(ctx context.Context, sessionID SessionID) (ACPCommandCatalog, error)
	SetACPConfigOption(ctx context.Context, sessionID SessionID, key, value string) (string, error)
}
