package subagent

import (
	"context"
	"fmt"
	"strings"

	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/helpdoc"
)

func formatDefinitionList(ctx context.Context, ws workspace.Service, dirs []string) (string, error) {
	defs, err := LoadDefinitions(ctx, ws, dirs)
	if err != nil {
		return "", err
	}
	if len(defs) == 0 {
		return "Registered subagents:\n  (none defined)\n\nUse /subagent <name> for details.", nil
	}
	width := 0
	for _, def := range defs {
		width = max(width, len(def.Name))
	}
	var b strings.Builder
	b.WriteString("Registered subagents:\n")
	for _, def := range defs {
		line := def.Name
		if def.Description != "" {
			line = fmt.Sprintf("%-*s  %s", width, def.Name, def.Description)
		}
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\nUse /subagent <name> for details.")
	return b.String(), nil
}

func definitionDoc(ctx context.Context, ws workspace.Service, dirs []string, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("subagent name is required")
	}
	defs, err := LoadDefinitions(ctx, ws, dirs)
	if err != nil {
		return "", err
	}
	if def, ok := FindDefinition(defs, name); ok {
		return formatDefinition(def), nil
	}
	return helpdoc.KindDoc(helpdoc.SubagentKindPrefix, name)
}

func formatDefinition(def capsubagent.Definition) string {
	var b strings.Builder
	fmt.Fprintf(&b, "subagent %q\n", def.Name)
	if def.Path != "" {
		fmt.Fprintf(&b, "path: %s\n", def.Path)
	}
	if def.Description != "" {
		fmt.Fprintf(&b, "description: %s\n", def.Description)
	}
	if len(def.Tools) > 0 {
		fmt.Fprintf(&b, "tools: %s\n", strings.Join(def.Tools, ", "))
	} else {
		b.WriteString("tools: (all tools available to the subagent runtime)\n")
	}
	if def.Model != "" {
		fmt.Fprintf(&b, "model: %s\n", def.Model)
	}
	if def.MaxSteps > 0 {
		fmt.Fprintf(&b, "maxSteps: %d\n", def.MaxSteps)
	}
	if def.Prompt != "" {
		b.WriteString("\nprompt:\n")
		b.WriteString(def.Prompt)
	}
	return strings.TrimRight(b.String(), "\n")
}
