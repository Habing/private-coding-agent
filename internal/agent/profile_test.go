package agent_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/agent"
)

func TestDefaultCodingProfile(t *testing.T) {
	p := agent.DefaultCodingProfile()
	require.Equal(t, "coding", p.Name)
	require.NotEmpty(t, p.Description)
	require.NotEmpty(t, p.SystemPrompt)
	require.Equal(t, 16, p.MaxSteps)
	require.Contains(t, p.ToolAllowlist, "fs.read")
	require.Contains(t, p.ToolAllowlist, "shell.exec")
	require.Contains(t, p.ToolAllowlist, "llm.chat")
	require.Contains(t, p.ToolAllowlist, "memory.save")
	require.Contains(t, p.ToolAllowlist, "memory.search")
	require.Contains(t, p.ToolAllowlist, "agent.delegate")
	require.Len(t, p.ToolAllowlist, 13)
	require.Equal(t, []string{"platform-coding-standards"}, p.SkillIDs)
}

func TestDefaultReviewProfile(t *testing.T) {
	p := agent.DefaultReviewProfile()
	require.Equal(t, "review", p.Name)
	require.NotEmpty(t, p.Description)
	require.Equal(t, 8, p.MaxSteps)
	require.Contains(t, p.ToolAllowlist, "fs.read")
	require.Contains(t, p.ToolAllowlist, "grep")
	require.Contains(t, p.ToolAllowlist, "llm.chat")
	require.NotContains(t, p.ToolAllowlist, "fs.write")
	require.NotContains(t, p.ToolAllowlist, "shell.exec")
	require.NotContains(t, p.ToolAllowlist, "memory.save")
	require.NotContains(t, p.ToolAllowlist, "agent.delegate")
	require.Contains(t, p.SystemPrompt, "E2E_DELEGATE_SUB_V1")
	require.Empty(t, p.SkillIDs)
}

func TestDefaultResearchProfile(t *testing.T) {
	p := agent.DefaultResearchProfile()
	require.Equal(t, "research", p.Name)
	require.NotEmpty(t, p.Description)
	require.Equal(t, 8, p.MaxSteps)
	require.Contains(t, p.ToolAllowlist, "llm.chat")
	require.Contains(t, p.ToolAllowlist, "llm.embed")
	require.Contains(t, p.ToolAllowlist, "memory.search")
	require.Contains(t, p.ToolAllowlist, "memory.save")
	require.NotContains(t, p.ToolAllowlist, "fs.read")
	require.NotContains(t, p.ToolAllowlist, "shell.exec")
	require.NotContains(t, p.ToolAllowlist, "agent.delegate")
}

func TestDefaultWorkflowAuthoringProfile(t *testing.T) {
	p := agent.DefaultWorkflowAuthoringProfile()
	require.Equal(t, "workflow-authoring", p.Name)
	require.NotEmpty(t, p.Description)
	require.Equal(t, 6, p.MaxSteps)
	require.Contains(t, p.ToolAllowlist, "llm.chat")
	require.Contains(t, p.ToolAllowlist, "memory.search")
	require.Contains(t, p.ToolAllowlist, "fs.read")
	require.Contains(t, p.ToolAllowlist, "grep")
	require.NotContains(t, p.ToolAllowlist, "fs.write")
	require.NotContains(t, p.ToolAllowlist, "shell.exec")
	require.NotContains(t, p.ToolAllowlist, "agent.delegate")
}
