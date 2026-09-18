package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

// feishuPreviewHandle stores the message ID for an editable preview message.
// Card 2.0 path needs mu/status/lastContent to let SetPreviewStatus patch
// the header color without re-rendering the whole card.
type feishuPreviewHandle struct {
	mu          sync.Mutex
	messageID   string
	chatID      string
	cardID      string
	elementID   string
	sequence    int
	streaming   bool
	status      cardStatus
	lastContent string
}

// buildCardJSON builds a Feishu interactive card JSON string with a markdown element.
// Uses schema 2.0 which supports code blocks, tables, and inline formatting.
// Card font is inherently smaller than Post/Text — this is a Feishu platform limitation.
func buildCardJSON(content string) string {
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
	b, _ := json.Marshal(card)
	return string(b)
}

func isZhLikeProgressLang(lang string) bool {
	l := strings.ToLower(strings.TrimSpace(lang))
	return strings.HasPrefix(l, "zh")
}

func progressAgentLabel(agent string) string {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return "Agent"
	}
	return agent
}

func progressStateMeta(state common.ProgressCardState, lang string, agent string) (title string, template string, footer string) {
	zh := isZhLikeProgressLang(lang)
	switch state {
	case common.ProgressCardStateCompleted:
		if zh {
			return fmt.Sprintf("%s · 已完成", agent), "green", "本过程卡片已停止更新，完整答复见下一条消息。"
		}
		return fmt.Sprintf("%s · Completed", agent), "green", "This progress card is no longer updating. Full response is in the next message."
	case common.ProgressCardStateFailed:
		if zh {
			return fmt.Sprintf("%s · 失败", agent), "red", "本过程卡片已停止更新（失败），完整错误说明见下一条消息。"
		}
		return fmt.Sprintf("%s · Failed", agent), "red", "This progress card has stopped (failed). See the next message for details."
	default:
		if zh {
			return fmt.Sprintf("%s · 进行中", agent), "blue", ""
		}
		return fmt.Sprintf("%s · Running", agent), "blue", ""
	}
}

func progressKindLabel(kind common.ProgressCardEntryKind, lang string) string {
	zh := isZhLikeProgressLang(lang)
	switch kind {
	case common.ProgressEntryThinking:
		if zh {
			return "思考"
		}
		return "Thinking"
	case common.ProgressEntryToolUse:
		if zh {
			return "工具调用"
		}
		return "Tool"
	case common.ProgressEntryToolResult:
		if zh {
			return "工具结果"
		}
		return "Result"
	case common.ProgressEntryError:
		if zh {
			return "错误"
		}
		return "Error"
	default:
		if zh {
			return "更新"
		}
		return "Update"
	}
}

func normalizeProgressItems(payload *common.ProgressCardPayload) []common.ProgressCardEntry {
	if payload == nil {
		return nil
	}
	if len(payload.Items) > 0 {
		return payload.Items
	}
	out := make([]common.ProgressCardEntry, 0, len(payload.Entries))
	for _, entry := range payload.Entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		kind := common.ProgressEntryInfo
		switch {
		case strings.HasPrefix(entry, "💭"):
			kind = common.ProgressEntryThinking
		case strings.HasPrefix(entry, "🔧"), strings.Contains(entry, "**Tool #"):
			kind = common.ProgressEntryToolUse
		case strings.HasPrefix(entry, "🧾"):
			kind = common.ProgressEntryToolResult
		case strings.HasPrefix(entry, "❌"):
			kind = common.ProgressEntryError
		}
		out = append(out, common.ProgressCardEntry{Kind: kind, Text: entry})
	}
	return out
}

func inlineCodeText(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "`", "'")
}

func isBashToolName(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "bash", "shell", "run_shell_command":
		return true
	default:
		return false
	}
}

func isTodoWriteToolName(toolName string) bool {
	return strings.EqualFold(strings.TrimSpace(toolName), "todowrite")
}

// todoItem represents a single todo item from TodoWrite tool input.
type todoItem struct {
	ActiveForm string `json:"activeForm"`
	Content    string `json:"content"`
	Status     string `json:"status"`
}

// todoWriteInput represents the TodoWrite tool input structure.
type todoWriteInput struct {
	Todos []todoItem `json:"todos"`
}

