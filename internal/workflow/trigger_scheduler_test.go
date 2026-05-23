package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeTriggerInputs_BodyOverridesDefaults(t *testing.T) {
	out := mergeTriggerInputs(map[string]any{"a": 1, "b": 2}, map[string]any{"b": 9})
	require.Equal(t, 1, out["a"])
	require.Equal(t, 9, out["b"])
}

func TestParseDefaultInputs_Empty(t *testing.T) {
	out, err := parseDefaultInputs(nil)
	require.NoError(t, err)
	require.Empty(t, out)
}
