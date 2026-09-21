package learning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/lengzhao/agentkit/cap/filesystem"
)

// SkillsPolicy is tenant-local skill workshop capture mode.
type SkillsPolicy struct {
	SkillsMode string `json:"skillsMode,omitempty"` // off | propose | auto
}

func (p SkillsPolicy) Normalized() SkillsPolicy {
	p.SkillsMode = strings.ToLower(strings.TrimSpace(p.SkillsMode))
	return p
}

// SkillsPolicyStore persists learning/policy.json (relative to memory root).
// Path is a filesystem.Service-relative path.
type SkillsPolicyStore struct {
	FS   filesystem.Service
	Path string
}

func (s *SkillsPolicyStore) Load(ctx context.Context) (SkillsPolicy, error) {
	if s.Path == "" {
		return SkillsPolicy{}, fmt.Errorf("policy path is required")
	}
	data, err := s.FS.Read(ctx, s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SkillsPolicy{}, nil
		}
		return SkillsPolicy{}, err
	}
	var p SkillsPolicy
	if err := json.Unmarshal(data, &p); err != nil {
		return SkillsPolicy{}, err
	}
	return p.Normalized(), nil
}

func (s *SkillsPolicyStore) Save(ctx context.Context, p SkillsPolicy) error {
	if s.Path == "" {
		return fmt.Errorf("policy path is required")
	}
	p = p.Normalized()
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return s.FS.Write(ctx, s.Path, raw)
}

// FormatSkillsPolicyStatus renders /learn policy output.
func FormatSkillsPolicyStatus(skillsMode string, fromFile bool) string {
	src := "config"
	if fromFile {
		src = "learning/policy.json"
	}
	return fmt.Sprintf("skills policy (source: %s):\n  workshop capture: %s\n\noff | propose (pending apply) | auto (apply when safe)",
		src, skillsMode)
}
