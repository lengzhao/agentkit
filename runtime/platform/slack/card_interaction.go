package slack

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/interaction"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
	slackapi "github.com/slack-go/slack"
)

func slackInteractionSnapshot(cb slackapi.InteractionCallback, a *slackapi.BlockAction, session, channel, message, thread, action string, payload ...json.RawMessage) interaction.Event {
	raw, _ := json.Marshal(cb)
	if len(payload) > 0 && json.Valid(payload[0]) {
		raw = append([]byte(nil), payload[0]...)
	}
	// Slack forms are part of actual operation identity, not just the button value.
	rawAction, _ := json.Marshal(struct {
		Action    *slackapi.BlockAction       `json:"action"`
		State     *slackapi.BlockActionStates `json:"state,omitempty"`
		ViewState *slackapi.ViewState         `json:"view_state,omitempty"`
	}{a, cb.BlockActionState, cb.View.State})
	e := interaction.Event{EventID: cb.TriggerID, Platform: "slack", Type: string(cb.Type), UserID: cb.User.ID, ChatID: channel, MessageID: message, ThreadID: thread, SessionID: session, Action: action, RawCallback: raw, RawAction: rawAction, CardStatus: interaction.CardUnavailable}
	if e.EventID == "" && a != nil {
		e.EventID = a.ActionTs
	}
	add := func(a slackapi.BlockAction) {
		answer := func(value any, label string) {
			e.Answers = append(e.Answers, interaction.Answer{Control: a.ActionID, Value: value, Label: label})
		}
		if a.Value != "" || a.Type == slackapi.ActionType("plain_text_input") {
			answer(a.Value, "")
		}
		if a.SelectedOption.Value != "" {
			label := ""
			if a.SelectedOption.Text != nil {
				label = a.SelectedOption.Text.Text
			}
			answer(a.SelectedOption.Value, label)
		}
		for _, o := range a.SelectedOptions {
			label := ""
			if o.Text != nil {
				label = o.Text.Text
			}
			answer(o.Value, label)
		}
		// These controls do not use selected_option.
		for _, v := range []string{a.SelectedUser, a.SelectedChannel, a.SelectedConversation, a.SelectedDate, a.SelectedTime} {
			if v != "" {
				answer(v, "")
			}
		}
		for _, list := range [][]string{a.SelectedUsers, a.SelectedChannels, a.SelectedConversations} {
			for _, v := range list {
				answer(v, "")
			}
		}
		if a.SelectedDateTime != 0 {
			answer(a.SelectedDateTime, "")
		}
	}
	if a != nil {
		add(*a)
	}
	addState := func(values map[string]map[string]slackapi.BlockAction) {
		blocks := make([]string, 0, len(values))
		for k := range values {
			blocks = append(blocks, k)
		}
		sort.Strings(blocks)
		for _, block := range blocks {
			ids := make([]string, 0, len(values[block]))
			for id := range values[block] {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				v := values[block][id]
				v.ActionID = id
				v.BlockID = block
				add(v)
			}
		}
	}
	if cb.BlockActionState != nil {
		addState(cb.BlockActionState.Values)
	}
	if cb.View.State != nil {
		addState(cb.View.State.Values)
	}
	if len(cb.Message.Blocks.BlockSet) > 0 || cb.Message.Text != "" {
		e.RawCard, _ = json.Marshal(cb.Message)
		e.CardText = extractSlackBlockMessageText(cb.Message)
		e.CardStatus = interaction.CardAvailable
	}
	if cb.View.ID != "" {
		e.InteractionID = cb.View.ID
		e.RawCard, _ = json.Marshal(cb.View)
		e.CardStatus = interaction.CardAvailable
	}
	// Preserve unknown fields from the original Socket Mode payload as well.
	if len(payload) > 0 && json.Valid(payload[0]) {
		var original struct {
			Actions []json.RawMessage `json:"actions"`
			State   json.RawMessage   `json:"state"`
			Message json.RawMessage   `json:"message"`
			View    json.RawMessage   `json:"view"`
		}
		if json.Unmarshal(payload[0], &original) == nil {
			var selected json.RawMessage
			for _, raw := range original.Actions {
				var v slackapi.BlockAction
				if json.Unmarshal(raw, &v) == nil && a != nil && v.ActionID == a.ActionID && v.BlockID == a.BlockID && v.ActionTs == a.ActionTs {
					selected = raw
					break
				}
			}
			var view struct {
				State json.RawMessage `json:"state"`
			}
			_ = json.Unmarshal(original.View, &view)
			if selected != nil || original.State != nil || view.State != nil {
				e.RawAction, _ = json.Marshal(struct {
					Action    json.RawMessage `json:"action"`
					State     json.RawMessage `json:"state"`
					ViewState json.RawMessage `json:"view_state,omitempty"`
				}{selected, original.State, view.State})
			}
			if cb.View.ID != "" && len(original.View) > 0 {
				e.RawCard = original.View
			} else if len(original.Message) > 0 && string(original.Message) != "null" && e.CardStatus == interaction.CardAvailable {
				e.RawCard = original.Message
			}
		}
	}

	return e
}

