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
)

// feishuPreviewHandle stores the message ID for an editable preview message.
// Card 2.0 path needs mu/status/lastContent to let SetPreviewStatus patch
// the header color without re-rendering the whole card.
type feishuPreviewHandle struct {
	mu sync.Mutex
	// reqMu 串行化同一 card_id 的 CardKit 写请求（取 seq + HTTP 往返整体持锁）。
	// CardKit 要求 sequence 严格递增到达；并发在途请求经代理连接池乱序落地
	// 会被 300317「sequence number compare failed」拒绝。
	reqMu       sync.Mutex
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

func buildPreviewCardJSON(content string) string {
	return buildCardJSON(sanitizeMarkdownURLs(content))
}

// buildFinalPreviewCardJSON builds card JSON for the final update of a streaming preview.
// Unlike buildReplyContent, it must not wrap plain text in IM API {"text":...} JSON.
func buildFinalPreviewCardJSON(text string) string {
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
	} else {
		cardJSON = buildPreviewCardJSON(content)
		sendContent = cardJSON
	}

	if sendContent == "" {
		sendContent = cardJSON
	}

	sendRC := rc
	if !p.shouldUseThreadOrReplyAPI(rc) {
		sendRC.messageID = ""
	}
	result, err := p.writeMessage(ctx, sendRC, larkim.MsgTypeInteractive, sendContent, "send preview")
	if err != nil {
		return nil, err
	}
	msgID := result.messageID

	return &feishuPreviewHandle{messageID: msgID, chatID: chatID, cardID: cardEntityID}, nil
}

// UpdateMessage edits an existing card message identified by previewHandle.
// Uses the Patch API (HTTP PATCH) which is required for interactive card messages.
// 对外固定标准重试预算；需要其他预算的调用方走 updateMessageProfile。
func (p *Platform) UpdateMessage(ctx context.Context, previewHandle any, content string) error {
	return p.updateMessageProfile(ctx, previewHandle, content, retryProfileStandard)
}

// updateMessageProfile 同 UpdateMessage，但允许调用方指定 transient 重试预算
// （中途流式帧用 retryProfileQuick 快速失败，定稿挽救用 retryProfileStandard）。
func (p *Platform) updateMessageProfile(ctx context.Context, previewHandle any, content string, profile transientRetryProfile) error {
	if !p.useInteractiveCard {
		return errNotSupported
	}

	h, ok := previewHandle.(*feishuPreviewHandle)
	if !ok {
		return fmt.Errorf("%s: invalid preview handle type %T", p.tag(), previewHandle)
	}

	if h.cardID != "" {
		cardJSON := content
		if !isCardJSON(content) {
			cardJSON = buildFinalPreviewCardJSON(content)
		}
		return p.updateCardEntity(ctx, h, cardJSON, profile)
	}

	cardJSON := ""
	if isCardJSON(content) {
		// Card 2.0: engine passes full card JSON directly, skip all processing.
		cardJSON = content
		h.mu.Lock()
		h.lastContent = content
		h.mu.Unlock()
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
	return p.withTransientRetryProfile(ctx, "patch message", profile, func() error {
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
