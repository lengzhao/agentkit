package feishu

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/ingress"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/derive"
)

const ingressPolicyTimeout = 2 * time.Second

func (p *Platform) evaluateIngress(ctx context.Context, in ingress.Input) ingress.Plan {
	if p.inboundPolicy == nil {
		return in.Default
	}
	ctx, cancel := context.WithTimeout(ctx, ingressPolicyTimeout)
	defer cancel()
	decision, err := p.inboundPolicy.Evaluate(ctx, in)
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		var plan ingress.Plan
		plan, err = ingress.Resolve(in, decision)
		if err == nil {
			slog.Debug(p.tag()+": ingress decision", "kind", in.Kind, "id", in.ID, "agent", plan.Agent, "handlers", plan.Handlers, "reason", decision.Reason)
			return plan
		}
	}
	slog.Warn(p.tag()+": ingress policy failed", "kind", in.Kind, "id", in.ID, "error", err)
	return ingress.Plan{Agent: ingress.Skip, Rejected: true}
}

func (p *Platform) ingressEnvelope(ctx context.Context, rc replyContext, user string) (agentkit.TurnEnvelope, context.Context, error) {
	msg := inboundMessage{sessionID: agentkit.SessionID(rc.sessionKey), userID: user, rctx: rc}
	route := rctx.BuildSessionRoute(p.inboundRoute(msg))
	policy := rctx.RoutePolicyForPlatform(p.tag(), rctx.DefaultRoutePolicy(p.sessionScope))
	env := rctx.ResolveEnvelope(agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Route: route, Actor: agentkit.ActorRef{UserID: user}}}, policy)
	ctx = rctx.ApplyEnvelopeToContext(ctx, env)
	owner, err := capsession.ResolveActiveSessionID(ctx, p.sessionStore, rctx.ActiveEntryKey(route, policy, user))
	env.Conversation = string(owner)
	return env, rctx.ApplyEnvelopeToContext(ctx, env), err
}

func (p *Platform) ingressContext(ctx context.Context, rc replyContext, user string) (agentkit.SessionID, ingress.ContextState) {
	env, ctx, err := p.ingressEnvelope(ctx, rc, user)
	owner := agentkit.SessionID(env.Conversation)
	if err != nil || owner == "" || p.sessionStore == nil {
		return owner, ingress.Unknown
	}
	p.ingressMu.Lock()
	accepted := p.ingressAccepted[owner]
	p.ingressMu.Unlock()
	if !accepted.IsZero() && time.Since(accepted) < time.Hour {
		return owner, ingress.Existing
	}
	session, err := p.sessionStore.Get(ctx, owner)
	if err != nil {
		return owner, ingress.Unknown
	}
	messages, err := session.DeriveMessages(ctx)
	if err != nil {
		return owner, ingress.Unknown
	}
	if derive.HasUserInstruction(messages) {
		return owner, ingress.Existing
	}
	return owner, ingress.New
}
func (p *Platform) rememberIngressText(owner agentkit.SessionID) {
	if owner == "" {
		return
	}
	p.ingressMu.Lock()
	defer p.ingressMu.Unlock()
	if p.ingressAccepted == nil {
		p.ingressAccepted = make(map[agentkit.SessionID]time.Time)
	}
	if len(p.ingressAccepted) > 1024 {
		for id, at := range p.ingressAccepted {
			if time.Since(at) > time.Hour {
				delete(p.ingressAccepted, id)
			}
		}
	}
	p.ingressAccepted[owner] = time.Now()
}

// ingressExecutor is lazy so unit tests and embedded hosts can construct a
// Platform directly. It owns no idle goroutines and is bound to run's lifetime.
func (p *Platform) ingressExecutor() *common.KeyedExecutor {
	p.ingressOnce.Do(func() { p.ingressWork = common.NewKeyedExecutor(8, 256) })
	return p.ingressWork
}

// queueIngress preserves receive order across download IO without blocking SDK
// ACKs. A full queue is an admission failure, not an accepted business operation.
func (p *Platform) queueIngress(key string, work func(context.Context)) error {
	return p.ingressExecutor().Submit(key, work)
}

func (p *Platform) normalMessageTrigger(kind, content, session string, mentions []*larkim.MentionEvent, chatType string) bool {
	if chatType != "group" || p.botOpenID == "" || isBotMentioned(mentions, p.botOpenID) {
		return true
	}
	return p.respondToAtEveryoneAndHere && strings.Contains(content, "@_all") || p.allowsUnmentionedGroupMessage(kind, session)
}
func instructionText(kind, content string, mentions []*larkim.MentionEvent, bot string) string {
	if kind == "text" {
		var v struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal([]byte(content), &v)
		return strings.TrimSpace(stripMentions(v.Text, mentions, bot))
	}
	if kind != "post" {
		return ""
	}
	// Match supported rich-text body tags without downloading images or resolving users.
	var raw any
	_ = json.Unmarshal([]byte(content), &raw)
	var text []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if title, ok := x["title"].(string); ok && title != "" {
				text = append(text, title)
			}
			tag, _ := x["tag"].(string)
			if tag == "text" || tag == "a" || tag == "markdown" || tag == "code_block" {
				if s, ok := x["text"].(string); ok {
					text = append(text, s)
				}
			}
			for _, v := range x {
				walk(v)
			}
		case []any:
			for _, v := range x {
				walk(v)
			}
		}
	}
	walk(raw)
	return strings.TrimSpace(stripMentions(strings.Join(text, "\n"), mentions, bot))
}

