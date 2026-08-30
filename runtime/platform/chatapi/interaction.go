package chatapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

func (p *Platform) handleConversations(w http.ResponseWriter, r *http.Request) {
	channel, ok := p.resolveChannel(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		user, ok := p.resolveUser(w, r, false)
		if !ok {
			return
		}
		_ = user
		list, err := p.listConversations(r.Context(), channel, 50)
		if err != nil {
			slog.Error("chat-api: list conversations", "err", err)
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		views := make([]map[string]any, 0, len(list))
		for _, c := range list {
			views = append(views, toConversationView(c))
		}
		writeOK(w, http.StatusOK, map[string]any{
			"limit":         50,
			"has_more":      false,
			"conversations": views,
		})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "invalid request")
	}
}

func (p *Platform) handleConversationSub(w http.ResponseWriter, r *http.Request) {
	sub := strings.TrimPrefix(r.URL.Path, p.path+"conversations/")
	if sub == "" {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	parts := strings.Split(sub, "/")
	conversationID := parts[0]
	if conversationID == "" {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	if len(parts) == 2 && parts[1] == "messages" {
		p.handleConversationMessages(w, r, conversationID)
		return
	}
	if len(parts) != 1 {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	channel, ok := p.resolveChannel(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		c, err := p.resolveConversation(r.Context(), channel, conversationID, "")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		if c == nil {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeOK(w, http.StatusOK, toConversationView(c))
	default:
		writeErr(w, http.StatusMethodNotAllowed, "invalid request")
	}
}

func (p *Platform) handleRunRoutes(w http.ResponseWriter, r *http.Request) {
	sub := strings.TrimPrefix(r.URL.Path, p.path+"runs/")
	if sub == "" {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	parts := strings.Split(sub, "/")
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "invalid request")
		return
	}
	switch {
	case len(parts) == 4 && parts[1] == "interactions" && parts[3] == "respond":
		p.handleRespondInteraction(w, r, parts[0], parts[2])
	default:
		writeErr(w, http.StatusNotFound, "not found")
	}
}

type interactionRespondRequest struct {
	Answer string `json:"answer"`
}

func (p *Platform) handleRespondInteraction(w http.ResponseWriter, r *http.Request, runID, interactionID string) {
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
	ix := run.getInteraction(interactionID)
	if ix == nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if ix.Responded {
		writeErr(w, http.StatusConflict, "interaction already responded")
		return
	}
	if time.Now().After(ix.ExpiresAt) {
		writeErr(w, http.StatusConflict, "interaction expired")
		return
	}

	var body interactionRespondRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody)).Decode(&body); err != nil {
		if errors.Is(err, io.EOF) {
			writeErr(w, http.StatusBadRequest, "invalid request")
			return
		}
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	answer := strings.TrimSpace(body.Answer)
	if answer == "" {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := run.markInteractionResponded(interactionID); err != nil {
		writeErr(w, http.StatusConflict, "interaction already responded")
		return
	}

	event := common.PermissionReplyEvent(run.agentID, run.sessionID, "chat-api", user, permission.Reply{
		RequestID: interactionID,
		UserID:    user,
		Text:      answer,
	})
	if md := p.metadataFromRequest(r); len(md) > 0 {
		event.Metadata = md
	}
	if err := p.inbox.Push(r.Context(), event); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeOK(w, http.StatusOK, map[string]string{"result": "success"})
}

func (p *Platform) onInteractionTimeout(runID, interactionID string) {
	run := p.pending.get(runID)
	if run == nil {
		return
	}
	ix := run.getInteraction(interactionID)
	if ix == nil || ix.Responded {
		return
	}
	p.pending.finish(runID, pendingResult{err: errInteractionExpired})
}
