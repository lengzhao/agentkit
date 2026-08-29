package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

var errNotSupported = fmt.Errorf("feishu: not supported")

type Config struct {
	common.AgentRoutingConfig
	AppID                      string            `json:"appId"`
	AppSecret                  string            `json:"appSecret"`
	Domain                     string            `json:"domain"`
	AllowFrom                  string            `json:"allowFrom"`
	AllowChat                  string            `json:"allowChat"`
	GroupReplyAll              bool              `json:"groupReplyAll"`
	ShareSessionInChannel      bool              `json:"shareSessionInChannel"` // deprecated: use runner.config.sessionScope
	ThreadIsolation            bool              `json:"threadIsolation"`
	ReactionEmoji              string            `json:"reactionEmoji"`
	DoneEmoji                  string            `json:"doneEmoji"`
	GroupOnly                  bool              `json:"groupOnly"`
	RespondToAtEveryoneAndHere bool              `json:"respondToAtEveryoneAndHere"`
	ReplyToTrigger             *bool             `json:"replyToTrigger"`
	ResolveMentions            bool              `json:"resolveMentions"`
	PeerBots                   map[string]string `json:"peerBots"`
	ProgressStyle              string            `json:"progressStyle"`
	EnableFeishuCard           *bool             `json:"enableFeishuCard"`
	EncryptKey                 string            `json:"encryptKey"`
	Port                       string            `json:"port"`
	CallbackPath               string            `json:"callbackPath"`
}

type Deps struct {
	Commands     agentkit.Commands     `json:"commands,omitempty"`
	SessionStore agentkit.SessionStore `json:"sessionStore,omitempty"`
}

type inboundMessage struct {
	sessionID    agentkit.SessionID
	messageID    string
	userID       string
	content      string
	extraContent string
	images       []common.ImageAttachment
	files        []common.FileAttachment
	audio        *common.AudioAttachment
	rctx         replyContext
}

type streamState struct {
	mu          sync.Mutex
	handle      any
	accumulated string
	lastUpdate  time.Time
}

type cardStatus string

const (
	cardStatusThinking cardStatus = "thinking"
	cardStatusWorking  cardStatus = "working"
	cardStatusDone     cardStatus = "done"
	cardStatusError    cardStatus = "error"
)

type toolStepKind string

const (
	toolStepKindTool     toolStepKind = "tool"
	toolStepKindThinking toolStepKind = "thinking"
)

type toolStep struct {
	Kind     toolStepKind
	Name     string
	Summary  string
	Result   string
	Status   string
	ExitCode *int
	Success  *bool
	Done     bool
}

// New registers platform/feishu: Feishu WebSocket / webhook.
func New(cfg Config, deps Deps) (agentkit.Platform, error) {
	return newPlatform("feishu", lark.FeishuBaseUrl, cfg, deps)
}

