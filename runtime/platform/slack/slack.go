package slack

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

type Config struct {
	common.AgentRoutingConfig
	BotToken    string `json:"botToken"`
	BotTokenRef string `json:"botTokenRef"`
	AppToken    string `json:"appToken"`
	AppTokenRef string `json:"appTokenRef"`
	Domain        string `json:"domain"` // optional Slack Web API base URL override
	AllowFrom     string `json:"allowFrom"`
	AllowChannels string `json:"allowChannels"`
	GroupReplyAll bool   `json:"groupReplyAll"`
}

type Deps struct {
	Commands     agentkit.Commands     `json:"commands,omitempty"`
	SessionStore agentkit.SessionStore `json:"sessionStore,omitempty"`
	Workspace    workspace.Service     `json:"workspace,omitempty"`
	Credentials  credentials.Store     `json:"credentials,omitempty"`
}

type delivery struct {
	channel          string
	threadTS         string
	msgTS            string
	directMessage    bool
	sessionID        agentkit.SessionID
	slashResponseURL string
}

type Platform struct {
	cfg        Config
	agentID    agentkit.AgentID
	apiURL     string
	commands   agentkit.Commands
	sessionScope session.SessionScope
	workspace  workspace.Service
	inbox      *common.Inbox
	outbound   *common.Outbound
	deliveries sync.Map

	client *slack.Client
	socket *socketmode.Client
	cancel context.CancelFunc

	cardMsgMu   sync.Mutex
	cardMsgRefs map[string]cardMessageRef

	channelCacheMu   sync.RWMutex
	channelNameCache map[string]string
	userNameCache    sync.Map

	typingMu    sync.Mutex
	typingStops map[agentkit.SessionID]func()

	startOnce sync.Once
}

// New registers platform/slack: Slack Socket Mode; SessionID follows cc-connect slack conventions.
func New(cfg Config, deps Deps) (agentkit.Platform, error) {
	botToken, err := resolveToken(context.Background(), cfg.BotToken, cfg.BotTokenRef, deps.Credentials, "botTokenRef")
	if err != nil {
		return nil, err
	}
	appToken, err := resolveToken(context.Background(), cfg.AppToken, cfg.AppTokenRef, deps.Credentials, "appTokenRef")
	if err != nil {
		return nil, err
	}
	if botToken == "" || appToken == "" {
		return nil, fmt.Errorf("platform/slack requires botToken/botTokenRef and appToken/appTokenRef")
	}
	apiURL, err := common.NormalizeSlackAPIURL(cfg.Domain)
	if err != nil {
		return nil, fmt.Errorf("platform/slack: %w", err)
	}
	common.WarnAllowFromEmpty("slack", cfg.AllowFrom)
	p := &Platform{
		cfg: Config{
			AgentRoutingConfig: cfg.AgentRoutingConfig,
			BotToken:           botToken,
			AppToken:           appToken,
			Domain:             cfg.Domain,
			AllowFrom:          cfg.AllowFrom,
			AllowChannels:      cfg.AllowChannels,
			GroupReplyAll:      cfg.GroupReplyAll,
		},
		agentID:          cfg.ResolveAgentID(),
		apiURL:           apiURL,
		commands:         deps.Commands,
		workspace:          deps.Workspace,
		sessionScope:     session.ParseScope(cfg.SessionScope),
		inbox:            common.NewInbox(64),
		channelNameCache: make(map[string]string),
		typingStops:      make(map[agentkit.SessionID]func()),
	}
	p.outbound = common.NewOutbound(p.sendText, p.sendMediaPart)
	return p, nil
}

func (p *Platform) PlatformID() string { return "slack" }

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
	case agentkit.EventMessageStart:
		p.stopTyping(event.SessionID)
		if stop := p.startTypingForSession(event.SessionID); stop != nil {
			p.typingMu.Lock()
			p.typingStops[event.SessionID] = stop
			p.typingMu.Unlock()
		}
		return p.outbound.Handle(ctx, event)
	case agentkit.EventTurnEnd:
		p.stopTyping(event.SessionID)
		p.reactDone(ctx, event.SessionID)
		return nil
	default:
		return p.outbound.Handle(ctx, event)
	}
}

func (p *Platform) stopTyping(sessionID agentkit.SessionID) {
	p.typingMu.Lock()
	stop := p.typingStops[sessionID]
	delete(p.typingStops, sessionID)
	p.typingMu.Unlock()
	if stop != nil {
		stop()
	}
}

func (p *Platform) startTypingForSession(sessionID agentkit.SessionID) func() {
	raw, ok := p.deliveries.Load(sessionID)
	if !ok {
		return nil
	}
	return p.startTypingProgress(context.Background(), raw.(delivery))
}

