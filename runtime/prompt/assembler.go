package prompt

import (
	"context"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
)

type AssemblerConfig struct{}

type AssemblerDeps struct {
	Sections []agentkit.SectionProvider `json:"sections,omitempty"`
}

type Assembler struct {
	sections []agentkit.Section
}

func NewAssembler(_ AssemblerConfig, deps AssemblerDeps) (agentkit.PromptAssembler, error) {
	var sections []agentkit.Section
	for _, provider := range deps.Sections {
		if provider == nil {
			continue
		}
		sections = append(sections, provider.Sections()...)
	}
	sort.Slice(sections, func(i, j int) bool {
		if sections[i].Order == sections[j].Order {
			return sections[i].Name < sections[j].Name
		}
		return sections[i].Order < sections[j].Order
	})
	return &Assembler{sections: sections}, nil
}

func (a *Assembler) Assemble(ctx context.Context, req agentkit.PromptRequest) (agentkit.Prompt, error) {
	var built []agentkit.PromptSection
	for _, section := range a.sections {
		if section.Build == nil {
			continue
		}
		ps, err := section.Build(ctx, req)
		if err != nil {
			return agentkit.Prompt{}, err
		}
		if strings.TrimSpace(ps.Content) == "" {
			continue
		}
		built = append(built, ps)
	}
	messages := append([]agentkit.ModelMessage(nil), req.Messages...)
	if len(built) > 0 {
		var b strings.Builder
		for _, section := range built {
			b.WriteString(section.Content)
			b.WriteString("\n\n")
		}
		messages = append([]agentkit.ModelMessage{{
			Role:    "system",
			Content: []agentkit.ContentPart{{Type: "text", Text: strings.TrimSpace(b.String())}},
		}}, messages...)
	}
	return agentkit.Prompt{Messages: messages, Sections: built}, nil
}
