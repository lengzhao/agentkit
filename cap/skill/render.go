package skill

import (
	"strings"
)

// RenderLoaded formats a loaded skill for the model, including resource-base guidance.
func RenderLoaded(content Content) string {
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
	b.WriteString("\nResolve relative paths mentioned by this skill against the base directory. Read supporting files with read; run bundled scripts with bash.\n")
}

func escapeAttr(value string) string {
	return strings.NewReplacer("&", "&amp;", "\"", "&quot;", "<", "&lt;").Replace(value)
}
