package smoke_test

import (
	"context"
	"io"
	"testing"

	capllm "github.com/lengzhao/agentkit/cap/llm"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/llm"
)

func TestLLMRouterSmoke(t *testing.T) {
	t.Parallel()
	responses := &namedCatalogProvider{name: "openai-responses", ids: []string{"gpt-4o"}}
	defaultP := &namedCatalogProvider{name: "gateway-default"}

	router, err := llm.NewRouter(struct{}{}, llm.RouterDeps{
		Protocols: []agentkit.LLMProvider{responses, defaultP},
		Default:   defaultP,
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := router.Stream(context.Background(), agentkit.LLMRequest{Model: "gpt-4o"})
	if err != nil {
		t.Fatal(err)
	}
	ev, err := stream.Recv()
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if ev.Message == nil || ev.Message.Content[0].Text != "provider:openai-responses" {
		t.Fatalf("unexpected response: %+v", ev.Message)
	}
	_ = stream.Close()

	stream, err = router.Stream(context.Background(), agentkit.LLMRequest{Model: "custom-via-gateway"})
	if err != nil {
		t.Fatal(err)
	}
	ev, _ = stream.Recv()
	if ev.Message == nil || ev.Message.Content[0].Text != "provider:gateway-default" {
		t.Fatalf("expected default route, got %+v", ev.Message)
	}
	_ = stream.Close()
}

type namedCatalogProvider struct {
	name string
	ids  []string
}

func (p *namedCatalogProvider) Name() string { return p.name }

func (p *namedCatalogProvider) CatalogModels() []capllm.ModelEntry {
	out := make([]capllm.ModelEntry, len(p.ids))
	for i, id := range p.ids {
		out[i] = capllm.ModelEntry{ID: id}
	}
	return out
}

func (p *namedCatalogProvider) Stream(_ context.Context, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	text := "provider:" + p.name
	return &oneLineLLMStream{text: text}, nil
}

type oneLineLLMStream struct {
	text string
	sent bool
}

func (s *oneLineLLMStream) Recv() (agentkit.LLMEvent, error) {
	if s.sent {
		return agentkit.LLMEvent{}, io.EOF
	}
	s.sent = true
	msg := agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: s.text}},
	}
	return agentkit.LLMEvent{Type: agentkit.LLMEventMessage, Message: &msg}, io.EOF
}

func (s *oneLineLLMStream) Close() error { return nil }