// formatTodoWriteInput formats TodoWrite JSON input into a readable markdown list.
// Returns empty string if parsing fails or input is invalid.
func formatTodoWriteInput(text string, _ string) string {
	var input todoWriteInput
	if err := json.Unmarshal([]byte(text), &input); err != nil {
		return "" // Fall back to default formatting
	}
	if len(input.Todos) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, todo := range input.Todos {
		var icon string
		switch strings.ToLower(strings.TrimSpace(todo.Status)) {
		case "completed":
			icon = "✅"
		case "in_progress":
			icon = "🔄"
		case "pending":
			icon = "⏳"
		default:
			icon = "•"
		}

		content := strings.TrimSpace(todo.Content)
		if content == "" {
			continue
		}

		// Escape markdown special characters
		content = strings.ReplaceAll(content, "`", "'")

		sb.WriteString(icon)
		sb.WriteString(" ")
		sb.WriteString(content)

		activeForm := strings.TrimSpace(todo.ActiveForm)
		if activeForm != "" && activeForm != content {
			sb.WriteString(" _(")
			sb.WriteString(strings.ReplaceAll(activeForm, "`", "'"))
			sb.WriteString(")_")
		}
		sb.WriteString("\n")
	}

	return strings.TrimSuffix(sb.String(), "\n")
}

func formatProgressToolInput(toolName, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	// Special handling for TodoWrite tool - format JSON as readable list
	if isTodoWriteToolName(toolName) {
		if formatted := formatTodoWriteInput(text, ""); formatted != "" {
			return formatted
		}
		// JSON parsing failed or empty todos - show raw input as text block
		return fmt.Sprintf("```text\n%s\n```", text)
	}

	text = preprocessFeishuMarkdown(sanitizeMarkdownURLs(text))
	if strings.Contains(text, "```") {
		return text
	}
	if isBashToolName(toolName) {
		return fmt.Sprintf("```bash\n%s\n```", text)
	}
	if strings.Contains(text, "\n") || len(text) > 180 {
		return fmt.Sprintf("```text\n%s\n```", text)
	}
	return fmt.Sprintf("`%s`", inlineCodeText(text))
}

func formatProgressToolResult(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = preprocessFeishuMarkdown(sanitizeMarkdownURLs(text))
	if strings.Contains(text, "```") {
		return text
	}
	if strings.Contains(text, "\n") || len(text) > 220 {
		return fmt.Sprintf("```\n%s\n```", text)
	}
	return text
}

func progressNoOutputText(lang string) string {
	if isZhLikeProgressLang(lang) {
		return "无输出"
	}
	return "No output"
}

func progressResultDot(item common.ProgressCardEntry) string {
	if item.Success != nil {
		if *item.Success {
			return "🟢"
		}
		return "🔴"
	}
	if item.ExitCode != nil {
		if *item.ExitCode == 0 {
			return "🟢"
		}
		return "🔴"
	}
	if strings.EqualFold(strings.TrimSpace(item.Status), "completed") || strings.EqualFold(strings.TrimSpace(item.Status), "success") || strings.EqualFold(strings.TrimSpace(item.Status), "succeeded") || strings.EqualFold(strings.TrimSpace(item.Status), "ok") {
		return "🟢"
	}
	if strings.EqualFold(strings.TrimSpace(item.Status), "failed") || strings.EqualFold(strings.TrimSpace(item.Status), "error") {
		return "🔴"
	}
	return "⚪"
}

