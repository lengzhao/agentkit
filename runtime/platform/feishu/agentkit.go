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
	"github.com/lengzhao/agentkit/cap/interaction"
	caplarkevent "github.com/lengzhao/agentkit/cap/larkevent"
	"github.com/lengzhao/agentkit/cap/permission"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

var errNotSupported = fmt.Errorf("feishu: not supported")

type Config struct {
	common.AgentRoutingConfig
	AppID                      string `json:"appId"`
	AppSecret                  string `json:"appSecret"`
	Domain                     string `json:"domain"`
	AllowFrom                  string `json:"allowFrom"`
	AllowChat                  string `json:"allowChat"`
	GroupReplyAll              bool   `json:"groupReplyAll"`
	ShareSessionInChannel      bool   `json:"shareSessionInChannel"` // deprecated: use runner.config.sessionScope
	ThreadIsolation            bool   `json:"threadIsolation"`
	ReplyInThread              *bool  `json:"replyInThread"`
	ThreadNoMention            bool   `json:"threadNoMention"` // 话题被 @bot 激活后，话题内消息（含纯文字）免 @；需 threadIsolation
	ReactionEmoji              string `json:"reactionEmoji"`
	DoneEmoji                  string `json:"doneEmoji"`
	CancelledEmoji             string `json:"cancelledEmoji"`
	ErrorEmoji                 string `json:"errorEmoji"`
	// SteerEmoji 是入站消息被并入进行中 turn（steer）时替换到该消息上的 emoji。
	// 收到 turn/steer 事件后，平台把该消息原来的处理中 reaction（reactionEmoji）
	// 换成 SteerEmoji，让发消息的人立刻看到“这条已被当前回复接收”；turn/end 时
	// 再统一换成 done/cancelled/error。默认 "Typing"，"none" 关闭替换
	//（此时消息只保留处理中 reaction）。
	SteerEmoji string `json:"steerEmoji"`
	GroupOnly                  bool   `json:"groupOnly"`
	RespondToAtEveryoneAndHere bool   `json:"respondToAtEveryoneAndHere"`
	ReplyToTrigger             *bool  `json:"replyToTrigger"`
	// StopOnRecall 为 nil 时默认 true：用户撤回触发（或 steer 进）进行中
	// turn 的消息时，平台自动按 /stop 取消该 turn；显式 false 关闭。
	// 撤回与进行中 turn 无关的旧消息不影响执行。
	StopOnRecall              *bool             `json:"stopOnRecall"`
	ResolveMentions           bool              `json:"resolveMentions"`
	PeerBots                  map[string]string `json:"peerBots"`
	ShowThinking              *bool             `json:"showThinking"`
	ShowToolProgress          *bool             `json:"showToolProgress"`
	AsyncSubagentProgressCard *bool             `json:"asyncSubagentProgressCard"`
	EnableFeishuCard          *bool             `json:"enableFeishuCard"`
	EncryptKey                string            `json:"encryptKey"`
	Port                      string            `json:"port"`
	CallbackPath              string            `json:"callbackPath"`
	UnknownCardAction         string            `json:"unknownCardAction"` // forward (default) | ignore
	CardSubmissionTTL         string            `json:"cardSubmissionTTL"` // positive duration; default 24h
	// ForwardEventsToAgent 列出直转 agent 处理的自定义事件类型
	// （如 drive.file.created_v1）。事件序列化为一条 inbound 消息；
	// 每条事件使用独立 session（按 event_id），避免同群/同用户事件共享历史。
	// 回复仍按事件上下文：带 chat_id 回群，只带 user_id 回 p2p，都无则
	// 无回复目标。妙记/会议等用户身份
	// 订阅事件（minutes.minute.generated_v1、vc.*）按 subscriber_ids[0]
	// 的 open_id 路由到该用户的 p2p 会话。事件类型需在 Lark 开发者后台
	// 同步订阅；用户身份事件还需用户 OAuth 授权后用 user_access_token
	// 调对应服务端订阅 API 注册（见 docs/guides/lark-user-events.zh.md）。
	// 停机期间未 ack 的事件由飞书重推补送，重启后仍会投递（不按进程
	// 启动时间丢弃），重复投递由 event_id 去重。
	ForwardEventsToAgent []string `json:"forwardEventsToAgent,omitempty"`
}

