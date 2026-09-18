package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

type replyContext struct {
	messageID            string
	chatID               string
	chatType             string
	sessionKey           string
	processingReactionID string
}

type Platform struct {
	platformTag                string
	defaultDomain              string
	domain                     string
	appID                      string
	appSecret                  string
	progressStyle              string
	showThinking               bool
	showToolProgress           bool
	asyncSubagentProgressCard  bool
	useInteractiveCard         bool
	reactionEmoji              string
	doneEmoji                  string
	cancelledEmoji             string
	errorEmoji                 string
	allowFrom                  string
	allowChat                  string
	groupOnly                  bool
	groupReplyAll              bool
	respondToAtEveryoneAndHere bool
	shareSessionInChannel      bool
	threadIsolation            bool
	replyInThread              bool
	noReplyToTrigger           bool
	resolveMentions            bool
	cfg                        Config
	agentID                    agentkit.AgentID
	commands                   agentkit.Commands
	sessionScope               agentkit.SessionScope
	workspace                  workspace.Service
	inbox                      *common.Inbox
	outbound                   *common.Outbound
	deliveries                 sync.Map
	turnTriggers               sync.Map
	turnReactions              sync.Map
	streams                    sync.Map
	asyncSubagentByJob         sync.Map // jobID -> *asyncSubagentCard
	asyncSubagentByStream      sync.Map // streamKey -> jobID
	client                     *lark.Client
	replayClient               *lark.Client
	replayClientMu             sync.Mutex
	wsClient                   *larkws.Client
	cancel                     context.CancelFunc
	dedup                      *common.MessageDedup
	botOpenID                  string
	peerBots                   map[string]string
	userProfileCache           sync.Map
	chatNameCache              sync.Map
	chatMemberCache            sync.Map
	recalledMu                 sync.Mutex
	recalledMsgIDs             map[string]time.Time
	server                     *http.Server
	port                       string
	callbackPath               string
	encryptKey                 string
	eventHandler               *dispatcher.EventDispatcher
	cardActionMsgMu            sync.Mutex
	cardActionMsgIDs           map[string]string
	startOnce                  sync.Once
}

type feishuRequestFunc func(client *lark.Client, options ...larkcore.RequestOptionFunc) error

func (p *Platform) tag() string { return p.platformTag }

func (p *Platform) shouldUseWebhookMode() bool {
	return strings.TrimSpace(p.encryptKey) != ""
}

// startWebSocketMode starts the WebSocket long connection mode.
// startWebhookMode starts the HTTP webhook server mode (for Lark international version)
func (p *Platform) startWebhookMode() error {
	mux := http.NewServeMux()
	mux.HandleFunc(p.callbackPath, p.webhookHandler)

	p.server = &http.Server{
		Addr:    ":" + p.port,
		Handler: mux,
	}

	_, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	go func() {
		slog.Info(p.tag()+": webhook server listening", "port", p.port, "path", p.callbackPath)
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error(p.tag()+": webhook server error", "error", err)
		}
	}()

	return nil
}

// webhookHandler handles HTTP webhook requests from Lark international version
func (p *Platform) webhookHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error(p.tag()+": read webhook body failed", "error", err)
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}

	// Build EventReq from HTTP request
	req := &larkevent.EventReq{
		Header:     r.Header,
		Body:       body,
		RequestURI: r.RequestURI,
	}

	// Use the SDK's event dispatcher to handle the request
	resp := p.eventHandler.Handle(r.Context(), req)

	// Write response
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(resp.Body)
}

