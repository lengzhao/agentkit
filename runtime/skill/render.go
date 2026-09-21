package skill

import (
	"strings"

	capsskill "github.com/lengzhao/agentkit/cap/skill"
)

// RenderLoaded formats a loaded skill for the model, including resource-base guidance.
func RenderLoaded(content capsskill.Content) string {
	var b strings.Builder
	b.WriteString("<skill_content name=\"")
	b.WriteString(escapeAttr(content.Name))
	b.WriteString("\">\n<skill_resources>\n")
	writeResourceHint(&b, content.Path)
	b.WriteString("</skill_resources>\n\n<skill_instructions>\n")
	b.WriteString(content.Body)
	b.WriteString("\n</skill_instructions>\n</skill_content>")
	return b.String()
}

func writeResourceHint(b *strings.Builder, resourceBase string) {
	base := strings.TrimSpace(resourceBase)
	if base == "" {
		b.WriteString("Load referenced resources only as needed.\n")
		return
	}
	b.WriteString("Base directory for this skill: ")
	b.WriteString(base)
	b.WriteString("\nResolve relative paths in this skill against the base directory (absolute path). Read supporting files with read using absolute paths; run bundled scripts with bash (cd to the base path first).\n")
}

func escapeAttr(value string) string {
	return strings.NewReplacer("&", "&amp;", "\"", "&quot;", "<", "&lt;").Replace(value)
}
