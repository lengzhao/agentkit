package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
)

const skillsCatalogHeader = "Available skills (use the skill tool to load one):\n"

// filteredTools narrows a tool runtime to a named subset. Wrapping rather than
// building a second runtime keeps the whole pipeline — policy, approval, hooks,
// per-tool timeouts, result truncation — intact for the child; the allowlist only
// decides which tools exist.
type filteredTools struct {
	inner      agentkit.ToolRuntime
	allow      map[string]bool
	skillAllow map[string]bool
}

// newFilteredTools resolves an allowlist against what the runtime actually
// exposes. Names that do not exist are dropped with a warning: the definition
// file is human-authored and a typo there should not silently become a child
// agent that appears to have a tool it never had.
//
// skillNames, when non-empty, restricts skill tool loads to that set (case-insensitive).
func newFilteredTools(ctx context.Context, inner agentkit.ToolRuntime, names []string, skillNames []string) (agentkit.ToolRuntime, error) {
	if len(names) == 0 && len(skillNames) == 0 {
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
	if len(names) > 0 && len(allow) == 0 {
		return nil, &emptyToolSetError{requested: names}
	}
	skillAllow := skillAllowSet(skillNames)
	if len(names) == 0 {
		return &filteredTools{inner: inner, skillAllow: skillAllow}, nil
	}
	return &filteredTools{inner: inner, allow: allow, skillAllow: skillAllow}, nil
}

func skillAllowSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	out := make(map[string]bool, len(names))
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			out[strings.ToLower(name)] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (f *filteredTools) Visible(ctx context.Context) ([]agentkit.ToolSpec, error) {
	specs, err := f.inner.Visible(ctx)
	if err != nil {
		return nil, err
	}
	if len(f.allow) == 0 {
		return specs, nil
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
	if len(f.allow) > 0 && !f.allow[call.Name] {
		return denyToolCall(call, "not in subagent tool allowlist"), nil
	}
	if call.Name == "skill" && len(f.skillAllow) > 0 {
		name, err := parseSkillToolName(call.Input)
		if err != nil || !f.skillAllow[strings.ToLower(name)] {
			return denyToolCall(call, "skill not available to this subagent"), nil
		}
	}
	return f.inner.Execute(ctx, call)
}

func denyToolCall(call agentkit.ToolCall, reason string) agentkit.ToolResult {
	return agentkit.ToolResult{
		ID:      call.ID,
		Name:    call.Name,
		Content: "tool not available to this subagent",
		Audit:   map[string]string{"decision": "deny", "reason": reason},
	}
}

func parseSkillToolName(input json.RawMessage) (string, error) {
	var payload struct {
		Name string `json:"name"`
	}
	if len(input) == 0 {
		return "", fmt.Errorf("empty skill input")
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.Name), nil
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
	inner   agentkit.PromptAssembler
	body    string
	skills  []string
}

func (d *definitionPrompt) Assemble(ctx context.Context, req agentkit.PromptRequest) ([]agentkit.ModelMessage, error) {
	messages, err := d.inner.Assemble(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(d.skills) > 0 {
		messages = filterSkillsInMessages(messages, d.skills)
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

// filterSkillsInMessages narrows the injected skills catalog to the allowlist.
func filterSkillsInMessages(messages []agentkit.ModelMessage, allowed []string) []agentkit.ModelMessage {
	out := messages
	for i, msg := range messages {
		if msg.Role != "system" {
			continue
		}
		for j, part := range msg.Content {
			if part.Type != "text" || !strings.Contains(part.Text, skillsCatalogHeader) {
				continue
			}
			filtered, unknown := filterSkillsCatalog(part.Text, allowed)
			if len(unknown) > 0 {
				slog.Warn("subagent skill allowlist has unknown names",
					"unknown", strings.Join(unknown, ","))
			}
			out[i].Content[j].Text = filtered
		}
	}
	return out
}

func filterSkillsCatalog(text string, allowNames []string) (string, []string) {
	allow := skillAllowSet(allowNames)
	if allow == nil {
		return text, nil
	}
	start := strings.Index(text, skillsCatalogHeader)
	if start < 0 {
		return text, allowNames
	}
	before := text[:start]
	afterHeader := text[start+len(skillsCatalogHeader):]
	lines := strings.Split(afterHeader, "\n")
	var skillLines []string
	var restStart int
	for restStart < len(lines) && lines[restStart] != "" && strings.HasPrefix(lines[restStart], "- ") {
		skillLines = append(skillLines, lines[restStart])
		restStart++
	}
	var kept []string
	matched := make(map[string]bool)
	for _, line := range skillLines {
		name := skillLineName(line)
		if allow[strings.ToLower(name)] {
			kept = append(kept, line)
			matched[strings.ToLower(name)] = true
		}
	}
	var unknown []string
	for name := range allow {
		if !matched[name] {
			unknown = append(unknown, name)
		}
	}
	var b strings.Builder
	b.WriteString(before)
	if len(kept) > 0 {
		b.WriteString(skillsCatalogHeader)
		b.WriteString(strings.Join(kept, "\n"))
		b.WriteByte('\n')
	}
	if restStart < len(lines) {
		b.WriteString(strings.Join(lines[restStart:], "\n"))
	}
	return b.String(), unknown
}

func skillLineName(line string) string {
	line = strings.TrimPrefix(line, "- ")
	if i := strings.Index(line, ": "); i >= 0 {
		return line[:i]
	}
	return line
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
