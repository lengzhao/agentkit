package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/telemetry"
	"github.com/lengzhao/pluginkit/build"
	openai "github.com/sashabaranov/go-openai"
)

type stubProvider struct {
	name      string
	model     string
	failOpen  []bool
	failRecv  []bool
	replyText string
	mu        sync.Mutex
	openCalls int
}

func (p *stubProvider) Name() string {
	if p.name != "" {
		return p.name
	}
	return "stub"
}

func (p *stubProvider) Stream(_ context.Context, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.openCalls++
	idx := p.openCalls - 1
	if idx < len(p.failOpen) && p.failOpen[idx] {
		return nil, errors.New("rate limit exceeded")
	}
	model := req.Model
	if model == "" {
		model = p.model
	}
	return &stubStream{
		provider: p,
		model:    model,
		text:     p.replyText,
		failRecv: append([]bool(nil), p.failRecv...),
	}, nil
}

type stubStream struct {
	provider *stubProvider
	model    string
	text     string
	failRecv []bool
	recvIdx  int
	closed   bool
}

func (s *stubStream) Recv() (agentkit.LLMEvent, error) {
	if s.closed {
		return agentkit.LLMEvent{}, io.EOF
	}
	if s.recvIdx < len(s.failRecv) && s.failRecv[s.recvIdx] {
		s.recvIdx++
		return agentkit.LLMEvent{}, errors.New("connection lost")
	}
	s.closed = true
	msg := agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: s.text}},
	}
	if s.text == "" {
		s.text = fmt.Sprintf("ok:%s", s.model)
		msg.Content = []agentkit.ContentPart{{Type: "text", Text: s.text}}
	}
	return agentkit.LLMEvent{Type: agentkit.LLMEventMessage, Message: &msg}, io.EOF
}

func (s *stubStream) Close() error { return nil }

func TestNewFallbackRequiresProvider(t *testing.T) {
	t.Parallel()
	_, err := NewFallback(FallbackConfig{FallbackModels: []string{"gpt-4o"}}, FallbackDeps{})
	if err == nil {
		t.Fatal("expected error without provider")
	}
}

func TestNewFallbackRequiresModels(t *testing.T) {
	t.Parallel()
	_, err := NewFallback(FallbackConfig{}, FallbackDeps{Provider: &stubProvider{}})
	if err == nil {
		t.Fatal("expected error without models")
	}
}

func TestFallbackUsesPrimaryModelOnSuccess(t *testing.T) {
	t.Parallel()
	primary := &stubProvider{name: "primary", replyText: "primary-ok"}
	fallback, err := NewFallback(FallbackConfig{
		FallbackModels: []string{"gpt-4o"},
	}, FallbackDeps{Provider: primary})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := fallback.Stream(context.Background(), agentkit.LLMRequest{Model: "gpt-5.4"})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	ev, err := stream.Recv()
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if ev.Message == nil || messageText(ev.Message.Content) != "primary-ok" {
		t.Fatalf("unexpected event: %+v", ev)
	}
	if primary.openCalls != 1 {
		t.Fatalf("open calls = %d, want 1", primary.openCalls)
	}
}