func (p *Platform) processBlockInteraction(ctx context.Context, e interaction.Event) error {
	return common.ProcessInteraction(ctx, e, common.InteractionProcessing{
		Mode: p.unknownCardAction, Handlers: p.interactionHandlers,
		Allowed: func(_ context.Context, e interaction.Event) bool {
			return common.AllowList(p.cfg.AllowFrom, e.UserID) && ((e.InteractionID != "" && e.ChatID == e.UserID) || p.channelAllowed(e.ChatID, inferSlackChannelType(e.ChatID)))
		},
		Enrich: func(ctx context.Context, e *interaction.Event) {
			if e.CardStatus == interaction.CardAvailable {
				return
			}
			queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			ctx = queryCtx
			if p.client == nil || e.ChatID == "" || e.MessageID == "" {
				e.CardError = "original message context or client unavailable"
				return
			}
			var messages []slackapi.Message
			var err error
			if e.ThreadID != "" {
				messages, _, _, err = p.client.GetConversationRepliesContext(ctx, &slackapi.GetConversationRepliesParameters{ChannelID: e.ChatID, Timestamp: e.ThreadID, Latest: e.MessageID, Oldest: e.MessageID, Inclusive: true, Limit: 1})
			} else {
				var resp *slackapi.GetConversationHistoryResponse
				resp, err = p.client.GetConversationHistoryContext(ctx, &slackapi.GetConversationHistoryParameters{ChannelID: e.ChatID, Latest: e.MessageID, Oldest: e.MessageID, Inclusive: true, Limit: 1})
				if resp != nil {
					messages = resp.Messages
				}
			}
			if err != nil {
				e.CardStatus = interaction.CardFailed
				e.CardError = err.Error()
				slog.Warn("slack: card query failed", "message_id", e.MessageID, "error", err)
				return
			}
			for _, m := range messages {
				if m.Timestamp == e.MessageID {
					e.RawCard, _ = json.Marshal(m)
					e.CardText = extractSlackBlockMessageText(m)
					e.CardStatus = interaction.CardAvailable
					return
				}
			}
			e.CardError = "original message not returned"
		},
		Builtin: func(ctx context.Context, e interaction.Event) bool {
			d := interactionDelivery(e)
			p.deliveries.Store(d.sessionID, d)
			return common.ProcessInteractionCommand(ctx, e, common.InteractionCommand{
				Routing: p.cfg.AgentRoutingConfig, Commands: p.commands,
				Slash: common.SlashContext{Route: rctx.BuildSessionRoute(d.inboundRoute(e.UserID)), SessionScope: p.sessionScope, UserID: e.UserID},
				Metadata: func(ctx context.Context, text string) map[string]any {
					return p.inboundMetadataContext(ctx, e.UserID, text)
				},
				Reply: func(ctx context.Context, text string) error {
					if p.client == nil {
						return nil
					}
					return p.replyText(ctx, d, text)
				},
			})
		},
		Forward: func(ctx context.Context, e interaction.Event) error {
			d := interactionDelivery(e)
			p.deliveries.Store(d.sessionID, d)
			event := common.InboundFromContent(p.agentID, d.inboundRoute(e.UserID), e.UserID, common.FormatInteraction(e), "", nil, nil, nil, nil, common.InboundOptsFor(p.workspace))
			metadata := common.InteractionMetadata(ctx, func(ctx context.Context) map[string]any { return p.userProfileMetadataContext(ctx, e.UserID) })
			return p.inbox.Push(ctx, common.WithMetadata(event, metadata))
		},
	})
}
func interactionDelivery(e interaction.Event) delivery {
	direct := isDirectMessageChannel(e.ChatID, inferSlackChannelType(e.ChatID))
	if e.InteractionID != "" && e.ChatID == e.UserID {
		direct = true
		e.ThreadID = ""
	}
	return delivery{channel: e.ChatID, threadTS: replyThreadTS(direct, e.ThreadID, e.MessageID), msgTS: e.MessageID, directMessage: direct, sessionID: agentkit.SessionID(e.SessionID)}
}

// Modal forms carry their original delivery route in private_metadata as the
// same action-value JSON used on buttons. Without it, use a per-view DM session
// rather than attributing a submission to an arbitrary channel.
func (p *Platform) slackViewRoute(cb slackapi.InteractionCallback) (session, channel, thread string) {
	_, session, _ = decodeActionValue(cb.View.PrivateMetadata)
	if d, err := parseSessionKey(session); err == nil {
		parts := rctx.ParseDelivery(d.sessionID, "")
		if parts.User == "" || parts.User == cb.User.ID {
			return session, d.channel, d.threadTS
		}
	}
	channel = cb.User.ID
	return p.buildSessionKey(channel, cb.User.ID, "view-"+cb.View.ID), channel, ""
}
func (p *Platform) handleViewSubmission(cb slackapi.InteractionCallback, payload ...json.RawMessage) {
	session, channel, thread := p.slackViewRoute(cb)
	e := slackInteractionSnapshot(cb, nil, session, channel, "", thread, cb.View.CallbackID, payload...)
	if strings.HasPrefix(e.Action, "perm:") {
		return
	}
	if !common.AllowList(p.cfg.AllowFrom, e.UserID) {
		slog.Info("slack: interaction permission filtered", "user", e.UserID)
		return
	}
	complete, duplicate := p.interactionDedup.Begin(common.InteractionEventKey(e))
	if duplicate {
		slog.Info("slack: duplicate interaction ignored", "view_id", cb.View.ID)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		complete(p.processBlockInteraction(ctx, e))
	}()
}
