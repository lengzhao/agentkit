package fs

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit/cap/filesystem"
)

const (
	defaultReadMaxLines = 2000
	defaultReadMaxBytes = 50 * 1024
	defaultGrepLimit    = 100
	defaultFindLimit    = 1000
	defaultListLimit    = 500
	grepMaxLineLength   = 500
)

type readSliceOptions struct {
	MaxBytes int
	Offset   int // 1-indexed; 0 means start of file
	Limit    int // max lines; 0 means until byte limit
}

type readSliceResult struct {
	Content    string
	TotalLines int
	Truncated  bool
	Hint       string
}

func sliceReadContent(content string, opts readSliceOptions) (readSliceResult, error) {
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultReadMaxBytes
	}
	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	start := 0
	if opts.Offset > 0 {
		start = opts.Offset - 1
	}
	if start >= len(lines) {
		return readSliceResult{}, fmt.Errorf("offset %d is beyond end of file (%d lines total)", opts.Offset, totalLines)
	}
	selected := lines[start:]
	maxLines := opts.Limit
	if maxLines <= 0 {
		maxLines = defaultReadMaxLines
	}
	if len(selected) > maxLines {
		selected = selected[:maxLines]
	}

	var b strings.Builder
	truncated := false
	outputLines := 0
	for i, line := range selected {
		lineBytes := len(line) + 1
		if i > 0 && b.Len()+lineBytes > maxBytes {
			truncated = true
			break
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
		outputLines++
	}

	out := readSliceResult{
		Content:    b.String(),
		TotalLines: totalLines,
		Truncated:  truncated,
	}
	endLine := start + outputLines
	if truncated {
		next := endLine + 1
		out.Hint = fmt.Sprintf("Showing lines %d-%d of %d (%dKB limit). Use offset=%d to continue.", start+1, endLine, totalLines, maxBytes/1024, next)
		out.Truncated = true
		return out, nil
	}
	if start+maxLines < totalLines {
		next := start + maxLines + 1
		remaining := totalLines - (start + maxLines)
		out.Hint = fmt.Sprintf("%d more lines in file. Use offset=%d to continue.", remaining, next)
		if endLine < start+maxLines {
			out.Truncated = true
		}
	}
	return out, nil
}

func applyEditsOnOriginal(content string, edits []FileEdit) (string, error) {
	type span struct {
		start   int
		end     int
		newText string
	}
	spans := make([]span, 0, len(edits))
	for i, edit := range edits {
		if edit.OldText == "" {
			return "", fmt.Errorf("edits[%d].oldText is required", i)
		}
		idx := strings.Index(content, edit.OldText)
		if idx < 0 {
			return "", fmt.Errorf("edits[%d].oldText not found in file", i)
		}
		if strings.Count(content, edit.OldText) > 1 {
			return "", fmt.Errorf("edits[%d].oldText is not unique in file", i)
		}
		spans = append(spans, span{
			start:   idx,
			end:     idx + len(edit.OldText),
			newText: edit.NewText,
		})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	for i := 1; i < len(spans); i++ {
		if spans[i].start < spans[i-1].end {
			return "", fmt.Errorf("edits overlap; merge nearby changes into one edit")
		}
	}
	out := content
	for i := len(spans) - 1; i >= 0; i-- {
		s := spans[i]
		out = out[:s.start] + s.newText + out[s.end:]
	}
	return out, nil
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

func formatReadText(path string, startLine int, sliced readSliceResult) string {
	var b strings.Builder
	if path != "" {
		b.WriteString(path)
		b.WriteByte('\n')
	}
	lines := strings.Split(sliced.Content, "\n")
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%6d|%s", startLine+i, line)
	}
	return appendHint(strings.TrimRight(b.String(), "\n"), sliced.Hint)
}

func formatWriteResult(path string) string {
	return "Wrote " + path
}

func formatEditResult(path string, applied bool) string {
	if applied {
		return "Edited " + path
	}
	return "No changes applied to " + path
}

func formatGrepResult(result filesystem.GrepResult) string {
	text := strings.TrimSpace(result.Text)
	if text == "" {
		text = "No matches found"
	}
	return appendHint(text, result.Hint)
}

func formatFindResult(result filesystem.FindResult) string {
	text := strings.TrimSpace(result.Text)
	if text == "" {
		text = "No files found"
	}
	return appendHint(text, result.Hint)
}

func formatListResult(text, hint string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "(empty directory)"
	}
	return appendHint(text, hint)
}

func appendHint(text, hint string) string {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return text
	}
	if text == "" {
		return hint
	}
	return text + "\n\n" + hint
}

func formatGrepMatches(matches []filesystem.GrepMatch, truncated bool, limit int, linesTruncated bool) (text string, hint string) {
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

func formatFindPaths(paths []string, truncated bool, limit int) (text string, hint string) {
	text = strings.Join(paths, "\n")
	if truncated {
		hint = fmt.Sprintf("%d results limit reached. Use limit=%d for more, or refine pattern", limit, limit*2)
	}
	return text, hint
}

func formatListEntries(entries []filesystem.DirEntry) (text string) {
	if len(entries) == 0 {
		return "(empty directory)"
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name
		if entry.IsDir {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return strings.Join(names, "\n")
}

func effectiveLimit(requested, configured, fallback int) int {
	limit := requested
	if limit <= 0 {
		limit = fallback
	}
	if configured > 0 && limit > configured {
		limit = configured
	}
	return limit
}

type grepCollector struct {
	Matches        []filesystem.GrepMatch
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
	c.Matches = append(c.Matches, filesystem.GrepMatch{
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

func (c *grepCollector) Result() filesystem.GrepResult {
	text := strings.Join(c.TextLines, "\n")
	hint := ""
	if c.Truncated || c.LinesTruncated {
		_, hint = formatGrepMatches(c.Matches, c.Truncated, c.Limit, c.LinesTruncated)
	}
	return filesystem.GrepResult{
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
