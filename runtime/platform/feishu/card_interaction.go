package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/interaction"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func feishuInteractionSnapshot(ev *callback.CardActionTriggerEvent, session, chat, message, user, action string) interaction.Event {
	raw, _ := json.Marshal(struct {
		Event *callback.CardActionTriggerRequest `json:"event"`
	}{ev.Event})
	if ev.EventReq != nil && json.Valid(ev.EventReq.Body) {
		raw = append([]byte(nil), ev.EventReq.Body...)
	}
	rawAction, _ := json.Marshal(ev.Event.Action)
	var envelope struct {
		Header struct {
			EventID string `json:"event_id"`
		} `json:"header"`
		Event struct {
			Action json.RawMessage `json:"action"`
		} `json:"event"`
	}
	if json.Unmarshal(raw, &envelope) == nil && len(envelope.Event.Action) > 0 {
		rawAction = envelope.Event.Action
	}
	e := interaction.Event{Platform: "feishu", Type: "card.action.trigger", UserID: user, ChatID: chat, MessageID: message, SessionID: session, Action: action, RawCallback: raw, RawAction: rawAction, CardStatus: interaction.CardUnavailable}
	e.EventID = envelope.Header.EventID
	if e.EventID == "" && ev.EventV2Base != nil && ev.EventV2Base.Header != nil {
		e.EventID = ev.EventV2Base.Header.EventID
	}
	var a callback.CallBackAction
	decoder := json.NewDecoder(bytes.NewReader(rawAction))
	decoder.UseNumber()
	_ = decoder.Decode(&a)
	var add func(string, any)
	add = func(control string, v any) {
		switch x := v.(type) {
		case []any:
			if len(x) > 0 {
				for _, item := range x {
					add(control, item)
				}
				return
			}
		case map[string]any:
			if opts, ok := x["options"]; ok {
				add(control, opts)
				return
			}
			if value, ok := x["value"]; ok {
				add(control, value)
				return
			}
		}
		e.Answers = append(e.Answers, interaction.Answer{Control: control, Value: v})
	}

	if a.Option != "" {
		add(a.Name, a.Option)
	}
	for _, opt := range a.Options {
		add(a.Name, opt)
	}
	if a.Tag == "input" || a.InputValue != "" {
		add(a.Name, a.InputValue)
	}
	if a.Tag == "checker" || a.Tag == "checkbox" {
		add(a.Name, a.Checked)
	}
	if len(a.Value) > 0 {
		add(a.Name, a.Value)
	}
	keys := make([]string, 0, len(a.FormValue))
	for k := range a.FormValue {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		add(k, a.FormValue[k])
	}
	normalizeFeishuInteraction(&e)
	return e
}

type cardMessageSnapshot struct {
	raw                            json.RawMessage
	text, rootID, threadID, chatID string
	status                         interaction.CardStatus
	err                            string
}

func (p *Platform) fetchCardMessage(ctx context.Context, id string) cardMessageSnapshot {
	result := cardMessageSnapshot{status: interaction.CardUnavailable}
	if id == "" {
		result.err = "missing message ID"
		return result
	}
	if p.client == nil {
		result.err = "message client unavailable"
		return result
	}
	req := larkim.NewGetMessageReqBuilder().MessageId(id).UserIdType(larkim.UserIdTypeOpenId).CardMsgContentType("user_card_content").Build()
	var resp *larkim.GetMessageResp
	err := p.withTransientRetry(ctx, "get card message", func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "get card message", func(client *lark.Client, opts ...larkcore.RequestOptionFunc) error {
			var err error
			resp, err = client.Im.Message.Get(ctx, req, opts...)
			if err != nil {
				return err
			}
			if resp == nil {
				return fmt.Errorf("nil message response")
			}
			if !resp.Success() {
				return fmt.Errorf("get message code=%d: %s", resp.Code, resp.Msg)
			}
			return nil
		})
	})
	if err != nil {
		result.status = interaction.CardFailed
		result.err = err.Error()
		slog.Warn(p.tag()+": card query failed", "message_id", id, "error", err)
		return result
	}
	if resp.Data == nil || len(resp.Data.Items) == 0 || resp.Data.Items[0] == nil {
		result.err = "original message not returned"
		return result
	}
	item := resp.Data.Items[0]
	result.rootID = stringValue(item.RootId)
	result.threadID = stringValue(item.ThreadId)
	result.chatID = stringValue(item.ChatId)
	if item.Body == nil || item.Body.Content == nil || strings.TrimSpace(*item.Body.Content) == "" {
		result.err = "original message content unavailable"
		return result
	}
	content := *item.Body.Content
	if json.Valid([]byte(content)) {
		result.raw = json.RawMessage(content)
	} else {
		result.raw, _ = json.Marshal(content)
	}
	result.text = p.extractHistoryText(stringValue(item.MsgType), content, item.Mentions)
	result.status = interaction.CardAvailable
	return result
}

