package agent

import (
	"fmt"
	"strings"
)

// AgentCatalogEntry describes this agent for the /agent catalog command.
func (a *Runtime) AgentCatalogEntry() string {
	var b strings.Builder
	fmt.Fprintf(&b, "agent %q\n", a.id)
	b.WriteString("kind: agent/coding\n")
	if a.model != "" {
		fmt.Fprintf(&b, "model: %s\n", a.model)
	}
	return strings.TrimRight(b.String(), "\n")
}
