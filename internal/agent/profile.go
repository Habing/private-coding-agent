package agent

// Profile bundles the static configuration of an Agent persona: system prompt,
// allowed tools, default step ceiling, and a list of Skill ids to inject when
// neither RunInput.SkillIDs nor Session.SkillIDs are set.
type Profile struct {
	Name          string
	Description   string   // short, human-readable; surfaced via GET /agent/profiles
	SystemPrompt  string
	ToolAllowlist []string // tool names; empty list means "allow none"
	MaxSteps      int      // upper bound on ReAct iterations
	SkillIDs      []string // default Skill ids; overridden by Session/Run scope
}

// DefaultCodingProfile returns the P0 "coding" profile: lets the agent reach
// for every internal tool and capped at 16 iterations. memory.* tools are
// included so agents can save/recall lessons across runs; the platform Skill
// gives them the SOP for when to do so. As of Slice 18 it is the only profile
// permitted to call agent.delegate (sub-profiles do not carry it).
func DefaultCodingProfile() Profile {
	return Profile{
		Name:        "coding",
		Description: "Full-capability coding agent with sandbox access; can delegate to review/research/workflow-authoring sub-agents.",
		SystemPrompt: "You are a coding agent operating inside a private development platform. " +
			"You have access to a sandbox via tools. Use the provided tools to inspect files, " +
			"run shell commands, search code, and call LLMs as needed. For public web pages use " +
			"http.fetch (server-side; works when enabled in config) instead of curl in the sandbox. " +
			"Prefer concrete actions over speculation. " +
			"When a user asks for a code review or extensive research, delegate to the appropriate sub-profile via agent.delegate. " +
			"Respond with a final answer once the task is complete.",
		ToolAllowlist: []string{
			"fs.read", "fs.write", "fs.list", "fs.glob",
			"grep", "shell.exec",
			"llm.chat", "llm.embed",
			"http.fetch",
			"memory.save", "memory.search", "memory.list", "memory.delete",
			"agent.delegate",
			"workflow.create", "workflow.update", "workflow.list", "workflow.get",
			"workflow.propose", "workflow.publish",
		},
		MaxSteps: 16,
		SkillIDs: []string{"platform-coding-standards", "workflow-dsl-authoring"},
	}
}

// DefaultReviewProfile returns the read-only "review" sub-profile. Used by
// agent.delegate to spin a child Run that reviews code or documents without
// any ability to modify the sandbox. The system prompt embeds an internal
// marker so the mock provider can recognise it in E2E.
func DefaultReviewProfile() Profile {
	return Profile{
		Name:        "review",
		Description: "Read-only code/document reviewer. Cannot modify the sandbox. Used as a delegate target.",
		SystemPrompt: "You are a meticulous code reviewer. " +
			"You have read-only access to the workspace via fs.read / fs.list / fs.glob / grep, " +
			"and may consult memories and call llm.chat. You must not modify any file, run shell commands, or write memory. " +
			"Produce a concise review with concrete findings and recommended changes. " +
			"Internal marker (do not echo to user): E2E_DELEGATE_SUB_V1",
		ToolAllowlist: []string{
			"fs.read", "fs.list", "fs.glob", "grep",
			"memory.search", "memory.list",
			"llm.chat",
		},
		MaxSteps: 8,
	}
}

// DefaultResearchProfile returns the "research" sub-profile. Used by
// agent.delegate for information-gathering subtasks: it can search memory,
// embed, chat with the LLM, and save findings back to memory, but does not
// touch the sandbox at all.
func DefaultResearchProfile() Profile {
	return Profile{
		Name:        "research",
		Description: "Information-gathering assistant. No sandbox access; may consult LLMs and persist findings to memory.",
		SystemPrompt: "You are a research assistant. " +
			"Use llm.chat / llm.embed and memory.* tools to gather, synthesise, and persist findings. " +
			"You have no access to the workspace or shell. Produce a focused summary as your final answer.",
		ToolAllowlist: []string{
			"llm.chat", "llm.embed",
			"memory.search", "memory.list", "memory.save",
		},
		MaxSteps: 8,
	}
}

// DefaultWorkflowAuthoringProfile pre-registers the "workflow-authoring"
// profile for Slice 19. The current allowlist is intentionally small — once
// Slice 19 introduces workflow.* tools they will be appended here without
// touching call sites.
func DefaultWorkflowAuthoringProfile() Profile {
	return Profile{
		Name:        "workflow-authoring",
		Description: "Drafts and persists Workflow Engine DSL from natural-language requirements; cannot publish.",
		SystemPrompt: "You help users transform natural-language requirements into Workflow Engine YAML DSL. " +
			"Inspect related files with fs.read / fs.glob / grep, consult memories, and call llm.chat to refine the draft. " +
			"Persist drafts with workflow.create or workflow.propose (template/summary path); modify with workflow.update. " +
			"Use workflow.propose for NL template flows with automatic dry-run. " +
			"You cannot publish — admin uses workflow.publish or REST confirm. " +
			"You also cannot modify the sandbox or execute shell commands.",
		ToolAllowlist: []string{
			"llm.chat",
			"memory.search",
			"fs.read", "fs.glob", "grep",
			"workflow.create", "workflow.update", "workflow.list", "workflow.get",
			"workflow.propose",
		},
		MaxSteps: 6,
		SkillIDs: []string{"workflow-dsl-authoring", "workflow-template-authoring"},
	}
}
