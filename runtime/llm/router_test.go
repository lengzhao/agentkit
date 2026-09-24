package llm

import (
	"context"
	"io"
	"testing"

	capllm "github.com/lengzhao/agentkit/cap/llm"
	"github.com/lengzhao/agentkit"
)

type catalogStub struct {
	name    string
	models  []capllm.ModelEntry
	lastReq agentkit.LLMRequest
}

func (s *catalogStub) Name() string { return s.name }

func (s *catalogStub) CatalogModels() []capllm.ModelEntry { return s.models }

func (s *catalogStub) Stream(_ context.Context, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	s.lastReq = req
	return &stubOneShotStream{text: "from:" + s.name}, nil
}

type stubOneShotStream struct {
	text string
	done bool
}

func (s *stubOneShotStream) Recv() (agentkit.LLMEvent, error) {
	if s.done {
		return agentkit.LLMEvent{}, io.EOF
	}
	s.done = true
	msg := agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: s.text}},
	}
	return agentkit.LLMEvent{Type: agentkit.LLMEventMessage, Message: &msg}, io.EOF
}

func (s *stubOneShotStream) Close() error { return nil }

func TestRouterRoutesByModelID(t *testing.T) {
	t.Parallel()
	alpha := &catalogStub{name: "alpha", models: []capllm.ModelEntry{{ID: "gpt-4o"}}}
	beta := &catalogStub{name: "beta", models: []capllm.ModelEntry{{ID: "claude"}}}
	defaultP := &catalogStub{name: "default"}

	r, err := NewRouter(struct{}{}, RouterDeps{
		Protocols: []agentkit.LLMProvider{alpha, beta, defaultP},
		Default:   defaultP,
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := r.Stream(context.Background(), agentkit.LLMRequest{Model: "claude", Messages: []agentkit.ModelMessage{{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	ev, err := stream.Recv()
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if ev.Message == nil || ev.Message.Content[0].Text != "from:beta" {
		t.Fatalf("got %+v", ev.Message)
	}
	_ = stream.Close()
}

func TestRouterUsesDefaultWhenUnknown(t *testing.T) {
	t.Parallel()
	alpha := &catalogStub{name: "alpha", models: []capllm.ModelEntry{{ID: "gpt-4o"}}}
	defaultP := &catalogStub{name: "default"}

	r, err := NewRouter(struct{}{}, RouterDeps{
		Protocols: []agentkit.LLMProvider{alpha, defaultP},
		Default:   defaultP,
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := r.Stream(context.Background(), agentkit.LLMRequest{Model: "unknown-model"})
	if err != nil {
		t.Fatal(err)
	}
	ev, _ := stream.Recv()
	if ev.Message == nil || ev.Message.Content[0].Text != "from:default" {
		t.Fatalf("expected default provider, got %+v", ev.Message)
	}
	if defaultP.lastReq.Model != "unknown-model" {
		t.Fatalf("model rewritten: %q", defaultP.lastReq.Model)
	}
	_ = stream.Close()
}

func TestRouterDuplicateModelIDFails(t *testing.T) {
	t.Parallel()
	a := &catalogStub{name: "a", models: []capllm.ModelEntry{{ID: "same"}}}
	b := &catalogStub{name: "b", models: []capllm.ModelEntry{{ID: "same"}}}
	defaultP := &catalogStub{name: "default"}

	_, err := NewRouter(struct{}{}, RouterDeps{
		Protocols: []agentkit.LLMProvider{a, b, defaultP},
		Default:   defaultP,
	})
	if err == nil {
		t.Fatal("expected duplicate id error")
	}
}

func TestOpenAICatalogModalitiesForModel(t *testing.T) {
	t.Parallel()
	p, err := NewOpenAIChat(OpenAIConfig{
		APIKey: "k",
		Models: []ModelCatalogEntry{
			{ID: "vision", Modalities: []string{"text", "image"}},
			{ID: "text-only", Modalities: []string{"text"}},
		},
	}, OpenAIDeps{})
	if err != nil {
		t.Fatal(err)
	}
	aware, ok := p.(agentkit.ModelModalityAwareLLM)
	if !ok {
		t.Fatal("expected ModelModalityAwareLLM")
	}
	if !agentkit.SupportsModality(aware.ModalitiesForModel("vision"), agentkit.ModalityImage) {
		t.Fatal("vision model expected image")
	}
	if agentkit.SupportsModality(aware.ModalitiesForModel("text-only"), agentkit.ModalityImage) {
		t.Fatal("text-only expected no image")
	}
}
