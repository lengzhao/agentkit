package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const defaultToolIcon = "setting-inter_outlined"

var toolIconMap = map[string]string{
	"Bash":      "terminal-two_outlined",
	"Edit":      "edit_outlined",
	"Read":      "file-open_outlined",
	"Write":     "notes_outlined",
	"Glob":      "folder-open_outlined",
	"Grep":      "search_outlined",
	"WebFetch":  "internet_outlined",
	"WebSearch": "internet_outlined",
	"Agent":     "robot_outlined",
	"Skill":     "code_outlined",
	"LSP":       "code_outlined",
}

var markdownTablePattern = regexp.MustCompile(`(?m)^\|.+\|\s*\n\|[\s:|-]+\|\s*\n(?:\|.+\|\s*\n?)+`)

func getToolIcon(toolName string) string {
	if icon, ok := toolIconMap[toolName]; ok {
		return icon
	}
	return defaultToolIcon
}

func richStepDisplayName(step toolStep) string {
	if step.Kind == toolStepKindThinking {
		return "Thinking"
	}
	if step.Kind == toolStepKindSubagent {
		name := strings.TrimSpace(step.Name)
		if name == "" {
			return "subagent"
		}
		return name
	}
	if step.Kind == toolStepKindToolResult {
		name := strings.TrimSpace(step.Name)
		if name == "" {
			return "Result"
		}
		return name
	}
	name := strings.TrimSpace(step.Name)
	if name == "" {
		return "Tool"
	}
	return name
}

func richStepBody(step toolStep) string {
	name := richStepDisplayName(step)
	switch step.Kind {
	case toolStepKindThinking:
		summary := strings.TrimSpace(step.Summary)
		if summary == "" {
			return name
		}
		return summary
	case toolStepKindTool:
		summary := strings.TrimSpace(step.Summary)
		if summary == "" {
			summary = name
		}
		lines := []string{"🔧 工具调用 · " + name}
		if body := formatProgressToolInput(name, summary); body != "" {
			lines = append(lines, body)
		} else {
			lines = append(lines, summary)
		}
		if !step.Done {
			lines = append(lines, "status: running")
		}
		return strings.Join(lines, "\n")
	case toolStepKindSubagent:
		summary := strings.TrimSpace(step.Summary)
		if summary == "" {
			summary = name
		}
		if step.Done {
			lines := []string{"🤖 子 Agent · " + name}
			if step.Success != nil {
				if *step.Success {
					lines = append(lines, "🟢")
				} else {
					lines = append(lines, "🔴")
				}
			}
			result := strings.TrimSpace(step.Result)
			if result == "" {
				result = summary
			}
			if body := formatProgressToolResult(result); body != "" {
				lines = append(lines, body)
			} else if result != "" {
				lines = append(lines, result)
			}
			return strings.Join(lines, "\n")
		}
		lines := []string{"🤖 子 Agent · " + name, summary}
		if !step.Done {
			lines = append(lines, "status: running")
		}
		return strings.Join(lines, "\n")
	case toolStepKindToolResult:
		lines := []string{"🧾 工具结果 · " + name}
		if step.Success != nil {
			if *step.Success {
				lines = append(lines, "🟢")
			} else {
				lines = append(lines, "🔴")
			}
		}
		result := strings.TrimSpace(step.Result)
		if result == "" {
			result = strings.TrimSpace(step.Summary)
		}
		if body := formatProgressToolResult(result); body != "" {
			lines = append(lines, body)
		} else {
			lines = append(lines, progressNoOutputText("zh"))
		}
		return strings.Join(lines, "\n")
	default:
		summary := strings.TrimSpace(step.Summary)
		if summary == "" {
			summary = name
		}
		return summary
	}
}

// isCardJSON returns true if content looks like a complete Feishu card JSON
// (has "schema" and "body"). Used to avoid double-wrapping rich card output.
func isCardJSON(content string) bool {
	if len(content) < 10 || content[0] != '{' {
		return false
	}
	return strings.Contains(content, `"schema"`) && strings.Contains(content, `"body"`)
}

// buildCardJSONWithStatus builds a Feishu card JSON with a colored header
// reflecting the given status. Used as a fallback when rich-card assembly fails.
func buildCardJSONWithStatus(content string, status cardStatus) string {
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"wide_screen_mode": true,
		},
		"body": map[string]any{
			"elements": []map[string]any{
				{
					"tag":     "markdown",
					"content": content,
				},
			},
		},
	}
	if header := richCardTerminalHeader(status); header != nil {
		card["header"] = header
	}
	b, _ := json.Marshal(card)
	return string(b)
}

