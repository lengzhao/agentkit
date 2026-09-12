package learning

import (
	"context"
	"fmt"
	"strings"

	rtlearning "github.com/lengzhao/agentkit/runtime/learning"
)

func policyRelPath(root string) string {
	return joinUnderMemoryRoot(root, "memory", "learning", "policy.json")
}

func (s *Service) policyStore(ctx context.Context) (*rtlearning.PolicyStore, error) {
	path, err := s.workspace.Resolve(ctx, policyRelPath(s.memoryRoot))
	if err != nil {
		return nil, err
	}
	return &rtlearning.PolicyStore{Path: path}, nil
}

func (s *Service) tenantPolicy(ctx context.Context) (rtlearning.Policy, bool, error) {
	store, err := s.policyStore(ctx)
	if err != nil {
		return rtlearning.Policy{}, false, err
	}
	p, err := store.Load()
	if err != nil {
		return rtlearning.Policy{}, false, err
	}
	fromFile := p.MemoryWrite != "" || p.SkillsMode != ""
	return p, fromFile, nil
}

func (s *Service) effectiveMemoryWrite(ctx context.Context) (string, bool) {
	p, fromFile, err := s.tenantPolicy(ctx)
	if err != nil {
		return defaultMemoryWriteMode(s.review), false
	}
	if p.MemoryWrite != "" {
		return p.MemoryWrite, fromFile
	}
	return defaultMemoryWriteMode(s.review), fromFile
}

func defaultMemoryWriteMode(review ReviewServiceConfig) string {
	if review.WriteApproval != nil && *review.WriteApproval {
		return "approve"
	}
	return "auto"
}

func (s *Service) memoryWriteRequiresApproval(ctx context.Context, source string) bool {
	if source != "background-review" {
		return false
	}
	mode, _ := s.effectiveMemoryWrite(ctx)
	return mode == "approve"
}

func (s *Service) effectiveSkillsMode(ctx context.Context) (string, bool) {
	p, fromFile, err := s.tenantPolicy(ctx)
	if err != nil {
		return s.workshopCfg().Mode, false
	}
	if p.SkillsMode != "" {
		return p.SkillsMode, fromFile
	}
	return s.workshopCfg().Mode, fromFile
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
	if source == "background-review" && s.memoryWriteRequiresApproval(ctx, source) {
		return false
	}
	return true
}

func (s *Service) handlePolicy(ctx context.Context, args []string) (string, error) {
	if len(args) == 0 {
		mem, memFile := s.effectiveMemoryWrite(ctx)
		skill, skillFile := s.effectiveSkillsMode(ctx)
		return rtlearning.FormatPolicyStatus(mem, skill, memFile || skillFile), nil
	}
	switch strings.ToLower(args[0]) {
	case "show", "status":
		return s.handlePolicy(ctx, nil)
	case "reset":
		store, err := s.policyStore(ctx)
		if err != nil {
			return "", err
		}
		if err := store.Clear(); err != nil {
			return "", err
		}
		return "policy reset; using config.base defaults again", nil
	case "memory":
		return s.setMemoryPolicy(ctx, args[1:])
	case "skills", "skill":
		return s.setSkillsPolicy(ctx, args[1:])
	default:
		return "", fmt.Errorf("usage: /learn policy [show]|memory approve|auto|skills off|propose|auto|reset")
	}
}

func (s *Service) setMemoryPolicy(ctx context.Context, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: /learn policy memory approve|auto")
	}
	mode := normalizeMemoryWriteMode(args[0])
	if mode == "" {
		return "", fmt.Errorf("memory policy must be approve or auto")
	}
	store, err := s.policyStore(ctx)
	if err != nil {
		return "", err
	}
	p, _, err := s.tenantPolicy(ctx)
	if err != nil {
		return "", err
	}
	p.MemoryWrite = mode
	if err := store.Save(p); err != nil {
		return "", err
	}
	if mode == "approve" {
		return "background-review memory will be staged until /learn approve", nil
	}
	return "background-review memory will write memory.md directly", nil
}

func (s *Service) setSkillsPolicy(ctx context.Context, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: /learn policy skills off|propose|auto")
	}
	mode := normalizeSkillsMode(args[0])
	if mode == "" {
		return "", fmt.Errorf("skills policy must be off, propose, or auto")
	}
	store, err := s.policyStore(ctx)
	if err != nil {
		return "", err
	}
	p, _, err := s.tenantPolicy(ctx)
	if err != nil {
		return "", err
	}
	p.SkillsMode = mode
	if err := store.Save(p); err != nil {
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

func normalizeMemoryWriteMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "approve", "approval", "auth", "authorized", "需要授权", "授权":
		return "approve"
	case "auto", "automatic", "自动":
		return "auto"
	default:
		return ""
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
