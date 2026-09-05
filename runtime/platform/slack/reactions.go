package slack

import (
	"context"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/slack-go/slack"
)

const (
	reactionReceived = "eyes"
	reactionDone     = "white_check_mark"
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

func (p *Platform) reactDone(ctx context.Context, sessionID agentkit.SessionID) {
	raw, ok := p.deliveries.Load(sessionID)
	if !ok {
		return
	}
	d := raw.(delivery)
	go func() {
		bg := context.WithoutCancel(ctx)
		p.removeReaction(bg, d, reactionReceived)
		p.addReaction(bg, d, reactionDone)
	}()
}
