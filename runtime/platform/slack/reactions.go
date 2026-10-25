package slack

import (
	"context"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/slack-go/slack"
)

const (
	reactionReceived  = "eyes"
	reactionDone      = "white_check_mark"
	reactionCancelled = "broken_heart"
	reactionError     = "x"
	// reactionSteer replaces reactionReceived when a message is folded into a
	// running turn. Deliberately outside the typing-progress set
	// (eyes/hourglass_flowing_sand/gear/bulb/rocket/zap) so the two cannot
	// collide on the same message.
	reactionSteer = "inbox_tray"
)

func isDirectMessageChannel(channel, channelType string) bool {
	if isDirectMessage(channelType) {
		return true
	}
	// app_mention events omit channel_type; Slack DM channel IDs start with "D".
	return strings.HasPrefix(channel, "D")
}

func replyThreadTS(direct bool, eventThreadTS, msgTS string) string {
	if direct {
		if eventThreadTS != "" {
			return eventThreadTS
		}
		return ""
	}
	return threadRootTS(eventThreadTS, msgTS)
}

func (d delivery) replyInThread() bool {
	return d.threadTS != "" && !d.directMessage
}

func (p *Platform) addReaction(ctx context.Context, d delivery, name string) {
	if p.client == nil || d.channel == "" || d.msgTS == "" {
		return
	}
	err := p.client.AddReactionContext(ctx, name, slack.ItemRef{
		Channel:   d.channel,
		Timestamp: d.msgTS,
	})
	if err != nil {
		slog.Debug("slack: add reaction failed", "emoji", name, "channel", d.channel, "ts", d.msgTS, "err", err)
	}
}

func (p *Platform) removeReaction(ctx context.Context, d delivery, name string) {
	if p.client == nil || d.channel == "" || d.msgTS == "" {
		return
	}
	err := p.client.RemoveReactionContext(ctx, name, slack.ItemRef{
		Channel:   d.channel,
		Timestamp: d.msgTS,
	})
	if err != nil {
		slog.Debug("slack: remove reaction failed", "emoji", name, "channel", d.channel, "ts", d.msgTS, "err", err)
	}
}

func (p *Platform) reactReceived(ctx context.Context, d delivery) {
	go p.addReaction(context.WithoutCancel(ctx), d, reactionReceived)
}

// applyTurnSteer acknowledges a steered message on the message itself: the
// in-flight reaction (eyes) is replaced by steerEmoji so the sender sees the
// message was folded into the running turn rather than queued for a later one.
// turn/end removes steerEmoji alongside reactionReceived for messages tracked by
// the running turn's reaction batch. Best effort — an emoji failure never
// affects the turn.
func (p *Platform) applyTurnSteer(ctx context.Context, event agentkit.OutboundEvent) error {
	if p.steerEmoji == "" {
		return nil
	}
	sessionID := rctx.OutboundRouteID(event)
	d, ok := p.deliveryForReaction(sessionID, event)
	if !ok || d.msgTS == "" {
		slog.Debug("slack: turn/steer without a reaction target",
			"session_id", string(sessionID))
		return nil
	}
	// Swap synchronously (like Feishu) so the batch and the wire agree before
	// this returns; turn/end then retires steerEmoji instead of leaving it on a
	// finished message. Messages outside the running turn's batch are still
	// acknowledged, but nowhere to remember the swap — turn/end clears both.
	bg := context.WithoutCancel(ctx)
	p.removeReaction(bg, d, reactionReceived)
	p.addReaction(bg, d, p.steerEmoji)
	p.turnReactionBatchFor(sessionID).swapReaction(d, p.steerEmoji)
	return nil
}

// deliveryForReaction resolves the (channel, ts) to react on for a steer event.
// The steered message's own ts rides in the route's reply anchor; the channel
// comes from the route, falling back to the delivery cached at turn start.
func (p *Platform) deliveryForReaction(sessionID agentkit.SessionID, event agentkit.OutboundEvent) (delivery, bool) {
	cached, ok := p.deliveryForSession(sessionID)
	target, hasTarget := rctx.RouteTargetFromRoute(event.Route)
	d := cached
	if hasTarget {
		if target.ChannelID != "" {
			d.channel = target.ChannelID
		}
		if target.ThreadID != "" {
			d.threadTS = target.ThreadID
		}
		if target.ReplyTo != "" {
			d.msgTS = target.ReplyTo
		}
	}
	d.sessionID = sessionID
	if d.msgTS == "" {
		return delivery{}, false
	}
	if d.channel == "" && !ok {
		return delivery{}, false
	}
	return d, true
}
