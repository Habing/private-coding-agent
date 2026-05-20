package agent

// Profile bundles the static configuration of an Agent persona: system prompt,
// allowed tools, and a default step ceiling. Profiles are registered with the
// Engine via NewEngine and selected per-call by RunInput.ProfileName.
type Profile struct {
	Name          string
	SystemPrompt  string
	ToolAllowlist []string // tool names; empty list means "allow none"
	MaxSteps      int      // upper bound on ReAct iterations
}

// DefaultCodingProfile returns the P0 "coding" profile: lets the agent reach
// for every internal tool and capped at 16 iterations.
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
		},
		MaxSteps: 16,
	}
}
