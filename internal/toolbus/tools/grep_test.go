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

const ripgrepJSON = `{"type":"match","data":{"path":{"text":"src/foo.go"},"line_number":12,"lines":{"text":"func Foo() {}\n"}}}
{"type":"match","data":{"path":{"text":"src/bar.go"},"line_number":3,"lines":{"text":"func Foo2() {}\n"}}}
`

func TestGrep_ParsesRipgrepJSON(t *testing.T) {
	rt := &mockRuntime{execRet: &sandbox.ExecResult{
		ExitCode: 0,
		Stdout:   []byte(ripgrepJSON),
	}}
	tool := tools.NewGrep(rt)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"pattern":"Foo"}`))
	require.NoError(t, err)

	var got struct {
		Matches []struct {
			Path string `json:"path"`
			Line int    `json:"line"`
			Text string `json:"text"`
		} `json:"matches"`
	}
	require.NoError(t, json.Unmarshal(out, &got))
	require.Len(t, got.Matches, 2)
	require.Equal(t, "src/foo.go", got.Matches[0].Path)
	require.Equal(t, 12, got.Matches[0].Line)
}

func TestGrep_MaxResults(t *testing.T) {
	var sb []byte
	for i := 0; i < 200; i++ {
		sb = append(sb, []byte(`{"type":"match","data":{"path":{"text":"a.go"},"line_number":1,"lines":{"text":"x\n"}}}`+"\n")...)
	}
	rt := &mockRuntime{execRet: &sandbox.ExecResult{ExitCode: 0, Stdout: sb}}
	tool := tools.NewGrep(rt)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"pattern":"x"}`))
	require.NoError(t, err)
	var got struct {
		Matches []map[string]any `json:"matches"`
	}
	require.NoError(t, json.Unmarshal(out, &got))
	require.Len(t, got.Matches, 100)
}

func TestGrep_RGExit1NoMatchIsOK(t *testing.T) {
	rt := &mockRuntime{execRet: &sandbox.ExecResult{ExitCode: 1, Stdout: nil}}
	tool := tools.NewGrep(rt)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"pattern":"missing"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"matches":[]}`, string(out))
}
