package subagent

import (
	"context"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
)

// filteredTools narrows a tool runtime to a named subset. Wrapping rather than
// building a second runtime keeps the whole pipeline — policy, approval, hooks,
// per-tool timeouts, result truncation — intact for the child; the allowlist only
// decides which tools exist.
type filteredTools struct {
	inner agentkit.ToolRuntime
	allow map[string]bool
}

// newFilteredTools resolves an allowlist against what the runtime actually
// exposes. Names that do not exist are dropped with a warning: the definition
// file is human-authored and a typo there should not silently become a child
// agent that appears to have a tool it never had.
func newFilteredTools(ctx context.Context, inner agentkit.ToolRuntime, names []string) (agentkit.ToolRuntime, error) {
	if len(names) == 0 {
		return inner, nil
	}
	specs, err := inner.Visible(ctx)
	if err != nil {
		return nil, err
	}
	available := make(map[string]bool, len(specs))
	for _, spec := range specs {
		available[spec.Name] = true
	}
	allow := make(map[string]bool, len(names))
	var unknown []string
	for _, name := range names {
		if available[name] {
			allow[name] = true
			continue
		}
		unknown = append(unknown, name)
	}
	if len(unknown) > 0 {
		slog.Warn("subagent tool allowlist has unknown names",
			"unknown", strings.Join(unknown, ","), "available", len(available))
	}
	if len(allow) == 0 {
		return nil, &emptyToolSetError{requested: names}
	}
	return &filteredTools{inner: inner, allow: allow}, nil
}

func (f *filteredTools) Visible(ctx context.Context) ([]agentkit.ToolSpec, error) {
	specs, err := f.inner.Visible(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]agentkit.ToolSpec, 0, len(f.allow))
	for _, spec := range specs {
		if f.allow[spec.Name] {
			out = append(out, spec)
		}
	}
	return out, nil
}

func (f *filteredTools) Execute(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
	if !f.allow[call.Name] {
		// A model-readable denial, matching how the tool runtime answers an
		// unknown tool name. Returning an error instead would abort the turn.
		return agentkit.ToolResult{
			ID:      call.ID,
			Name:    call.Name,
			Content: "tool not available to this subagent",
			Audit:   map[string]string{"decision": "deny", "reason": "not in subagent tool allowlist"},
		}, nil
	}
	return f.inner.Execute(ctx, call)
}

type emptyToolSetError struct {
	requested []string
}

func (e *emptyToolSetError) Error() string {
	return "none of the requested tools exist: " + strings.Join(e.requested, ", ")
}

// definitionPrompt layers a child agent's persona on top of the shared prompt
// sections, so AGENTS.md, time and the skill catalog still reach the child.
type definitionPrompt struct {
	inner agentkit.PromptAssembler
	body  string
}

func (d *definitionPrompt) Assemble(ctx context.Context, req agentkit.PromptRequest) ([]agentkit.ModelMessage, error) {
	messages, err := d.inner.Assemble(ctx, req)
	if err != nil {
		return nil, err
	}
	// The assembler already merged its sections into one leading system message;
	// extend that one rather than emitting a second system message, which some
	// providers reject.
	for i, msg := range messages {
		if msg.Role != "system" {
			continue
		}
		messages[i] = appendSystemText(msg, d.body)
		return messages, nil
	}
	return append([]agentkit.ModelMessage{{
		Role:    "system",
		Content: []agentkit.ContentPart{{Type: "text", Text: d.body}},
	}}, messages...), nil
}

// appendSystemText adds one more text part. Providers concatenate a message's
// text parts with no separator (runtime/llm/openai_convert.go:262), so the blank
// line has to be part of the payload.
func appendSystemText(msg agentkit.ModelMessage, text string) agentkit.ModelMessage {
	out := msg
	out.Content = append(append([]agentkit.ContentPart(nil), msg.Content...),
		agentkit.ContentPart{Type: "text", Text: "\n\n" + text})
	return out
}