func richCardTerminalHeader(status cardStatus) map[string]any {
	switch status {
	case cardStatusDone:
		return map[string]any{
			"template": "green",
			"title":    map[string]any{"tag": "plain_text", "content": "☑️"},
		}
	case cardStatusCancelled:
		return map[string]any{
			"template": "orange",
			"title":    map[string]any{"tag": "plain_text", "content": "已取消"},
		}
	case cardStatusError:
		return map[string]any{
			"template": "red",
			"title":    map[string]any{"tag": "plain_text", "content": "出错"},
		}
	default:
		return nil
	}
}

// formatElapsedCN renders a human-readable duration in Chinese.
// Examples: "3.2 秒", "1 分 23 秒", "1 小时 05 分"。
func formatElapsedCN(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSec := int64(d / time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%.1f 秒", d.Seconds())
	case d < time.Hour:
		m := totalSec / 60
		s := totalSec % 60
		return fmt.Sprintf("%d 分 %02d 秒", m, s)
	default:
		h := totalSec / 3600
		m := (totalSec % 3600) / 60
		return fmt.Sprintf("%d 小时 %02d 分", h, m)
	}
}

// richCardMainTextElementID is the markdown element_id for CardKit streaming text updates.
const richCardMainTextElementID = "main_text"

// richCardStatusLine is the single user-facing status string (time + done emoji).
func richCardStatusLine(status cardStatus, elapsed time.Duration) string {
	switch status {
	case cardStatusDone:
		if elapsed <= 0 {
			return "☑️ 已完成"
		}
		return "☑️ 用时 " + formatElapsedCN(elapsed)
	case cardStatusCancelled:
		if elapsed <= 0 {
			return "已取消"
		}
		return "已取消 · 用时 " + formatElapsedCN(elapsed)
	case cardStatusError:
		if elapsed <= 0 {
			return "出错"
		}
		return "出错 · 用时 " + formatElapsedCN(elapsed)
	default:
		return ""
	}
}

const richCardMinShowElapsed = 500 * time.Millisecond

func richCardShowElapsed(elapsed time.Duration) bool {
	return elapsed >= richCardMinShowElapsed
}

func richCardBodyMarkdown(status cardStatus, markdown string, elapsed time.Duration) string {
	md := strings.TrimSpace(markdown)
	line := richCardStatusLine(status, elapsed)
	if line == "" || !isTerminalCardStatus(status) {
		return markdown
	}
	if md == "" {
		return line
	}
	if strings.HasPrefix(md, line) || strings.HasPrefix(md, "☑️") {
		return md
	}
	return line + "\n\n" + md
}

func isTerminalCardStatus(status cardStatus) bool {
	switch status {
	case cardStatusDone, cardStatusCancelled, cardStatusError:
		return true
	default:
		return false
	}
}

func richCardToolCount(steps []toolStep) int {
	n := 0
	for _, step := range steps {
		if step.Kind == toolStepKindTool {
			n++
		}
	}
	return n
}

func richCardHasThinking(steps []toolStep) bool {
	for _, s := range steps {
		if s.Kind == toolStepKindThinking {
			return true
		}
	}
	return false
}

func richCardPanelTitle(steps []toolStep, elapsed time.Duration, streaming bool) string {
	toolCount := richCardToolCount(steps)
	hasThinking := richCardHasThinking(steps)
	if streaming {
		if toolCount > 0 {
			if richCardShowElapsed(elapsed) {
				return fmt.Sprintf("处理中 · %d 个工具 · ⏱ %s...", toolCount, formatElapsedCN(elapsed))
			}
			return fmt.Sprintf("处理中 · %d 个工具", toolCount)
		}
		if hasThinking {
			if richCardShowElapsed(elapsed) {
				return "思考中 · ⏱ " + formatElapsedCN(elapsed) + "..."
			}
			return "思考中"
		}
		return ""
	}
	if toolCount > 0 {
		return fmt.Sprintf("已调用 %d 个工具", toolCount)
	}
	if hasThinking {
		return "思考记录"
	}
	return ""
}

func richCardStepIcon(step toolStep) string {
	if step.Kind == toolStepKindThinking {
		return "idea_outlined"
	}
	return getToolIcon(step.Name)
}

