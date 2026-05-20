package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/toolbus/tools"
)

func TestShellExec_ForwardsCmdAndReturnsResult(t *testing.T) {
	rt := &mockRuntime{execRet: &sandbox.ExecResult{
		ExitCode: 0, Stdout: []byte("hi\n"), Stderr: nil,
		DurationMS: 5, Truncated: false, TimedOut: false,
	}}
	tool := tools.NewShellExec(rt)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"cmd":["echo","hi"]}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"exit_code":0,"stdout":"hi\n","stderr":"","truncated":false,"duration_ms":5,"timed_out":false}`,
		string(out))
	require.Equal(t, []string{"echo", "hi"}, rt.lastExec.Cmd)
}

func TestShellExec_TimeoutForwarded(t *testing.T) {
	rt := &mockRuntime{execRet: &sandbox.ExecResult{}}
	tool := tools.NewShellExec(rt)
	_, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"cmd":["true"],"timeout_sec":15}`))
	require.NoError(t, err)
	require.Equal(t, 15, rt.lastExec.TimeoutSec)
}