func newPlatform(name, defaultDomain string, cfg Config, deps Deps) (agentkit.Platform, error) {
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return nil, fmt.Errorf("platform/%s requires appId and appSecret", name)
	}
	domain, err := common.ResolveDomain(cfg.Domain, defaultDomain)
	if err != nil {
		return nil, fmt.Errorf("platform/%s: %w", name, err)
	}
	common.WarnAllowFromEmpty(name, cfg.AllowFrom)

	reactionEmoji := cfg.ReactionEmoji
	if reactionEmoji == "" {
		reactionEmoji = "OnIt"
	}
	if reactionEmoji == "none" {
		reactionEmoji = ""
	}
	doneEmoji := cfg.DoneEmoji
	if doneEmoji == "none" {
		doneEmoji = ""
	}

	progressStyle := "legacy"
	if v := strings.TrimSpace(cfg.ProgressStyle); v != "" {
		switch strings.ToLower(v) {
		case "legacy":
			progressStyle = "legacy"
		case "compact", "card":
			progressStyle = strings.ToLower(v)
		default:
			return nil, fmt.Errorf("platform/%s: invalid progressStyle %q (want legacy, compact, or card)", name, v)
		}
	}

	useInteractiveCard := true
	if cfg.EnableFeishuCard != nil {
		useInteractiveCard = *cfg.EnableFeishuCard
	}

	noReplyToTrigger := false
	if cfg.ReplyToTrigger != nil && !*cfg.ReplyToTrigger {
		noReplyToTrigger = true
	}

	port := strings.TrimSpace(cfg.Port)
	if port == "" {
		port = "8080"
	}
	callbackPath := strings.TrimSpace(cfg.CallbackPath)
	if callbackPath == "" {
		callbackPath = "/feishu/webhook"
	}

	peerBots := map[string]string{}
	for k, v := range cfg.PeerBots {
		if strings.TrimSpace(v) != "" {
			peerBots[k] = v
		}
	}

	var clientOpts []lark.ClientOptionFunc
	if domain != defaultDomain {
		clientOpts = append(clientOpts, lark.WithOpenBaseUrl(domain))
	}

	p := &Platform{
		platformTag:                name,
		defaultDomain:              defaultDomain,
		domain:                     domain,
		appID:                      cfg.AppID,
		appSecret:                  cfg.AppSecret,
		progressStyle:              progressStyle,
		useInteractiveCard:         useInteractiveCard,
		reactionEmoji:              reactionEmoji,
		doneEmoji:                  doneEmoji,
		allowFrom:                  cfg.AllowFrom,
		allowChat:                  cfg.AllowChat,
		groupOnly:                  cfg.GroupOnly,
		groupReplyAll:              cfg.GroupReplyAll,
		respondToAtEveryoneAndHere: cfg.RespondToAtEveryoneAndHere,
		shareSessionInChannel:      cfg.ShareSessionInChannel,
		threadIsolation:            cfg.ThreadIsolation,
		resolveMentions:            cfg.ResolveMentions,
		noReplyToTrigger:           noReplyToTrigger,
		client:                     lark.NewClient(cfg.AppID, cfg.AppSecret, clientOpts...),
		replayClient:               newFeishuReplayClient(cfg.AppID, cfg.AppSecret, domain),
		dedup:                      &common.MessageDedup{},
		port:                       port,
		callbackPath:               callbackPath,
		encryptKey:                 cfg.EncryptKey,
		peerBots:                   peerBots,
		cfg:                        cfg,
		agentID:                    cfg.ResolveAgentID(),
		commands:                   deps.Commands,
		inbox:                      common.NewInbox(64),
		startOnce:                  sync.Once{},
	}
	p.outbound = common.NewOutbound(p.sendText)
	return p, nil
}

func (p *Platform) PlatformID() string { return p.platformTag }

func (p *Platform) PermissionCapability() permission.Capability {
	return permission.Capability{
		Interactive:    true,
		DefaultTimeout: permission.DefaultTimeout,
		AnswerScope:    permission.ScopeAsker,
	}
}

func (p *Platform) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
	p.startOnce.Do(func() {
		runCtx, cancel := context.WithCancel(ctx)
		p.cancel = cancel
		go p.run(runCtx)
	})
	event, err := p.inbox.Receive(ctx)
	if err != nil {
		return agentkit.MessageEvent{}, err
	}
	if event.Message.Role == "" && len(event.Reply) == 0 {
		return agentkit.MessageEvent{}, nil
	}
	return event, nil
}

func (p *Platform) Send(ctx context.Context, event agentkit.OutboundEvent) error {
	switch event.Type {
	case agentkit.EventPermissionRequest:
		return p.sendPermissionCard(ctx, event)
	case agentkit.EventTurnEnd:
		p.addDoneReactionForSession(event.SessionID)
		return nil
	}
	if !p.useInteractiveCard {
		return p.outbound.Handle(ctx, event)
	}
	switch event.Type {
	case agentkit.EventMessageStart:
		p.clearStream(event.SessionID)
		return nil
	case agentkit.EventMessageUpdate:
		return p.handleStreamUpdate(ctx, event)
	case agentkit.EventMessageEnd, agentkit.EventAssistantMessage:
		return p.handleStreamEnd(ctx, event)
	default:
		return p.outbound.Handle(ctx, event)
	}
}

