package slack

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
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
	// this returns; turn/end then retires the current emoji instead of leaving
	// it on a finished message. A terminal queued/injected means the message
	// joined the running turn; next-turn/rejected undo the swap so the message
	// keeps the plain processing reaction.
	//
	// The API can finish after turn/end has drained the batch; if the swap can no
	// longer be recorded, retire the late reaction ourselves.
	bg := context.WithoutCancel(ctx)
	want := p.steerEmoji
	if !steerAckAccepted(event) {
		want = reactionReceived
	}
	p.removeReaction(bg, d, p.currentReaction(sessionID, d))
	p.addReaction(bg, d, want)
	if !p.turnReactionBatchFor(sessionID).swapReaction(d, want) {
		p.removeReaction(bg, d, want)
	}
	return nil
}

// steerAckAccepted reports whether a turn/steer event claims the message was
// taken into the running turn's work. queued/injected mean the sender will see
// its input folded into (or run right after) the current reply; next-turn and
// rejected contradict that, so the swap is undone.
func steerAckAccepted(event agentkit.OutboundEvent) bool {
	var data capsession.TurnSteerData
	if len(event.Data) == 0 {
		return true
	}
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return true
	}
	switch data.Outcome {
	case "", capsession.SteerOutcomeQueued, capsession.SteerOutcomeInjected:
		return true
	default:
		return false
	}
}

// currentReaction returns the emoji the running turn currently has on the
// message, falling back to the processing reaction when it is untracked.
func (p *Platform) currentReaction(sessionID agentkit.SessionID, d delivery) string {
	if name, ok := p.turnReactionBatchFor(sessionID).reactionFor(d); ok {
		return name
	}
	return reactionReceived
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
