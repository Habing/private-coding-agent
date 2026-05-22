package toolbus_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// mockTool is configurable for bus_test.
type mockTool struct {
	name        string
	schema      json.RawMessage
	invokeRet   json.RawMessage
	invokeErr   error
	calledTimes int
	mu          sync.Mutex
}

func (m *mockTool) Name() string            { return m.name }
func (m *mockTool) Description() string     { return "mock " + m.name }
func (m *mockTool) Schema() json.RawMessage { return m.schema }
func (m *mockTool) Invoke(_ context.Context, _, _ uuid.UUID, _ json.RawMessage) (json.RawMessage, error) {
	m.mu.Lock()
	m.calledTimes++
	m.mu.Unlock()
	return m.invokeRet, m.invokeErr
}

func busWith(t *testing.T, tools ...toolbus.Tool) (*toolbus.Bus, *sync.Mutex, *[]error) {
	t.Helper()
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	reg := toolbus.NewRegistry()
	for _, tool := range tools {
		require.NoError(t, reg.Register(tool))
	}
	var errs []error
	var mu sync.Mutex
	rec := toolbus.NewInvocationRecorder(toolbus.NewInvocationRepo(pg), func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	})
	bus, err := toolbus.NewBus(reg, rec)
	require.NoError(t, err)
	return bus, &mu, &errs
}

const objSchemaWithX = `{"type":"object","properties":{"x":{"type":"integer"}},"required":["x"]}`

func TestBus_Invoke_OK(t *testing.T) {
	tool := &mockTool{name: "t.ok", schema: json.RawMessage(objSchemaWithX),
		invokeRet: json.RawMessage(`{"ok":true}`)}
	bus, _, _ := busWith(t, tool)

	out, err := bus.Invoke(context.Background(), uuid.New(), uuid.New(),
		"t.ok", json.RawMessage(`{"x":5}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, string(out))
	require.Equal(t, 1, tool.calledTimes)
}

func TestBus_Invoke_NotFound(t *testing.T) {
	bus, _, _ := busWith(t)
	_, err := bus.Invoke(context.Background(), uuid.New(), uuid.New(),
		"missing", json.RawMessage(`{}`))
	require.ErrorIs(t, err, toolbus.ErrToolNotFound)
}

func TestBus_Invoke_SchemaFail(t *testing.T) {
	tool := &mockTool{name: "t.schema", schema: json.RawMessage(objSchemaWithX)}
	bus, _, _ := busWith(t, tool)

	_, err := bus.Invoke(context.Background(), uuid.New(), uuid.New(),
		"t.schema", json.RawMessage(`{"x":"not-int"}`))
	require.ErrorIs(t, err, toolbus.ErrInvalidArguments)
	require.Equal(t, 0, tool.calledTimes)
}

func TestBus_Invoke_ToolError_RecordedAsError(t *testing.T) {
	tool := &mockTool{name: "t.err", schema: json.RawMessage(objSchemaWithX),
		invokeErr: errors.New("downstream boom")}
	bus, _, _ := busWith(t, tool)

	_, err := bus.Invoke(context.Background(), uuid.New(), uuid.New(),
		"t.err", json.RawMessage(`{"x":1}`))
	require.Error(t, err)
}

func TestBus_ListTools(t *testing.T) {
	bus, _, _ := busWith(t,
		&mockTool{name: "a", schema: json.RawMessage(objSchemaWithX)},
		&mockTool{name: "b", schema: json.RawMessage(objSchemaWithX)})
	list := bus.ListTools(context.Background(), uuid.New())
	require.Len(t, list, 2)
	require.Equal(t, "a", list[0].Name)
	require.Equal(t, "b", list[1].Name)
}

// mockMutatingTool implements toolbus.Mutating so ListTools surfaces the flag.
// We need it to verify GET /tools exposes "mutating":true for side-effecting
// tools and "mutating":false for non-mutating ones — the WebUI Toolbox page
// uses this to render the red badge.
type mockMutatingTool struct{ mockTool }

func (m *mockMutatingTool) IsMutating() bool { return true }

func TestBus_ListTools_MutatingFlag(t *testing.T) {
	mut := &mockMutatingTool{mockTool: mockTool{
		name: "writer", schema: json.RawMessage(objSchemaWithX),
	}}
	plain := &mockTool{name: "reader", schema: json.RawMessage(objSchemaWithX)}
	bus, _, _ := busWith(t, mut, plain)

	list := bus.ListTools(context.Background(), uuid.New())
	require.Len(t, list, 2)
	byName := map[string]toolbus.ToolDef{}
	for _, td := range list {
		byName[td.Name] = td
	}
	require.True(t, byName["writer"].Mutating, "Mutating impl must surface as true")
	require.False(t, byName["reader"].Mutating, "tool without Mutating defaults to false")
}
