package agent_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/agent"
)

func TestErrorSentinels(t *testing.T) {
	require.Error(t, agent.ErrUnknownProfile)
	require.Error(t, agent.ErrEmptyMessages)
	require.Error(t, agent.ErrMaxStepsExceeded)
	require.Error(t, agent.ErrLLMFailed)
	require.Error(t, agent.ErrToolCallParseFailed)
}
