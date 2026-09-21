package smoke_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/lengzhao/agentkit"
	capcompaction "github.com/lengzhao/agentkit/cap/compaction"
	capsession "github.com/lengzhao/agentkit/cap/session"
	plugincompaction "github.com/lengzhao/agentkit/plugins/compaction"
	pluginhook "github.com/lengzhao/agentkit/plugins/hook"
	"github.com/lengzhao/agentkit/runtime/agent"
	rtcompaction "github.com/lengzhao/agentkit/runtime/compaction"
	"github.com/lengzhao/agentkit/runtime/hooks"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

// This file covers the compaction/overflow fix chain end to end: real
// agent/coding runtime, real plugins/compaction services, real JSONL session
// store — only the LLMs are faked. Scenarios map to docs/guides/e2e-scenarios
// E2E-023 / E2E-301 / E2E-304 / E2E-305 / E2E-306.

const compactionAgentID = agentkit.AgentID("smoke")

// gateLLM is a fake main-model provider: it records the prompt size of every
// call and fails the first overflowFailures calls with a context-overflow
// error, then replies "ok".
type gateLLM struct {
	mu               sync.Mutex
	overflowFailures int
	sizes            []int
}

func (g *gateLLM) Name() string { return "gate" }

func (g *gateLLM) Stream(_ context.Context, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	total := 0
	for _, msg := range req.Messages {
		for _, part := range msg.Content {
			total += len(part.Text) + len(part.URL)
		}
	}
	g.sizes = append(g.sizes, total)
	if g.overflowFailures > 0 {
		g.overflowFailures--
		return nil, fmt.Errorf("maximum context length exceeded")
	}
	return &oneShotStream{msg: agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: "ok"}},
	}}, nil
}

func (g *gateLLM) calls() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.sizes)
}

func (g *gateLLM) promptSizes() []int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]int(nil), g.sizes...)
}

type oneShotStream struct {
	msg  agentkit.ModelMessage
	sent bool
}

func (s *oneShotStream) Recv() (agentkit.LLMEvent, error) {
	if s.sent {
		return agentkit.LLMEvent{}, io.EOF
	}
	s.sent = true
	return agentkit.LLMEvent{Type: agentkit.LLMEventMessage, Message: &s.msg}, nil
}

func (s *oneShotStream) Close() error { return nil }

// countingLLM wraps a provider and counts Stream calls, so tests can assert
// whether the summary model was invoked at all.
type countingLLM struct {
	inner agentkit.LLMProvider
	mu    sync.Mutex
	n     int
}

func (c *countingLLM) Name() string { return c.inner.Name() }

func (c *countingLLM) Stream(ctx context.Context, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return c.inner.Stream(ctx, req)
}

