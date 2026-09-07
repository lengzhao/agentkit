package chatapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/session"
)

var errUserCanceled = errors.New("canceled by user")

func (p *Platform) handleCancelRun(w http.ResponseWriter, r *http.Request, runID string) {
	channel, ok := p.resolveChannel(w, r)
	if !ok {
		return
	}
	user, ok := p.resolveUser(w, r, true)
	if !ok {
		return
	}
	run := p.pending.get(runID)
	if run == nil || run.user != user || run.channelKey != channel {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	p.dispatchStop(r.Context(), run)
	if !p.pending.cancelUser(runID) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeOK(w, http.StatusOK, map[string]string{"result": "success"})
}

func (p *Platform) dispatchStop(ctx context.Context, run *runState) {
	if run == nil {
		return
	}
	event := common.InboundFromContent(run.agentID, session.SessionRouteInput{
		Platform:    "chat-api",
		DeliveryID:  run.sessionID,
		ReplyTo:     run.messageID,
		ScopeUserID: run.user,
	}, run.user, "/stop", "", nil, nil, nil, nil, common.InboundOptsFor(p.workspace))
	_ = p.inbox.Push(ctx, event)
}