// onCardAction handles card.action.trigger callbacks via the official SDK event dispatcher.
// Three prefixes are supported:
//   - nav:/xxx   — render a card page and update the original card in-place
//   - act:/xxx   — execute an action, then render and update the card in-place
//   - cmd:/xxx   — legacy: dispatch as a user command (sends a new message)
func (p *Platform) onCardAction(event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
	if event.Event == nil || event.Event.Action == nil {
		return nil, nil
	}

	// Check allow_chat filter: skip card actions from chats this platform doesn't own.
	if event.Event.Context != nil && event.Event.Context.OpenChatID != "" {
		if !common.AllowList(p.allowChat, event.Event.Context.OpenChatID) {
			return nil, nil
		}
	}

	actionVal, _ := event.Event.Action.Value["action"].(string)

	// select_static callbacks put the chosen value in event.Event.Action.Option
	if actionVal == "" && event.Event.Action.Option != "" {
		actionVal = event.Event.Action.Option
	}
	if actionVal == "" {
		switch event.Event.Action.Name {
		case "delete_mode_submit":
			actionVal = "act:/delete-mode form-submit"
		case "delete_mode_cancel":
			actionVal = "act:/delete-mode cancel"
		}
	}
	if actionVal == "act:/delete-mode form-submit" {
		ids := collectDeleteModeSelectedFromFormValue(event.Event.Action.FormValue)
		if len(ids) > 0 {
			actionVal += " " + strings.Join(ids, ",")
		}
	}

	userID := ""
	if event.Event.Operator != nil {
		userID = event.Event.Operator.OpenID
	}
	chatID := ""
	messageID := ""
	if event.Event.Context != nil {
		chatID = event.Event.Context.OpenChatID
		messageID = event.Event.Context.OpenMessageID
	}
	if chatID == "" {
		chatID = userID
	}
	sessionKey := p.sessionKeyFromCardAction(chatID, userID, event.Event.Action.Value)
	extra := map[string]string{}
	for k, v := range event.Event.Action.Value {
		if s, ok := v.(string); ok {
			extra[k] = s
		}
	}
	if sessionKey == "" && chatID != "" && userID != "" {
		sessionKey = p.makeSessionKey(nil, chatID, userID)
	}
	if reply, ok := common.PermissionReplyFromAction(actionVal, userID, extra); ok {
		if !common.AllowList(p.allowFrom, userID) {
			return nil, nil
		}
		p.pushPermissionReply(context.Background(), sessionKey, reply, extra)
		confirmed := common.ConfirmedCardFromReply(reply, extra)
		return &callback.CardActionTriggerResponse{
			Card: &callback.Card{
				Type: "raw",
				Data: renderCardMap(confirmed, sessionKey),
			},
		}, nil
	}

	// nav: / act: — synchronous card update
	if strings.HasPrefix(actionVal, "nav:") || strings.HasPrefix(actionVal, "act:") {
		if messageID != "" {
			p.cardActionMsgMu.Lock()
			if p.cardActionMsgIDs == nil {
				p.cardActionMsgIDs = make(map[string]string)
			}
			p.cardActionMsgIDs[sessionKey] = messageID
			p.cardActionMsgMu.Unlock()
		}
		// Feishu uses native form checker for delete-mode toggle,
		// so return a toast without calling cardNavHandler to avoid a full card refresh.
		if strings.HasPrefix(actionVal, "act:/delete-mode toggle ") {
			return &callback.CardActionTriggerResponse{
				Toast: &callback.Toast{
					Type:    "info",
					Content: "已记录选择（Selection recorded）",
				},
			}, nil
		}
		if strings.HasPrefix(actionVal, "act:") {
			slog.Debug(p.tag()+": card action produced no card update", "action", actionVal)
			return nil, nil
		}
		slog.Warn(p.tag()+": card nav returned nil, ignoring", "action", actionVal)
		return nil, nil
	}

	// cmd: — async command dispatch
	if strings.HasPrefix(actionVal, "cmd:") {
		cmdText := strings.TrimPrefix(actionVal, "cmd:")
		rctx := replyContext{messageID: messageID, chatID: chatID, sessionKey: sessionKey}

		slog.Info(p.tag()+": card action dispatched as command", "cmd", cmdText, "user", userID)

		go p.dispatchInbound(context.Background(), inboundMessage{
			sessionID: agentkit.SessionID(sessionKey),
			userID:    userID,
			content:   cmdText,
			rctx:      rctx,
		})
	}

	return nil, nil
}

