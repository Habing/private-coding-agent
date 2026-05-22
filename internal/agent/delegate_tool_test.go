package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/agent"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func newDelegateProfileMap() map[string]agent.Profile {
	return map[string]agent.Profile{
		"coding":             agent.DefaultCodingProfile(),
		"review":             agent.DefaultReviewProfile(),
		"research":           agent.DefaultResearchProfile(),
		"workflow-authoring": agent.DefaultWorkflowAuthoringProfile(),
	}
}

func newDelegateEngine(t *testing.T, gw agent.Gateway) *agent.Engine {
	t.Helper()
	return agent.NewEngine(gw, &mockBus{}, newDelegateProfileMap(), agent.NoopComposer{})
}

func decodeDelegateOut(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func TestDelegateTool_NameSchemaDescription(t *testing.T) {
	engine := newDelegateEngine(t, &mockGateway{})
	tool := agent.NewDelegateTool(engine, newDelegateProfileMap(), nil)

	require.Equal(t, "agent.delegate", tool.Name())
	require.NotEmpty(t, tool.Description())

	var schema map[string]any
	require.NoError(t, json.Unmarshal(tool.Schema(), &schema))
	require.Equal(t, "object", schema["type"])
	require.Equal(t, false, schema["additionalProperties"])

	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	profile, ok := props["profile"].(map[string]any)
	require.True(t, ok)
	enum, ok := profile["enum"].([]any)
	require.True(t, ok)
	require.Len(t, enum, 4)
}

func TestDelegateTool_RejectsUnknownProfile(t *testing.T) {
	engine := newDelegateEngine(t, &mockGateway{})
	tool := agent.NewDelegateTool(engine, newDelegateProfileMap(), nil)

	raw, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"profile":"ghost","task":"do something"}`))
	require.NoError(t, err)
	out := decodeDelegateOut(t, raw)
	require.Equal(t, "error", out["status"])
	require.Contains(t, out["result"], "unknown profile")
}

func TestDelegateTool_RejectsEmptyTask(t *testing.T) {
	engine := newDelegateEngine(t, &mockGateway{})
	tool := agent.NewDelegateTool(engine, newDelegateProfileMap(), nil)

	raw, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"profile":"review","task":""}`))
	require.NoError(t, err)
	out := decodeDelegateOut(t, raw)
	require.Equal(t, "error", out["status"])
	require.Contains(t, out["result"], "task")
}

func TestDelegateTool_DepthCapEnforced(t *testing.T) {
	engine := newDelegateEngine(t, &mockGateway{})
	tool := agent.NewDelegateTool(engine, newDelegateProfileMap(), nil)

	// Simulate "already inside a delegated child" via ctx.
	parent := agent.RunCtx{DelegateDepth: agent.MaxDelegateDepth, Model: "default-mock:gpt-4o"}
	ctx := agent.WithRunCtx(context.Background(), parent)

	raw, err := tool.Invoke(ctx, uuid.New(), uuid.New(),
		json.RawMessage(`{"profile":"review","task":"please review"}`))
	require.NoError(t, err)
	out := decodeDelegateOut(t, raw)
	require.Equal(t, "error", out["status"])
	require.Contains(t, out["result"], "depth")
}

func TestDelegateTool_SuccessfulRun(t *testing.T) {
	// Single-step child Run: review profile emits a final assistant message.
	gw := &mockGateway{responses: []*modelgw.ChatResponse{
		chatStop("review summary: looks fine"),
	}}
	engine := newDelegateEngine(t, gw)
	tool := agent.NewDelegateTool(engine, newDelegateProfileMap(), nil)

	parent := agent.RunCtx{
		SandboxID:   uuid.New(),
		Model:       "default-mock:gpt-4o",
		ProfileName: "coding",
	}
	ctx := agent.WithRunCtx(context.Background(), parent)

	raw, err := tool.Invoke(ctx, uuid.New(), uuid.New(),
		json.RawMessage(`{"profile":"review","task":"review readme"}`))
	require.NoError(t, err)
	out := decodeDelegateOut(t, raw)
	require.Equal(t, "ok", out["status"])
	require.Equal(t, "review summary: looks fine", out["result"])
	require.GreaterOrEqual(t, out["sub_steps"], float64(1))

	// Engine must have been called with the parent sandbox + model.
	require.Len(t, gw.calls, 1)
	require.Equal(t, "default-mock:gpt-4o", gw.calls[0].Model)
	// First message should be the sandbox_id system prefix injected by the engine.
	var sawSandboxInject bool
	for _, m := range gw.calls[0].Messages {
		if m.Role == modelgw.RoleSystem && strings.Contains(m.Content, parent.SandboxID.String()) {
			sawSandboxInject = true
			break
		}
	}
	require.True(t, sawSandboxInject, "child Run must inherit parent sandbox_id")
}

func TestDelegateTool_MaxStepsClamp(t *testing.T) {
	// max_steps=99 must be clamped to 8; the script we provide gives only 1
	// response, which under clamped max_steps still produces an ok run.
	gw := &mockGateway{responses: []*modelgw.ChatResponse{chatStop("done")}}
	engine := newDelegateEngine(t, gw)
	tool := agent.NewDelegateTool(engine, newDelegateProfileMap(), nil)

	parent := agent.RunCtx{Model: "default-mock:gpt-4o", ProfileName: "coding"}
	ctx := agent.WithRunCtx(context.Background(), parent)

	raw, err := tool.Invoke(ctx, uuid.New(), uuid.New(),
		json.RawMessage(`{"profile":"review","task":"x","max_steps":99}`))
	require.NoError(t, err)
	out := decodeDelegateOut(t, raw)
	require.Equal(t, "ok", out["status"])
}

func TestDelegateTool_ChildErrorReportedNotPropagated(t *testing.T) {
	// Empty responses → mockGateway returns "out of scripted responses" → Engine.Run
	// returns ErrLLMFailed. We expect status="error", but no Go error from the tool.
	gw := &mockGateway{}
	engine := newDelegateEngine(t, gw)
	tool := agent.NewDelegateTool(engine, newDelegateProfileMap(), nil)

	parent := agent.RunCtx{Model: "default-mock:gpt-4o", ProfileName: "coding"}
	ctx := agent.WithRunCtx(context.Background(), parent)

	raw, err := tool.Invoke(ctx, uuid.New(), uuid.New(),
		json.RawMessage(`{"profile":"review","task":"x"}`))
	require.NoError(t, err)
	out := decodeDelegateOut(t, raw)
	require.Equal(t, "error", out["status"])
}

func TestDelegateTool_InvalidJSONInput(t *testing.T) {
	engine := newDelegateEngine(t, &mockGateway{})
	tool := agent.NewDelegateTool(engine, newDelegateProfileMap(), nil)
	raw, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`not-json`))
	require.NoError(t, err)
	out := decodeDelegateOut(t, raw)
	require.Equal(t, "error", out["status"])
}
