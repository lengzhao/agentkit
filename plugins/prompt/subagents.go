package prompt

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/subagent"
	rtllm "github.com/lengzhao/agentkit/runtime/llm"
)

type SubagentsSectionConfig struct{}

type SubagentsSectionDeps struct {
	Subagent subagent.Spawner       `json:"subagent"`
	LLM      agentkit.LLMProvider   `json:"llm,omitempty"`
}

type subagentsSectionProvider struct {
	spawner subagent.Spawner
	llm     agentkit.LLMProvider
}

// NewSubagentsSection registers prompt/section/subagents: Inject the catalog of delegatable subagents so the model knows who it can hand work to.
//
// Best practices:
//   - Mount this wherever tool/subagent is mounted: the delegate tool's description is static, so without this section the model has no list of valid agent names.
//   - deps.llm is optional; when the main model is text-only, this section adds delegation hints for image attachments.
func NewSubagentsSection(_ SubagentsSectionConfig, deps SubagentsSectionDeps) (agentkit.SectionProvider, error) {
	if deps.Subagent == nil {
		return nil, fmt.Errorf("prompt/section/subagents requires subagent dependency")
	}
	return &subagentsSectionProvider{spawner: deps.Subagent, llm: deps.LLM}, nil
}

func (p *subagentsSectionProvider) Sections() []agentkit.Section {
	return []agentkit.Section{{
		Name:  "subagents",
		Build: p.build,
	}}
}

func (p *subagentsSectionProvider) build(ctx context.Context, _ agentkit.PromptRequest) (agentkit.PromptSection, error) {
	list, err := p.spawner.Definitions(ctx)
	if err != nil {
		return agentkit.PromptSection{}, err
	}
	if len(list) == 0 {
		return agentkit.PromptSection{}, nil
	}
	parentMods := rtllm.ProviderModalities(p.llm)
	var b strings.Builder
	b.WriteString("Available subagents (use the delegate tool to run one):\n")
	for _, item := range list {
		b.WriteString("- ")
		b.WriteString(item.Name)
		if item.Description != "" {
			b.WriteString(": ")
			b.WriteString(item.Description)
		}
		if len(item.Modalities) > 0 {
			b.WriteString(" [modalities: ")
			b.WriteString(strings.Join(item.Modalities, ", "))
			b.WriteString("]")
		}
		if item.Async {
			b.WriteString(" (default: async)")
		}
		if item.Backend == subagent.BackendLoop {
			b.WriteString(" [loop]")
		}
		b.WriteString("\n")
	}
	if !agentkit.SupportsModality(parentMods, agentkit.ModalityImage) && subagentSupportsModality(list, agentkit.ModalityImage) {
		b.WriteString("\nYour model accepts text only. When the user attaches images (paths under work/upload/ or [attachment: …] in the message), delegate to a subagent whose modalities include image. Put the workspace path and what you need extracted into task.\n")
	}
	return agentkit.PromptSection{
		Name:    "subagents",
		Content: strings.TrimSpace(b.String()),
	}, nil
}

func subagentSupportsModality(list []subagent.Definition, mod string) bool {
	for _, item := range list {
		if len(item.Modalities) == 0 {
			continue
		}
		if agentkit.SupportsModality(item.Modalities, mod) {
			return true
		}
	}
	return false
}