func (p *Platform) processCardInteraction(e interaction.Event) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	e.Platform = p.tag()
	chatType := ""
	validRoute := true
	ttl, _ := common.ParseCardSubmissionTTL(p.cfg.CardSubmissionTTL)
	return common.ProcessInteraction(ctx, e, common.InteractionProcessing{
		Submissions: &p.cardSubmissions, SubmissionTTL: ttl, CloseCard: p.closeInteractionCard, NotifyUser: p.notifyInteractionUser,
		Mode: p.unknownCardAction, Handlers: p.interactionHandlers,
		Enrich: func(ctx context.Context, e *interaction.Event) {
			chatType, validRoute = p.enrichCardInteraction(ctx, e)
			normalizeFeishuInteraction(e)
		},
		Allowed: func(_ context.Context, e interaction.Event) bool {
			return validRoute && common.AllowList(p.allowChat, e.ChatID)
		},
		Builtin: func(ctx context.Context, e interaction.Event) bool {
			// RefreshCard is used by native navigation/action implementations.
			if e.MessageID != "" && (strings.HasPrefix(e.Action, "nav:") || strings.HasPrefix(e.Action, "act:")) {
				p.cardActionMsgMu.Lock()
				if p.cardActionMsgIDs == nil {
					p.cardActionMsgIDs = make(map[string]string)
				}
				p.cardActionMsgIDs[e.SessionID] = e.MessageID
				p.cardActionMsgMu.Unlock()
			}
			if e.Action == "" {
				return false
			}
			if strings.HasPrefix(e.Action, "act:/delete-mode toggle ") {
				return true
			}
			msg := inboundMessage{sessionID: agentkit.SessionID(e.SessionID), messageID: e.MessageID, userID: e.UserID, rctx: replyContext{messageID: e.MessageID, chatID: e.ChatID, chatType: chatType, sessionKey: e.SessionID}}
			p.storeDelivery(msg.sessionID, msg.rctx)
			return common.ProcessInteractionCommand(ctx, e, common.InteractionCommand{
				Routing: p.cfg.AgentRoutingConfig, Commands: p.commands,
				Slash:    common.SlashContext{Route: rctx.BuildSessionRoute(msg.inboundRoute(p.tag())), SessionScope: p.sessionScope, UserID: e.UserID},
				Metadata: func(ctx context.Context, _ string) map[string]any { return p.userProfileMetadataContext(ctx, e.UserID) },
				Reply: func(ctx context.Context, text string) error {
					if p.client == nil {
						return nil
					}
					return p.sendText(ctx, msg.sessionID, text)
				},
			})
		},
		Forward: func(ctx context.Context, e interaction.Event) error {
			msg := inboundMessage{sessionID: agentkit.SessionID(e.SessionID), messageID: e.MessageID, userID: e.UserID, rctx: replyContext{messageID: e.MessageID, chatID: e.ChatID, chatType: chatType, sessionKey: e.SessionID}}
			p.storeDelivery(msg.sessionID, msg.rctx)
			event := common.InboundFromContent(p.agentID, msg.inboundRoute(p.tag()), e.UserID, common.FormatInteraction(e), "", nil, nil, nil, nil, common.InboundOptsFor(p.workspace))
			metadata := common.InteractionMetadata(ctx, func(ctx context.Context) map[string]any { return p.userProfileMetadataContext(ctx, e.UserID) })
			return p.inbox.Push(ctx, enrichInboundActorFromProfile(common.WithMetadata(event, metadata)))
		},
	})
}