// buildRichCard: 正文 + 可折叠 tool/thinking 区；结束时正文首行 ☑️ 用时。
func buildRichCard(status cardStatus, _ string, steps []toolStep, markdown string, streaming bool, elapsed time.Duration) string {
	panelTitle := richCardPanelTitle(steps, elapsed, streaming)

	panelElements := make([]map[string]any, 0, len(steps))
	if len(steps) > 0 {
		visible := steps
		overflow := 0
		if len(steps) > maxRecentProgressSteps {
			visible = steps[len(steps)-maxRecentProgressSteps:]
			overflow = len(steps) - maxRecentProgressSteps
		}
		for _, step := range visible {
			panelElements = append(panelElements, map[string]any{
				"tag":  "div",
				"icon": map[string]any{"tag": "standard_icon", "token": richCardStepIcon(step)},
				"text": map[string]any{"tag": "plain_text", "content": richStepBody(step)},
			})
		}
		if overflow > 0 {
			panelElements = append(panelElements, map[string]any{
				"tag":  "div",
				"text": map[string]any{"tag": "plain_text", "content": fmt.Sprintf("… 另有 %d 步", overflow)},
			})
		}
	}

	var panelMap map[string]any
	if len(panelElements) > 0 {
		if panelTitle == "" {
			panelTitle = "处理记录"
		}
		panelMap = map[string]any{
			"tag":              "collapsible_panel",
			"expanded":         false,
			"background_color": "grey",
			"header": map[string]any{
				"title": map[string]any{"tag": "plain_text", "content": panelTitle},
			},
			"border":           map[string]any{"color": "grey"},
			"vertical_spacing": "8px",
			"padding":          "4px 8px",
			"elements":         panelElements,
		}
	}

	bodyMD := richCardBodyMarkdown(status, markdown, elapsed)
	markdownMap := map[string]any{
		"tag":        "markdown",
		"element_id": richCardMainTextElementID,
		"content":    preprocessFeishuMarkdown(bodyMD),
	}

	var elements []map[string]any
	if panelMap != nil {
		elements = append(elements, panelMap, markdownMap)
	} else {
		elements = append(elements, markdownMap)
	}

	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"streaming_mode":             streaming,
			"update_multi":               true,
			"enable_forward_interaction": true,
		},
		"body": map[string]any{"elements": elements},
	}

	b, err := json.Marshal(card)
	if err != nil {
		slog.Debug("feishu: build rich card marshal failed, fallback to basic card", "error", err)
		return buildCardJSONWithStatus(preprocessFeishuMarkdown(markdown), status)
	}
	// Feishu interactive card payload limit is ~30KB; over that the API
	// rejects the whole card and the lark client may render it as a
	// mangled JSON dump. Drop the panel and keep just the markdown body.
	const maxCardJSONBytes = 28000
	if len(b) > maxCardJSONBytes {
		slog.Debug("feishu: rich card exceeds size limit, fallback to basic card", "size", len(b))
		return buildCardJSONWithStatus(preprocessFeishuMarkdown(markdown), status)
	}
	return string(b)
}

func splitMarkdownByTables(md string, maxTables int) []string {
	if maxTables <= 0 {
		return []string{md}
	}
	matches := markdownTablePattern.FindAllStringIndex(md, -1)
	if len(matches) <= maxTables {
		return []string{md}
	}
	parts := make([]string, 0, len(matches)-maxTables+1)
	firstEnd := len(md)
	if len(matches) > maxTables {
		firstEnd = matches[maxTables][0]
	}
	first := strings.TrimSpace(md[:firstEnd])
	if first != "" {
		parts = append(parts, first)
	}
	for _, match := range matches[maxTables:] {
		block := strings.TrimSpace(md[match[0]:match[1]])
		if block != "" {
			parts = append(parts, block)
		}
	}
	return parts
}

// BuildRichCard implements core.RichCardSupporter. Feishu engine passes an
// elapsed duration via the preview handle; buildRichCard itself is the
// renderer and must be called with the duration from engine state.
func (p *Platform) BuildRichCard(status cardStatus, title string, steps []toolStep, markdown string, streaming bool, elapsed time.Duration) string {
	return buildRichCard(status, title, steps, markdown, streaming, elapsed)
}

// SplitMarkdownByTables implements core.MarkdownTableSplitter.
func (p *Platform) SplitMarkdownByTables(md string, maxTables int) []string {
	return splitMarkdownByTables(md, maxTables)
}

// SetPreviewStatus updates the card header color to reflect the agent's current state.
func (p *Platform) SetPreviewStatus(previewHandle any, status cardStatus) {
	h, ok := previewHandle.(*feishuPreviewHandle)
	if !ok {
		return
	}

	h.mu.Lock()
	h.status = status
	lastContent := h.lastContent
	h.mu.Unlock()

	if lastContent == "" {
		return
	}
	cardJSON := buildCardJSONWithStatus(lastContent, status)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := p.client.Im.Message.Patch(ctx, larkim.NewPatchMessageReqBuilder().
		MessageId(h.messageID).
		Body(larkim.NewPatchMessageReqBodyBuilder().
			Content(cardJSON).
			Build()).
		Build())
	if err != nil {
		slog.Debug("feishu: set preview status patch failed", "error", err)
		return
	}
	if !resp.Success() {
		slog.Debug("feishu: set preview status patch failed", "code", resp.Code, "msg", resp.Msg)
	}
}