func renderProgressEntryElement(item common.ProgressCardEntry, lang string) map[string]any {
	text := strings.TrimSpace(item.Text)
	if text == "" {
		text = " "
	}
	switch item.Kind {
	case common.ProgressEntryThinking:
		return map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":        "plain_text",
				"content":    "💭 " + inlineCodeText(text),
				"text_size":  "notation",
				"text_color": "grey",
			},
		}
	case common.ProgressEntryToolUse:
		toolName := strings.TrimSpace(item.Tool)
		if toolName == "" {
			toolName = "Tool"
		}
		content := fmt.Sprintf("<text_tag color='blue'>%s</text_tag> `%s`", progressKindLabel(item.Kind, lang), inlineCodeText(toolName))
		if body := formatProgressToolInput(toolName, text); body != "" {
			content += "\n" + body
		}
		return map[string]any{
			"tag":     "markdown",
			"content": content,
		}
	case common.ProgressEntryToolResult:
		toolName := strings.TrimSpace(item.Tool)
		content := fmt.Sprintf("<text_tag color='turquoise'>%s</text_tag>", progressKindLabel(item.Kind, lang))
		if toolName != "" {
			content += " `" + inlineCodeText(toolName) + "`"
		}
		dot := progressResultDot(item)
		meta := dot
		if item.ExitCode != nil {
			meta += fmt.Sprintf(" exit code: `%d`", *item.ExitCode)
		}
		content += "\n" + meta
		if body := formatProgressToolResult(item.Text); body != "" {
			content += "\n" + body
		} else {
			content += "\n_" + progressNoOutputText(lang) + "_"
		}
		return map[string]any{
			"tag":     "markdown",
			"content": content,
		}
	case common.ProgressEntryError:
		content := fmt.Sprintf("<text_tag color='red'>%s</text_tag>\n%s", progressKindLabel(item.Kind, lang), preprocessFeishuMarkdown(sanitizeMarkdownURLs(text)))
		return map[string]any{
			"tag":     "markdown",
			"content": content,
		}
	default:
		return map[string]any{
			"tag":     "markdown",
			"content": preprocessFeishuMarkdown(sanitizeMarkdownURLs(text)),
		}
	}
}

func buildProgressCardJSONFromPayload(payload *common.ProgressCardPayload) string {
	items := normalizeProgressItems(payload)
	if len(items) == 0 {
		return buildCardJSON(" ")
	}

	agent := progressAgentLabel(payload.Agent)
	title, template, footer := progressStateMeta(payload.State, payload.Lang, agent)

	elements := make([]map[string]any, 0, len(items)+3)
	if payload.Truncated {
		truncatedText := "Showing latest updates only."
		if isZhLikeProgressLang(payload.Lang) {
			truncatedText = "仅显示最近更新。"
		}
		elements = append(elements, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":        "plain_text",
				"content":    truncatedText,
				"text_size":  "notation",
				"text_color": "grey",
			},
		})
		elements = append(elements, map[string]any{"tag": "hr"})
	}

	for i, item := range items {
		elements = append(elements, renderProgressEntryElement(item, payload.Lang))
		if i < len(items)-1 {
			elements = append(elements, map[string]any{"tag": "hr"})
		}
	}
	if footer != "" {
		elements = append(elements, map[string]any{"tag": "hr"})
		elements = append(elements, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":        "plain_text",
				"content":    footer,
				"text_size":  "notation",
				"text_color": "grey",
			},
		})
	}

	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"wide_screen_mode": true,
		},
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": title,
			},
			"template": template,
		},
		"body": map[string]any{
			"elements": elements,
		},
	}
	b, _ := json.Marshal(card)
	return string(b)
}

func buildPreviewCardJSON(content string) string {
	if payload, ok := common.ParseProgressCardPayload(content); ok {
		return buildProgressCardJSONFromPayload(payload)
	}
	return buildCardJSON(sanitizeMarkdownURLs(content))
}

// buildFinalPreviewCardJSON builds card JSON for the final update of a streaming preview.
// Unlike buildReplyContent, it must not wrap plain text in IM API {"text":...} JSON.
func buildFinalPreviewCardJSON(text string) string {
	if payload, ok := common.ParseProgressCardPayload(text); ok {
		return buildProgressCardJSONFromPayload(payload)
	}
	processed := text
	if containsMarkdown(text) {
		processed = preprocessFeishuMarkdown(text)
	}
	return buildCardJSON(sanitizeMarkdownURLs(processed))
}

