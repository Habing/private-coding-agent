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

// mockRuntime captures the args of the last call.
type mockRuntime struct {
	readRet  []byte
	readErr  error
	writeErr error
	execRet  *sandbox.ExecResult
	execErr  error

	lastRead  string
	lastWrite struct {
		path string
		data []byte
	}
	lastExec sandbox.ExecOpts
}

func (m *mockRuntime) Exec(_ context.Context, _, _ uuid.UUID, opts sandbox.ExecOpts) (*sandbox.ExecResult, error) {
	m.lastExec = opts
	return m.execRet, m.execErr
}
func (m *mockRuntime) ReadFile(_ context.Context, _, _ uuid.UUID, path string) ([]byte, error) {
	m.lastRead = path
	return m.readRet, m.readErr
}
func (m *mockRuntime) WriteFile(_ context.Context, _, _ uuid.UUID, path string, data []byte) error {
	m.lastWrite.path = path
	m.lastWrite.data = data
	return m.writeErr
}

const validSandboxJSON = `"00000000-0000-0000-0000-000000000001"`

func TestFSRead_OK(t *testing.T) {
	rt := &mockRuntime{readRet: []byte("hello")}
	tool := tools.NewFSRead(rt)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"path":"foo.txt"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"content":"hello","size":5}`, string(out))
	require.Equal(t, "foo.txt", rt.lastRead)
}

func TestFSRead_DownstreamError(t *testing.T) {
	rt := &mockRuntime{readErr: sandbox.ErrSandboxNotFound}
	tool := tools.NewFSRead(rt)
	_, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"path":"foo.txt"}`))
	require.ErrorIs(t, err, sandbox.ErrSandboxNotFound)
}

func TestFSRead_InvalidUTF8Replaced(t *testing.T) {
	rt := &mockRuntime{readRet: []byte{0xff, 0xfe, 'a'}}
	tool := tools.NewFSRead(rt)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"path":"x"}`))
	require.NoError(t, err)
	var got struct {
		Content string `json:"content"`
		Size    int    `json:"size"`
	}
	require.NoError(t, json.Unmarshal(out, &got))
	require.Contains(t, got.Content, "a")
	require.Equal(t, 3, got.Size)
}

func TestFSWrite_OK(t *testing.T) {
	rt := &mockRuntime{}
	tool := tools.NewFSWrite(rt)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"path":"a.txt","content":"hello"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"bytes_written":5}`, string(out))
	require.Equal(t, "a.txt", rt.lastWrite.path)
	require.Equal(t, []byte("hello"), rt.lastWrite.data)
}

func TestFSList_ParsesFindOutput(t *testing.T) {
	rt := &mockRuntime{execRet: &sandbox.ExecResult{
		ExitCode: 0,
		Stdout:   []byte("src\td\t4096\ngo.mod\tf\t123\n"),
	}}
	tool := tools.NewFSList(rt)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"path":"."}`))
	require.NoError(t, err)
	var got struct {
		Entries []struct {
			Name string `json:"name"`
			Type string `json:"type"`
			Size *int   `json:"size,omitempty"`
		} `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(out, &got))
	require.Len(t, got.Entries, 2)
	require.Equal(t, "src", got.Entries[0].Name)
	require.Equal(t, "dir", got.Entries[0].Type)
	require.Equal(t, "go.mod", got.Entries[1].Name)
	require.Equal(t, "file", got.Entries[1].Type)
	require.NotNil(t, got.Entries[1].Size)
	require.Equal(t, 123, *got.Entries[1].Size)
}

func TestFSGlob_FiltersWithDoublestar(t *testing.T) {
	rt := &mockRuntime{execRet: &sandbox.ExecResult{
		ExitCode: 0,
		Stdout:   []byte("src/main.go\nsrc/test.txt\nsrc/lib/util.go\n"),
	}}
	tool := tools.NewFSGlob(rt)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"pattern":"**/*.go"}`))
	require.NoError(t, err)
	var got struct {
		Matches []string `json:"matches"`
	}
	require.NoError(t, json.Unmarshal(out, &got))
	require.ElementsMatch(t, []string{"src/main.go", "src/lib/util.go"}, got.Matches)
}

func TestFSRead_MissingSandboxID(t *testing.T) {
	rt := &mockRuntime{}
	tool := tools.NewFSRead(rt)
	_, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"path":"foo.txt"}`))
	require.Error(t, err)
}