// SetDefaults implements pluginkit.Defaulter. Emoji 字段保留 "none" 哨兵语义：
// 仅在为空时填默认值，"none" 由 newPlatform 映射为空串（禁用）。
func (c *Config) SetDefaults() {
	if strings.TrimSpace(c.ReactionEmoji) == "" {
		c.ReactionEmoji = "OnIt"
	}
	if c.DoneEmoji == "" {
		c.DoneEmoji = "CheckMark"
	}
	if c.CancelledEmoji == "" {
		c.CancelledEmoji = "HEARTBROKEN"
	}
	if c.ErrorEmoji == "" {
		c.ErrorEmoji = "CrossMark"
	}
	if strings.TrimSpace(c.SteerEmoji) == "" {
		c.SteerEmoji = "Typing"
	}
}

// Validate implements pluginkit.Validator.
func (c *Config) Validate() error {
	if c.AppID == "" || c.AppSecret == "" {
		return fmt.Errorf("platform feishu/lark requires appId and appSecret")
	}
	switch strings.ToLower(strings.TrimSpace(c.UnknownCardAction)) {
	case "", "forward", "ignore":
	default:
		return fmt.Errorf("platform feishu/lark unknownCardAction must be forward or ignore, got %q", c.UnknownCardAction)
	}
	_, err := common.ParseCardSubmissionTTL(c.CardSubmissionTTL)
	return err
}

type Deps struct {
	InteractionHandlers []interaction.Handler `json:"interactionHandlers,omitempty"`
	Commands            agentkit.Commands     `json:"commands,omitempty"`
	SessionStore        agentkit.SessionStore `json:"sessionStore,omitempty"`
	Workspace           workspace.Service     `json:"workspace,omitempty"`
	// EventHandlers 订阅自定义 Lark 事件的 handler 插件（cap/larkevent）。
	// 每个 handler 自行声明 EventTypes()，平台按事件类型聚合后注册
	// OnCustomizedEvent；同一事件类型可命中多个 handler。
	EventHandlers []caplarkevent.Handler `json:"eventHandlers,omitempty"`
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
	presaved     []common.PresavedAttachment
	mentions     []*larkim.MentionEvent
	rctx         replyContext
}

func (msg inboundMessage) inboundRoute(platform string) agentkit.SessionRouteInput {
	return agentkit.SessionRouteInput{
		Platform:    platform,
		DeliveryID:  msg.sessionID,
		ChannelID:   msg.rctx.chatID,
		ThreadID:    threadIDFromSessionKey(string(msg.sessionID)),
		ReplyTo:     msg.messageID,
		ScopeUserID: msg.userID,
	}
}

type streamCardKind string

const (
	streamCardProgress streamCardKind = "progress"
	streamCardBody     streamCardKind = "body"
	maxStreamCards                    = 3
)

type streamSegmentKind string

const (
	streamSegmentNone     streamSegmentKind = ""
	streamSegmentThinking streamSegmentKind = "thinking"
	streamSegmentTool     streamSegmentKind = "tool"
	streamSegmentBody     streamSegmentKind = "body"
)

type streamCard struct {
	Kind   streamCardKind
	Handle any
}

type cardStatus string

const (
	cardStatusThinking  cardStatus = "thinking"
	cardStatusWorking   cardStatus = "working"
	cardStatusDone      cardStatus = "done"
	cardStatusCancelled cardStatus = "cancelled"
	cardStatusError     cardStatus = "error"
)

type toolStepKind string

const (
	toolStepKindTool       toolStepKind = "tool"
	toolStepKindToolResult toolStepKind = "tool_result"
	toolStepKindThinking   toolStepKind = "thinking"
	toolStepKindSubagent   toolStepKind = "subagent"
)

