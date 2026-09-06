package skill

// ParseDescription extracts the catalog description from SKILL.md content.
func ParseDescription(body string) string {
	parsed, err := ParseFile(body, ParseOptions{})
	if err != nil {
		return ""
	}
	return parsed.Description
}
