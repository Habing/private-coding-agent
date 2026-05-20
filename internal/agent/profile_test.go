package agent_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/agent"
)

func TestDefaultCodingProfile(t *testing.T) {
	p := agent.DefaultCodingProfile()
	require.Equal(t, "coding", p.Name)
	require.NotEmpty(t, p.SystemPrompt)
	require.Equal(t, 16, p.MaxSteps)
	require.Contains(t, p.ToolAllowlist, "fs.read")
	require.Contains(t, p.ToolAllowlist, "shell.exec")
	require.Contains(t, p.ToolAllowlist, "llm.chat")
	require.Len(t, p.ToolAllowlist, 8)
}
