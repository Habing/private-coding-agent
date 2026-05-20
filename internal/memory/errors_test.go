package memory_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/memory"
)

func TestErrors_NonEmpty(t *testing.T) {
	for _, e := range []error{
		memory.ErrMemoryNotFound,
		memory.ErrEmptyContent,
		memory.ErrInvalidType,
		memory.ErrEmptySearch,
	} {
		require.NotNil(t, e)
		require.NotEmpty(t, e.Error())
	}
}
