package prompt

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
)

// WorkspaceDirsConfig configures prompt/section/workspace-dirs.
type WorkspaceDirsConfig struct {
	// Name is section label, used for ordering and debugging.
	Name string `json:"name"`
	// Intro is optional lead-in text rendered above the directory groups,
	// e.g. the legend of the local:/global: configuration vocabulary.
	Intro string `json:"intro,omitempty"`
	// Entries are directory groups in display order.
	Entries []WorkspaceDirEntry `json:"entries"`
}

// WorkspaceDirEntry is one named group of directories that share a purpose.
type WorkspaceDirEntry struct {
	// Label says what the directories hold, e.g. "workflows".
	Label string `json:"label"`
	// Dirs are workspace directories in precedence order, usually carrying the
	// local:/global: configuration prefixes; the first dir is where new files
	// belong and shadows same-named files in later dirs.
	Dirs []string `json:"dirs"`
	// Note is optional guidance rendered after the group, e.g. precedence and
	// reload rules.
	Note string `json:"note,omitempty"`
}

// WorkspaceDirsDeps are the dependencies of prompt/section/workspace-dirs.
type WorkspaceDirsDeps struct {
	Workspace workspace.Service `json:"workspace"`
}

// SetDefaults implements pluginkit.Defaulter.
func (c *WorkspaceDirsConfig) SetDefaults() {
	if c.Name == "" {
		c.Name = "workspace-dirs"
	}
}

// Validate implements pluginkit.Validator.
func (c *WorkspaceDirsConfig) Validate() error {
	if len(c.Entries) == 0 {
		return fmt.Errorf("prompt/section/workspace-dirs requires at least one entry")
	}
	for i, entry := range c.Entries {
		if strings.TrimSpace(entry.Label) == "" {
			return fmt.Errorf("prompt/section/workspace-dirs entry %d: empty label", i)
		}
		if len(entry.Dirs) == 0 {
			return fmt.Errorf("prompt/section/workspace-dirs entry %d (%s): no dirs", i, entry.Label)
		}
	}
	return nil
}

type workspaceDirsProvider struct {
	name    string
	intro   string
	entries []WorkspaceDirEntry
	ws      workspace.Service
}

// NewWorkspaceDirs registers prompt/section/workspace-dirs: Inject the resolved absolute paths of scoped workspace directories (local:/global:) so the model knows where discovered assets live and where to create new ones.
//
// Best practices:
//   - Mirror the dirs of the discovery plugin the group belongs to (e.g. tool/workflow config.dirs), in the same precedence order; keep them in sync when either side changes.
//   - Put precedence/reload rules in the entry note; use intro to explain the local:/global: vocabulary.
//   - Resolution runs per turn with the request context, so per-tenant local roots (workspace/tenant) render correctly for each session.
func NewWorkspaceDirs(cfg WorkspaceDirsConfig, deps WorkspaceDirsDeps) (agentkit.SectionProvider, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("prompt/section/workspace-dirs requires workspace")
	}
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &workspaceDirsProvider{
		name:    cfg.Name,
		intro:   strings.TrimSpace(cfg.Intro),
		entries: cfg.Entries,
		ws:      deps.Workspace,
	}, nil
}

func (p *workspaceDirsProvider) Sections() []agentkit.Section {
	return []agentkit.Section{{
		Name:  p.name,
		Build: p.build,
	}}
}

func (p *workspaceDirsProvider) build(ctx context.Context, _ agentkit.PromptRequest) (agentkit.PromptSection, error) {
	var groups []string
	for _, entry := range p.entries {
		var lines []string
		for _, dir := range entry.Dirs {
			abs, err := p.ws.Resolve(ctx, dir)
			if err != nil {
				// A bad dir must not fail the whole prompt; discovery simply
				// misses the same dir for the same reason.
				slog.Warn("prompt/section/workspace-dirs skipped a dir", "dir", dir, "err", err)
				continue
			}
			line := "  - " + abs
			if scope, _, ok := workspace.ParseScoped(dir); ok {
				line += " (" + scope + ")"
			}
			lines = append(lines, line)
		}
		if len(lines) == 0 {
			continue
		}
		var g strings.Builder
		g.WriteString("- ")
		g.WriteString(entry.Label)
		g.WriteString(":\n")
		g.WriteString(strings.Join(lines, "\n"))
		if note := strings.TrimSpace(entry.Note); note != "" {
			g.WriteString("\n  ")
			g.WriteString(note)
		}
		groups = append(groups, g.String())
	}
	var b strings.Builder
	if p.intro != "" && len(groups) > 0 {
		b.WriteString(p.intro)
		b.WriteString("\n\n")
	}
	b.WriteString(strings.Join(groups, "\n"))
	return agentkit.PromptSection{
		Name:    p.name,
		Content: strings.TrimSpace(b.String()),
	}, nil
}
