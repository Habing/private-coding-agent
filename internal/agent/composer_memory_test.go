package agent_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/agent"
)

func TestMemoryComposer_AppendsSection(t *testing.T) {
	c := agent.WrapMemoryComposer(agent.NoopComposer{})
	msgs, meta, err := c.ComposeSystem(context.Background(), agent.ComposeInput{
		TenantID:        uuid.New(),
		UserID:          uuid.New(),
		MemorySection:   "## Relevant memories\n- [knowledge] hello\n",
		MemoryIDs:       []string{"id-1"},
		MemoryCharCount: 30,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Contains(t, msgs[0].Content, "Relevant memories")
	require.Equal(t, []string{"id-1"}, meta.MemoryIDs)
	require.Equal(t, 30, meta.MemoryCharCount)
}

func TestMemoryComposer_PassthroughWithoutSection(t *testing.T) {
	c := agent.WrapMemoryComposer(agent.NoopComposer{})
	msgs, meta, err := c.ComposeSystem(context.Background(), agent.ComposeInput{})
	require.NoError(t, err)
	require.Empty(t, msgs)
	require.Empty(t, meta.MemoryIDs)
}
