package compaction

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
	capcompaction "github.com/lengzhao/agentkit/cap/compaction"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/llm"
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
	LLM           agentkit.LLMProvider  `json:"llm"`
	SessionEvents capsession.Compaction `json:"sessionEvents"`
}

type summaryService struct {
	cfg    SummaryConfig
	llm    agentkit.LLMProvider
	events capsession.Compaction
}

// NewSummary registers compaction/summary: Pi-style summary + retained tail compaction.
func NewSummary(cfg SummaryConfig, deps SummaryDeps) (capcompaction.Service, error) {
	if deps.LLM == nil {
		return nil, fmt.Errorf("compaction/summary requires llm dependency")
	}
	if deps.SessionEvents == nil {
		return nil, fmt.Errorf("compaction/summary requires sessionEvents dependency")
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
	return &summaryService{cfg: cfg, llm: deps.LLM, events: deps.SessionEvents}, nil
}

func (s *summaryService) Compact(ctx context.Context, req capcompaction.Request) (capcompaction.Result, error) {
	if !req.Force && s.cfg.MinMessages > 0 && len(req.Messages) < s.cfg.MinMessages {
		return capcompaction.Result{}, nil
	}
	if req.Session == nil {
		return capcompaction.Result{}, fmt.Errorf("compaction/summary requires session")
	}

	events, err := req.Session.Read(ctx, 0)
	if err != nil {
		return capcompaction.Result{}, err
	}
	indexed, err := s.events.IndexForCompaction(ctx, req.Session, req.AgentID)
	if err != nil {
		return capcompaction.Result{}, err
	}
	if len(indexed) == 0 {
		return capcompaction.Result{}, nil
	}

	boundaryStart := 0
	previousSummary := ""
	if _, prevData, ok := latestCompactionData(events, req.AgentID); ok {
		boundaryStart = 1
		previousSummary = prevData.PreviousSummaryText()
	}

	tokensBefore := EstimateMessagesTokens(req.Messages)
	prep := Prepare(indexed, boundaryStart, s.cfg.KeepRecentTokens, previousSummary, tokensBefore)
	if prep == nil {
		if req.Force {
			// Include a previous summary (indexed[0]) so an oversized checkpoint
			// can still be truncated; skipping it is how a bad summary permanently
			// stuck the session.
			return s.truncateOnlyCompaction(ctx, req, indexed, boundaryStart, tokensBefore)
		}
		return capcompaction.Result{}, nil
	}
	// A single retained message larger than the keep budget would keep the
	// post-compaction history oversized; bound it in the model-visible view.
	// Attachments whose recorded size exceeds the budget are neutralized into
	// text hints so hydration cannot re-inflate the retained tail.
	if bounded, ok := BoundOversizedIndexedMessages(indexed[prep.FirstKeptIndex:], s.maxRetainedChars()); ok {
		prep.RetainedTail = bounded
	}

	policy := ResolveRetrySettings(s.cfg.Retry)
	var summaryText string
	err = RetryCall(ctx, policy, llm.IsRetryableError, func() error {
		text, err := s.summarizePrepared(ctx, prep)
		if err != nil {
			return err
		}
		summaryText = text
		return nil
	}, &capcompaction.SummarizationRetryCallbacks{
		OnScheduled: func(attempt, maxAttempts, delayMs int, errorMessage string) {
			_ = s.events.AppendSummarizationRetryStart(ctx, req.Session, req.AgentID, capsession.RetryStartData{
				Attempt:      attempt,
				MaxAttempts:  maxAttempts,
				DelayMs:      delayMs,
				ErrorMessage: errorMessage,
			})
		},
		OnFinished: func(success bool, attempt int, finalError string) {
			_ = s.events.AppendSummarizationRetryEnd(ctx, req.Session, req.AgentID, capsession.RetryEndData{
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
	if err := s.events.AppendCompaction(ctx, req.Session, req.AgentID, data); err != nil {
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
func (s *summaryService) truncateOnlyCompaction(ctx context.Context, req capcompaction.Request, indexed []capcompaction.IndexedMessage, boundaryStart int, tokensBefore int) (capcompaction.Result, error) {
	if len(indexed) == 0 {
		return capcompaction.Result{}, nil
	}
	if boundaryStart < 0 {
		boundaryStart = 0
	}
	if boundaryStart > len(indexed) {
		boundaryStart = len(indexed)
	}
	bounded, ok := FitIndexedMessagesToBudget(indexed, s.maxRetainedChars())
	if !ok {
		slog.Warn("forced compaction did not apply: nothing to summarize and no oversized retained content",
			"session_id", req.SessionID,
			"agent_id", req.AgentID,
			"indexed_messages", len(indexed),
			"tokens_before", tokensBefore,
			"keep_recent_tokens", s.cfg.KeepRecentTokens,
		)
		return capcompaction.Result{}, nil
	}
	summary := agentkit.ModelMessage{
		Role: "user",
		Content: []agentkit.ContentPart{{
			Type: "text",
			Text: "[Earlier context could not be summarized: a single message exceeded the retention budget and was truncated.]",
		}},
	}
	drop := len(indexed) - len(bounded)
	if drop < 0 {
		drop = 0
	}
	tail := bounded
	firstKept := indexed[drop].Seq
	// Promote the bounded previous summary only when a non-empty tail remains:
	// derive treats an empty RetainedTail as a legacy marker and replays events
	// after BeforeSeq (0 here), which would resurrect compacted-away history.
	if boundaryStart > drop && len(bounded) > 1 {
		summary = bounded[0]
		tail = bounded[1:]
		if drop+1 < len(indexed) {
			firstKept = indexed[drop+1].Seq
		}
	}
	slog.Info("forced compaction applied without summary: bounded oversized retained content",
		"session_id", req.SessionID,
		"agent_id", req.AgentID,
		"indexed_messages", len(indexed),
		"retained_messages", len(tail),
		"tokens_before", tokensBefore,
	)
	data := capcompaction.EventData{
		FirstKeptSeq: firstKept,
		RetainedTail: tail,
		TokensBefore: tokensBefore,
		Kind:         capcompaction.KindSummary,
		Summary:      summary,
	}
	if err := s.events.AppendCompaction(ctx, req.Session, req.AgentID, data); err != nil {
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
	conversationText := SerializeConversationWithBudget(messages, s.maxInputChars())
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
		// A legacy oversized summary (e.g. left by a streaming bug) must not
		// bloat the update prompt beyond the summary model window either.
		previousSummary = s.boundSummaryText(previousSummary)
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
	text, err := llm.CollectAssistantText(stream)
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", fmt.Errorf("empty compaction summary")
	}
	if bounded := s.boundSummaryText(text); bounded != text {
		slog.Warn("compaction summary exceeded reserveTokens, truncating",
			"summary_chars", len(text),
			"reserve_chars", s.cfg.ReserveTokens*4,
		)
		text = bounded
	}
	return text, nil
}

// boundSummaryText caps summary text to the reserve budget (marker included, at
// a rune boundary) so neither the persisted summary nor a previous summary
// embedded in the next prompt can exceed it.
func (s *summaryService) boundSummaryText(text string) string {
	reserveChars := s.cfg.ReserveTokens * 4
	if reserveChars <= 0 || len(text) <= reserveChars {
		return text
	}
	msgs, _ := TruncateOversizedMessageTexts([]agentkit.ModelMessage{{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: text}},
	}}, reserveChars)
	return msgs[0].Content[0].Text
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
