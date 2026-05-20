package agent_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/agent"
)

func TestTruncate_SmallReturnsOriginal(t *testing.T) {
	in := json.RawMessage(`{"ok":true}`)
	out, trunc := agent.TruncateToolOutput(in, 1024)
	require.False(t, trunc)
	require.Equal(t, []byte(in), []byte(out))
}

func TestTruncate_LargeWrapped(t *testing.T) {
	big := bytes.Repeat([]byte("x"), 60*1024)
	in := json.RawMessage(big)
	out, trunc := agent.TruncateToolOutput(in, agent.DefaultMaxToolOutputBytes)
	require.True(t, trunc)
	var env struct {
		Truncated    bool   `json:"truncated"`
		OriginalSize int    `json:"original_size"`
		Preview      string `json:"preview"`
	}
	require.NoError(t, json.Unmarshal(out, &env))
	require.True(t, env.Truncated)
	require.Equal(t, len(big), env.OriginalSize)
	require.NotEmpty(t, env.Preview)
	require.LessOrEqual(t, len(out), agent.DefaultMaxToolOutputBytes)
}

func TestTruncate_MaxZeroSkips(t *testing.T) {
	in := json.RawMessage(`{"x":1}`)
	out, trunc := agent.TruncateToolOutput(in, 0)
	require.False(t, trunc)
	require.Equal(t, []byte(in), []byte(out))
}
