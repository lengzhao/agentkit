package filesystem

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	capfs "github.com/lengzhao/agentkit/cap/filesystem"
)

const (
	defaultGrepLimit  = 100
	defaultFindLimit  = 1000
	grepMaxLineLength = 500
)

func compileGrep(req capfs.GrepRequest) (*regexp.Regexp, error) {
	if req.Literal {
		return nil, nil
	}
	pattern := req.Pattern
	if req.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}
	return re, nil
}

func matchFilePattern(pattern, relPath string) (bool, error) {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	relPath = filepath.ToSlash(strings.Trim(relPath, "/"))
	if pattern == "" {
		return false, fmt.Errorf("pattern is required")
	}
	if !strings.Contains(pattern, "**") && !strings.Contains(pattern, "/") {
		return filepath.Match(pattern, filepath.Base(relPath))
	}
	if !strings.HasPrefix(pattern, "/") && !strings.HasPrefix(pattern, "**/") && strings.Contains(pattern, "/") {
		pattern = "**/" + pattern
	}
	return matchGlobPattern(pattern, relPath)
}

func matchGlobPattern(pattern, relPath string) (bool, error) {
	pattern = filepath.ToSlash(pattern)
	relPath = filepath.ToSlash(relPath)
	if pattern == "**" {
		return true, nil
	}
	if strings.Contains(pattern, "**") {
		parts := strings.Split(pattern, "**")
		if len(parts) == 2 && parts[0] == "" && parts[1] != "" {
			suffix := strings.TrimPrefix(parts[1], "/")
			if suffix == "" {
				return true, nil
			}
			return matchSuffixSegments(suffix, relPath)
		}
	}
	if matched, err := filepath.Match(pattern, relPath); err != nil {
		return false, err
	} else if matched {
		return true, nil
	}
	return filepath.Match(pattern, filepath.Base(relPath))
}

func matchSuffixSegments(suffix, relPath string) (bool, error) {
	segments := strings.Split(strings.Trim(suffix, "/"), "/")
	pathSegments := strings.Split(relPath, "/")
	if len(segments) > len(pathSegments) {
		return false, nil
	}
	for start := 0; start <= len(pathSegments)-len(segments); start++ {
		ok := true
		for i, seg := range segments {
			matched, err := filepath.Match(seg, pathSegments[start+i])
			if err != nil {
				return false, err
			}
			if !matched {
				ok = false
				break
			}
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func formatFindPaths(paths []string, truncated bool, limit int) (text string, hint string) {
	text = strings.Join(paths, "\n")
	if truncated {
		hint = fmt.Sprintf("%d results limit reached. Use limit=%d for more, or refine pattern", limit, limit*2)
	}
	return text, hint
}

func formatGrepMatches(matches []capfs.GrepMatch, truncated bool, limit int, linesTruncated bool) (text string, hint string) {
	var b strings.Builder
	for _, match := range matches {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(fmt.Sprintf("%s:%d: %s", match.Path, match.Line, match.Content))
	}
	text = b.String()
	var notices []string
	if truncated {
		notices = append(notices, fmt.Sprintf("%d matches limit reached. Use limit=%d for more, or refine pattern", limit, limit*2))
	}
	if linesTruncated {
		notices = append(notices, fmt.Sprintf("Some lines truncated to %d chars. Use read for full lines", grepMaxLineLength))
	}
	if len(notices) > 0 {
		hint = strings.Join(notices, ". ")
	}
	return text, hint
}

type grepCollector struct {
	Matches        []capfs.GrepMatch
	TextLines      []string
	seenText       map[string]struct{}
	Limit          int
	Truncated      bool
	LinesTruncated bool
}

func newGrepCollector(limit int) *grepCollector {
	return &grepCollector{
		seenText: make(map[string]struct{}),
		Limit:    limit,
	}
}

func (c *grepCollector) addMatch(relPath string, lineNo int, lines []string, context int) bool {
	if len(c.Matches) >= c.Limit {
		c.Truncated = true
		return false
	}
	line := lines[lineNo-1]
	line, truncated := truncateGrepLine(strings.TrimRight(line, "\r"))
	if truncated {
		c.LinesTruncated = true
	}
	c.Matches = append(c.Matches, capfs.GrepMatch{
		Path:    relPath,
		Line:    lineNo,
		Content: line,
	})

	start := lineNo
	end := lineNo
	if context > 0 {
		start = max(1, lineNo-context)
		end = min(len(lines), lineNo+context)
	}
	for current := start; current <= end; current++ {
		textLine := lines[current-1]
		textLine, truncated := truncateGrepLine(strings.TrimRight(textLine, "\r"))
		if truncated {
			c.LinesTruncated = true
		}
		var formatted string
		switch {
		case current == lineNo:
			formatted = fmt.Sprintf("%s:%d: %s", relPath, current, textLine)
		default:
			formatted = fmt.Sprintf("%s-%d- %s", relPath, current, textLine)
		}
		if _, ok := c.seenText[formatted]; ok {
			continue
		}
		c.seenText[formatted] = struct{}{}
		c.TextLines = append(c.TextLines, formatted)
	}
	return true
}

func (c *grepCollector) Result() capfs.GrepResult {
	text := strings.Join(c.TextLines, "\n")
	hint := ""
	if c.Truncated || c.LinesTruncated {
		_, hint = formatGrepMatches(c.Matches, c.Truncated, c.Limit, c.LinesTruncated)
	}
	return capfs.GrepResult{
		Matches:   c.Matches,
		Truncated: c.Truncated,
		Text:      text,
		Hint:      hint,
	}
}

func lineMatches(pattern, line string, ignoreCase, literal bool, re *regexp.Regexp) bool {
	if literal {
		if ignoreCase {
			return strings.Contains(strings.ToLower(line), strings.ToLower(pattern))
		}
		return strings.Contains(line, pattern)
	}
	return re.MatchString(line)
}

func grepFileBytes(data []byte, rel, pattern string, ignoreCase, literal bool, re *regexp.Regexp, context int, collector *grepCollector) error {
	if bytes.IndexByte(data, 0) >= 0 {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if !lineMatches(pattern, line, ignoreCase, literal, re) {
			continue
		}
		if !collector.addMatch(rel, i+1, lines, context) {
			return nil
		}
	}
	return nil
}

func truncateGrepLine(line string) (string, bool) {
	if len(line) <= grepMaxLineLength {
		return line, false
	}
	return line[:grepMaxLineLength], true
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sortDirEntries(out []capfs.DirEntry) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
}
