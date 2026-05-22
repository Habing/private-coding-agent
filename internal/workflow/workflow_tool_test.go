package workflow_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/toolbus"
	"github.com/yourorg/private-coding-agent/internal/workflow"
)

// TestWorkflowTool_NameSchemaDescription locks the contract the Bus uses to
// list tools (Name has the "workflow." prefix, Schema is generated, Mutating
// is conservatively true).
func TestWorkflowTool_NameSchemaDescription(t *testing.T) {
	svc, _, bus, _, tid := newService(t)
	ctx := context.Background()
	_, err := svc.Create(ctx, tid, "greet", "Greet", "say hi", svcDSL)
	require.NoError(t, err)
	require.NoError(t, svc.Publish(ctx, tid, "greet"))

	// Grab the registered tool through the fakeBus surface.
	tool := bus.tools["workflow.greet"]
	require.NotNil(t, tool)
	require.Equal(t, "workflow.greet", tool.Name())
	require.Equal(t, "say hi", tool.Description())

	var schema map[string]any
	require.NoError(t, json.Unmarshal(tool.Schema(), &schema))
	require.Equal(t, "object", schema["type"])
	props, _ := schema["properties"].(map[string]any)
	require.Contains(t, props, "who")

	m, ok := tool.(toolbus.Mutating)
	require.True(t, ok)
	require.True(t, m.IsMutating(), "workflow.<slug> is conservatively mutating")
}

// TestWorkflowTool_InvokeHappyPath dispatches via Bus.Invoke (going through
// the WorkflowTool wrapper) and asserts the outputs come back as JSON.
func TestWorkflowTool_InvokeHappyPath(t *testing.T) {
	svc, _, bus, _, tid := newService(t)
	ctx := context.Background()
	_, err := svc.Create(ctx, tid, "greet", "G", "", svcDSL)
	require.NoError(t, err)
	require.NoError(t, svc.Publish(ctx, tid, "greet"))

	uid := uuid.New()
	out, err := bus.Invoke(ctx, tid, uid, "workflow.greet", json.RawMessage(`{"who":"Bob"}`))
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	require.Equal(t, "hello Bob", got["said"])
}

// TestWorkflowTool_CrossTenantRefused: tool registered for tenant A; a call
// arriving with tenant B's claims is refused at the tool boundary.
func TestWorkflowTool_CrossTenantRefused(t *testing.T) {
	svc, _, bus, _, tidA := newService(t)
	ctx := context.Background()
	_, err := svc.Create(ctx, tidA, "greet", "G", "", svcDSL)
	require.NoError(t, err)
	require.NoError(t, svc.Publish(ctx, tidA, "greet"))

	tidB := uuid.New()
	_, err = bus.Invoke(ctx, tidB, uuid.New(), "workflow.greet", json.RawMessage(`{}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "cross-tenant")
}

// TestSchemaFromInputs covers the standalone schema-from-DSL helper: typed
// inputs translate, defaults exempt the key from `required`, raw schema
// overrides win.
func TestSchemaFromInputs(t *testing.T) {
	in := map[string]workflow.InputSpec{
		"name":   {Type: "string"},
		"count":  {Type: "int", Default: 5},
		"items":  {Type: "array", Schema: map[string]any{"items": map[string]any{"type": "string"}}},
		"active": {Type: "bool"},
	}
	raw, err := workflow.SchemaFromInputs(in)
	require.NoError(t, err)
	var s map[string]any
	require.NoError(t, json.Unmarshal(raw, &s))
	require.Equal(t, "object", s["type"])
	props := s["properties"].(map[string]any)
	require.Equal(t, "integer", props["count"].(map[string]any)["type"])
	require.Equal(t, "boolean", props["active"].(map[string]any)["type"])

	itemsSchema := props["items"].(map[string]any)
	require.Equal(t, "array", itemsSchema["type"])
	require.Equal(t, "string", itemsSchema["items"].(map[string]any)["type"])

	req, _ := s["required"].([]any)
	// "count" has Default → not required; the others are.
	hasCount := false
	for _, k := range req {
		if k.(string) == "count" {
			hasCount = true
		}
	}
	require.False(t, hasCount, "count has default and should not appear in required")
}
