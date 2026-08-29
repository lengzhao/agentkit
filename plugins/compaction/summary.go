package compaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/lengzhao/agentkit"
	capcompaction "github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/session"
)

const (
	defaultKeepRecentTokens = 20000
	defaultReserveTokens    = 16384
)

type SummaryConfig struct {
	// MinMessages is an optional pre-gate when Force is false. Zero disables it.
	MinMessages int `json:"minMessages"`
	// KeepRecentTokens is the recent context budget kept verbatim (Pi-compatible).
	KeepRecentTokens int `json:"keepRecentTokens"`
	// ReserveTokens caps summarization output size.
	ReserveTokens int `json:"reserveTokens"`
	// KeepRecent is deprecated; use keepRecentTokens.
	KeepRecent int `json:"keepRecent"`
	// SummaryModel is model used for the summary; defaults to the agent's model.
	SummaryModel string `json:"summaryModel"`
	// SummaryPrompt overrides the built-in summarisation instruction.
	SummaryPrompt string `json:"summaryPrompt"`
	Retry         *capcompaction.RetryConfig `json:"retry,omitempty"`
}

type SummaryDeps struct {
	LLM agentkit.LLMProvider `json:"llm"`
}

type summaryService struct {
	cfg SummaryConfig
	llm agentkit.LLMProvider
}

// NewSummary registers compaction/summary: Pi-style summary + retained tail compaction.
func NewSummary(cfg SummaryConfig, deps SummaryDeps) (capcompaction.Service, error) {
	if deps.LLM == nil {
		return nil, fmt.Errorf("compaction/summary requires llm dependency")
	}
	if cfg.KeepRecentTokens <= 0 {
		if cfg.KeepRecent > 0 {
			cfg.KeepRecentTokens = cfg.KeepRecent * 500
		} else {
			cfg.KeepRecentTokens = defaultKeepRecentTokens
		}
	}
	if cfg.ReserveTokens <= 0 {
		cfg.ReserveTokens = defaultReserveTokens
	}
	if cfg.SummaryPrompt == "" {
		cfg.SummaryPrompt = initialSummarizationPrompt
	}
	return &summaryService{cfg: cfg, llm: deps.LLM}, nil
}

