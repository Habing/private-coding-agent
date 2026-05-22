package toolbus_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

type mutatingMockTool struct{ mockTool }

func (mutatingMockTool) IsMutating() bool { return true }

func TestBus_Unregister_RoundTrip(t *testing.T) {
	a := &mockTool{name: "ephemeral", schema: json.RawMessage(objSchemaWithX),
		invokeRet: json.RawMessage(`{"ok":true}`)}
	bus, _, _ := busWith(t, a)

	// Sanity: in list pre-unregister.
	pre := bus.ListTools(context.Background(), uuid.New())
	require.Len(t, pre, 1)

	require.NoError(t, bus.Unregister("ephemeral"))

	post := bus.ListTools(context.Background(), uuid.New())
	require.Empty(t, post)

	// Invoke after unregister surfaces ErrToolNotFound (schema cache also dropped).
	_, err := bus.Invoke(context.Background(), uuid.New(), uuid.New(),
		"ephemeral", json.RawMessage(`{"x":1}`))
	require.ErrorIs(t, err, toolbus.ErrToolNotFound)
}

func TestBus_Unregister_Missing(t *testing.T) {
	bus, _, _ := busWith(t)
	err := bus.Unregister("never-was-here")
	require.Error(t, err)
}

func TestBus_RegisterAfterUnregister(t *testing.T) {
	a := &mockTool{name: "swap", schema: json.RawMessage(objSchemaWithX),
		invokeRet: json.RawMessage(`{"v":1}`)}
	bus, _, _ := busWith(t, a)
	require.NoError(t, bus.Unregister("swap"))

	b := &mockTool{name: "swap", schema: json.RawMessage(objSchemaWithX),
		invokeRet: json.RawMessage(`{"v":2}`)}
	require.NoError(t, bus.Register(b))

	out, err := bus.Invoke(context.Background(), uuid.New(), uuid.New(),
		"swap", json.RawMessage(`{"x":1}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"v":2}`, string(out))
}

func TestBus_IsMutating(t *testing.T) {
	mut := &mutatingMockTool{mockTool: mockTool{name: "danger", schema: json.RawMessage(objSchemaWithX)}}
	plain := &mockTool{name: "safe", schema: json.RawMessage(objSchemaWithX)}
	bus, _, _ := busWith(t, mut, plain)

	require.True(t, bus.IsMutating("danger"))
	require.False(t, bus.IsMutating("safe"))
	// Unknown tool: false (caller will surface ErrToolNotFound during Invoke).
	require.False(t, bus.IsMutating("ghost"))
}
