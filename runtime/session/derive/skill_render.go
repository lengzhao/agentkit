package derive

import (
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/skill"
)

type skillLoadEvent struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Body         string `json:"body"`
	ResourceBase string `json:"resourceBase,omitempty"`
	Rendered     string `json:"rendered,omitempty"`
}

// RenderSkillLoaded formats a loaded skill for the model, including resource-base guidance.
func RenderSkillLoaded(content skill.Content) string {
	var b strings.Builder
	b.WriteString("<skill_content name=\"")
	b.WriteString(escapeSkillAttr(content.Name))
	b.WriteString("\">\n<skill_resources>\n")
	writeSkillResourceHint(&b, content.Path)
	b.WriteString("</skill_resources>\n\n<skill_instructions>\n")
	b.WriteString(content.Body)
	b.WriteString("\n</skill_instructions>\n</skill_content>")
	return b.String()
}

func skillLoadMessage(load skillLoadEvent) agentkit.ModelMessage {
	text := strings.TrimSpace(load.Rendered)
	if text == "" {
		text = RenderSkillLoaded(skill.Content{
			Name:        load.Name,
			Description: load.Description,
			Body:        load.Body,
			Path:        load.ResourceBase,
		})
	}
	return agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: text}},
	}
}

func writeSkillResourceHint(b *strings.Builder, resourceBase string) {
	base := strings.TrimSpace(resourceBase)
	if base == "" {
		b.WriteString("Load referenced resources only as needed.\n")
		return
	}
	b.WriteString("Base directory for this skill: ")
	b.WriteString(base)
	b.WriteString("\nResolve relative paths in this skill against the base directory (absolute path). Read supporting files with read using absolute paths; run bundled scripts with bash (cd to the base path first).\n")
}

func escapeSkillAttr(value string) string {
	return strings.NewReplacer("&", "&amp;", "\"", "&quot;", "<", "&lt;").Replace(value)
}
