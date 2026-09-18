package feishu

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/lengzhao/agentkit"
)

func buildOutboundContent(ctx context.Context, content string) (msgType string, body string) {
	if agentkit.ProactiveSendRawFromContext(ctx) {
		return buildPlainTextContent(content)
	}
	return buildReplyContent(content)
}

func buildPlainTextContent(content string) (msgType string, body string) {
	b, _ := json.Marshal(map[string]string{"text": content})
	return larkim.MsgTypeText, string(b)
}

func buildReplyContent(content string) (msgType string, body string) {
	if !containsMarkdown(content) {
		return buildPlainTextContent(content)
	}
	// Prefer card for all markdown content — card schema 2.0 has the best
	// markdown rendering (headings, blockquotes, code blocks, tables, links,
	// strikethrough, etc.). Only fall back to post md tag when the content
	// exceeds the card table limit (Feishu API error 11310: max 5 tables).
	if countMarkdownTables(content) > maxCardTables {
		return larkim.MsgTypePost, buildPostMdJSON(content)
	}
	return larkim.MsgTypeInteractive, buildCardJSON(sanitizeMarkdownURLs(preprocessFeishuMarkdown(content)))
}

// maxCardTables is the Feishu interactive card limit for table components.
// A single card supports at most 5 tables; exceeding this causes API error 11310.
const maxCardTables = 5

// countMarkdownTables counts the number of distinct markdown tables in s.
// A table is a group of consecutive lines where each line starts and ends with '|'.
func countMarkdownTables(s string) int {
	count := 0
	inTable := false
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		isTableLine := len(trimmed) > 1 && trimmed[0] == '|' && trimmed[len(trimmed)-1] == '|'
		if isTableLine && !inTable {
			count++
			inTable = true
		} else if !isTableLine {
			inTable = false
		}
	}
	return count
}

// buildPostMdJSON builds a Feishu post message using the md tag,
// which renders markdown at normal chat font size.
func buildPostMdJSON(content string) string {
	content = sanitizeMarkdownURLs(content)
	post := map[string]any{
		"zh_cn": map[string]any{
			"content": [][]map[string]any{
				{
					{"tag": "md", "text": content},
				},
			},
		},
	}
	b, _ := json.Marshal(post)
	return string(b)
}

// preprocessFeishuMarkdown ensures code fences have a newline before them,
// which prevents rendering issues in Feishu card markdown.
// Tables, headings, blockquotes, etc. are rendered natively by the card markdown element.
func preprocessFeishuMarkdown(md string) string {
	// Ensure ``` has a newline before it (unless at start of text)
	var b strings.Builder
	b.Grow(len(md) + 32)
	for i := 0; i < len(md); i++ {
		if i > 0 && md[i] == '`' && i+2 < len(md) && md[i+1] == '`' && md[i+2] == '`' && md[i-1] != '\n' {
			b.WriteByte('\n')
		}
		b.WriteByte(md[i])
	}
	return b.String()
}

var markdownIndicators = []string{
	"```", "**", "~~", "`", "\n- ", "\n* ", "\n1. ", "\n# ", "---",
}

func containsMarkdown(s string) bool {
	for _, ind := range markdownIndicators {
		if strings.Contains(s, ind) {
			return true
		}
	}
	return false
}

// isValidFeishuHref checks whether a URL is acceptable as a Feishu post href.
// Feishu rejects non-HTTP(S) URLs with "invalid href" (code 230001).
func isValidFeishuHref(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

var mdLinkRe = regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)

// sanitizeMarkdownURLs rewrites markdown links with non-HTTP(S) schemes
// to plain text, preventing Feishu API rejection (code 230001).
func sanitizeMarkdownURLs(md string) string {
	return mdLinkRe.ReplaceAllStringFunc(md, func(match string) string {
		parts := mdLinkRe.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		if isValidFeishuHref(parts[2]) {
			return match
		}
		// Convert invalid-scheme link to "text (url)" plain text
		return parts[1] + " (" + parts[2] + ")"
	})
}
