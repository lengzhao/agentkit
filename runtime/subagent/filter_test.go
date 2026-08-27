package subagent

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
)

// fakeTools records what reached the wrapped runtime, so a denial can be told
// apart from an execution that returned a refusal.
type fakeTools struct {
	names    []string
	executed []string
}

func (f *fakeTools) Visible(context.Context) ([]agentkit.ToolSpec, error) {
	specs := make([]agentkit.ToolSpec, 0, len(f.names))
	for _, name := range f.names {
		specs = append(specs, agentkit.ToolSpec{Name: name, Description: name + " tool"})
	}
	return specs, nil
}

func (f *fakeTools) Execute(_ context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
	f.executed = append(f.executed, call.Name)
	return agentkit.ToolResult{
		ID:      call.ID,
		Name:    call.Name,
		Content: "ran " + call.Name,
	}, nil
}

func TestFilteredToolsNarrowsVisibleSet(t *testing.T) {
	t.Parallel()

	inner := &fakeTools{names: []string{"read", "write", "grep"}}
	tools, err := newFilteredTools(context.Background(), inner, []string{"read", "grep", "nonexistent"})
	if err != nil {
		t.Fatalf("newFilteredTools: %v", err)
	}
	specs, err := tools.Visible(context.Background())
	if err != nil {
		t.Fatalf("visible: %v", err)
	}
	var got []string
	for _, spec := range specs {
		got = append(got, spec.Name)
	}
	if len(got) != 2 || got[0] != "read" || got[1] != "grep" {
		t.Fatalf("visible = %v, want [read grep]", got)
	}
}

func TestFilteredToolsDeniesOutsideAllowlist(t *testing.T) {
	t.Parallel()

	inner := &fakeTools{names: []string{"read", "write"}}
	tools, err := newFilteredTools(context.Background(), inner, []string{"read"})
	if err != nil {
		t.Fatalf("newFilteredTools: %v", err)
	}

	res, err := tools.Execute(context.Background(), agentkit.ToolCall{ID: "c1", Name: "write"})
	if err != nil {
		t.Fatalf("a denial must be a result, not an error: %v", err)
	}
	if res.Audit["decision"] != "deny" {
		t.Errorf("audit = %#v, want decision=deny", res.Audit)
	}
	if len(inner.executed) != 0 {
		t.Errorf("inner runtime saw %v, want nothing", inner.executed)
	}

	if _, err := tools.Execute(context.Background(), agentkit.ToolCall{ID: "c2", Name: "read"}); err != nil {
		t.Fatalf("execute read: %v", err)
	}
	if len(inner.executed) != 1 || inner.executed[0] != "read" {
		t.Errorf("inner runtime saw %v, want [read]", inner.executed)
	}
}

func TestNewFilteredToolsEmptyAllowlistKeepsInner(t *testing.T) {
	t.Parallel()

	inner := &fakeTools{names: []string{"read"}}
	tools, err := newFilteredTools(context.Background(), inner, nil)
	if err != nil {
		t.Fatalf("newFilteredTools: %v", err)
	}
	if tools != agentkit.ToolRuntime(inner) {
		t.Fatal("an empty allowlist should hand back the runtime unchanged")
	}
}

func TestNewFilteredToolsAllNamesUnknown(t *testing.T) {
	t.Parallel()

	inner := &fakeTools{names: []string{"read"}}
	_, err := newFilteredTools(context.Background(), inner, []string{"typo", "alsoTypo"})
	if err == nil {
		t.Fatal("expected an error rather than a child agent with no tools")
	}
	if !strings.Contains(err.Error(), "typo") {
		t.Errorf("error = %q, want the requested names", err)
	}
}

// stubAssembler stands in for the shared prompt pipeline.
type stubAssembler struct {
	messages []agentkit.ModelMessage
}

func (s *stubAssembler) Assemble(_ context.Context, req agentkit.PromptRequest) ([]agentkit.ModelMessage, error) {
	return append(append([]agentkit.ModelMessage(nil), s.messages...), req.Messages...), nil
}

func TestDefinitionPromptExtendsExistingSystemMessage(t *testing.T) {
	t.Parallel()

	inner := &stubAssembler{
		messages: []agentkit.ModelMessage{{
			Role:    "system",
			Content: []agentkit.ContentPart{{Type: "text", Text: "shared rules"}},
		}},
	}
	p := &definitionPrompt{inner: inner, body: "you are the reviewer"}
	messages, err := p.Assemble(context.Background(), agentkit.PromptRequest{
		Messages: []agentkit.ModelMessage{{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}

	var systems int
	for _, msg := range messages {
		if msg.Role == "system" {
			systems++
		}
	}
	if systems != 1 {
		t.Fatalf("system messages = %d, want 1", systems)
	}
	text := systemText(messages)
	if !strings.Contains(text, "shared rules") || !strings.Contains(text, "you are the reviewer") {
		t.Fatalf("system text = %q, want both the shared sections and the persona", text)
	}
	// Providers join text parts with no separator, so the blank line must be in
	// the payload or the persona would be glued onto the previous word.
	if !strings.Contains(text, "shared rules\n\nyou are the reviewer") {
		t.Errorf("system text = %q, want a blank line between the two", text)
	}
}

func TestDefinitionPromptAddsSystemMessageWhenAbsent(t *testing.T) {
	t.Parallel()

	p := &definitionPrompt{inner: &stubAssembler{}, body: "you are the reviewer"}
	messages, err := p.Assemble(context.Background(), agentkit.PromptRequest{
		Messages: []agentkit.ModelMessage{{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %#v, want system then user", messages)
	}
	if messages[0].Role != "system" {
		t.Fatalf("first message role = %q, want system", messages[0].Role)
	}
	if got := systemText(messages); got != "you are the reviewer" {
		t.Errorf("system text = %q", got)
	}
}

func systemText(messages []agentkit.ModelMessage) string {
	var b strings.Builder
	for _, msg := range messages {
		if msg.Role != "system" {
			continue
		}
		for _, part := range msg.Content {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}