func (p *Platform) run(ctx context.Context) {
	if !p.shouldUseWebhookMode() {
		if openID, err := p.fetchBotOpenID(); err != nil {
			slog.Warn(p.platformTag+": failed to get bot open_id, group chat filtering disabled", "error", err)
		} else {
			p.botOpenID = openID
			slog.Info(p.platformTag+": bot identified", "open_id", openID)
		}
	}

	p.eventHandler = dispatcher.NewEventDispatcher("", p.encryptKey).
		OnP2MessageReceiveV1(p.onMessage).
		OnP2MessageRecalledV1(p.onMessageRecalled).
		OnP2MessageReadV1(func(context.Context, *larkim.P2MessageReadV1) error { return nil }).
		OnP2ChatAccessEventBotP2pChatEnteredV1(func(context.Context, *larkim.P2ChatAccessEventBotP2pChatEnteredV1) error {
			return nil
		}).
		OnP1P2PChatCreatedV1(func(context.Context, *larkim.P1P2PChatCreatedV1) error { return nil }).
		OnP2MessageReactionCreatedV1(func(context.Context, *larkim.P2MessageReactionCreatedV1) error { return nil }).
		OnP2MessageReactionDeletedV1(func(context.Context, *larkim.P2MessageReactionDeletedV1) error { return nil }).
		OnP2CardActionTrigger(func(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
			return p.onCardAction(event)
		}).
		OnP2BotMenuV6(func(ctx context.Context, event *larkapplication.P2BotMenuV6) error {
			return p.onBotMenu(event)
		})

	if p.useInteractiveCard {
		slog.Info(p.platformTag + ": interactive card mode enabled, ensure card.action.trigger event is subscribed in Feishu console")
	}

	if p.shouldUseWebhookMode() {
		_ = p.startWebhookMode()
	} else {
		_ = p.startWebSocketMode()
	}
	<-ctx.Done()
	if p.server != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.server.Shutdown(shutdownCtx)
	}
}

func (p *Platform) dispatchCoreMessage(msg *inboundMessage) {
	if msg == nil {
		return
	}
	if p.isMessageRecalled(msg.messageID) {
		slog.Debug(p.tag()+": recalled message dispatch dropped", "message_id", msg.messageID)
		return
	}
	p.dispatchInbound(context.Background(), *msg)
}

func (p *Platform) dispatchInbound(ctx context.Context, msg inboundMessage) {
	p.storeDelivery(msg.sessionID, msg.rctx)
	if msg.messageID != "" && p.reactionEmoji != "" {
		p.addReaction(msg.messageID)
	}

	text := strings.TrimSpace(msg.content)
	if text != "" {
		outcome, err := common.ProcessSlash(ctx, p.commands, msg.sessionID, text)
		if err != nil {
			_ = p.sendText(ctx, msg.sessionID, fmt.Sprintf("命令执行失败: %v", err))
			return
		}
		switch outcome.Kind {
		case common.SlashHandled:
			if outcome.Reply != "" {
				_ = p.sendText(ctx, msg.sessionID, outcome.Reply)
			}
			return
		case common.SlashForward:
			if outcome.Reply != "" {
				_ = p.sendText(ctx, msg.sessionID, outcome.Reply)
			}
		case common.SlashNotCommand:
		}
	}

	_ = p.inbox.Push(ctx, common.InboundFromContent(
		p.agentID, msg.sessionID, p.platformTag, msg.userID,
		msg.content, msg.extraContent, msg.images, msg.files, msg.audio, nil,
	))
}

func (p *Platform) storeDelivery(sessionID agentkit.SessionID, rc replyContext) {
	p.deliveries.Store(sessionID, rc)
}

func (p *Platform) deliveryFor(sessionID agentkit.SessionID) (replyContext, bool) {
	raw, ok := p.deliveries.Load(sessionID)
	if !ok {
		return replyContext{}, false
	}
	rc, ok := raw.(replyContext)
	return rc, ok
}

func (p *Platform) sendText(ctx context.Context, sessionID agentkit.SessionID, text string) error {
	rc, ok := p.deliveryFor(sessionID)
	if !ok {
		return fmt.Errorf("%s: unknown session %s", p.tag(), sessionID)
	}
	return p.sendIMContent(ctx, rc, text)
}

func (p *Platform) sendPermissionCard(ctx context.Context, event agentkit.OutboundEvent) error {
	var payload permission.RequestPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return err
	}
	rc, ok := p.deliveryFor(event.SessionID)
	if !ok {
		return nil
	}
	card := common.PermissionCardFromPayload(payload)
	return p.sendCard(ctx, rc, card)
}

func (p *Platform) sendCard(ctx context.Context, rc replyContext, card *common.Card) error {
	cardJSON := renderCard(card, string(rc.sessionKey))
	if p.shouldUseThreadOrReplyAPI(rc) {
		return p.replyMessage(ctx, rc, larkim.MsgTypeInteractive, cardJSON)
	}
	return p.createMessage(ctx, rc.chatID, larkim.MsgTypeInteractive, cardJSON, "send card")
}

func (p *Platform) pushPermissionReply(ctx context.Context, sessionKey string, reply permission.Reply) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	_ = p.inbox.Push(ctx, common.PermissionReplyEvent(p.agentID, agentkit.SessionID(sessionKey), p.platformTag, reply.UserID, reply))
}

