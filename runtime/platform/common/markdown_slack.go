package common

import (
	"regexp"
	"strings"
)

var (
	reSlackCodeBlock  = regexp.MustCompile("(?s)```[a-zA-Z]*\n?(.*?)```")
	reSlackInlineCode = regexp.MustCompile("`([^`]+)`")
	reSlackLink       = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reSlackBoldItalic = regexp.MustCompile(`\*\*\*(.+?)\*\*\*`)
	reSlackBold       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reSlackStrike     = regexp.MustCompile(`~~(.+?)~~`)
	reSlackHeading    = regexp.MustCompile(`^#{1,6}\s+(.+)$`)
	reSlackImgTag     = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
)

// MarkdownToSlackMrkdwn converts standard Markdown to Slack mrkdwn format.
func MarkdownToSlackMrkdwn(md string) string {
	type segment struct {
		text   string
		isCode bool
	}
	var segments []segment
	rest := md
	for {
		loc := reSlackCodeBlock.FindStringIndex(rest)
		if loc == nil {
			segments = append(segments, segment{text: rest})
			break
		}
		if loc[0] > 0 {
			segments = append(segments, segment{text: rest[:loc[0]]})
		}
		segments = append(segments, segment{text: rest[loc[0]:loc[1]], isCode: true})
		rest = rest[loc[1]:]
	}
	var b strings.Builder
	b.Grow(len(md) + len(md)/8)
	for _, seg := range segments {
		if seg.isCode {
			b.WriteString(seg.text)
			continue
		}
		b.WriteString(convertSlackInline(seg.text))
	}
	return b.String()
}

func convertSlackInline(s string) string {
	type placeholder struct {
		key     string
		content string
	}
	var phs []placeholder
	phIdx := 0
	nextPH := func(content string) string {
		key := "\x00SL" + string(rune('0'+phIdx)) + "\x00"
		phs = append(phs, placeholder{key: key, content: content})
		phIdx++
		return key
	}
	s = reSlackInlineCode.ReplaceAllStringFunc(s, func(m string) string { return nextPH(m) })
	s = reSlackImgTag.ReplaceAllStringFunc(s, func(m string) string {
		sm := reSlackImgTag.FindStringSubmatch(m)
		if sm[1] != "" {
			return sm[1]
		}
		return sm[2]
	})
	s = reSlackLink.ReplaceAllStringFunc(s, func(m string) string {
		sm := reSlackLink.FindStringSubmatch(m)
		if len(sm) < 3 {
			return m
		}
		return nextPH("<" + sm[2] + "|" + sm[1] + ">")
	})
	s = reSlackBoldItalic.ReplaceAllString(s, "*_${1}_*")
	s = reSlackBold.ReplaceAllString(s, "*${1}*")
	s = reSlackStrike.ReplaceAllString(s, "~${1}~")
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if m := reSlackHeading.FindStringSubmatch(line); m != nil {
			lines[i] = "*" + m[1] + "*"
		}
	}
	s = strings.Join(lines, "\n")
	for _, ph := range phs {
		s = strings.Replace(s, ph.key, ph.content, 1)
	}
	return s
}
