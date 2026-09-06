package markdown

import "strings"

const frontmatterDelimiter = "---"

// Split separates a leading --- delimited YAML block from the body.
// Only the first two standalone "---" lines participate: the opener and closer.
// Any additional "---" or "----" lines after the closer remain in the body.
func Split(raw string) (yaml string, body string, ok bool) {
	yamlStart, closeStart, bodyStart, found := findFrontmatterBounds(raw)
	if !found {
		return "", raw, false
	}
	return raw[yamlStart:closeStart], raw[bodyStart:], true
}

func findFrontmatterBounds(raw string) (yamlStart, closeStart, bodyStart int, ok bool) {
	delimiters := 0
	lineStart := 0
	for lineStart <= len(raw) {
		nextNewline := strings.Index(raw[lineStart:], "\n")
		var lineEnd int
		if nextNewline < 0 {
			lineEnd = len(raw)
		} else {
			lineEnd = lineStart + nextNewline
		}
		line := strings.TrimSuffix(raw[lineStart:lineEnd], "\r")
		if line == frontmatterDelimiter {
			delimiters++
			switch delimiters {
			case 1:
				if lineStart != 0 {
					return 0, 0, 0, false
				}
				if nextNewline < 0 {
					return 0, 0, 0, false
				}
				yamlStart = lineEnd + 1
			case 2:
				closeStart = lineStart
				if nextNewline < 0 {
					bodyStart = len(raw)
				} else {
					bodyStart = lineEnd + 1
				}
				return yamlStart, closeStart, bodyStart, true
			}
		}
		if nextNewline < 0 {
			return 0, 0, 0, false
		}
		lineStart = lineEnd + 1
	}
	return 0, 0, 0, false
}