func (p *Platform) onMessage(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	msg := event.Event.Message
	sender := event.Event.Sender

	msgType := ""
	if msg.MessageType != nil {
		msgType = *msg.MessageType
	}

	chatID := ""
	if msg.ChatId != nil {
		chatID = *msg.ChatId
	}
	userID := userIDFromEvent(sender.SenderId)
	// userName and chatName are resolved in dispatchMessage to avoid blocking
	// the SDK dispatcher goroutine with synchronous HTTP calls.

	messageID := ""
	if msg.MessageId != nil {
		messageID = *msg.MessageId
	}

	if p.isMessageRecalled(messageID) {
		slog.Debug(p.tag()+": recalled message ignored before dispatch", "message_id", messageID)
		return nil
	}

	if p.dedup.IsDuplicate(messageID) {
		slog.Debug(p.tag()+": duplicate message ignored", "message_id", messageID)
		return nil
	}

	if msg.CreateTime != nil {
		if ms, err := strconv.ParseInt(*msg.CreateTime, 10, 64); err == nil {
			msgTime := time.Unix(ms/1000, (ms%1000)*int64(time.Millisecond))
			if common.IsOldMessage(msgTime) {
				slog.Debug(p.tag()+": ignoring old message after restart", "create_time", *msg.CreateTime)
				return nil
			}
		}
	}

	chatType := ""
	if msg.ChatType != nil {
		chatType = *msg.ChatType
	}
	mentionCount := len(msg.Mentions)
	slog.Debug(p.tag()+": inbound message",
		"message_id", messageID,
		"chat_id", chatID,
		"chat_type", chatType,
		"root_id", stringValue(msg.RootId),
		"thread_id", stringValue(msg.ThreadId),
		"parent_id", stringValue(msg.ParentId),
		"mentions", mentionCount,
		"group_reply_all", p.groupReplyAll,
		"thread_isolation", p.threadIsolation,
	)

	if chatType == "group" && !p.groupReplyAll && p.botOpenID != "" {
		if !isBotMentioned(msg.Mentions, p.botOpenID) {
			// Feishu @all sends {"text":"@_all"} with 0 mentions.
			if p.respondToAtEveryoneAndHere && msg.Content != nil && strings.Contains(*msg.Content, "@_all") {
				slog.Debug(p.tag()+": responding to @all message", "chat_id", chatID)
			} else {
				slog.Debug(p.tag()+": ignoring group message without bot mention", "chat_id", chatID)
				return nil
			}
		}
	}

	if !common.AllowList(p.allowFrom, userID) {
		slog.Debug(p.tag()+": message from unauthorized user", "user", userID)
		return nil
	}

	if chatType == "group" && !common.AllowList(p.allowChat, chatID) {
		slog.Debug(p.tag()+": message from unauthorized chat", "chat_id", chatID)
		return nil
	}
	if chatType != "group" && p.groupOnly {
		slog.Debug(p.tag()+": p2p message skipped (group_only=true)", "chat_type", chatType)
		return nil
	}

	if msg.Content == nil && msgType != "merge_forward" {
		slog.Debug(p.tag()+": message content is nil", "message_id", messageID, "type", msgType)
		return nil
	}

	// Capture content before going async — the SDK may reuse the event object.
	content := ""
	if msg.Content != nil {
		content = *msg.Content
	}
	mentions := msg.Mentions
	parentID := stringValue(msg.ParentId)

	sessionKey := p.makeSessionKey(msg, chatID, userID)
	rctx := replyContext{messageID: messageID, chatID: chatID, chatType: chatType, sessionKey: sessionKey}
	slog.Debug(p.tag()+": routed inbound message",
		"message_id", messageID,
		"session_key", sessionKey,
		"reply_in_thread", p.shouldReplyInThread(rctx),
	)

	// Dispatch message handling asynchronously so the SDK event loop is not
	// blocked by IO-heavy operations (image/audio download, handler HTTP calls).
	// The dedup and old-message checks above remain synchronous to guarantee
	// correctness before spawning the goroutine.
	go p.dispatchMessage(ctx, msgType, content, mentions, messageID, sessionKey, userID, chatID, rctx, parentID)

	return nil
}

