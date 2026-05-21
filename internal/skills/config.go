package skills

// Config controls the Skills subsystem at startup and Resolve time.
type Config struct {
	Enabled          bool     `mapstructure:"enabled"`
	Dirs             []string `mapstructure:"dirs"`
	DefaultSkillIDs  []string `mapstructure:"default_skill_ids"`
	MaxInjectedChars int      `mapstructure:"max_injected_chars"`
	MaxSkillsPerRun  int      `mapstructure:"max_skills_per_run"`
	HotReload        bool     `mapstructure:"hot_reload"`
}

// DefaultConfig returns the baseline used when the user omits the `skills:`
// section entirely. Enabled defaults to true so the bundled platform Skill
// is injected by default; Dirs is empty (caller wires `skills/`).
func DefaultConfig() Config {
	return Config{
		Enabled:          true,
		Dirs:             nil,
		DefaultSkillIDs:  nil,
		MaxInjectedChars: 24000,
		MaxSkillsPerRun:  5,
		HotReload:        false,
	}
}
