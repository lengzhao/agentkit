package chatapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

const maxRequestBody = 10 << 20

type chatRequest struct {
	ConversationID string      `json:"conversation_id"`
	Query          string      `json:"query"`
	AgentID        string      `json:"agent_id"`
	Inputs         []chatInput `json:"inputs"`
}

func (p *Platform) handleChatMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "invalid request")
		return
	}
	user, ok := p.resolveUser(w, r, true)
	if !ok {
		return
	}
	channelKey, ok := p.resolveChannel(w, r)
	if !ok {
		return
	}
	accept := r.Header.Get("Accept")
	if accept != "" && !strings.Contains(accept, "text/event-stream") && !strings.Contains(accept, "*/*") {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}

	var body chatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody)).Decode(&body); err != nil {
		if errors.Is(err, io.EOF) {
			writeErr(w, http.StatusBadRequest, "invalid request")
			return
		}
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	query := strings.TrimSpace(body.Query)
	if query == "" && len(body.Inputs) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	requestAgentID := agentkit.AgentID(body.AgentID)
	if err := p.validateAgentID(requestAgentID); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}

	var conv *conversation
	if strings.TrimSpace(body.ConversationID) == "" {
		c, err := p.createConversation(r.Context(), channelKey, user)
		if err != nil {
			slog.Error("chat-api: create conversation", "err", err)
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		conv = c
	} else {
		c, err := p.resolveConversation(r.Context(), channelKey, body.ConversationID, user)
		if err != nil {
			slog.Error("chat-api: resolve conversation", "err", err)
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		if c == nil {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		conv = c
	}
	conv.bindAgent(requestAgentID)
	p.persistConversationIndex(r.Context(), channelKey)

	if p.busyPolicy == busyPolicyReject && p.activeConvBusy(conv.ID) {
		writeErr(w, http.StatusConflict, "conversation busy")
		return
	}

	engineSessionKey := engineSessionKey(channelKey, conv.ID)
	p.conversations.bumpTurn(conv.ID)
	p.persistConversationIndex(r.Context(), channelKey)
	msgID := messageID(conv.ID, conv.TurnCount)
	runID := newRunID()
	inboundAgentID := p.resolveInboundAgentID(requestAgentID, conv)

	sse, err := newSSEWriter(w)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	run := newRunState(runID, user, channelKey, inboundAgentID, agentkit.SessionID(engineSessionKey), conv.ID, msgID, p, sse)
	run.apiBase = p.apiBaseFromRequest(r)
	if !p.pending.create(run) {
		_ = sse.Error("too many concurrent requests")
		return
	}
	p.setActiveConv(conv.ID, runID)

	if err := sse.Event("message", map[string]any{
		"conversation_id": conv.ID,
		"message_id":      msgID,
		"run_id":          runID,
	}); err != nil {
		p.clearActiveConv(conv.ID, runID)
		p.pending.delete(runID)
		return
	}

	engineSessionID := agentkit.SessionID(engineSessionKey)
	slashResult, err := p.processChatSlash(r.Context(), channelKey, conv, engineSessionID, query)
	if err != nil {
		p.pending.finish(runID, pendingResult{err: err})
		p.clearActiveConv(conv.ID, runID)
		return
	}
	outcome := slashResult.outcome
	switch outcome.Kind {
	case common.SlashHandled:
		run.mu.Lock()
		if strings.TrimSpace(run.answerText) == "" && outcome.Reply != "" {
			run.answerText = outcome.Reply
		}
		answer := run.answerText
		run.mu.Unlock()
		_ = run.flushDeltas()
		p.pending.finish(runID, pendingResult{answer: answer})
		p.clearActiveConv(conv.ID, runID)
		return
	case common.SlashForward:
		if outcome.Reply != "" {
			run.mu.Lock()
			run.answerText = outcome.Reply
			run.mu.Unlock()
			_ = run.flushDeltas()
			run.mu.Lock()
			run.answerText = ""
			run.sentAnswer = ""
			run.mu.Unlock()
		}
	case common.SlashNotCommand:
	}

	images, files, audio, filePaths, err := p.inputsToCore(r.Context(), channelKey, body.Inputs)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	event := common.InboundFromContent(inboundAgentID, agentkit.SessionID(engineSessionKey), "chat-api", user, query, "", images, files, audio, filePaths, common.InboundOptsFor(p.workspace))
	if md := p.metadataFromRequest(r); len(md) > 0 {
		event.Metadata = md
	}
	if err := p.inbox.Push(r.Context(), event); err != nil {
		p.pending.finish(runID, pendingResult{err: err})
		p.clearActiveConv(conv.ID, runID)
		return
	}

	select {
	case result := <-run.done:
		p.clearActiveConv(conv.ID, runID)
		if result.err != nil {
			slog.Debug("chat-api: run finished with error", "run_id", runID, "err", result.err)
		}
	case <-r.Context().Done():
		p.clearActiveConv(conv.ID, runID)
		p.pending.finish(runID, pendingResult{err: r.Context().Err()})
	}
}

func (p *Platform) emitTerminalSSE(run *runState, result pendingResult) {
	if run == nil || run.sse == nil {
		return
	}
	_ = run.flushDeltas()
	if result.err != nil {
		_ = run.sse.Error(result.err.Error())
		return
	}
	_ = run.sse.Event("message_end", map[string]string{
		"message_id":      run.messageID,
		"conversation_id": run.conversationID,
	})
}

func (p *Platform) activeConvBusy(convID string) bool {
	p.activeMu.RLock()
	defer p.activeMu.RUnlock()
	_, ok := p.activeByConv[convID]
	return ok
}

func (p *Platform) setActiveConv(convID, runID string) {
	p.activeMu.Lock()
	p.activeByConv[convID] = runID
	p.activeMu.Unlock()
}

func (p *Platform) clearActiveConv(convID, runID string) {
	p.activeMu.Lock()
	if p.activeByConv[convID] == runID {
		delete(p.activeByConv, convID)
	}
	p.activeMu.Unlock()
}

func (p *Platform) runForSession(sessionID agentkit.SessionID) *runState {
	p.activeMu.RLock()
	defer p.activeMu.RUnlock()
	for _, runID := range p.activeByConv {
		run := p.pending.get(runID)
		if run != nil && run.sessionID == sessionID {
			return run
		}
	}
	return nil
}

func (p *Platform) handleOutbound(ctx context.Context, event agentkit.OutboundEvent) error {
	run := p.runForSession(event.SessionID)
	if run == nil {
		if event.Type == agentkit.EventAssistantMessage {
			return fmt.Errorf("chat-api: no active request for session %s", event.SessionID)
		}
		return nil
	}

	switch event.Type {
	case agentkit.EventTurnStart:
		run.cancelFinish()
		return nil
	case agentkit.EventMessageStart:
		run.cancelFinish()
		return nil
	case agentkit.EventMessageUpdate:
		var payload agentkit.MessageUpdatePayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		switch payload.AssistantMessageEvent.Type {
		case agentkit.AssistantEventTextDelta:
			run.appendAnswer(payload.AssistantMessageEvent.Delta)
			return run.flushDeltas()
		case agentkit.AssistantEventThinkingDelta:
			run.appendThinking(payload.AssistantMessageEvent.Delta)
			return run.flushDeltas()
		case agentkit.AssistantEventToolCallStart, agentkit.AssistantEventToolCallEnd:
			return run.emitToolCallSSE(event, payload)
		}
		return nil
	case agentkit.EventMessageEnd:
		var payload agentkit.MessageEndPayload
		if err := json.Unmarshal(event.Data, &payload); err == nil {
			if text := common.ModelMessageText(payload.Message); text != "" {
				run.mu.Lock()
				run.answerText = text
				run.mu.Unlock()
			}
		}
		return run.flushDeltas()
	case agentkit.EventAssistantMessage:
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(event.Data, &msg); err == nil {
			if text := common.ModelMessageText(msg); text != "" {
				run.mu.Lock()
				run.answerText = text
				run.mu.Unlock()
			}
			if err := p.emitAssistantMedia(ctx, run, msg); err != nil {
				return err
			}
		}
		return run.flushDeltas()
	case agentkit.EventTurnEnd:
		if err := run.flushDeltas(); err != nil {
			return err
		}
		run.scheduleFinish()
		return nil
	case agentkit.EventPermissionRequest:
		var payload permission.RequestPayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		requestID := strings.TrimSpace(payload.ID)
		if requestID == "" {
			requestID = newInteractionID()
		}
		expires := time.Now().Add(p.interactionTimeout)
		prompt, actions, options := permissionPromptView(payload)
		run.setInteraction(&interactionState{
			ID:        requestID,
			Prompt:    prompt,
			Options:   options,
			ExpiresAt: expires,
		})
		eventName := "permission_request"
		if payload.Kind == permission.KindQuestion {
			eventName = "question_request"
		}
		return run.sse.Event(eventName, map[string]any{
			"interaction_id": requestID,
			"request_id":     requestID,
			"run_id":         run.id,
			"message_id":     run.messageID,
			"kind":           payload.Kind,
			"prompt":         prompt,
			"expires_at":     expires.Unix(),
			"actions":        actions,
		})
	case "error":
		var payload struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(event.Data, &payload); err == nil && payload.Error != "" {
			p.pending.finish(run.id, pendingResult{err: fmt.Errorf("%s", payload.Error)})
		}
	}
	return nil
}

func (r *runState) emitToolCallSSE(event agentkit.OutboundEvent, payload agentkit.MessageUpdatePayload) error {
	ame := payload.AssistantMessageEvent
	toolID := ame.ID
	name := ame.ToolName
	var input string
	if ame.ToolCall != nil {
		if toolID == "" {
			toolID = string(ame.ToolCall.ID)
		}
		if name == "" {
			name = ame.ToolCall.Name
		}
		input = string(ame.ToolCall.Input)
	}
	if toolID == "" {
		return nil
	}
	data := map[string]any{
		"message_id":   r.messageID,
		"tool_call_id": toolID,
		"name":         name,
	}
	if input != "" {
		data["input"] = input
	}
	if event.AgentID != "" {
		data["agent_id"] = string(event.AgentID)
	}
	return r.sse.Event("tool_call", data)
}

func permissionPromptView(payload permission.RequestPayload) (prompt string, actions []map[string]string, options []string) {
	switch payload.Kind {
	case permission.KindQuestion:
		if payload.Question == nil {
			return "", nil, nil
		}
		q := payload.Question
		actions = make([]map[string]string, 0, len(q.Options))
		options = make([]string, 0, len(q.Options))
		for i, opt := range q.Options {
			label := opt.Label
			options = append(options, label)
			actions = append(actions, map[string]string{
				"id":    fmt.Sprintf("%d", i+1),
				"label": label,
			})
		}
		return q.Prompt, actions, options
	case permission.KindAllowDeny:
		prompt := strings.TrimSpace(payload.Reason)
		if prompt == "" {
			prompt = "是否允许执行该操作？"
		}
		if payload.ToolCall != nil && payload.ToolCall.Name != "" {
			prompt = fmt.Sprintf("是否允许执行工具 %q？\n%s", payload.ToolCall.Name, prompt)
		}
		return prompt, []map[string]string{
			{"id": "allow", "label": "允许"},
			{"id": "deny", "label": "拒绝"},
		}, []string{"允许", "拒绝"}
	default:
		return strings.TrimSpace(payload.Reason), nil, nil
	}
}
