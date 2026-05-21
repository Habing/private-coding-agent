package memory_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/memory"
)

func TestFormatRelevantMemories_Truncates(t *testing.T) {
	results := []memory.SearchResult{
		{Memory: memory.Memory{ID: uuid.New(), Type: memory.TypeKnowledge, Content: strings.Repeat("x", 200)}},
		{Memory: memory.Memory{ID: uuid.New(), Type: memory.TypePreference, Content: "short"}},
	}
	section, ids, chars, trunc := memory.FormatRelevantMemories(results, 80)
	require.True(t, trunc)
	require.Len(t, ids, 2)
	require.LessOrEqual(t, chars, 80)
	require.Contains(t, section, "## Relevant memories")
}