type toolStep struct {
	Kind     toolStepKind
	Name     string
	Summary  string
	Result   string
	Status   string
	CallID   string
	ExitCode *int
	Success  *bool
	Done     bool
}

// New registers platform/feishu: Feishu WebSocket / webhook.
func New(cfg Config, deps Deps) (agentkit.Platform, error) {
	return newPlatform("feishu", lark.FeishuBaseUrl, cfg, deps)
}

func newPlatform(name, defaultDomain string, cfg Config, deps Deps) (agentkit.Platform, error) {
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("platform/%s: %w", name, err)
	}
	if deps.Workspace == nil {
		return nil, fmt.Errorf("platform/%s requires workspace", name)
	}
	domain, err := common.ResolveDomain(cfg.Domain, defaultDomain)
	if err != nil {
		return nil, fmt.Errorf("platform/%s: %w", name, err)
	}
	common.WarnAllowFromEmpty(name, cfg.AllowFrom)

	// "none" 哨兵：禁用对应 emoji。
	noneToEmpty := func(s string) string {
		if s == "none" {
			return ""
		}
		return s
	}
	reactionEmoji := noneToEmpty(strings.TrimSpace(cfg.ReactionEmoji))
	doneEmoji := noneToEmpty(cfg.DoneEmoji)
	cancelledEmoji := noneToEmpty(cfg.CancelledEmoji)
	errorEmoji := noneToEmpty(cfg.ErrorEmoji)
	steerEmoji := noneToEmpty(strings.TrimSpace(cfg.SteerEmoji))

	useInteractiveCard := true
	if cfg.EnableFeishuCard != nil {
		useInteractiveCard = *cfg.EnableFeishuCard
	}

	showThinking := false
	if cfg.ShowThinking != nil {
		showThinking = *cfg.ShowThinking
	}
	showToolProgress := true
	if cfg.ShowToolProgress != nil {
		showToolProgress = *cfg.ShowToolProgress
	}
	asyncSubagentProgressCard := true
	if cfg.AsyncSubagentProgressCard != nil {
		asyncSubagentProgressCard = *cfg.AsyncSubagentProgressCard
	}

	noReplyToTrigger := false
	if cfg.ReplyToTrigger != nil && !*cfg.ReplyToTrigger {
		noReplyToTrigger = true
	}

	stopOnRecall := true
	if cfg.StopOnRecall != nil {
		stopOnRecall = *cfg.StopOnRecall
	}

	replyInThread := true
	if cfg.ReplyInThread != nil {
		replyInThread = *cfg.ReplyInThread
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

	forwardToAgent := map[string]bool{}
	for _, eventType := range cfg.ForwardEventsToAgent {
		if eventType = strings.TrimSpace(eventType); eventType != "" {
			forwardToAgent[eventType] = true
		}
	}

	var clientOpts []lark.ClientOptionFunc
	clientOpts = append(clientOpts, lark.WithOpenBaseUrl(domain))

	p := &Platform{
		platformTag:                name,
		defaultDomain:              defaultDomain,
		domain:                     domain,
		appID:                      cfg.AppID,
		appSecret:                  cfg.AppSecret,
		showThinking:               showThinking,
		showToolProgress:           showToolProgress,
		asyncSubagentProgressCard:  asyncSubagentProgressCard,
		useInteractiveCard:         useInteractiveCard,
		reactionEmoji:              reactionEmoji,
		doneEmoji:                  doneEmoji,
		cancelledEmoji:             cancelledEmoji,
		errorEmoji:                 errorEmoji,
		steerEmoji:                 steerEmoji,
		allowFrom:                  cfg.AllowFrom,
		allowChat:                  cfg.AllowChat,
		groupOnly:                  cfg.GroupOnly,
		groupReplyAll:              cfg.GroupReplyAll,
		respondToAtEveryoneAndHere: cfg.RespondToAtEveryoneAndHere,
		shareSessionInChannel:      cfg.ShareSessionInChannel,
		threadIsolation:            cfg.ThreadIsolation,
		threadNoMention:            cfg.ThreadNoMention,
		replyInThread:              replyInThread,
		resolveMentions:            cfg.ResolveMentions,
		noReplyToTrigger:           noReplyToTrigger,
		stopOnRecall:               stopOnRecall,
		unknownCardAction:          common.NormalizeUnknownInteraction(cfg.UnknownCardAction),
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
		workspace:                  deps.Workspace,
		sessionScope:               cfg.SlashSessionScope(),
		inbox:                      common.NewInbox(64),
		startOnce:                  sync.Once{},
		eventHandlers:              deps.EventHandlers,
		interactionHandlers:        deps.InteractionHandlers,
		forwardToAgent:             forwardToAgent,
	}
	p.outbound = common.NewOutbound(p.sendText, p.sendMediaPart)
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
	// turn/steer never touches a stream: it only re-marks the steered message.
	// Handle it before streamKeyForOutbound so an acknowledgement cannot register
	// stream state for a route that has no card.
	if event.Type == agentkit.EventTurnSteer {
		return p.applyTurnSteer(event)
	}
	delivery := rctx.OutboundRouteID(event)
	streamKey := p.streamKeyForOutbound(event)
	switch event.Type {
	case agentkit.EventPermissionRequest:
		return p.sendPermissionCard(ctx, event)
	case agentkit.EventTurnEnd:
		endData := parseTurnEndData(event)
		p.applyTurnEndReactions(delivery, endData)
		if p.useInteractiveCard {
			return p.handleRichTurnEnd(ctx, streamKey, endData)
		}
		return nil
	}
	if event.Type == agentkit.EventTurnStart {
		p.onTurnStartReactions(delivery)
	}
	if !p.useInteractiveCard {
		return p.outbound.Handle(ctx, event)
	}
	switch event.Type {
	case agentkit.EventTurnStart:
		p.clearStreamState(streamKey)
		p.richStreamState(streamKey)
		if err := p.bootstrapReplyCard(ctx, streamKey); err != nil {
			slog.Debug(p.tag()+": bootstrap reply card failed", "session_id", streamKey, "error", err)
		}
		return nil
	case agentkit.EventMessageStart:
		return p.handleRichStreamMessageStart(ctx, streamKey)
	case agentkit.EventMessageUpdate:
		return p.handleStreamUpdate(ctx, event)
	case agentkit.EventMessageEnd:
		return p.handleRichStreamMessageEnd(ctx, event)
	case agentkit.EventToolResult:
		return p.handleRichToolResult(ctx, event)
	case agentkit.EventSubagentStart, agentkit.EventSubagentEnd:
		return p.handleRichSubagentEvent(ctx, event)
	case agentkit.EventAssistantMessage:
		return p.handleRichProactiveAssistant(ctx, event)
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

	// deps.eventHandlers 声明的订阅 + config.forwardEventsToAgent 的
	// agent 直转，统一注册为 OnCustomizedEvent（见 events.go）。
	p.eventHandler = p.registerCustomEvents(p.eventHandler)

	if p.useInteractiveCard {
		slog.Info(p.platformTag + ": interactive card mode enabled, ensure card.action.trigger event is subscribed in Feishu console")
	}

	if p.shouldUseWebhookMode() {
		_ = p.startWebhookMode()
	} else {
		_ = p.startWebSocketMode(ctx)
	}
	<-ctx.Done()
	p.flushPendingAttachments()
	if p.wsClient != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := p.wsClient.CloseAndWait(shutdownCtx); err != nil && shutdownCtx.Err() == nil {
			slog.Debug(p.tag()+": websocket close", "error", err)
		}
	}
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
	if msg.messageID != "" && p.reactionEmoji != "" {
		msg.rctx.processingReactionID = p.addReaction(msg.messageID)
	}
	p.appendTurnReactionIfActive(msg.sessionID, msg.rctx)
	p.storeDelivery(msg.sessionID, msg.rctx)

	text := strings.TrimSpace(msg.content)
	metadata := p.inboundMetadata(msg)
	if text != "" {
		outcome, err := common.ProcessPlatformSlash(ctx, p.cfg.AgentRoutingConfig, p.commands, common.SlashContext{
			Route:        rctx.BuildSessionRoute(msg.inboundRoute(p.platformTag)),
			SessionScope: p.sessionScope,
			UserID:       msg.userID,
			Metadata:     metadata,
		}, text)
		if err != nil {
			_ = p.sendText(ctx, msg.sessionID, fmt.Sprintf("命令执行失败: %v", err))
			p.addDoneReactionForSession(msg.sessionID)
			return
		}
		switch outcome.Kind {
		case common.SlashHandled:
			if outcome.Reply != "" {
				_ = p.sendText(ctx, msg.sessionID, outcome.Reply)
			}
			p.addDoneReactionForSession(msg.sessionID)
			return
		case common.SlashForward:
			if outcome.Reply != "" {
				_ = p.sendText(ctx, msg.sessionID, outcome.Reply)
			}
		case common.SlashNotCommand:
		}
	}

	opts := common.InboundOptsFor(p.workspace)
	if len(msg.presaved) > 0 {
		if opts == nil {
			opts = &common.InboundOpts{Workspace: p.workspace}
		}
		opts.PresavedAttachments = msg.presaved
	}
	event := common.InboundFromContent(
		p.agentID, msg.inboundRoute(p.platformTag), msg.userID,
		msg.content, msg.extraContent, msg.images, msg.files, msg.audio, nil,
		opts,
	)
	event = enrichInboundActorFromProfile(common.WithMetadata(event, metadata))
	_ = p.inbox.Push(ctx, event)
}

