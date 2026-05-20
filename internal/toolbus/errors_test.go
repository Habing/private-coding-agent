package toolbus_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

func TestErrorSentinels(t *testing.T) {
	require.Error(t, toolbus.ErrToolNotFound)
	require.Error(t, toolbus.ErrInvalidArguments)
	require.Error(t, toolbus.ErrSandboxIDRequired)
	require.Error(t, toolbus.ErrToolFailed)
}
