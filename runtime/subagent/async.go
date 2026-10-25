package subagent

import (
	"context"
	"log/slog"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
)

// finishAsyncSubagent records subagent/end on the parent session and delivers
// the [subagent-complete] follow-up turn. Shared by the loop-agent and
// inprocess async paths; the caller runs it after the child turn finished,
// before draining any forwarded progress-card stream (the follow-up, not the
// card, is what resumes the parent agent — see docs/guides/subagent.zh.md §6.1).
func finishAsyncSubagent(store agentkit.SessionStore, submit capschedule.SubmitFunc, parent parentContext, agentName string, childID agentkit.SessionID, jobID string, parentID agentkit.SessionID, parentAgent agentkit.AgentID, result subagent.Result, runErr error) {
	bg := context.Background()
	ctx := parent.asyncRunContext()
	ctx = rctx.WithAgentID(ctx, parentAgent)

	end := sessevents.SubagentEndData{
		Agent:   agentName,
		Session: string(childID),
		Status:  result.Status,
		Summary: result.Summary,
		Steps:   result.Steps,
		JobID:   jobID,
	}
	if runErr != nil {
		end.Error = runErr.Error()
	}
	parentSess := parent.parentSession
	if parentSess == nil || parentSess.ID() != parentID {
		slog.Warn("async subagent: parent session not retained, store get", "job_id", jobID, "parent", parentID)
		var err error
		parentSess, err = store.Get(bg, parentID)
		if err != nil {
			slog.Error("async subagent: load parent session", "job_id", jobID, "err", err)
			parentSess = nil
		}
	} else {
		slog.Debug("async subagent: append end on retained parent session", "job_id", jobID, "parent", parentID)
	}
	if parentSess != nil {
		if err := sessevents.AppendSubagentEnd(bg, parentSess, parentAgent, end); err != nil {
			slog.Error("async subagent: append end", "job_id", jobID, "err", err)
		} else {
			emitSubagentLifecycle(ctx, parentAgent, agentkit.EventSubagentEnd, end)
		}
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("async subagent: submit complete panic", "job_id", jobID, "agent", agentName, "panic", r)
			}
		}()
		submitSubagentComplete(bg, submit, parent, parentAgent, agentName, jobID, result, runErr)
	}()
}

// submitSubagentComplete delivers the [subagent-complete ...] follow-up as a
// new turn on the parent conversation via the runner-bound submit func.
func submitSubagentComplete(ctx context.Context, submit capschedule.SubmitFunc, parent parentContext, parentAgent agentkit.AgentID, agentName, jobID string, result subagent.Result, runErr error) {
	if submit == nil {
		return
	}
	text := formatSubagentComplete(agentName, jobID, result, runErr)
	parentEnv := parent.envelope
	if parentEnv.Conversation == "" {
		parentEnv = parentEnv.WithConversation(string(parent.conversation))
	}
	parentEnv = parentEnv.WithAgentID(parentAgent).WithMetadata(map[string]any{
		"subagent_complete": true,
		"subagent_job_id":   jobID,
		"subagent_agent":    agentName,
		"subagent_status":   result.Status,
	})
	event := agentkit.MessageEvent{
		Envelope: parentEnv,
		Message:  agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: text}}},
	}
	if err := submit(ctx, event); err != nil {
		slog.Error("async subagent: submit follow-up", "job_id", jobID, "err", err)
	}
}