func (s *summaryService) Compact(ctx context.Context, req capcompaction.Request) (capcompaction.Result, error) {
	if !req.Force && s.cfg.MinMessages > 0 && len(req.Messages) < s.cfg.MinMessages {
		return capcompaction.Result{}, nil
	}
	if req.Session == nil {
		return capcompaction.Result{}, fmt.Errorf("compaction/summary requires session")
	}

	events, err := session.ReadAllEvents(ctx, req.Session)
	if err != nil {
		return capcompaction.Result{}, err
	}
	indexed := session.IndexMessagesForCompaction(ctx, events, "")
	if len(indexed) == 0 {
		return capcompaction.Result{}, nil
	}

	boundaryStart := 0
	previousSummary := ""
	if _, prevData, ok := latestCompactionData(events, req.AgentID); ok {
		boundaryStart = 1
		previousSummary = capcompaction.PreviousSummaryText(prevData)
	}

	tokensBefore := capcompaction.EstimateMessagesTokens(req.Messages)
	prep := capcompaction.Prepare(indexed, boundaryStart, s.cfg.KeepRecentTokens, previousSummary, tokensBefore)
	if prep == nil {
		return capcompaction.Result{}, nil
	}

	policy := capcompaction.ResolveRetrySettings(s.cfg.Retry)
	var summaryText string
	err = capcompaction.RetryCall(ctx, policy, llm.IsRetryableError, func() error {
		text, err := s.summarizePrepared(ctx, prep)
		if err != nil {
			return err
		}
		summaryText = text
		return nil
	}, &capcompaction.SummarizationRetryCallbacks{
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
		return capcompaction.Result{}, err
	}

	data := capcompaction.EventData{
		BeforeSeq:    prep.FirstKeptSeq - 1,
		FirstKeptSeq: prep.FirstKeptSeq,
		RetainedTail: prep.RetainedTail,
		TokensBefore: prep.TokensBefore,
		Kind:         capcompaction.KindSummary,
		Summary: agentkit.ModelMessage{
			Role: "user",
			Content: []agentkit.ContentPart{{
				Type: "text",
				Text: summaryText,
			}},
		},
	}
	if err := session.AppendCompaction(ctx, req.Session, req.AgentID, data); err != nil {
		return capcompaction.Result{}, err
	}
	return capcompaction.Result{Applied: true}, nil
}

func latestCompactionData(events []agentkit.SessionEvent, agentID agentkit.AgentID) (agentkit.EventSeq, capcompaction.EventData, bool) {
	var (
		seq  agentkit.EventSeq
		data capcompaction.EventData
		ok   bool
	)
	for _, ev := range events {
		if ev.Type != agentkit.EventCompaction {
			continue
		}
		if agentID != "" && ev.AgentID != "" && ev.AgentID != agentID {
			continue
		}
		var parsed capcompaction.EventData
		if err := json.Unmarshal(ev.Data, &parsed); err != nil {
			continue
		}
		seq = ev.Seq
		data = parsed
		ok = true
	}
	return seq, data, ok
}

func (s *summaryService) summarizePrepared(ctx context.Context, prep *capcompaction.Preparation) (string, error) {
	if prep.IsSplitTurn && len(prep.TurnPrefixMessages) > 0 {
		historyText := "No prior history."
		if len(prep.MessagesToSummarize) > 0 {
			text, err := s.summarizeOnce(ctx, prep.MessagesToSummarize, prep.PreviousSummary, false)
			if err != nil {
				return "", err
			}
			historyText = text
		}
		prefix, err := s.summarizeOnce(ctx, prep.TurnPrefixMessages, "", true)
		if err != nil {
			return "", err
		}
		return historyText + "\n\n---\n\n**Turn Context (split turn):**\n\n" + prefix, nil
	}
	return s.summarizeOnce(ctx, prep.MessagesToSummarize, prep.PreviousSummary, false)
}

func (s *summaryService) summarizeOnce(ctx context.Context, messages []agentkit.ModelMessage, previousSummary string, turnPrefix bool) (string, error) {
	conversationText := capcompaction.SerializeConversation(messages)
	prompt := s.cfg.SummaryPrompt
	if previousSummary != "" {
		prompt = updateSummarizationPrompt
	}
	if turnPrefix {
		prompt = turnPrefixSummarizationPrompt
	}

	var promptText strings.Builder
	promptText.WriteString("<conversation>\n")
	promptText.WriteString(conversationText)
	promptText.WriteString("\n</conversation>\n\n")
	if previousSummary != "" {
		promptText.WriteString("<previous-summary>\n")
		promptText.WriteString(previousSummary)
		promptText.WriteString("\n</previous-summary>\n\n")
	}
	promptText.WriteString(prompt)

	model := s.cfg.SummaryModel
	stream, err := s.llm.Stream(ctx, agentkit.LLMRequest{
		Model: model,
		Messages: []agentkit.ModelMessage{
			{Role: "system", Content: []agentkit.ContentPart{{Type: "text", Text: summarizationSystemPrompt}}},
			{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: promptText.String()}}},
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

const summarizationSystemPrompt = `You are a context summarization assistant. Your task is to read a conversation between a user and an AI assistant, then produce a structured summary following the exact format specified.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.`

const initialSummarizationPrompt = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Use this EXACT format:

## Goal
[What is the user trying to accomplish?]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

const updateSummarizationPrompt = `The messages above are NEW conversation messages to incorporate into the existing summary provided in <previous-summary> tags.

Update the existing structured summary with new information. RULES:
- PRESERVE all existing information from the previous summary
- ADD new progress, decisions, and context from the new messages
- UPDATE the Progress section: move items from "In Progress" to "Done" when completed
- UPDATE "Next Steps" based on what was accomplished
- PRESERVE exact file paths, function names, and error messages

Use the same structured format as the initial summary.`

const turnPrefixSummarizationPrompt = `This is the PREFIX of a turn that was too large to keep. The SUFFIX (recent work) is retained.

Summarize the prefix to provide context for the retained suffix:

## Original Request
[What did the user ask for in this turn?]

## Early Progress
- [Key decisions and work done in the prefix]

## Context for Suffix
- [Information needed to understand the retained recent work]

Be concise. Focus on what's needed to understand the kept suffix.`