func (p *Platform) addDoneReactionForSession(sessionID agentkit.SessionID) {
	if p.doneEmoji == "" {
		return
	}
	rc, ok := p.deliveryFor(sessionID)
	if !ok || rc.messageID == "" {
		return
	}
	p.addReactionWithEmoji(rc.messageID, p.doneEmoji)
}

const streamUpdateInterval = 800 * time.Millisecond

func (p *Platform) streamState(sessionID agentkit.SessionID) *streamState {
	if raw, ok := p.streams.Load(sessionID); ok {
		return raw.(*streamState)
	}
	st := &streamState{}
	actual, _ := p.streams.LoadOrStore(sessionID, st)
	return actual.(*streamState)
}

func (p *Platform) clearStream(sessionID agentkit.SessionID) {
	p.streams.Delete(sessionID)
}

func (p *Platform) handleStreamUpdate(ctx context.Context, event agentkit.OutboundEvent) error {
	var payload agentkit.MessageUpdatePayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return err
	}
	delta := ""
	switch payload.AssistantMessageEvent.Type {
	case agentkit.AssistantEventTextDelta, agentkit.AssistantEventThinkingDelta:
		delta = payload.AssistantMessageEvent.Delta
	}
	if delta == "" {
		return nil
	}

	st := p.streamState(event.SessionID)
	st.mu.Lock()
	st.accumulated += delta
	accumulated := st.accumulated
	shouldFlush := st.handle == nil || time.Since(st.lastUpdate) >= streamUpdateInterval
	st.mu.Unlock()

	if !shouldFlush {
		return nil
	}
	return p.flushStream(ctx, event.SessionID, accumulated)
}

func (p *Platform) handleStreamEnd(ctx context.Context, event agentkit.OutboundEvent) error {
	st := p.streamState(event.SessionID)
	st.mu.Lock()
	text := st.accumulated
	handle := st.handle
	st.mu.Unlock()

	if event.Type == agentkit.EventAssistantMessage {
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(event.Data, &msg); err == nil {
			if t := assistantText(msg); t != "" {
				text = t
			}
		}
	}
	p.clearStream(event.SessionID)
	if strings.TrimSpace(text) == "" {
		return nil
	}

	rc, ok := p.deliveryFor(event.SessionID)
	if !ok {
		return fmt.Errorf("%s: unknown session %s", p.tag(), event.SessionID)
	}

	if handle != nil {
		msgType, body := buildReplyContent(text)
		cardJSON := buildPreviewCardJSON(body)
		if msgType != larkim.MsgTypeInteractive {
			cardJSON = buildCardJSON(sanitizeMarkdownURLs(body))
		}
		return p.UpdateMessage(ctx, handle, cardJSON)
	}
	return p.sendIMContent(ctx, rc, text)
}

func (p *Platform) flushStream(ctx context.Context, sessionID agentkit.SessionID, text string) error {
	rc, ok := p.deliveryFor(sessionID)
	if !ok {
		return nil
	}
	st := p.streamState(sessionID)
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.handle == nil {
		handle, err := p.SendPreviewStart(ctx, rc, text)
		if err != nil {
			return err
		}
		st.handle = handle
		st.lastUpdate = time.Now()
		return nil
	}
	if err := p.UpdateMessage(ctx, st.handle, text); err != nil {
		return err
	}
	st.lastUpdate = time.Now()
	return nil
}

func assistantText(msg agentkit.ModelMessage) string {
	var b []byte
	for _, part := range msg.Content {
		if part.Type == "text" && part.Text != "" {
			b = append(b, part.Text...)
		}
	}
	return string(b)
}

func (p *Platform) startWebSocketMode() error {
	wsOpts := []larkws.ClientOption{
		larkws.WithEventHandler(p.eventHandler),
		larkws.WithLogLevel(larkcore.LogLevelInfo),
		larkws.WithLogger(&sanitizingLogger{inner: larkcore.NewEventLogger()}),
	}
	if p.domain != p.defaultDomain {
		wsOpts = append(wsOpts, larkws.WithDomain(p.domain))
	}
	p.wsClient = larkws.NewClient(p.appID, p.appSecret, wsOpts...)
	go func() {
		if err := p.wsClient.Start(context.Background()); err != nil {
			slog.Error(p.tag()+": websocket error", "error", err)
		}
	}()
	slog.Info(p.tag() + ": websocket started")
	return nil
}
