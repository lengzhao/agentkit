package feishu

import (
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func (p *Platform) ReconstructReplyCtx(sessionKey string) (any, error) {
	parts := rctx.ParseDelivery(agentkit.SessionID(sessionKey), "")
	if !parts.Routable || parts.Platform != p.platformTag {
		return nil, fmt.Errorf("%s: invalid session key %q", p.tag(), sessionKey)
	}
	rc := replyContext{chatID: parts.Channel, sessionKey: sessionKey}
	if parts.Thread != "" {
		rc.messageID = parts.Thread
	}
	return rc, nil
}

func isThreadSessionKey(sessionKey string) bool {
	parts := rctx.ParseDelivery(agentkit.SessionID(sessionKey), "")
	return parts.Thread != ""
}