func (c *countingLLM) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func mustSummaryService(t *testing.T, keepRecentTokens int, summaryLLM agentkit.LLMProvider) capcompaction.Service {
	t.Helper()
	events, err := sessevents.New()
	if err != nil {
		t.Fatal(err)
	}
	svc, err := rtcompaction.NewSummary(rtcompaction.SummaryConfig{
		KeepRecentTokens: keepRecentTokens,
	}, rtcompaction.SummaryDeps{LLM: summaryLLM, SessionEvents: events})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func mustPruneService(t *testing.T, maxBytes int) capcompaction.Service {
	t.Helper()
	svc, err := rtcompaction.NewPrune(rtcompaction.PruneConfig{MaxToolResultBytes: maxBytes})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// newCompactionAgent builds a real agent/coding runtime with retry disabled so
// the overflow path is deterministic.
func newCompactionAgent(t *testing.T, store agentkit.SessionStore, root string, mainLLM agentkit.LLMProvider, services []capcompaction.Service, maxPromptTokens int, hookRuntime agentkit.HookRuntime) agentkit.Agent {
	t.Helper()
	disabled := false
	ag, err := agent.New(agent.Config{
		ID:              compactionAgentID,
		Model:           "gate",
		Retry:           &agent.RetryConfig{Enabled: &disabled},
		MaxPromptTokens: maxPromptTokens,
	}, agent.Deps{
		SessionStore: store,
		LLM:          mainLLM,
		Tools:        agenttest.EmptyToolsRuntime(t),
		Prompt:       agenttest.DefaultAssembler(t),
		Hooks:        hookRuntime,
		Compaction:   services,
		Workspace:    rtworkspace.Static(root),
	})
	if err != nil {
		t.Fatal(err)
	}
	return ag
}

func seedText(t *testing.T, ctx context.Context, store agentkit.SessionStore, sessionID agentkit.SessionID, role, text string) {
	t.Helper()
	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	typ := agentkit.EventUserMessage
	if role == "assistant" {
		typ = agentkit.EventAssistantMessage
	}
	if err := sessevents.Default.AppendMessage(ctx, sess, compactionAgentID, typ, agentkit.ModelMessage{
		Role:    role,
		Content: []agentkit.ContentPart{{Type: "text", Text: text}},
	}); err != nil {
		t.Fatal(err)
	}
}

// seedToolCall writes an assistant tool call followed by its (large) result,
// mirroring a real tool loop in the session log.
func seedToolCall(t *testing.T, ctx context.Context, store agentkit.SessionStore, sessionID agentkit.SessionID, callID agentkit.ToolCallID, result string) {
	t.Helper()
	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendMessage(ctx, sess, compactionAgentID, agentkit.EventAssistantMessage, agentkit.ModelMessage{
		Role: "assistant",
		ToolCalls: []agentkit.ToolCall{{
			ID:    callID,
			Name:  "read",
			Input: []byte(`{"path":"big.txt"}`),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendToolResult(ctx, sess, compactionAgentID, agentkit.ToolResult{
		ID:      callID,
		Name:    "read",
		Content: result,
	}); err != nil {
		t.Fatal(err)
	}
}

func runText(t *testing.T, ctx context.Context, ag agentkit.Agent, text string) error {
	t.Helper()
	return ag.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		},
	})
}

func compactionEvents(t *testing.T, events []agentkit.SessionEvent) []capcompaction.EventData {
	t.Helper()
	var out []capcompaction.EventData
	for _, ev := range events {
		if ev.Type != agentkit.EventCompaction {
			continue
		}
		var data capcompaction.EventData
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			t.Fatal(err)
		}
		out = append(out, data)
	}
	return out
}

func overflowRecoveries(t *testing.T, events []agentkit.SessionEvent) []capsession.OverflowRecoveryData {
	t.Helper()
	var out []capsession.OverflowRecoveryData
	for _, ev := range events {
		if ev.Type != agentkit.EventOverflowRecovery {
			continue
		}
		var data capsession.OverflowRecoveryData
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			t.Fatal(err)
		}
		out = append(out, data)
	}
	return out
}

// E2E-304: overflow -> real compaction/summary persists a compaction event ->
// the retried step sends a strictly smaller prompt and the turn completes.
func TestSmokeOverflowRecoveryCompactsWithRealSummary(t *testing.T) {
	t.Parallel()

	store, root := agenttest.TempFileStore(t)
	sessionID := agentkit.SessionID("smoke:overflow-summary")
	ctx := agenttest.TurnContext(sessionID, compactionAgentID)

	// 4 x 300 chars; keepRecent=100 tokens keeps the last user/assistant pair,
	// so forced compaction summarizes the older two messages.
	seedText(t, ctx, store, sessionID, "user", strings.Repeat("u", 300))
	seedText(t, ctx, store, sessionID, "assistant", strings.Repeat("a", 300))
	seedText(t, ctx, store, sessionID, "user", strings.Repeat("v", 300))
	seedText(t, ctx, store, sessionID, "assistant", strings.Repeat("b", 300))

	summaryLLM := &countingLLM{inner: agenttest.MustScripted(t, llm.ScriptedStep{Text: "brief summary"})}
	summary := mustSummaryService(t, 100, summaryLLM)
	main := &gateLLM{overflowFailures: 1}
	ag := newCompactionAgent(t, store, root, main, []capcompaction.Service{summary}, 0, nil)

	agenttest.RunTurn(t, ctx, ag, "hi")

	if got := main.calls(); got != 2 {
		t.Fatalf("main llm calls = %d, want 2 (overflow + retry)", got)
	}
	sizes := main.promptSizes()
	if sizes[1] >= sizes[0] {
		t.Fatalf("retry prompt not smaller: first=%d second=%d", sizes[0], sizes[1])
	}
	if got := summaryLLM.count(); got != 1 {
		t.Fatalf("summary llm calls = %d, want 1", got)
	}

	events := agenttest.SessionEvents(t, context.Background(), store, sessionID)
	compactions := compactionEvents(t, events)
	if len(compactions) != 1 {
		t.Fatalf("session/compaction = %d, want 1", len(compactions))
	}
	if compactions[0].Kind != capcompaction.KindSummary {
		t.Fatalf("compaction kind = %q, want %q", compactions[0].Kind, capcompaction.KindSummary)
	}
	recoveries := overflowRecoveries(t, events)
	if len(recoveries) != 1 || recoveries[0].Applied != 1 {
		t.Fatalf("overflow/recovery = %+v, want exactly 1 event with applied=1", recoveries)
	}
}

// E2E-304: a prune-only chain may report Applied (it did truncate the in-flight
// view) but persists no compaction event; overflow recovery must not retry the
// same oversized request and must surface an error instead.
func TestSmokeOverflowRecoveryPruneOnlyChainDoesNotRetry(t *testing.T) {
	t.Parallel()

	store, root := agenttest.TempFileStore(t)
	sessionID := agentkit.SessionID("smoke:overflow-prune-only")
	ctx := agenttest.TurnContext(sessionID, compactionAgentID)

	seedText(t, ctx, store, sessionID, "user", "read the big file")
	seedToolCall(t, ctx, store, sessionID, "call-read", strings.Repeat("x", 5000))

	prune := mustPruneService(t, 100) // truncates the tool result, reports Applied
	main := &gateLLM{overflowFailures: 1 << 30}
	ag := newCompactionAgent(t, store, root, main, []capcompaction.Service{prune}, 0, nil)

	err := runText(t, ctx, ag, "hi")
	if err == nil {
		t.Fatal("expected overflow recovery failure")
	}
	if !strings.Contains(err.Error(), "compaction did not apply") {
		t.Fatalf("error = %v, want compaction-did-not-apply", err)
	}
	if got := main.calls(); got != 1 {
		t.Fatalf("main llm calls = %d, want 1: prune-only chain must not retry", got)
	}

	events := agenttest.SessionEvents(t, context.Background(), store, sessionID)
	if got := len(compactionEvents(t, events)); got != 0 {
		t.Fatalf("session/compaction = %d, want 0", got)
	}
	recoveries := overflowRecoveries(t, events)
	if len(recoveries) != 1 || recoveries[0].Applied != 0 {
		t.Fatalf("overflow/recovery = %+v, want exactly 1 event with applied=0", recoveries)
	}
}

// E2E-304: the production pipeline order prune -> summary recovers from
// overflow: prune trims the in-flight view, summary persists the real
// compaction event that unlocks the retry.
func TestSmokeOverflowRecoveryPruneThenSummaryChain(t *testing.T) {
	t.Parallel()

	store, root := agenttest.TempFileStore(t)
	sessionID := agentkit.SessionID("smoke:overflow-chain")
	ctx := agenttest.TurnContext(sessionID, compactionAgentID)

	seedText(t, ctx, store, sessionID, "user", strings.Repeat("u", 300))
	seedToolCall(t, ctx, store, sessionID, "call-read", strings.Repeat("x", 5000))
	seedText(t, ctx, store, sessionID, "assistant", strings.Repeat("a", 300))
	seedText(t, ctx, store, sessionID, "user", strings.Repeat("v", 300))
	seedText(t, ctx, store, sessionID, "assistant", strings.Repeat("b", 300))

	summaryLLM := &countingLLM{inner: agenttest.MustScripted(t, llm.ScriptedStep{Text: "brief summary"})}
	prune := mustPruneService(t, 100)
	summary := mustSummaryService(t, 100, summaryLLM)
	main := &gateLLM{overflowFailures: 1}
	ag := newCompactionAgent(t, store, root, main, []capcompaction.Service{prune, summary}, 0, nil)

	agenttest.RunTurn(t, ctx, ag, "hi")

	if got := main.calls(); got != 2 {
		t.Fatalf("main llm calls = %d, want 2 (overflow + retry)", got)
	}
	sizes := main.promptSizes()
	if sizes[1] >= sizes[0] {
		t.Fatalf("retry prompt not smaller: first=%d second=%d", sizes[0], sizes[1])
	}

	events := agenttest.SessionEvents(t, context.Background(), store, sessionID)
	if got := len(compactionEvents(t, events)); got != 1 {
		t.Fatalf("session/compaction = %d, want 1", got)
	}
	recoveries := overflowRecoveries(t, events)
	if len(recoveries) != 1 || recoveries[0].Applied != 2 {
		t.Fatalf("overflow/recovery = %+v, want exactly 1 event with applied=2 (prune+summary)", recoveries)
	}
}

// E2E-023: hook/before-step + compaction/token-limit trigger automatic
// compaction before the first model call once the estimated context crosses
// the threshold — no overflow error involved.
func TestSmokeBeforeStepTokenLimitCompacts(t *testing.T) {
	t.Parallel()

	store, root := agenttest.TempFileStore(t)
	sessionID := agentkit.SessionID("smoke:beforestep-tokenlimit")
	ctx := agenttest.TurnContext(sessionID, compactionAgentID)

	seedText(t, ctx, store, sessionID, "user", strings.Repeat("u", 300))
	seedText(t, ctx, store, sessionID, "assistant", strings.Repeat("a", 300))
	seedText(t, ctx, store, sessionID, "user", strings.Repeat("v", 300))
	seedText(t, ctx, store, sessionID, "assistant", strings.Repeat("b", 300))

	summaryLLM := &countingLLM{inner: agenttest.MustScripted(t, llm.ScriptedStep{Text: "brief summary"})}
	// keepRecent=100 keeps the last user/assistant pair plus "hi" (~156
	// tokens estimated) and lands the cut on a user message, so the summary
	// path is a single call (no split turn).
	summary := mustSummaryService(t, 100, summaryLLM)
	chain, err := rtcompaction.NewChain(struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	gated, err := plugincompaction.NewTokenLimit(plugincompaction.TokenLimitConfig{
		MaxTokens: 100, // seeded history estimates ~300 tokens
	}, plugincompaction.TokenLimitDeps{Chain: chain, Services: []capcompaction.Service{summary}})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := pluginhook.New(pluginhook.Config{}, pluginhook.Deps{
		Compaction:   gated,
		SessionStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	hookRuntime, err := hooks.New(hooks.Config{}, hooks.Deps{Providers: []agentkit.HookProvider{provider}})
	if err != nil {
		t.Fatal(err)
	}

	main := &gateLLM{}
	ag := newCompactionAgent(t, store, root, main, nil, 0, hookRuntime)

	agenttest.RunTurn(t, ctx, ag, "hi")

	if got := main.calls(); got != 1 {
		t.Fatalf("main llm calls = %d, want 1", got)
	}
	if sizes := main.promptSizes(); sizes[0] >= 1200 {
		t.Fatalf("first prompt not compacted by before-step hook, chars=%d", sizes[0])
	}
	if got := summaryLLM.count(); got != 1 {
		t.Fatalf("summary llm calls = %d, want 1", got)
	}

	events := agenttest.SessionEvents(t, context.Background(), store, sessionID)
	if got := len(compactionEvents(t, events)); got != 1 {
		t.Fatalf("session/compaction = %d, want 1", got)
	}
	if got := len(overflowRecoveries(t, events)); got != 0 {
		t.Fatalf("overflow/recovery = %d, want 0: compaction happened before send, not after overflow", got)
	}
}

// E2E-305: maxPromptTokens compacts the assembled prompt via the real summary
// service before the first LLM call; the provider never sees the oversized
// request and no overflow occurs.
func TestSmokePreSendGuardCompactsBeforeFirstSend(t *testing.T) {
	t.Parallel()

	store, root := agenttest.TempFileStore(t)
	sessionID := agentkit.SessionID("smoke:presend-compact")
	ctx := agenttest.TurnContext(sessionID, compactionAgentID)

	seedText(t, ctx, store, sessionID, "user", strings.Repeat("u", 300))
	seedText(t, ctx, store, sessionID, "assistant", strings.Repeat("a", 300))
	seedText(t, ctx, store, sessionID, "user", strings.Repeat("v", 300))
	seedText(t, ctx, store, sessionID, "assistant", strings.Repeat("b", 300))

	summaryLLM := &countingLLM{inner: agenttest.MustScripted(t, llm.ScriptedStep{Text: "brief summary"})}
	summary := mustSummaryService(t, 100, summaryLLM)
	main := &gateLLM{}
	// Seeded history + "hi" estimates ~300 tokens; the guard limit is 200.
	ag := newCompactionAgent(t, store, root, main, []capcompaction.Service{summary}, 200, nil)

	agenttest.RunTurn(t, ctx, ag, "hi")

	if got := main.calls(); got != 1 {
		t.Fatalf("main llm calls = %d, want 1: guard must compact before the first send", got)
	}
	if sizes := main.promptSizes(); sizes[0] > 200*4 {
		t.Fatalf("prompt sent over the guard limit, chars=%d", sizes[0])
	}
	if got := summaryLLM.count(); got != 1 {
		t.Fatalf("summary llm calls = %d, want 1", got)
	}

	events := agenttest.SessionEvents(t, context.Background(), store, sessionID)
	if got := len(compactionEvents(t, events)); got != 1 {
		t.Fatalf("session/compaction = %d, want 1", got)
	}
	if got := len(overflowRecoveries(t, events)); got != 0 {
		t.Fatalf("overflow/recovery = %d, want 0", got)
	}
}

// E2E-305: when the compaction chain cannot shrink the prompt (prune does not
// touch user text), the guard fails the step and nothing is sent.
func TestSmokePreSendGuardFailsWhenChainCannotCompact(t *testing.T) {
	t.Parallel()

	store, root := agenttest.TempFileStore(t)
	sessionID := agentkit.SessionID("smoke:presend-fail")
	ctx := agenttest.TurnContext(sessionID, compactionAgentID)

	seedText(t, ctx, store, sessionID, "user", strings.Repeat("y", 20000))

	prune := mustPruneService(t, 100)
	main := &gateLLM{}
	ag := newCompactionAgent(t, store, root, main, []capcompaction.Service{prune}, 1000, nil)

	err := runText(t, ctx, ag, "hi")
	if err == nil {
		t.Fatal("expected pre-send guard failure")
	}
	if !strings.Contains(err.Error(), "maxPromptTokens") {
		t.Fatalf("error = %v, want mention of maxPromptTokens", err)
	}
	if got := main.calls(); got != 0 {
		t.Fatalf("main llm calls = %d, want 0: oversized prompt must not be sent", got)
	}

	events := agenttest.SessionEvents(t, context.Background(), store, sessionID)
	if got := len(compactionEvents(t, events)); got != 0 {
		t.Fatalf("session/compaction = %d, want 0", got)
	}
}

// E2E-306: the whole history is one giant message, so there is nothing to
// summarize; forced compaction writes a truncate-only compaction event without
// calling the summary model, the guard then lets the bounded prompt through,
// and the durable log keeps the original full text.
func TestSmokePreSendGuardGiantMessageTruncateOnly(t *testing.T) {
	t.Parallel()

	store, root := agenttest.TempFileStore(t)
	sessionID := agentkit.SessionID("smoke:presend-giant")
	ctx := agenttest.TurnContext(sessionID, compactionAgentID)

	const giantLen = 20000
	seedText(t, ctx, store, sessionID, "user", strings.Repeat("y", giantLen))

	summaryLLM := &countingLLM{inner: agenttest.MustScripted(t, llm.ScriptedStep{Text: "unused"})}
	summary := mustSummaryService(t, 200, summaryLLM) // retained budget 800 chars
	main := &gateLLM{}
	ag := newCompactionAgent(t, store, root, main, []capcompaction.Service{summary}, 1000, nil)

	agenttest.RunTurn(t, ctx, ag, "hi")

	if got := summaryLLM.count(); got != 0 {
		t.Fatalf("summary llm calls = %d, want 0: nothing to summarize", got)
	}
	if got := main.calls(); got != 1 {
		t.Fatalf("main llm calls = %d, want 1", got)
	}
	if sizes := main.promptSizes(); sizes[0] > 1000*4 {
		t.Fatalf("prompt sent over the guard limit, chars=%d", sizes[0])
	}

	events := agenttest.SessionEvents(t, context.Background(), store, sessionID)
	compactions := compactionEvents(t, events)
	if len(compactions) != 1 {
		t.Fatalf("session/compaction = %d, want 1", len(compactions))
	}
	if got := compactions[0].Summary.Content[0].Text; !strings.Contains(got, "could not be summarized") {
		t.Fatalf("truncate-only summary marker missing, got %q", got)
	}
	if len(compactions[0].RetainedTail) == 0 {
		t.Fatal("truncate-only compaction must keep a retained tail")
	}
	// The giant message is bounded out of the model-visible view: truncated to
	// the per-message budget, then dropped oldest when the total still exceeds
	// the keep budget. The newest message is always retained.
	retainedChars := 0
	for _, msg := range compactions[0].RetainedTail {
		for _, part := range msg.Content {
			if len(part.Text) >= giantLen {
				t.Fatalf("retained tail still carries the full giant message, len=%d", len(part.Text))
			}
			retainedChars += len(part.Text)
		}
	}
	if retainedChars > 800 { // keepRecentTokens=200 → 800 chars
		t.Fatalf("retained tail exceeds the keep budget, chars=%d", retainedChars)
	}
	last := compactions[0].RetainedTail[len(compactions[0].RetainedTail)-1]
	if len(last.Content) == 0 || last.Content[0].Text != "hi" {
		t.Fatalf("newest message must be retained, got %#v", last.Content)
	}

	// Durable log keeps the original full text; only the model-visible view
	// inside the compaction event is truncated.
	full := 0
	for _, ev := range events {
		if ev.Type != agentkit.EventUserMessage {
			continue
		}
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			t.Fatal(err)
		}
		for _, part := range msg.Content {
			if len(part.Text) == giantLen {
				full++
			}
		}
	}
	if full != 1 {
		t.Fatalf("durable user message with full text = %d, want 1", full)
	}
}

// E2E-301: the compaction event is plain JSONL — a fresh store instance on the
// same directory derives the compacted (bounded) view after a restart.
func TestSmokeCompactionViewSurvivesStoreReopen(t *testing.T) {
	t.Parallel()

	store, root := agenttest.TempFileStore(t)
	sessionID := agentkit.SessionID("smoke:compaction-reopen")
	ctx := agenttest.TurnContext(sessionID, compactionAgentID)

	seedText(t, ctx, store, sessionID, "user", strings.Repeat("y", 20000))

	summaryLLM := &countingLLM{inner: agenttest.MustScripted(t, llm.ScriptedStep{Text: "unused"})}
	summary := mustSummaryService(t, 200, summaryLLM)
	main := &gateLLM{}
	ag := newCompactionAgent(t, store, root, main, []capcompaction.Service{summary}, 1000, nil)
	agenttest.RunTurn(t, ctx, ag, "hi")

	reopened, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{
		Workspace: rtworkspace.Static(root),
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := reopened.Get(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) == 0 {
		t.Fatal("derived history empty after reopen")
	}
	total := 0
	for _, msg := range messages {
		for _, part := range msg.Content {
			total += len(part.Text)
		}
	}
	if total >= 20000 {
		t.Fatalf("derived history after reopen still oversized, chars=%d", total)
	}
	if got := agenttest.ContentText(messages[0]); !strings.Contains(got, "could not be summarized") {
		t.Fatalf("first derived message should be the compaction summary, got %q", got)
	}
}
