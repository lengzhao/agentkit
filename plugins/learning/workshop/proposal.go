package workshop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	StatusPending  = "pending"
	StatusApplied  = "applied"
	StatusRejected = "rejected"

	KindCreate = "create"
	KindUpdate = "update"
)

// Meta describes one workshop proposal.
type Meta struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	SkillName    string    `json:"skillName"`
	Status       string    `json:"status"`
	Source       string    `json:"source"`
	SessionID    string    `json:"sessionId,omitempty"`
	Focus        string    `json:"focus,omitempty"`
	Autonomous   bool      `json:"autonomous,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	AppliedAt    time.Time `json:"appliedAt,omitempty"`
	RejectedAt   time.Time `json:"rejectedAt,omitempty"`
	ScanCritical []string  `json:"scanCritical,omitempty"`
}

// Proposal is meta + markdown body.
type Proposal struct {
	Meta Meta
	Body string
	Dir  string
}

// Store manages proposals under work/skills/.workshop/.
type Store struct {
	Root string
}

func (s *Store) List() ([]Proposal, error) {
	if s.Root == "" {
		return nil, fmt.Errorf("workshop root is required")
	}
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Proposal, 0)
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		p, err := s.Load(ent.Name())
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *Store) Load(id string) (Proposal, error) {
	dir := filepath.Join(s.Root, id)
	metaPath := filepath.Join(dir, "meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return Proposal{}, err
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return Proposal{}, err
	}
	body, err := os.ReadFile(filepath.Join(dir, "PROPOSAL.md"))
	if err != nil {
		return Proposal{}, err
	}
	return Proposal{Meta: meta, Body: string(body), Dir: dir}, nil
}

func (s *Store) PendingCount() (int, error) {
	all, err := s.List()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range all {
		if p.Meta.Status == StatusPending {
			n++
		}
	}
	return n, nil
}

// Create stores a new pending proposal.
func (s *Store) Create(skillName, body, source, sessionID, focus string, autonomous bool) (Proposal, error) {
	skillName = strings.TrimSpace(strings.ToLower(skillName))
	body = strings.TrimSpace(body)
	scan := Scan(skillName, body)
	if !scan.OK {
		return Proposal{}, fmt.Errorf("proposal failed scan: %s", strings.Join(scan.Critical, "; "))
	}
	id := uuid.NewString()
	meta := Meta{
		ID:         id,
		Kind:       KindCreate,
		SkillName:  skillName,
		Status:     StatusPending,
		Source:     source,
		SessionID:  sessionID,
		Focus:      focus,
		Autonomous: autonomous,
		CreatedAt:  time.Now().UTC(),
	}
	dir := filepath.Join(s.Root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Proposal{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), mustJSON(meta), 0o644); err != nil {
		return Proposal{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "PROPOSAL.md"), []byte(body), 0o644); err != nil {
		return Proposal{}, err
	}
	return Proposal{Meta: meta, Body: body, Dir: dir}, nil
}

func (s *Store) saveMeta(dir string, meta Meta) error {
	return os.WriteFile(filepath.Join(dir, "meta.json"), mustJSON(meta), 0o644)
}

func mustJSON(v any) []byte {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return data
}

// Apply writes SKILL.md to targetDir and marks proposal applied.
func (p *Proposal) Apply(targetDir string) error {
	if p.Meta.Status != StatusPending {
		return fmt.Errorf("proposal %s is %s", p.Meta.ID, p.Meta.Status)
	}
	scan := Scan(p.Meta.SkillName, p.Body)
	if !scan.OK {
		return fmt.Errorf("apply blocked: %s", strings.Join(scan.Critical, "; "))
	}
	skillDir := filepath.Join(targetDir, p.Meta.SkillName)
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if p.Meta.Kind == KindCreate {
		if _, err := os.Stat(skillDir); err == nil {
			return fmt.Errorf("skill %q already exists", p.Meta.SkillName)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(skillPath, []byte(p.Body), 0o644); err != nil {
		return err
	}
	p.Meta.Status = StatusApplied
	p.Meta.AppliedAt = time.Now().UTC()
	return os.WriteFile(filepath.Join(p.Dir, "meta.json"), mustJSON(p.Meta), 0o644)
}

// Reject marks a proposal rejected.
func (p *Proposal) Reject() error {
	if p.Meta.Status != StatusPending {
		return fmt.Errorf("proposal %s is %s", p.Meta.ID, p.Meta.Status)
	}
	p.Meta.Status = StatusRejected
	p.Meta.RejectedAt = time.Now().UTC()
	return os.WriteFile(filepath.Join(p.Dir, "meta.json"), mustJSON(p.Meta), 0o644)
}

// FormatList renders pending proposals for CLI.
func FormatList(proposals []Proposal) string {
	var b strings.Builder
	b.WriteString("skill workshop proposals:\n")
	n := 0
	for _, p := range proposals {
		if p.Meta.Status != StatusPending {
			continue
		}
		n++
		fmt.Fprintf(&b, "- %s skill=%s source=%s created=%s\n",
			p.Meta.ID, p.Meta.SkillName, p.Meta.Source, p.Meta.CreatedAt.Format(time.RFC3339))
	}
	if n == 0 {
		return "no skill workshop proposals"
	}
	return strings.TrimRight(b.String(), "\n")
}

// FormatProposal shows one proposal.
func FormatProposal(p Proposal) string {
	var b strings.Builder
	fmt.Fprintf(&b, "id: %s\n", p.Meta.ID)
	fmt.Fprintf(&b, "skill: %s\n", p.Meta.SkillName)
	fmt.Fprintf(&b, "status: %s\n", p.Meta.Status)
	fmt.Fprintf(&b, "source: %s\n", p.Meta.Source)
	if p.Meta.Focus != "" {
		fmt.Fprintf(&b, "focus: %s\n", p.Meta.Focus)
	}
	b.WriteString("\n")
	b.WriteString(p.Body)
	return b.String()
}

// DraftSkillBody builds a minimal SKILL.md from session notes.
func DraftSkillBody(name, description, procedure string) string {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	procedure = strings.TrimSpace(procedure)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", name)
	if description != "" {
		b.WriteString(description)
		b.WriteString("\n\n")
	}
	b.WriteString("## Procedure\n\n")
	b.WriteString(procedure)
	b.WriteByte('\n')
	return b.String()
}

// SuggestSkillName derives a skill name from focus or session text.
func SuggestSkillName(focus, fallback string) string {
	src := strings.TrimSpace(focus)
	if src == "" {
		src = strings.TrimSpace(fallback)
	}
	src = strings.ToLower(src)
	var b strings.Builder
	lastDash := false
	for _, r := range src {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) < 3 {
		out = "learned-workflow"
	}
	if len(out) > 48 {
		out = out[:48]
		out = strings.Trim(out, "-")
	}
	return out
}