// enrichCardInteraction restores a verified card delivery route. Query work has
// a smaller budget than the worker, so a query timeout cannot cancel forwarding.
func (p *Platform) enrichCardInteraction(ctx context.Context, e *interaction.Event) (string, bool) {
	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	candidate := e.SessionID
	chatType := ""
	p.deliveries.Range(func(key, value any) bool {
		rc, ok := value.(replyContext)
		if !ok || rc.messageID != e.MessageID || rc.chatID != e.ChatID {
			return true
		}
		parts := rctx.ParseDelivery(agentkit.SessionID(fmt.Sprint(key)), "")
		if parts.User != "" && parts.User != e.UserID {
			return true
		}
		chatType = rc.chatType
		if candidate == "" {
			candidate = fmt.Sprint(key)
		}
		return false
	})
	card := p.fetchCardMessage(queryCtx, e.MessageID)
	e.CardStatus = card.status
	e.CardError = card.err
	e.RawCard = card.raw
	e.CardText = card.text
	if card.chatID != "" && e.ChatID != e.UserID && card.chatID != e.ChatID {
		return chatType, false
	}
	if card.chatID != "" {
		e.ChatID = card.chatID
	}
	if card.rootID != "" || card.threadID != "" {
		chatType = "group"
	}
	if chatType == "" && p.client != nil && e.ChatID != "" {
		resp, err := p.client.Im.Chat.Get(queryCtx, larkim.NewGetChatReqBuilder().ChatId(e.ChatID).Build())
		if err == nil && resp != nil && resp.Success() && resp.Data != nil {
			switch stringValue(resp.Data.ChatMode) {
			case "p2p":
				chatType = "p2p"
			case "group", "topic":
				chatType = "group"
			}
		}
	}
	parts := rctx.ParseDelivery(agentkit.SessionID(candidate), "")
	matchesChat := parts.Channel == e.ChatID || (chatType == "p2p" && parts.Channel == e.UserID)
	matchesThread := !p.threadIsolation || card.rootID == "" || parts.Thread == card.rootID
	if candidate != "" && parts.Routable && parts.Platform == p.tag() && matchesChat && matchesThread && (parts.User == "" || parts.User == e.UserID) {
		e.SessionID = candidate
		e.RoutingStatus = "restored"
	} else {
		var msg *larkim.EventMessage
		if chatType == "group" {
			msg = &larkim.EventMessage{RootId: &card.rootID, ThreadId: &card.threadID, MessageId: &e.MessageID, ChatType: &chatType}
		}
		e.SessionID = p.makeSessionKey(msg, e.ChatID, e.UserID)
		e.RoutingStatus = "message_context"
		if p.threadIsolation && chatType == "" && e.MessageID != "" {
			e.SessionID = string(rctx.BuildDeliverySessionID(p.tag(), e.ChatID, e.MessageID, e.UserID))
			e.RoutingStatus = "isolated_card_fallback"
		}
	}
	e.ThreadID = rctx.ParseDelivery(agentkit.SessionID(e.SessionID), "").Thread
	if e.CardStatus != interaction.CardAvailable {
		slog.Info(p.tag()+": original card unavailable", "message_id", e.MessageID, "status", e.CardStatus, "reason", e.CardError)
	}
	return chatType, true
}
