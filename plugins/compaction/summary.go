package compaction

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/session"
)

type SummaryConfig struct {
	MinMessages   int                     `json:"minMessages"`
	KeepRecent    int                     `json:"keepRecent"`
	SummaryModel  string                  `json:"summaryModel"`
	SummaryPrompt string                  `json:"summaryPrompt"`
	Retry         *compaction.RetryConfig `json:"retry,omitempty"`
}

type SummaryDeps struct {
	LLM agentkit.LLMProvider `json:"llm"`
}

type summaryService struct {
	cfg SummaryConfig
	llm agentkit.LLMProvider
}

func NewSummary(cfg SummaryConfig, deps SummaryDeps) (compaction.Service, error) {
	if deps.LLM == nil {
		return nil, fmt.Errorf("compaction/summary requires llm dependency")
	}
	if cfg.MinMessages <= 0 {
		cfg.MinMessages = 20
	}
	if cfg.KeepRecent <= 0 {
		cfg.KeepRecent = 6
	}
	if cfg.SummaryPrompt == "" {
		cfg.SummaryPrompt = "Summarize the conversation so far for continuing the task. Preserve key decisions, file paths, errors, and unfinished work."
	}
	return &summaryService{cfg: cfg, llm: deps.LLM}, nil
}

func (s *summaryService) Compact(ctx context.Context, req compaction.Request) (compaction.Result, error) {
	if !req.Force && len(req.Messages) < s.cfg.MinMessages {
		return compaction.Result{}, nil
	}
	events, err := session.ReadAllEvents(ctx, req.Session)
	if err != nil {
		return compaction.Result{}, err
	}
	beforeSeq := session.LatestEventSeq(events)
	if beforeSeq == 0 {
		return compaction.Result{}, nil
	}

	// Force skips the minMessages gate, so the history can be shorter than
	// keepRecent. Nothing to summarize is a no-op, not a crash.
	keepRecent := min(s.cfg.KeepRecent, len(req.Messages))
	toSummarize := req.Messages[:len(req.Messages)-keepRecent]
	if len(toSummarize) == 0 {
		return compaction.Result{}, nil
	}
	policy := compaction.ResolveRetrySettings(s.cfg.Retry)
	var summaryText string
	err = compaction.RetryCall(ctx, policy, llm.IsRetryableError, func() error {
		text, err := s.summarizeOnce(ctx, toSummarize)
		if err != nil {
			return err
		}
		summaryText = text
		return nil
	}, &compaction.SummarizationRetryCallbacks{
		OnScheduled: func(attempt, maxAttempts, delayMs int, errorMessage string) {
			_ = session.AppendSummarizationRetryStart(ctx, req.Session, req.AgentID, session.SummarizationRetryStartData{
				Attempt:      attempt,
				MaxAttempts:  maxAttempts,
				DelayMs:      delayMs,
				ErrorMessage: errorMessage,
			})
		},
		OnFinished: func(success bool, attempt int, finalError string) {
			_ = session.AppendSummarizationRetryEnd(ctx, req.Session, req.AgentID, session.SummarizationRetryEndData{
				Success:    success,
				Attempt:    attempt,
				FinalError: finalError,
			})
		},
	})
	if err != nil {
		return compaction.Result{}, err
	}

	data := compaction.EventData{
		BeforeSeq: beforeSeq,
		Kind:      compaction.KindSummary,
		Summary: agentkit.ModelMessage{
			Role: "user",
			Content: []agentkit.ContentPart{{
				Type: "text",
				Text: "[Conversation summary]\n" + summaryText,
			}},
		},
	}
	if err := session.AppendCompaction(ctx, req.Session, req.AgentID, data); err != nil {
		return compaction.Result{}, err
	}
	return compaction.Result{Applied: true}, nil
}

func (s *summaryService) summarizeOnce(ctx context.Context, messages []agentkit.ModelMessage) (string, error) {
	var b strings.Builder
	for _, msg := range messages {
		b.WriteString(msg.Role)
		b.WriteString(": ")
		for _, part := range msg.Content {
			if part.Type == "text" {
				b.WriteString(part.Text)
			}
		}
		b.WriteString("\n")
	}

	model := s.cfg.SummaryModel
	stream, err := s.llm.Stream(ctx, agentkit.LLMRequest{
		Model: model,
		Messages: []agentkit.ModelMessage{
			{Role: "system", Content: []agentkit.ContentPart{{Type: "text", Text: s.cfg.SummaryPrompt}}},
			{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: b.String()}}},
		},
	})
	if err != nil {
		return "", err
	}
	defer stream.Close()

	var out strings.Builder
	for {
		ev, err := stream.Recv()
		if ev.Message != nil {
			for _, part := range ev.Message.Content {
				if part.Type == "text" {
					out.WriteString(part.Text)
				}
			}
		}
		if ev.Delta != "" {
			out.WriteString(ev.Delta)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
	}
	text := strings.TrimSpace(out.String())
	if text == "" {
		return "", fmt.Errorf("empty compaction summary")
	}
	return text, nil
}
