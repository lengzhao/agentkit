package workshop

// Config controls Skill Workshop behavior.
type Config struct {
	Mode       string `json:"mode"` // off, propose, auto
	MaxPending int    `json:"maxPending"`
	SkillsDir  string `json:"skillsDir"` // workspace-relative, default local:skills
}

func Defaults() Config {
	return Config{
		Mode:       "propose",
		MaxPending: 3,
		SkillsDir:  "local:skills",
	}
}

func (c Config) Normalized() Config {
	d := Defaults()
	if c.Mode == "" {
		c.Mode = d.Mode
	}
	if c.MaxPending <= 0 {
		c.MaxPending = d.MaxPending
	}
	if c.SkillsDir == "" {
		c.SkillsDir = d.SkillsDir
	}
	return c
}

func (c Config) AutoApply() bool {
	return c.Normalized().Mode == "auto"
}

func (c Config) Enabled() bool {
	return c.Normalized().Mode != "off"
}