func TestFallbackRecordsTraceOnSwitch(t *testing.T) {
	t.Parallel()
	rec := &telemetry.RecordingExporter{}
	ctx := WithActualModelSlot(telemetry.WithExporter(context.Background(), rec))

	primary := &stubProvider{name: "primary", failOpen: []bool{true}}
	secondary := &stubProvider{name: "secondary", replyText: "backup-ok"}
	fallback, err := NewFallback(FallbackConfig{
		FallbackModels: []string{"gpt-4o"},
	}, FallbackDeps{
		Provider:  primary,
		Fallbacks: []agentkit.LLMProvider{secondary},
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := fallback.Stream(ctx, agentkit.LLMRequest{Model: "gpt-5.4"})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Recv(); err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}

	_, _, events := rec.Snapshot()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.Name != "llm.fallback" {
		t.Fatalf("event name = %q", ev.Name)
	}
	if ev.Attrs["phase"] != "stream_open" || ev.Attrs["reason"] != "rate limit exceeded" {
		t.Fatalf("attrs = %v", ev.Attrs)
	}
	if ev.Attrs["from_provider"] != "primary" || ev.Attrs["to_provider"] != "secondary" {
		t.Fatalf("provider attrs = %v", ev.Attrs)
	}
	if ev.Attrs["from_model"] != "gpt-5.4" || ev.Attrs["to_model"] != "gpt-4o" {
		t.Fatalf("model attrs = %v", ev.Attrs)
	}
	if got := ActualModelFrom(ctx); got != "gpt-4o" {
		t.Fatalf("actual model = %q, want gpt-4o after fallback", got)
	}
}

func TestFallbackSwitchesModelOnRetryableError(t *testing.T) {
	t.Parallel()
	primary := &stubProvider{name: "primary", failOpen: []bool{true}}
	secondary := &stubProvider{name: "secondary", replyText: "backup-ok"}
	fallback, err := NewFallback(FallbackConfig{
		Models: []string{"gpt-5.4", "gpt-4o"},
	}, FallbackDeps{
		Provider:  primary,
		Fallbacks: []agentkit.LLMProvider{secondary},
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := fallback.Stream(context.Background(), agentkit.LLMRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	ev, err := stream.Recv()
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if messageText(ev.Message.Content) != "backup-ok" {
		t.Fatalf("reply = %q, want backup-ok", messageText(ev.Message.Content))
	}
	if primary.openCalls != 1 || secondary.openCalls != 1 {
		t.Fatalf("open calls primary=%d secondary=%d", primary.openCalls, secondary.openCalls)
	}
}

func TestFallbackUsesRequestModelBeforeFallbackModels(t *testing.T) {
	t.Parallel()
	var seen []string
	provider := &recordingProvider{models: &seen}
	fallback, err := NewFallback(FallbackConfig{
		FallbackModels: []string{"gpt-4o", "gpt-4o-mini"},
	}, FallbackDeps{Provider: provider})
	if err != nil {
		t.Fatal(err)
	}

	provider.failModel = "gpt-5.4"
	stream, err := fallback.Stream(context.Background(), agentkit.LLMRequest{Model: "gpt-5.4"})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Recv(); err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}

	if len(seen) != 2 || seen[0] != "gpt-5.4" || seen[1] != "gpt-4o" {
		t.Fatalf("models tried = %v", seen)
	}
}

func TestFallbackSwitchesOnModelUnavailableInGroup(t *testing.T) {
	t.Parallel()
	errUnavailable := &openai.APIError{
		HTTPStatusCode: 404,
		HTTPStatus:     "404 Not Found",
		Message:        "Model 'deepseek-v4-flash' is not available in this group.",
	}
	primary := &errorProvider{err: errUnavailable}
	secondary := &stubProvider{replyText: "backup"}
	fallback, err := NewFallback(FallbackConfig{
		FallbackModels: []string{"glm-5.3"},
	}, FallbackDeps{Provider: primary, Fallbacks: []agentkit.LLMProvider{secondary}})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := fallback.Stream(context.Background(), agentkit.LLMRequest{Model: "deepseek-v4-flash"})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	ev, err := stream.Recv()
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if messageText(ev.Message.Content) != "backup" {
		t.Fatalf("reply = %q", messageText(ev.Message.Content))
	}
	if secondary.openCalls != 1 {
		t.Fatalf("secondary open calls = %d, want 1", secondary.openCalls)
	}
	if IsRetryableError(errUnavailable) {
		t.Fatal("model unavailable in group must not retry the same model")
	}
}

func TestShouldFallbackModelUnavailableInGroup(t *testing.T) {
	t.Parallel()
	unavailable := &openai.APIError{
		HTTPStatusCode: 404,
		HTTPStatus:     "404 Not Found",
		Message:        "Model 'deepseek-v4-flash' is not available in this group.",
	}
	for _, mode := range []fallbackMode{fallbackOnRetryable, fallbackOnQuota, fallbackOnAny} {
		if !shouldFallback(unavailable, mode) {
			t.Fatalf("mode %s should fallback", mode)
		}
	}
	plain404 := &openai.APIError{HTTPStatusCode: 404, HTTPStatus: "404 Not Found", Message: "Not Found"}
	if shouldFallback(plain404, fallbackOnRetryable) {
		t.Fatal("unrelated 404 should not fallback")
	}
}

func TestFallbackDoesNotSwitchOnContextOverflow(t *testing.T) {
	t.Parallel()
	primary := &errorProvider{err: errors.New("maximum context length exceeded")}
	secondary := &stubProvider{replyText: "backup"}
	fallback, err := NewFallback(FallbackConfig{
		FallbackModels: []string{"gpt-4o"},
	}, FallbackDeps{Provider: primary, Fallbacks: []agentkit.LLMProvider{secondary}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = fallback.Stream(context.Background(), agentkit.LLMRequest{Model: "gpt-5.4"})
	if err == nil {
		t.Fatal("expected overflow error")
	}
	if secondary.openCalls != 0 {
		t.Fatalf("secondary open calls = %d, want 0", secondary.openCalls)
	}
}

func TestFallbackQuotaMode(t *testing.T) {
	t.Parallel()
	primary := &errorProvider{err: errors.New("insufficient_quota")}
	secondary := &stubProvider{replyText: "backup"}
	fallback, err := NewFallback(FallbackConfig{
		FallbackModels: []string{"gpt-4o"},
		FallbackOn:     "quota",
	}, FallbackDeps{Provider: primary, Fallbacks: []agentkit.LLMProvider{secondary}})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := fallback.Stream(context.Background(), agentkit.LLMRequest{Model: "gpt-5.4"})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	ev, err := stream.Recv()
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if messageText(ev.Message.Content) != "backup" {
		t.Fatalf("reply = %q", messageText(ev.Message.Content))
	}
}

func TestFallbackSwitchesOnDeadlineExceededBeforeOutput(t *testing.T) {
	t.Parallel()
	primary := &deadlineOnRecvProvider{}
	secondary := &stubProvider{replyText: "backup"}
	fallback, err := NewFallback(FallbackConfig{
		FallbackModels: []string{"gpt-4o"},
	}, FallbackDeps{Provider: primary, Fallbacks: []agentkit.LLMProvider{secondary}})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := fallback.Stream(context.Background(), agentkit.LLMRequest{Model: "gpt-5.4"})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	ev, err := stream.Recv()
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if messageText(ev.Message.Content) != "backup" {
		t.Fatalf("reply = %q", messageText(ev.Message.Content))
	}
	if primary.openCalls != 1 || secondary.openCalls != 1 {
		t.Fatalf("open calls primary=%d secondary=%d", primary.openCalls, secondary.openCalls)
	}
}

func TestFallbackRecvFailureBeforeOutput(t *testing.T) {
	t.Parallel()
	primary := &stubProvider{failRecv: []bool{true}}
	secondary := &stubProvider{replyText: "backup"}
	fallback, err := NewFallback(FallbackConfig{
		FallbackModels: []string{"gpt-4o"},
	}, FallbackDeps{Provider: primary, Fallbacks: []agentkit.LLMProvider{secondary}})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := fallback.Stream(context.Background(), agentkit.LLMRequest{Model: "gpt-5.4"})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	ev, err := stream.Recv()
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if messageText(ev.Message.Content) != "backup" {
		t.Fatalf("reply = %q", messageText(ev.Message.Content))
	}
}

func TestFallbackBuildViaPluginkit(t *testing.T) {
	graph := map[string]any{
		"primary": map[string]any{
			"use":    "llm/scripted",
			"config": map[string]any{"steps": []map[string]any{{"text": "ok"}}},
		},
		"backup": map[string]any{
			"use":    "llm/scripted",
			"config": map[string]any{"steps": []map[string]any{{"text": "backup"}}},
		},
		"llm": map[string]any{
			"use": "llm/fallback",
			"config": map[string]any{
				"fallbackModels": []string{"backup-model"},
			},
			"deps": map[string]any{
				"provider":  "primary",
				"fallbacks": []string{"backup"},
			},
		},
	}

	provider, _, err := build.Build[agentkit.LLMProvider](context.Background(), graph, "llm")
	if err != nil {
		t.Fatalf("build llm/fallback: %v", err)
	}
	if provider.Name() != "fallback" {
		t.Fatalf("name = %q", provider.Name())
	}
}

type deadlineOnRecvProvider struct {
	openCalls int
}

func (p *deadlineOnRecvProvider) Name() string { return "deadline" }

func (p *deadlineOnRecvProvider) Stream(context.Context, agentkit.LLMRequest) (agentkit.LLMStream, error) {
	p.openCalls++
	return &deadlineRecvStream{}, nil
}

type deadlineRecvStream struct{}

func (d *deadlineRecvStream) Recv() (agentkit.LLMEvent, error) {
	return agentkit.LLMEvent{}, context.DeadlineExceeded
}

func (d *deadlineRecvStream) Close() error { return nil }

type errorProvider struct {
	err error
}

func (p *errorProvider) Name() string { return "error" }

func (p *errorProvider) Stream(context.Context, agentkit.LLMRequest) (agentkit.LLMStream, error) {
	return nil, p.err
}

type recordingProvider struct {
	models    *[]string
	failModel string
}

func (p *recordingProvider) Name() string { return "recording" }

func (p *recordingProvider) Stream(_ context.Context, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	*p.models = append(*p.models, req.Model)
	if req.Model == p.failModel {
		return nil, errors.New("rate limit exceeded")
	}
	return (&stubProvider{model: req.Model, replyText: "ok"}).Stream(context.Background(), req)
}

func messageText(parts []agentkit.ContentPart) string {
	var out string
	for _, part := range parts {
		if part.Type == "text" {
			out += part.Text
		}
	}
	return out
}
