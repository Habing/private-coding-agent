package toolbus_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// noopTool 是测试 helper:满足 Tool 接口但实际不工作。
type noopTool struct {
	name string
}

func (n noopTool) Name() string            { return n.name }
func (n noopTool) Description() string      { return "noop" }
func (n noopTool) Schema() json.RawMessage  { return json.RawMessage(`{"type":"object"}`) }
func (n noopTool) Invoke(_ context.Context, _, _ uuid.UUID, _ json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := toolbus.NewRegistry()
	require.NoError(t, r.Register(noopTool{name: "fs.read"}))

	got, ok := r.Get("fs.read")
	require.True(t, ok)
	require.Equal(t, "fs.read", got.Name())
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := toolbus.NewRegistry()
	_, ok := r.Get("missing")
	require.False(t, ok)
}

func TestRegistry_DuplicateName(t *testing.T) {
	r := toolbus.NewRegistry()
	require.NoError(t, r.Register(noopTool{name: "a"}))
	require.Error(t, r.Register(noopTool{name: "a"}))
}

func TestRegistry_List_Sorted(t *testing.T) {
	r := toolbus.NewRegistry()
	require.NoError(t, r.Register(noopTool{name: "z.t"}))
	require.NoError(t, r.Register(noopTool{name: "a.t"}))
	require.NoError(t, r.Register(noopTool{name: "m.t"}))

	list := r.List()
	require.Len(t, list, 3)
	require.Equal(t, "a.t", list[0].Name())
	require.Equal(t, "m.t", list[1].Name())
	require.Equal(t, "z.t", list[2].Name())
}