// dispatchMessage handles the message content parsing, media download, and
// handler invocation. It runs in its own goroutine so that onMessage returns
// quickly and does not block the SDK event loop.
func (p *Platform) dispatchMessage(ctx context.Context, msgType, content string, mentions []*larkim.MentionEvent, messageID, sessionKey, userID, chatID string, rctx replyContext, parentID string) {
	if p.isMessageRecalled(messageID) {
		slog.Debug(p.tag()+": recalled message ignored in async dispatch", "message_id", messageID)
		return
	}

	// Resolve user profile asynchronously so SDK dispatcher is not blocked.
	_ = p.cachedUserProfile(userID)
	if len(mentions) > 0 {
		_ = p.mentionMetadata(mentions)
	}
	_ = p.resolveChatName(chatID)

	// If this message is a reply to another message, fetch the quoted content
	// and prepend it so the agent has full context.
	// Skip quote injection when thread_isolation is enabled and the message is
	// inside a thread — the thread already provides conversational context, and
	// long quoted prefixes can drown out the user's actual text (issue #764).
	quotedPrefix := ""
	if parentID != "" && !(p.threadIsolation && isThreadSessionKey(sessionKey)) {
		quotedPrefix = p.fetchQuotedMessage(ctx, parentID)
	}

	switch msgType {
	case "text":
		var textBody struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(content), &textBody); err != nil {
			slog.Error(p.tag()+": failed to parse text content", "error", err)
			return
		}
		text := stripMentions(textBody.Text, mentions, p.botOpenID)
		if text == "" {
			slog.Debug(p.tag()+": dropping empty text after mention stripping",
				"message_id", messageID,
				"raw_text_len", len(textBody.Text),
				"mentions", len(mentions),
			)
			return
		}
		p.dispatchCoreMessage(&inboundMessage{
			sessionID:    agentkit.SessionID(sessionKey),
			messageID:    messageID,
			userID:       userID,
			content:      text,
			extraContent: quotedPrefix,
			mentions:     mentions,
			rctx:         rctx,
		})

	case "image":
		var imgBody struct {
			ImageKey string `json:"image_key"`
		}
		if err := json.Unmarshal([]byte(content), &imgBody); err != nil {
			slog.Error(p.tag()+": failed to parse image content", "error", err)
			return
		}
		imgData, mimeType, err := p.downloadImage(messageID, imgBody.ImageKey)
		if err != nil {
			slog.Error(p.tag()+": download image failed", "error", err)
			if sendErr := p.sendIMContent(ctx, rctx, "⚠️ Image download failed (network error). Please resend."); sendErr != nil {
				slog.Error(p.tag()+": failed to notify user about image download failure", "error", sendErr)
			}
			return
		}
		p.dispatchCoreMessage(&inboundMessage{
			sessionID: agentkit.SessionID(sessionKey),
			messageID: messageID,
			userID:    userID,
			images:    []common.ImageAttachment{{MimeType: mimeType, Data: imgData}},
			mentions:  mentions,
			rctx:      rctx,
		})

	case "audio":
		var audioBody struct {
			FileKey  string `json:"file_key"`
			Duration int    `json:"duration"` // milliseconds
		}
		if err := json.Unmarshal([]byte(content), &audioBody); err != nil {
			slog.Error(p.tag()+": failed to parse audio content", "error", err)
			return
		}
		slog.Debug(p.tag()+": audio received", "user", userID, "file_key", audioBody.FileKey)
		audioData, err := p.downloadResource(messageID, audioBody.FileKey, "file")
		if err != nil {
			slog.Error(p.tag()+": download audio failed", "error", err)
			if sendErr := p.sendIMContent(ctx, rctx, "⚠️ Voice message download failed (network error). Please resend."); sendErr != nil {
				slog.Error(p.tag()+": failed to notify user about audio download failure", "error", sendErr)
			}
			return
		}
		p.dispatchCoreMessage(&inboundMessage{
			sessionID: agentkit.SessionID(sessionKey),
			messageID: messageID,
			userID:    userID,
			audio: &common.AudioAttachment{
				MimeType: "audio/opus",
				Data:     audioData,
				Format:   "ogg",
				Duration: audioBody.Duration / 1000,
			},
			mentions: mentions,
			rctx:     rctx,
		})

	case "post":
		textParts, images := p.parsePostContent(messageID, content)
		text := stripMentions(strings.Join(textParts, "\n"), mentions, p.botOpenID)
		if text == "" && len(images) == 0 {
			return
		}
		p.dispatchCoreMessage(&inboundMessage{
			sessionID:    agentkit.SessionID(sessionKey),
			messageID:    messageID,
			userID:       userID,
			content:      text,
			extraContent: quotedPrefix,
			images:       images,
			mentions:     mentions,
			rctx:         rctx,
		})

	case "file":
		var fileBody struct {
			FileKey  string `json:"file_key"`
			FileName string `json:"file_name"`
		}
		if err := json.Unmarshal([]byte(content), &fileBody); err != nil {
			slog.Error(p.tag()+": failed to parse file content", "error", err)
			return
		}
		slog.Info(p.tag()+": file received", "user", userID, "file_key", fileBody.FileKey, "file_name", fileBody.FileName)
		fileData, err := p.downloadResource(messageID, fileBody.FileKey, "file")
		if err != nil {
			slog.Error(p.tag()+": download file failed", "error", err)
			if sendErr := p.sendIMContent(ctx, rctx, "⚠️ File download failed (network error). Please resend."); sendErr != nil {
				slog.Error(p.tag()+": failed to notify user about file download failure", "error", sendErr)
			}
			return
		}
		slog.Debug(p.tag()+": file downloaded", "file_name", fileBody.FileName, "size", len(fileData))
		mimeType := detectMimeType(fileData)
		p.dispatchCoreMessage(&inboundMessage{
			sessionID: agentkit.SessionID(sessionKey),
			messageID: messageID,
			userID:    userID,
			files: []common.FileAttachment{{
				MimeType: mimeType,
				Data:     fileData,
				FileName: fileBody.FileName,
			}},
			mentions: mentions,
			rctx:     rctx,
		})

	case "merge_forward":
		text, images, files := p.parseMergeForward(messageID)
		if text == "" && len(images) == 0 && len(files) == 0 {
			slog.Warn(p.tag()+": merge_forward produced no content", "message_id", messageID)
			return
		}
		coreMsg := &inboundMessage{
			sessionID: agentkit.SessionID(sessionKey),
			messageID: messageID,
			userID:    userID,
			content:   text,
			images:    images,
			files:     files,
			mentions:  mentions,
			rctx:      rctx,
		}
		p.dispatchCoreMessage(coreMsg)

	case "sticker":
		var stickerBody struct {
			FileKey string `json:"file_key"`
		}
		if err := json.Unmarshal([]byte(content), &stickerBody); err != nil {
			slog.Error(p.tag()+": failed to parse sticker content", "error", err)
			return
		}
		slog.Info(p.tag()+": sticker received", "user", userID, "file_key", stickerBody.FileKey)
		imgData, mimeType, err := p.downloadImage(messageID, stickerBody.FileKey)
		if err != nil {
			slog.Warn(p.tag()+": download sticker failed, falling back to placeholder", "error", err)
			p.dispatchCoreMessage(&inboundMessage{
				sessionID:    agentkit.SessionID(sessionKey),
				messageID:    messageID,
				userID:       userID,
				content:      "[sticker]",
				extraContent: quotedPrefix,
				mentions:     mentions,
				rctx:         rctx,
			})
			return
		}
		p.dispatchCoreMessage(&inboundMessage{
			sessionID: agentkit.SessionID(sessionKey),
			messageID: messageID,
			userID:    userID,
			images:    []common.ImageAttachment{{MimeType: mimeType, Data: imgData}},
			mentions:  mentions,
			rctx:      rctx,
		})

	case "media":
		var mediaBody struct {
			FileKey  string `json:"file_key"`
			ImageKey string `json:"image_key"`
			FileName string `json:"file_name"`
			Duration int    `json:"duration"`
		}
		if err := json.Unmarshal([]byte(content), &mediaBody); err != nil {
			slog.Error(p.tag()+": failed to parse media content", "error", err)
			return
		}
		slog.Info(p.tag()+": media received", "user", userID, "file_key", mediaBody.FileKey, "file_name", mediaBody.FileName)
		text := "[video"
		if mediaBody.FileName != "" {
			text += ": " + mediaBody.FileName
		}
		if mediaBody.Duration > 0 {
			text += fmt.Sprintf(", %ds", mediaBody.Duration/1000)
		}
		text += "]"
		var images []common.ImageAttachment
		if mediaBody.ImageKey != "" {
			if thumbData, thumbMime, err := p.downloadImage(messageID, mediaBody.ImageKey); err == nil {
				images = append(images, common.ImageAttachment{MimeType: thumbMime, Data: thumbData})
			} else {
				slog.Warn(p.tag()+": download media thumbnail failed", "error", err)
			}
		}
		p.dispatchCoreMessage(&inboundMessage{
			sessionID:    agentkit.SessionID(sessionKey),
			messageID:    messageID,
			userID:       userID,
			content:      text,
			extraContent: quotedPrefix,
			images:       images,
			mentions:     mentions,
			rctx:         rctx,
		})

	default:
		slog.Debug(p.tag()+": ignoring unsupported message type", "type", msgType)
	}
}

