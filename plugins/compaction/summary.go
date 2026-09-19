package compaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
	capcompaction "github.com/lengzhao/agentkit/cap/compaction"
	rtcompaction "github.com/lengzhao/agentkit/runtime/compaction"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/session/derive"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
)

const (
	defaultKeepRecentTokens = 20000
	defaultReserveTokens    = 16384
	// defaultMaxInputTokens bounds the summarization request input so an
	// already-oversized history cannot 400 the summary model itself.
	defaultMaxInputTokens = 400_000
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
	// MaxInputTokens bounds the summarization request input (oldest messages
	// are dropped first, single messages truncated). Zero uses the default.
	MaxInputTokens int `json:"maxInputTokens"`
	// SummaryPrompt overrides the built-in summarisation instruction.
	SummaryPrompt string                     `json:"summaryPrompt"`
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
	if cfg.MaxInputTokens <= 0 {
		cfg.MaxInputTokens = defaultMaxInputTokens
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

	events, err := derive.ReadAllEvents(ctx, req.Session)
	if err != nil {
		return capcompaction.Result{}, err
	}
	indexed := derive.IndexMessagesForCompaction(ctx, events)
	if len(indexed) == 0 {
		return capcompaction.Result{}, nil
	}

	boundaryStart := 0
	previousSummary := ""
	if _, prevData, ok := latestCompactionData(events, req.AgentID); ok {
		boundaryStart = 1
		previousSummary = prevData.PreviousSummaryText()
	}

	tokensBefore := rtcompaction.EstimateMessagesTokens(req.Messages)
	prep := rtcompaction.Prepare(indexed, boundaryStart, s.cfg.KeepRecentTokens, previousSummary, tokensBefore)
	if prep == nil {
		if req.Force {
			return s.truncateOnlyCompaction(ctx, req, indexed[boundaryStart:], tokensBefore)
		}
		return capcompaction.Result{}, nil
	}
	// A single retained message larger than the keep budget would keep the
	// post-compaction history oversized; bound it in the model-visible view.
	// Attachments whose recorded size exceeds the budget are neutralized into
	// text hints so hydration cannot re-inflate the retained tail.
	if bounded, ok := rtcompaction.BoundOversizedIndexedMessages(indexed[prep.FirstKeptIndex:], s.maxRetainedChars()); ok {
		prep.RetainedTail = bounded
	}

	policy := rtcompaction.ResolveRetrySettings(s.cfg.Retry)
	var summaryText string
	err = rtcompaction.RetryCall(ctx, policy, llm.IsRetryableError, func() error {
		text, err := s.summarizePrepared(ctx, prep)
		if err != nil {
			return err
		}
		summaryText = text
		return nil
	}, &capcompaction.SummarizationRetryCallbacks{
		OnScheduled: func(attempt, maxAttempts, delayMs int, errorMessage string) {
			_ = sessevents.AppendSummarizationRetryStart(ctx, req.Session, req.AgentID, sessevents.SummarizationRetryStartData{
				Attempt:      attempt,
				MaxAttempts:  maxAttempts,
				DelayMs:      delayMs,
				ErrorMessage: errorMessage,
			})
		},
		OnFinished: func(success bool, attempt int, finalError string) {
			_ = sessevents.AppendSummarizationRetryEnd(ctx, req.Session, req.AgentID, sessevents.SummarizationRetryEndData{
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
	if err := sessevents.AppendCompaction(ctx, req.Session, req.AgentID, data); err != nil {
		return capcompaction.Result{}, err
	}
	return capcompaction.Result{Applied: true}, nil
}

// maxRetainedChars bounds a single retained message in the model-visible view;
// keepRecentTokens uses the chars/4 estimate, so the char budget matches.
func (s *summaryService) maxRetainedChars() int {
	return s.cfg.KeepRecentTokens * 4
}

// maxInputChars converts the summarization input token budget to chars (chars/4).
func (s *summaryService) maxInputChars() int {
	return s.cfg.MaxInputTokens * 4
}

// truncateOnlyCompaction handles forced compaction when there is nothing to
// summarize (e.g. the whole history is one oversized message): it persists a
// compaction event whose retained tail is the bounded history, so overflow
// recovery retries with a bounded prompt instead of the same oversized one.
func (s *summaryService) truncateOnlyCompaction(ctx context.Context, req capcompaction.Request, tail []capcompaction.IndexedMessage, tokensBefore int) (capcompaction.Result, error) {
	if len(tail) == 0 {
		return capcompaction.Result{}, nil
	}
	bounded, ok := rtcompaction.BoundOversizedIndexedMessages(tail, s.maxRetainedChars())
	if !ok {
		slog.Warn("forced compaction did not apply: nothing to summarize and no oversized retained content",
			"session_id", req.SessionID,
			"agent_id", req.AgentID,
			"indexed_messages", len(tail),
			"tokens_before", tokensBefore,
			"keep_recent_tokens", s.cfg.KeepRecentTokens,
		)
		return capcompaction.Result{}, nil
	}
	slog.Info("forced compaction applied without summary: bounded oversized retained content",
		"session_id", req.SessionID,
		"agent_id", req.AgentID,
		"indexed_messages", len(tail),
		"tokens_before", tokensBefore,
	)
	data := capcompaction.EventData{
		FirstKeptSeq: tail[0].Seq,
		RetainedTail: bounded,
		TokensBefore: tokensBefore,
		Kind:         capcompaction.KindSummary,
		Summary: agentkit.ModelMessage{
			Role: "user",
			Content: []agentkit.ContentPart{{
				Type: "text",
				Text: "[Earlier context could not be summarized: a single message exceeded the retention budget and was truncated.]",
			}},
		},
	}
	if err := sessevents.AppendCompaction(ctx, req.Session, req.AgentID, data); err != nil {
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
	conversationText := rtcompaction.SerializeConversationWithBudget(messages, s.maxInputChars())
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
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

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
- If something is no longer relevant, you may remove it

Use this EXACT format:

## Goal
[Preserve existing goals, add new ones if the task expanded]

## Constraints & Preferences
- [Preserve existing, add new ones discovered]

## Progress
### Done
- [x] [Include previously done items AND newly completed items]

### In Progress
- [ ] [Current work - update based on progress]

### Blocked
- [Current blockers - remove if resolved]

## Key Decisions
- **[Decision]**: [Brief rationale] (preserve all previous, add new)

## Next Steps
1. [Update based on current state]

## Critical Context
- [Preserve important context, add new if needed]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

const turnPrefixSummarizationPrompt = `This is the PREFIX of a turn that was too large to keep. The SUFFIX (recent work) is retained.

Summarize the prefix to provide context for the retained suffix:

## Original Request
[What did the user ask for in this turn?]

## Early Progress
- [Key decisions and work done in the prefix]

## Context for Suffix
- [Information needed to understand the retained recent work]

Be concise. Focus on what's needed to understand the kept suffix.`