func (p *Platform) inboundMetadata(msg inboundMessage) map[string]any {
	return common.MergeMetadata(p.userProfileMetadata(msg.userID), p.mentionMetadata(msg.mentions))
}

func (p *Platform) storeDelivery(sessionID agentkit.SessionID, rc replyContext) {
	if rc.chatType == "" {
		if existing, ok := p.deliveryFor(sessionID); ok && existing.chatType != "" {
			rc.chatType = existing.chatType
		}
	}
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

// deliveryForSend resolves reply context for proactive tool/send delivery.
// It prefers cached inbound context, then reconstructs routing from the session key.
func (p *Platform) deliveryForSend(sessionID agentkit.SessionID) (replyContext, bool) {
	if rc, ok := p.deliveryFor(sessionID); ok {
		return rc, true
	}
	raw, err := p.ReconstructReplyCtx(string(sessionID))
	if err != nil {
		return replyContext{}, false
	}
	rc, ok := raw.(replyContext)
	return rc, ok
}

func (p *Platform) sendText(ctx context.Context, sessionID agentkit.SessionID, text string) error {
	rc, ok := p.deliveryForSend(sessionID)
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
	rc, ok := p.deliveryFor(rctx.OutboundRouteID(event))
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

func (p *Platform) pushPermissionReply(ctx context.Context, sessionKey string, reply permission.Reply, extra map[string]string) error {
	if strings.TrimSpace(sessionKey) == "" {
		return fmt.Errorf("%s: missing permission session", p.tag())
	}
	return p.inbox.Push(ctx, common.PermissionReplyEventWithConversation(
		p.agentID,
		agentkit.SessionID(sessionKey),
		p.platformTag,
		reply.UserID,
		common.PermissionConversationFromExtra(extra),
		reply,
	))
}

func (p *Platform) addDoneReactionForSession(sessionID agentkit.SessionID) {
	rc, ok := p.deliveryFor(sessionID)
	if !ok {
		return
	}
	p.addDoneReaction(rc)
}

func (p *Platform) turnTriggerFor(sessionID agentkit.SessionID) (replyContext, bool) {
	if raw, ok := p.turnTriggers.LoadAndDelete(sessionID); ok {
		if rc, ok := raw.(replyContext); ok {
			return rc, true
		}
	}
	return p.deliveryFor(sessionID)
}

func (p *Platform) addDoneReaction(rc replyContext) {
	if rc.messageID == "" {
		return
	}
	if rc.processingReactionID != "" {
		p.removeReaction(rc.messageID, rc.processingReactionID)
	}
	if p.doneEmoji != "" {
		p.addReactionWithEmoji(rc.messageID, p.doneEmoji)
	}
}

func (p *Platform) addCancelledReaction(rc replyContext) {
	if rc.messageID == "" {
		return
	}
	if rc.processingReactionID != "" {
		p.removeReaction(rc.messageID, rc.processingReactionID)
	}
	if p.cancelledEmoji != "" {
		p.addReactionWithEmoji(rc.messageID, p.cancelledEmoji)
	}
}

func (p *Platform) addErrorReaction(rc replyContext) {
	if rc.messageID == "" {
		return
	}
	if rc.processingReactionID != "" {
		p.removeReaction(rc.messageID, rc.processingReactionID)
	}
	if p.errorEmoji != "" {
		p.addReactionWithEmoji(rc.messageID, p.errorEmoji)
	}
}

func parseTurnEndData(event agentkit.OutboundEvent) capsession.TurnEndData {
	var data capsession.TurnEndData
	if len(event.Data) == 0 {
		return data
	}
	_ = json.Unmarshal(event.Data, &data)
	return data
}

// cc-connect style: stream main_text via CardKit when the collapsible panel is unchanged.
const richCardBodyStreamInterval = 200 * time.Millisecond
const richCardBodyStreamMinRunes = 20
const richCardFullPatchInterval = 1500 * time.Millisecond

// richCardKeepaliveInterval 是进度卡片的心跳间隔：超过该时长没有任何卡片更新时
// 主动轻量 flush 一次，避免 CardKit 实体在长 LLM 推理期间被回收/拒绝。
const richCardKeepaliveInterval = 10 * time.Second

func (p *Platform) streamState(sessionID agentkit.SessionID) *streamState {
	if raw, ok := p.streams.Load(sessionID); ok {
		return raw.(*streamState)
	}
	st := newStreamState()
	actual, _ := p.streams.LoadOrStore(sessionID, st)
	return actual.(*streamState)
}

// clearStreamState drops in-memory stream state for a key. Route info lives in
// stream state and is dropped with it; the next outbound event re-captures it.
func (p *Platform) clearStreamState(sessionID agentkit.SessionID) {
	if raw, ok := p.streams.LoadAndDelete(sessionID); ok {
		st := raw.(*streamState)
		st.lock()
		stopStreamTimer(&st.bodyFlushTimer)
		stopStreamTimer(&st.keepaliveTimer)
		st.unlock()
	}
}

func (p *Platform) clearStream(sessionID agentkit.SessionID) {
	p.clearStreamState(sessionID)
}

func (p *Platform) handleStreamUpdate(ctx context.Context, event agentkit.OutboundEvent) error {
	streamKey := p.streamKeyForOutbound(event)
	var payload agentkit.MessageUpdatePayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return err
	}
	return p.handleRichStreamUpdate(ctx, streamKey, event, payload.AssistantMessageEvent)
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

func threadIDFromSessionKey(sessionKey string) string {
	parts := rctx.ParseDelivery(agentkit.SessionID(sessionKey), "")
	if !parts.Routable {
		return ""
	}
	return parts.Thread
}

func (p *Platform) startWebSocketMode(ctx context.Context) error {
	wsOpts := []larkws.ClientOption{
		larkws.WithEventHandler(p.eventHandler),
		larkws.WithLogLevel(larkcore.LogLevelInfo),
		larkws.WithLogger(&sanitizingLogger{inner: larkcore.NewEventLogger()}),
		larkws.WithDomain(p.domain),
	}
	p.wsClient = larkws.NewClient(p.appID, p.appSecret, wsOpts...)
	go func() {
		if err := p.wsClient.Start(ctx); err != nil && ctx.Err() == nil {
			slog.Error(p.tag()+": websocket error", "error", err)
		}
	}()
	slog.Info(p.tag() + ": websocket started")
	return nil
}
