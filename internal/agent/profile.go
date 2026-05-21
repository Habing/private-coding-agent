package agent

// Profile bundles the static configuration of an Agent persona: system prompt,
// allowed tools, default step ceiling, and a list of Skill ids to inject when
// neither RunInput.SkillIDs nor Session.SkillIDs are set.
type Profile struct {
	Name          string
	SystemPrompt  string
	ToolAllowlist []string // tool names; empty list means "allow none"
	MaxSteps      int      // upper bound on ReAct iterations
	SkillIDs      []string // default Skill ids; overridden by Session/Run scope
}

// DefaultCodingProfile returns the P0 "coding" profile: lets the agent reach
// for every internal tool and capped at 16 iterations. memory.* tools are
// included so agents can save/recall lessons across runs; the platform Skill
// gives them the SOP for when to do so.
func DefaultCodingProfile() Profile {
	return Profile{
		Name: "coding",
		SystemPrompt: "You are a coding agent operating inside a private development platform. " +
			"You have access to a sandbox via tools. Use the provided tools to inspect files, " +
			"run shell commands, search code, and call LLMs as needed. Prefer concrete actions over speculation. " +
			"Respond with a final answer once the task is complete.",
		ToolAllowlist: []string{
			"fs.read", "fs.write", "fs.list", "fs.glob",
			"grep", "shell.exec",
			"llm.chat", "llm.embed",
			"memory.save", "memory.search", "memory.list", "memory.delete",
		},
		MaxSteps: 16,
		SkillIDs: []string{"platform-coding-standards"},
	}
}