func (p *Platform) Reply(ctx context.Context, rctx any, content string) error {
	rc, ok := rctx.(replyContext)
	if !ok {
		return fmt.Errorf("%s: invalid reply context type %T", p.tag(), rctx)
	}

	content = p.resolveMentionsInContent(ctx, rc.chatID, content)
	msgType, msgBody := buildOutboundContent(ctx, content)

	if !p.shouldUseThreadOrReplyAPI(rc) {
		return p.sendNewMessageToChat(ctx, rc, msgType, msgBody)
	}
	return p.replyMessage(ctx, rc, msgType, msgBody)
}

// Send sends a message. When the original message ID is available, the message
// is sent as a reply (quoting the original) so the conversation stays threaded.
// Falls back to creating a standalone message when no message ID exists.
func (p *Platform) sendIMContent(ctx context.Context, rctx any, content string) error {
	rc, ok := rctx.(replyContext)
	if !ok {
		return fmt.Errorf("%s: invalid reply context type %T", p.tag(), rctx)
	}

	if p.shouldUseThreadOrReplyAPI(rc) {
		return p.Reply(ctx, rctx, content)
	}

	content = p.resolveMentionsInContent(ctx, rc.chatID, content)
	msgType, msgBody := buildOutboundContent(ctx, content)
	return p.sendNewMessageToChat(ctx, rc, msgType, msgBody)
}

