package tooloutput

import (
	"strings"
	"unicode/utf8"
)

// DefaultMaxLines and DefaultMaxBytes match pi coding-agent bash/read truncation.
const (
	DefaultMaxLines = 2000
	DefaultMaxBytes = 50 * 1024
)

// TailTruncation describes pi-style tail truncation of combined shell output.
type TailTruncation struct {
	Content         string
	Truncated       bool
	TruncatedBy     string // "", "lines", or "bytes"
	TotalLines      int
	TotalBytes      int
	OutputLines     int
	OutputBytes     int
	LastLinePartial bool
	MaxLines        int
	MaxBytes        int
}

func splitLinesForCounting(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if strings.HasSuffix(content, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// TruncateTail keeps the last maxLines/maxBytes of content (pi truncateTail).
func TruncateTail(content string, maxLines, maxBytes int) TailTruncation {
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	totalBytes := len(content)
	lines := splitLinesForCounting(content)
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return TailTruncation{
			Content:     content,
			TotalLines:  totalLines,
			TotalBytes:  totalBytes,
			OutputLines: totalLines,
			OutputBytes: totalBytes,
			MaxLines:    maxLines,
			MaxBytes:    maxBytes,
		}
	}

	output := make([]string, 0, maxLines)
	outputBytes := 0
	truncatedBy := "lines"
	lastLinePartial := false

	for i := len(lines) - 1; i >= 0 && len(output) < maxLines; i-- {
		line := lines[i]
		lineBytes := len(line)
		if len(output) > 0 {
			lineBytes++ // newline between lines
		}
		if outputBytes+lineBytes > maxBytes {
			truncatedBy = "bytes"
			if len(output) == 0 {
				truncated := truncateStringFromEnd(line, maxBytes)
				output = append(output, truncated)
				outputBytes = len(truncated)
				lastLinePartial = true
			}
			break
		}
		output = append([]string{line}, output...)
		outputBytes += lineBytes
	}

	if len(output) >= maxLines && outputBytes <= maxBytes {
		truncatedBy = "lines"
	}

	outContent := strings.Join(output, "\n")
	return TailTruncation{
		Content:         outContent,
		Truncated:       true,
		TruncatedBy:     truncatedBy,
		TotalLines:      totalLines,
		TotalBytes:      totalBytes,
		OutputLines:     len(output),
		OutputBytes:     len(outContent),
		LastLinePartial: lastLinePartial,
		MaxLines:        maxLines,
		MaxBytes:        maxBytes,
	}
}

func truncateStringFromEnd(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	start := len(s) - maxBytes
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}