// SendPreviewStart sends a new card message and returns a handle for subsequent edits.
// Using card (interactive) type for both preview and final message so updates
// are in-place without needing to delete and resend.
func (p *Platform) SendPreviewStart(ctx context.Context, rctx any, content string) (any, error) {
	if !p.useInteractiveCard {
		return nil, errNotSupported
	}

	rc, ok := rctx.(replyContext)
	if !ok {
		return nil, fmt.Errorf("%s: invalid reply context type %T", p.tag(), rctx)
	}

	chatID := rc.chatID
	if chatID == "" {
		return nil, fmt.Errorf("%s: chatID is empty", p.tag())
	}

	var cardJSON string
	streamElementID := ""
	sendContent := ""
	cardEntityID := ""
	if isCardJSON(content) {
		cardJSON = content
		if id, err := p.createCardEntity(ctx, cardJSON); err == nil {
			cardEntityID = id
			sendContent = buildIMCardEntityContent(id)
		} else {
			slog.Debug(p.tag()+": create card entity failed, falling back to inline card JSON", "error", err)
			sendContent = cardJSON
		}
	} else if p.useCardKitElementStream() {
		cardJSON = buildStreamingBodyCardEntityJSON()
		streamElementID = bodyStreamElementID
	} else {
		cardJSON = buildPreviewCardJSON(content)
		sendContent = cardJSON
	}

	if p.useCardKitElementStream() && !isCardJSON(content) {
		handle, err := p.createAndSendCardEntity(ctx, rc, cardJSON, streamElementID)
		if err != nil {
			slog.Debug(p.tag()+": cardkit preview start failed, falling back to patch", "error", err)
		} else {
			if streamElementID != "" && strings.TrimSpace(content) != "" {
				processed := content
				if containsMarkdown(content) {
					processed = preprocessFeishuMarkdown(content)
				}
				if streamErr := p.streamCardElementContent(ctx, handle, sanitizeMarkdownURLs(processed)); streamErr != nil {
					slog.Debug(p.tag()+": initial cardkit body stream failed", "error", streamErr)
				}
			}
			return handle, nil
		}
	}

	if sendContent == "" {
		sendContent = cardJSON
	}

	var msgID string
	if p.shouldUseThreadOrReplyAPI(rc) {
		req := larkim.NewReplyMessageReqBuilder().
			MessageId(rc.messageID).
			Body(p.buildReplyMessageReqBody(rc, larkim.MsgTypeInteractive, sendContent)).
			Build()
		var resp *larkim.ReplyMessageResp
		if err := p.withTransientRetry(ctx, "send preview", func() error {
			return p.withFreshTenantAccessTokenRetry(ctx, "send preview", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
				var err error
				resp, err = client.Im.Message.Reply(ctx, req, options...)
				if err != nil {
					return fmt.Errorf("%s: send preview (reply): %w", p.tag(), err)
				}
				if !resp.Success() {
					return fmt.Errorf("%s: send preview (reply) code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
				}
				return nil
			})
		}); err != nil {
			return nil, err
		}
		if resp.Data != nil && resp.Data.MessageId != nil {
			msgID = *resp.Data.MessageId
		}
	} else {
		req := larkim.NewCreateMessageReqBuilder().
			ReceiveIdType("chat_id").
			Body(larkim.NewCreateMessageReqBodyBuilder().
				ReceiveId(chatID).
				MsgType(larkim.MsgTypeInteractive).
				Content(sendContent).
				Build()).
			Build()
		var resp *larkim.CreateMessageResp
		if err := p.withTransientRetry(ctx, "send preview", func() error {
			return p.withFreshTenantAccessTokenRetry(ctx, "send preview", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
				var err error
				resp, err = client.Im.Message.Create(ctx, req, options...)
				if err != nil {
					return fmt.Errorf("%s: send preview: %w", p.tag(), err)
				}
				if !resp.Success() {
					return fmt.Errorf("%s: send preview code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
				}
				return nil
			})
		}); err != nil {
			return nil, err
		}
		if resp.Data != nil && resp.Data.MessageId != nil {
			msgID = *resp.Data.MessageId
		}
	}

	if msgID == "" {
		return nil, fmt.Errorf("%s: send preview: no message ID returned", p.tag())
	}

	return &feishuPreviewHandle{messageID: msgID, chatID: chatID, cardID: cardEntityID}, nil
}

