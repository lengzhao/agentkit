package markdown

import "strings"

// Split separates a leading --- delimited YAML block from the body.
// ok is false when the file has no valid frontmatter (missing opener, unterminated block, or non-object YAML head).
func Split(raw string) (yaml string, body string, ok bool) {
	firstLineEnd := strings.Index(raw, "\n")
	if firstLineEnd < 0 {
		return "", raw, false
	}
	firstLine := strings.TrimSuffix(raw[:firstLineEnd], "\r")
	if firstLine != "---" {
		return "", raw, false
	}
	start := firstLineEnd + 1
	closingStart, bodyStart, found := findClosingDelimiter(raw, start)
	if !found {
		return "", raw, false
	}
	return raw[start:closingStart], raw[bodyStart:], true
}

func findClosingDelimiter(raw string, start int) (closingStart, bodyStart int, ok bool) {
	lineStart := start
	for lineStart <= len(raw) {
		nextNewline := strings.Index(raw[lineStart:], "\n")
		var lineEnd int
		if nextNewline < 0 {
			lineEnd = len(raw)
		} else {
			lineEnd = lineStart + nextNewline
		}
		line := strings.TrimSuffix(raw[lineStart:lineEnd], "\r")
		if line == "---" {
			if nextNewline < 0 {
				return lineStart, len(raw), true
			}
			return lineStart, lineEnd + 1, true
		}
		if nextNewline < 0 {
			return 0, 0, false
		}
		lineStart = lineEnd + 1
	}
	return 0, 0, false
}
