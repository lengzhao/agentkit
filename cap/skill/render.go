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

// RenderResourceFile formats one supporting file read from a skill directory.
func RenderResourceFile(name, resourceBase, relPath, body string) string {
	var b strings.Builder
	b.WriteString("<skill_resource name=\"")
	b.WriteString(escapeAttr(name))
	b.WriteString("\" file=\"")
	b.WriteString(escapeAttr(relPath))
	b.WriteString("\">\n<skill_resources>\n")
	writeResourceHint(&b, resourceBase)
	b.WriteString("</skill_resources>\n\n")
	b.WriteString(body)
	b.WriteString("\n</skill_resource>")
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
	b.WriteString("\nResolve relative paths mentioned by this skill against the base directory before using them. Load referenced resources with skill(name, file=\"...\").\n")
}

func escapeAttr(value string) string {
	return strings.NewReplacer("&", "&amp;", "\"", "&quot;", "<", "&lt;").Replace(value)
}
