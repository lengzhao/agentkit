package prompt

import (
	"context"
	"errors"
	"testing"

	"github.com/lengzhao/agentkit"
)

type stubSectionProvider struct {
	sections []agentkit.Section
}

func (s stubSectionProvider) Sections() []agentkit.Section { return s.sections }

func TestNewAssemblerSkipsNilProviders(t *testing.T) {
	a, err := NewAssembler(AssemblerConfig{}, AssemblerDeps{
		Sections: []agentkit.SectionProvider{nil, stubSectionProvider{
			sections: []agentkit.Section{{Name: "a", Build: func(context.Context, agentkit.PromptRequest) (agentkit.PromptSection, error) {
				return agentkit.PromptSection{Content: "hello"}, nil
			}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := a.Assemble(context.Background(), agentkit.PromptRequest{
		Messages: []agentkit.ModelMessage{{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Fatalf("messages: %+v", msgs)
	}
	if msgs[0].Content[0].Text != "hello" {
		t.Fatalf("system: %q", msgs[0].Content[0].Text)
	}
}

func TestAssemblerSkipsEmptyAndNilBuild(t *testing.T) {
	a := &Assembler{sections: []agentkit.Section{
		{Name: "skip-nil"},
		{Name: "skip-blank", Build: func(context.Context, agentkit.PromptRequest) (agentkit.PromptSection, error) {
			return agentkit.PromptSection{Content: "   "}, nil
		}},
		{Name: "keep", Build: func(context.Context, agentkit.PromptRequest) (agentkit.PromptSection, error) {
			return agentkit.PromptSection{Content: "A"}, nil
		}},
		{Name: "keep2", Build: func(context.Context, agentkit.PromptRequest) (agentkit.PromptSection, error) {
			return agentkit.PromptSection{Content: "B"}, nil
		}},
	}}
	msgs, err := a.Assemble(context.Background(), agentkit.PromptRequest{
		Messages: []agentkit.ModelMessage{{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "x"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected system + user, got %d", len(msgs))
	}
	want := "A\n\nB"
	if msgs[0].Content[0].Text != want {
		t.Fatalf("system = %q, want %q", msgs[0].Content[0].Text, want)
	}
}

func TestAssemblerNoSystemWhenAllEmpty(t *testing.T) {
	a := &Assembler{sections: []agentkit.Section{
		{Build: func(context.Context, agentkit.PromptRequest) (agentkit.PromptSection, error) {
			return agentkit.PromptSection{}, nil
		}},
	}}
	msgs, err := a.Assemble(context.Background(), agentkit.PromptRequest{
		Messages: []agentkit.ModelMessage{{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "only"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Role != "user" {
		t.Fatalf("got %+v", msgs)
	}
}

func TestAssemblerBuildError(t *testing.T) {
	a := &Assembler{sections: []agentkit.Section{
		{Build: func(context.Context, agentkit.PromptRequest) (agentkit.PromptSection, error) {
			return agentkit.PromptSection{}, errors.New("build failed")
		}},
	}}
	_, err := a.Assemble(context.Background(), agentkit.PromptRequest{})
	if err == nil {
		t.Fatal("expected build error")
	}
}