// UpdateMessage edits an existing card message identified by previewHandle.
// Uses the Patch API (HTTP PATCH) which is required for interactive card messages.
func (p *Platform) UpdateMessage(ctx context.Context, previewHandle any, content string) error {
	if !p.useInteractiveCard {
		return errNotSupported
	}

	h, ok := previewHandle.(*feishuPreviewHandle)
	if !ok {
		return fmt.Errorf("%s: invalid preview handle type %T", p.tag(), previewHandle)
	}

	if h.cardID != "" {
		if h.elementID != "" && !isCardJSON(content) {
			processed := content
			if containsMarkdown(content) {
				processed = preprocessFeishuMarkdown(content)
			}
			return p.streamCardElementContent(ctx, h, sanitizeMarkdownURLs(processed))
		}
		cardJSON := content
		if !isCardJSON(content) {
			cardJSON = buildFinalPreviewCardJSON(content)
		}
		return p.updateCardEntity(ctx, h, cardJSON)
	}

	cardJSON := ""
	if isCardJSON(content) {
		// Card 2.0: engine passes full card JSON directly, skip all processing.
		cardJSON = content
		h.mu.Lock()
		h.lastContent = content
		h.mu.Unlock()
	} else if payload, ok := common.ParseProgressCardPayload(content); ok {
		cardJSON = buildProgressCardJSONFromPayload(payload)
	} else {
		processed := content
		if containsMarkdown(content) {
			processed = preprocessFeishuMarkdown(content)
		}
		cardJSON = buildCardJSON(sanitizeMarkdownURLs(processed))
	}
	req := larkim.NewPatchMessageReqBuilder().
		MessageId(h.messageID).
		Body(larkim.NewPatchMessageReqBodyBuilder().
			Content(cardJSON).
			Build()).
		Build()
	return p.withTransientRetry(ctx, "patch message", func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "patch message", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
			resp, err := client.Im.Message.Patch(ctx, req, options...)
			if err != nil {
				return fmt.Errorf("%s: patch message: %w", p.tag(), err)
			}
			if !resp.Success() {
				return fmt.Errorf("%s: patch message code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
			}
			return nil
		})
	})
}

// DeletePreviewMessage removes a preview message so the caller can send a
// separate final message without leaving a stale interactive card behind.
func (p *Platform) DeletePreviewMessage(ctx context.Context, previewHandle any) error {
	if !p.useInteractiveCard {
		return errNotSupported
	}

	h, ok := previewHandle.(*feishuPreviewHandle)
	if !ok {
		return fmt.Errorf("%s: invalid preview handle type %T", p.tag(), previewHandle)
	}

	req := larkim.NewDeleteMessageReqBuilder().
		MessageId(h.messageID).
		Build()
	return p.withTransientRetry(ctx, "delete preview message", func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "delete preview message", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
			resp, err := client.Im.Message.Delete(ctx, req, options...)
			if err != nil {
				return fmt.Errorf("%s: delete preview message: %w", p.tag(), err)
			}
			if !resp.Success() {
				return fmt.Errorf("%s: delete preview message code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
			}
			return nil
		})
	})
}

// SendAudio uploads audio bytes to Feishu and sends a voice message.
// Implements core.AudioSender interface.
// Feishu audio messages require opus format; non-opus input is converted via ffmpeg.
func (p *Platform) SendAudio(ctx context.Context, rctx any, audio []byte, format string) error {
	rc, ok := rctx.(replyContext)
	if !ok {
		return fmt.Errorf("%s: Sendaudio: invalid reply context type %T", p.tag(), rctx)
	}

	if format != "opus" {
		converted, err := convertAudioToOpus(ctx, audio, format)
		if err != nil {
			return fmt.Errorf("%s: convert to opus: %w", p.tag(), err)
		}
		audio = converted
		format = "opus"
	}

	var uploadResp *larkim.CreateFileResp
	if err := p.withTransientRetry(ctx, "upload audio", func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "upload audio", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
			req := larkim.NewCreateFileReqBuilder().
				Body(larkim.NewCreateFileReqBodyBuilder().
					FileType(larkim.CreateFileFileTypeOpus).
					FileName("tts_audio.opus").
					File(bytes.NewReader(audio)).
					Build()).
				Build()
			var err error
			uploadResp, err = client.Im.File.Create(ctx, req, options...)
			if err != nil {
				return fmt.Errorf("%s: upload audio: %w", p.tag(), err)
			}
			if !uploadResp.Success() {
				return fmt.Errorf("%s: upload audio code=%d msg=%s", p.tag(), uploadResp.Code, uploadResp.Msg)
			}
			return nil
		})
	}); err != nil {
		return err
	}
	if uploadResp.Data == nil || uploadResp.Data.FileKey == nil {
		return fmt.Errorf("%s: upload audio: no file_key returned", p.tag())
	}
	fileKey := *uploadResp.Data.FileKey

	slog.Debug(p.tag()+": audio uploaded", "file_key", fileKey, "format", format, "size", len(audio))

	audioMsg := larkim.MessageAudio{FileKey: fileKey}
	audioContent, err := audioMsg.String()
	if err != nil {
		return fmt.Errorf("%s: build audio message: %w", p.tag(), err)
	}

	return p.sendMediaMessage(ctx, rc, larkim.MsgTypeAudio, audioContent)
}