func (p *Platform) SendImage(ctx context.Context, rctx any, img common.ImageAttachment) error {
	rc, ok := rctx.(replyContext)
	if !ok {
		return fmt.Errorf("%s: SendImage: invalid reply context type %T", p.tag(), rctx)
	}

	var uploadResp *larkim.CreateImageResp
	if err := p.withTransientRetry(ctx, "upload image", func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "upload image", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
			req := larkim.NewCreateImageReqBuilder().
				Body(larkim.NewCreateImageReqBodyBuilder().
					ImageType("message").
					Image(bytes.NewReader(img.Data)).
					Build()).
				Build()
			var err error
			uploadResp, err = client.Im.Image.Create(ctx, req, options...)
			if err != nil {
				return fmt.Errorf("%s: upload image: %w", p.tag(), err)
			}
			if !uploadResp.Success() {
				return fmt.Errorf("%s: upload image code=%d msg=%s", p.tag(), uploadResp.Code, uploadResp.Msg)
			}
			return nil
		})
	}); err != nil {
		return err
	}
	if uploadResp.Data == nil || uploadResp.Data.ImageKey == nil {
		return fmt.Errorf("%s: upload image: no image_key returned", p.tag())
	}

	imageContent, err := (&larkim.MessageImage{ImageKey: *uploadResp.Data.ImageKey}).String()
	if err != nil {
		return fmt.Errorf("%s: build image message: %w", p.tag(), err)
	}

	return p.sendMediaMessage(ctx, rc, larkim.MsgTypeImage, imageContent)
}

func (p *Platform) SendFile(ctx context.Context, rctx any, file common.FileAttachment) error {
	rc, ok := rctx.(replyContext)
	if !ok {
		return fmt.Errorf("%s: SendFile: invalid reply context type %T", p.tag(), rctx)
	}

	fileName := file.FileName
	if fileName == "" {
		fileName = "attachment"
	}
	fileType := detectFeishuFileType(file.MimeType, fileName)
	var uploadResp *larkim.CreateFileResp
	if err := p.withTransientRetry(ctx, "upload file", func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "upload file", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
			req := larkim.NewCreateFileReqBuilder().
				Body(larkim.NewCreateFileReqBodyBuilder().
					FileType(fileType).
					FileName(fileName).
					File(bytes.NewReader(file.Data)).
					Build()).
				Build()
			var err error
			uploadResp, err = client.Im.File.Create(ctx, req, options...)
			if err != nil {
				return fmt.Errorf("%s: upload file: %w", p.tag(), err)
			}
			if !uploadResp.Success() {
				return fmt.Errorf("%s: upload file code=%d msg=%s", p.tag(), uploadResp.Code, uploadResp.Msg)
			}
			return nil
		})
	}); err != nil {
		return err
	}
	if uploadResp.Data == nil || uploadResp.Data.FileKey == nil {
		return fmt.Errorf("%s: upload file: no file_key returned", p.tag())
	}

	fileContent, err := (&larkim.MessageFile{FileKey: *uploadResp.Data.FileKey}).String()
	if err != nil {
		return fmt.Errorf("%s: build file message: %w", p.tag(), err)
	}

	return p.sendMediaMessage(ctx, rc, larkim.MsgTypeFile, fileContent)
}

func (p *Platform) sendMediaMessage(ctx context.Context, rc replyContext, msgType, content string) error {
	if p.shouldUseThreadOrReplyAPI(rc) {
		return p.replyMessage(ctx, rc, msgType, content)
	}
	return p.createMessage(ctx, rc.chatID, msgType, content, "send media message")
}

// ═══════════════════════════════════════════════════════════════
// Card 2.0 rich card support (based on upstream PR #309 + #306,
// extended with "agent reply elapsed time" in the footer).
// ═══════════════════════════════════════════════════════════════