func (p *Platform) dispatchMessage(ctx context.Context, kind, content string, mentions []*larkim.MentionEvent, messageID, sessionKey, userID, chatID string, rc replyContext, parentID string) {
	if !common.AllowList(p.allowFrom, userID) || rc.chatType == "group" && !common.AllowList(p.allowChat, chatID) || rc.chatType != "group" && p.groupOnly {
		return
	}
	text := instructionText(kind, content, mentions, p.botOpenID)
	// Slash commands must not claim a pending upload or silently resume another topic.
	command := strings.HasPrefix(text, "/")
	if p.inboundPolicy != nil {
		p.pruneDeferredAttachments(ctx, rc, userID)
	}
	if p.inboundPolicy != nil && (text != "" || isAttachmentMsgType(kind)) && !command {
		top := p.cfg.DMThreadMode && rc.chatType == "p2p" && parentID == "" && rctx.ParseDelivery(agentkit.SessionID(sessionKey), userID).Thread == messageID
		target, found, ambiguous := p.deferredRoute(rc, userID, top)
		if ambiguous && text != "" {
			if p.client != nil {
				_ = p.sendIMContent(ctx, rc, "有多个待处理的文件话题，请回到对应话题说明要求。")
			}
			return
		}
		if found {
			rc = target
			sessionKey = rc.sessionKey
		}
	}
	normal := p.normalMessageTrigger(kind, content, sessionKey, mentions, rc.chatType)
	triggered := normal || rc.chatType == "group" && p.groupReplyAll
	defaultAction := ingress.Skip
	if triggered {
		defaultAction = ingress.Submit
	}
	owner, state := agentkit.SessionID(""), ingress.Unknown
	if p.inboundPolicy != nil {
		owner, state = p.ingressContext(ctx, rc, userID)
	}
	chatKind := "unknown"
	if rc.chatType == "group" {
		chatKind = "group"
	} else if rc.chatType == "p2p" {
		chatKind = "direct"
	}
	var mentioned *bool
	if p.botOpenID != "" {
		v := isBotMentioned(mentions, p.botOpenID)
		mentioned = &v
	}
	facts := &ingress.MessageFacts{AttachmentOnly: isAttachmentMsgType(kind) || kind == "post" && text == "" && postHasImage(content), HasText: text != "", Mentioned: mentioned, GroupReplyAll: p.groupReplyAll, AddressedToAgent: normal}
	plan := p.evaluateIngress(ctx, ingress.Input{Platform: p.tag(), InstanceID: p.PlatformInstance(), Kind: ingress.Message, Type: kind, ID: messageID, ChatID: chatID, UserID: userID, ConversationKind: chatKind, ThreadID: rctx.ParseDelivery(agentkit.SessionID(sessionKey), userID).Thread, SessionID: string(owner), Context: state, Message: facts, Default: ingress.Plan{Agent: defaultAction}, Raw: json.RawMessage(content)})
	if plan.Agent == ingress.Skip {
		return
	}
	rc.ingressOwner = owner
	if p.inboundPolicy != nil {
		rc.ingressAction = plan.Agent
	}
	// Pure-image posts use the same attachment path when deferred.
	if plan.Agent == ingress.Defer && kind == "post" {
		_, images := p.parsePostContent(ctx, messageID, content)
		for _, img := range images {
			if err := p.deferAttachment(ctx, rc, userID, owner, img.Data, img.MimeType, img.FileName); err != nil {
				slog.Warn(p.tag()+": defer image post failed", "error", err)
			}
		}
		return
	}
	p.markThreadSessionActive(sessionKey)
	p.dispatchMessageContent(ctx, kind, content, mentions, messageID, sessionKey, userID, chatID, rc, parentID)
}

func postHasImage(content string) bool {
	var value any
	if json.Unmarshal([]byte(content), &value) != nil {
		return false
	}
	var image func(any) bool
	image = func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			if x["tag"] == "img" {
				key, _ := x["image_key"].(string)
				if key != "" {
					return true
				}
			}
			for _, v := range x {
				if image(v) {
					return true
				}
			}
		case []any:
			for _, v := range x {
				if image(v) {
					return true
				}
			}
		}
		return false
	}
	return image(value)
}