func (p *Platform) run(ctx context.Context) {
	opts := []slack.Option{slack.OptionAppLevelToken(p.cfg.AppToken)}
	if p.apiURL != "" {
		opts = append(opts, slack.OptionAPIURL(p.apiURL))
	}
	client := slack.New(p.cfg.BotToken, opts...)
	p.client = client
	p.socket = socketmode.New(client)

	go func() {
		if err := p.socket.RunContext(ctx); err != nil && ctx.Err() == nil {
			slog.Error("slack: socket mode stopped", "err", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-p.socket.Events:
			if !ok {
				return
			}
			p.handleEvent(ctx, evt)
		}
	}
}

func (p *Platform) handleEvent(ctx context.Context, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeConnected:
		attrs := []any{}
		if p.apiURL != "" {
			attrs = append(attrs, "api_url", p.apiURL)
		}
		if data, ok := evt.Data.(*socketmode.ConnectedEvent); ok {
			attrs = append(attrs, "connection_count", data.ConnectionCount)
		}
		slog.Info("slack: socket mode connected", attrs...)
	case socketmode.EventTypeInteractive:
		p.handleInteractive(evt)
	case socketmode.EventTypeSlashCommand:
		cmd, ok := evt.Data.(slack.SlashCommand)
		if !ok {
			slog.Debug("slack: slash command type assertion failed")
			return
		}
		p.ackSlashCommand(evt.Request)
		if !common.AllowList(p.cfg.AllowFrom, cmd.UserID) {
			return
		}
		if !p.channelAllowed(cmd.ChannelID, inferSlackChannelType(cmd.ChannelID)) {
			return
		}
		content := strings.TrimPrefix(cmd.Command, "/")
		content = "/" + content
		if cmd.Text != "" {
			content += " " + cmd.Text
		}
		direct := isDirectMessageChannel(cmd.ChannelID, "")
		sessionID := agentkit.SessionID(p.buildSessionKey(cmd.ChannelID, cmd.UserID, ""))
		d := delivery{
			channel:          cmd.ChannelID,
			threadTS:         replyThreadTS(direct, "", ""),
			directMessage:    direct,
			sessionID:        sessionID,
			slashResponseURL: cmd.ResponseURL,
		}
		p.deliveries.Store(sessionID, d)
		p.enqueueInbound(ctx, d, cmd.UserID, content, nil, nil, nil, false)
	case socketmode.EventTypeEventsAPI:
		data, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		if evt.Request != nil {
			p.socket.Ack(*evt.Request)
		}
		if data.Type != slackevents.CallbackEvent {
			return
		}
		switch ev := data.InnerEvent.Data.(type) {
		case *slackevents.AppMentionEvent:
			if ev.BotID != "" || ev.User == "" {
				return
			}
			if isSlackMessageOld(ev.TimeStamp) {
				return
			}
			if !common.AllowList(p.cfg.AllowFrom, ev.User) {
				return
			}
			if !p.channelAllowed(ev.Channel, inferSlackChannelType(ev.Channel)) {
				return
			}
			var shareFiles []slackevents.File
			if cb, ok := data.Data.(*slackevents.EventsAPICallbackEvent); ok {
				shareFiles = parseSlackInnerEventFiles(cb.InnerEvent)
			}
			images, audio, files := p.processSlackFileShares(shareFiles)
			text := stripAppMentionText(ev.Text)
			p.onInbound(ctx, ev.Channel, "", ev.User, text, ev.ThreadTimeStamp, ev.TimeStamp, images, audio, files)
		case *slackevents.AssistantThreadStartedEvent:
			_ = p.client.SetAssistantThreadsStatus(slack.AssistantThreadsSetStatusParameters{
				ChannelID: ev.AssistantThread.ChannelID,
				ThreadTS:  ev.AssistantThread.ThreadTimeStamp,
				Status:    "",
			})
		case *slackevents.MessageEvent:
			if ev.BotID != "" || ev.User == "" {
				return
			}
			if isSlackMessageOld(ev.TimeStamp) {
				return
			}
			if !common.AllowList(p.cfg.AllowFrom, ev.User) {
				return
			}
			if !p.channelAllowed(ev.Channel, ev.ChannelType) {
				return
			}
			if !p.shouldProcessChannelMessage(ev.ChannelType, false) {
				return
			}
			images, audio, files := p.processSlackFileShares(messageEventFiles(ev))
			p.onInbound(ctx, ev.Channel, ev.ChannelType, ev.User, ev.Text, ev.ThreadTimeStamp, ev.TimeStamp, images, audio, files)
		}
	default:
		slog.Debug("slack: socket mode event", "type", evt.Type)
	}
}

func isDirectMessage(channelType string) bool {
	switch channelType {
	case "im", "mim":
		return true
	default:
		return false
	}
}

func (p *Platform) onInbound(ctx context.Context, channel, channelType, user, text, eventThreadTS, msgTS string, images []common.ImageAttachment, audio *common.AudioAttachment, files []common.FileAttachment) {
	text = strings.TrimSpace(text)
	if text == "" && len(images) == 0 && audio == nil && len(files) == 0 {
		return
	}
	direct := isDirectMessageChannel(channel, channelType)
	sessionID := agentkit.SessionID(p.buildSessionKey(channel, user, deliveryThreadTS(eventThreadTS)))
	d := delivery{
		channel:       channel,
		threadTS:      replyThreadTS(direct, eventThreadTS, msgTS),
		msgTS:         msgTS,
		directMessage: direct,
		sessionID:     sessionID,
	}
	p.deliveries.Store(sessionID, d)
	p.enqueueInbound(ctx, d, user, text, images, audio, files, true)
}

