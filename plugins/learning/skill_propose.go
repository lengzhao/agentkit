package learning

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit/plugins/learning/workshop"
)

// skillProposeParams is input for createSkillProposal.
type skillProposeParams struct {
	Name       string
	Body       string
	Source     string
	SessionID  string
	Focus      string
	Autonomous bool // workshop Create scanner (background review uses true)
}

func (s *Service) createSkillProposal(ctx context.Context, p skillProposeParams) (string, error) {
	wsStore, skillsDir, err := s.workshopStore(ctx)
	if err != nil {
		return "", err
	}
	pending, err := wsStore.PendingCount(ctx)
	if err != nil {
		return "", err
	}
	maxPending := s.workshopCfg().MaxPending
	if pending >= maxPending {
		return "", fmt.Errorf("workshop has %d pending proposals (max %d); apply or reject first",
			pending, maxPending)
	}
	name := strings.TrimSpace(p.Name)
	body := strings.TrimSpace(p.Body)
	if body == "" {
		return "", fmt.Errorf("skill body is empty")
	}
	if name == "" {
		name = workshop.SuggestSkillName(p.Focus, body)
	}
	proposal, err := wsStore.Create(ctx, name, body, p.Source, p.SessionID, p.Focus, p.Autonomous)
	if err != nil {
		return "", err
	}
	if s.skillsAutoApply(ctx, p.Source) {
		if err := proposal.Apply(ctx, skillsDir); err != nil {
			return fmt.Sprintf("proposal %s created (auto-apply failed: %v)", proposal.Meta.ID, err), nil
		}
		return fmt.Sprintf("skill %q applied from proposal %s", name, proposal.Meta.ID), nil
	}
	return fmt.Sprintf("skill proposal %s created for %q (pending apply)", proposal.Meta.ID, name), nil
}
