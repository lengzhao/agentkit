package learning

import (
	"context"
	"fmt"
	"strings"

	rtlearning "github.com/lengzhao/agentkit/runtime/learning"
)

func (s *Service) skillsPolicyStore(ctx context.Context) (*rtlearning.SkillsPolicyStore, error) {
	path, err := s.memory.ResolveRel(ctx, "learning", "policy.json")
	if err != nil {
		return nil, err
	}
	return &rtlearning.SkillsPolicyStore{Path: path}, nil
}

func (s *Service) effectiveSkillsMode(ctx context.Context) (string, bool) {
	store, err := s.skillsPolicyStore(ctx)
	if err != nil {
		return s.workshopCfg().Mode, false
	}
	p, err := store.Load()
	if err != nil {
		return s.workshopCfg().Mode, false
	}
	if p.SkillsMode != "" {
		return p.SkillsMode, true
	}
	return s.workshopCfg().Mode, false
}

func (s *Service) skillsWorkshopEnabled(ctx context.Context) bool {
	mode, _ := s.effectiveSkillsMode(ctx)
	return mode != "off"
}

func (s *Service) skillsAutoApply(ctx context.Context, source string) bool {
	mode, _ := s.effectiveSkillsMode(ctx)
	if mode != "auto" {
		return false
	}
	if source == "background-review" && s.memory.BackgroundReviewRequiresStaging(ctx) {
		return false
	}
	return true
}

func (s *Service) handlePolicy(ctx context.Context, args []string) (string, error) {
	if len(args) == 0 {
		skill, skillFile := s.effectiveSkillsMode(ctx)
		return rtlearning.FormatSkillsPolicyStatus(skill, skillFile), nil
	}
	switch strings.ToLower(args[0]) {
	case "show", "status":
		return s.handlePolicy(ctx, nil)
	case "reset":
		store, err := s.skillsPolicyStore(ctx)
		if err != nil {
			return "", err
		}
		if err := store.Save(rtlearning.SkillsPolicy{}); err != nil {
			return "", err
		}
		return "skills policy reset; using config defaults again", nil
	case "memory":
		return "", fmt.Errorf("memory write policy moved to /memory policy (approve|auto)")
	case "skills", "skill":
		return s.setSkillsPolicy(ctx, args[1:])
	default:
		return "", fmt.Errorf("usage: /learn policy [show]|skills off|propose|auto|reset")
	}
}

func (s *Service) setSkillsPolicy(ctx context.Context, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: /learn policy skills off|propose|auto")
	}
	mode := normalizeSkillsMode(args[0])
	if mode == "" {
		return "", fmt.Errorf("skills policy must be off, propose, or auto")
	}
	store, err := s.skillsPolicyStore(ctx)
	if err != nil {
		return "", err
	}
	if err := store.Save(rtlearning.SkillsPolicy{SkillsMode: mode}); err != nil {
		return "", err
	}
	switch mode {
	case "off":
		return "skill workshop capture disabled (explicit /learn skill still works when workshop enabled in config)", nil
	case "propose":
		return "skills stay in workshop until /learn workshop apply", nil
	case "auto":
		return "scanner-passing skill proposals will auto-apply to skills/", nil
	default:
		return "", nil
	}
}

func normalizeSkillsMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "off", "disable", "disabled", "关", "关闭":
		return "off"
	case "propose", "proposal", "pending", "approve", "授权":
		return "propose"
	case "auto", "automatic", "自动":
		return "auto"
	default:
		return ""
	}
}