func (p *Platform) enqueueInbound(ctx context.Context, d delivery, user, text string, images []common.ImageAttachment, audio *common.AudioAttachment, files []common.FileAttachment, react bool) {
	outcome, err := common.ProcessSlash(ctx, p.commands, common.SlashContext{
		DeliverySessionID: d.sessionID,
		PlatformID:        "slack",
		SessionScope:      p.sessionScope,
		UserID:            user,
	}, text)
	if err != nil {
		_ = p.replyText(ctx, d, fmt.Sprintf("命令执行失败: %v", err))
		return
	}
	switch outcome.Kind {
	case common.SlashHandled:
		if outcome.Reply != "" {
			_ = p.replyText(ctx, d, formatSlashReply(text, outcome.Reply))
		}
		return
	case common.SlashForward:
		if outcome.Reply != "" {
			_ = p.replyText(ctx, d, outcome.Reply)
		}
	case common.SlashNotCommand:
	}

	if react {
		p.reactReceived(ctx, d)
	}
	event := common.InboundFromContent(p.agentID, d.sessionID, "slack", user, text, "", images, files, audio, nil, common.InboundOptsFor(p.workspace))
	_ = p.inbox.Push(ctx, event)
}

func (p *Platform) replyText(ctx context.Context, d delivery, text string) error {
	if d.slashResponseURL != "" {
		_, _, err := p.client.PostMessageContext(ctx, "",
			slack.MsgOptionResponseURL(d.slashResponseURL, slack.ResponseTypeInChannel),
			slack.MsgOptionText(text, false),
		)
		if err != nil {
			return fmt.Errorf("slack: slash command response: %w", err)
		}
		return nil
	}
	return p.sendText(ctx, d.sessionID, text)
}

func (p *Platform) ackSlashCommand(req *socketmode.Request) {
	if req == nil || p.socket == nil {
		return
	}
	payload := map[string]string{
		"response_type": slack.ResponseTypeEphemeral,
		"text":          "\u200b",
	}
	p.socket.Ack(*req, payload)
}

func (p *Platform) sendText(ctx context.Context, sessionID agentkit.SessionID, text string) error {
	d, ok := p.deliveryForSend(sessionID)
	if !ok {
		return fmt.Errorf("slack: unknown session %s", sessionID)
	}
	text = common.MarkdownToSlackMrkdwn(text)
	opts := []slack.MsgOption{slack.MsgOptionText(text, false)}
	if d.replyInThread() {
		opts = append(opts, slack.MsgOptionPostMessageParameters(slack.PostMessageParameters{
			ThreadTimestamp: d.threadTS,
		}))
	}
	_, _, err := p.client.PostMessageContext(ctx, d.channel, opts...)
	if err != nil {
		return fmt.Errorf("slack: post message: %w", err)
	}
	return nil
}

func (p *Platform) deliveryForSend(sessionID agentkit.SessionID) (delivery, bool) {
	if raw, ok := p.deliveries.Load(sessionID); ok {
		return raw.(delivery), true
	}
	d, err := parseSessionKey(string(sessionID))
	if err != nil {
		return delivery{}, false
	}
	return d, true
}

func (p *Platform) buildSessionKey(channel, user, threadTS string) string {
	return string(session.BuildDeliverySessionID("slack", channel, threadTS, user))
}

func threadRootTS(threadTS, msgTS string) string {
	if threadTS != "" {
		return threadTS
	}
	return msgTS
}

// deliveryThreadTS is the thread segment for session keys. Only real Slack
// thread timestamps are included so /new active-session mappings stay stable
// across top-level DM and channel messages.
func deliveryThreadTS(eventThreadTS string) string {
	return strings.TrimSpace(eventThreadTS)
}

func formatSlashReply(command, reply string) string {
	name, _, ok := common.ParseSlashCommand(command)
	if !ok || name != "new" {
		return reply
	}
	if strings.Contains(reply, ":new:") {
		return "已开始新会话。"
	}
	return reply
}

func stripBotMention(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	if strings.HasPrefix(fields[0], "<@") {
		fields = fields[1:]
	}
	return strings.TrimSpace(strings.Join(fields, " "))
}

func parseSessionKey(key string) (delivery, error) {
	parts := session.ParseDelivery(agentkit.SessionID(key), "")
	if !parts.Routable || parts.Platform != "slack" {
		return delivery{}, fmt.Errorf("slack: invalid session key %q", key)
	}
	return delivery{
		channel:   parts.Channel,
		threadTS:  parts.Thread,
		sessionID: agentkit.SessionID(key),
	}, nil
}

// ReconstructDelivery parses a cc-connect style slack session key for proactive sends.
func ReconstructDelivery(sessionKey string) (delivery, error) {
	return parseSessionKey(sessionKey)
}

var (
	_ agentkit.Platform  = (*Platform)(nil)
	_ permission.Capable = (*Platform)(nil)
)
