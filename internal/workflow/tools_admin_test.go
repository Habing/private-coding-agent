package workflow_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
	"github.com/yourorg/private-coding-agent/internal/workflow"
)

// adminToolsByName indexes NewAdminTools by Name so tests can pick one without
// caring about slice order.
func adminToolsByName(svc *workflow.Service) map[string]toolbus.Tool {
	out := map[string]toolbus.Tool{}
	for _, t := range workflow.NewAdminTools(svc) {
		out[t.Name()] = t
	}
	return out
}

func ctxWithRole(tenantID, userID uuid.UUID, role string) context.Context {
	return auth.WithClaims(context.Background(), &auth.Claims{
		TenantID: tenantID, UserID: userID, Role: role,
	})
}

func decodeEnvelope(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	return m
}

const toolDSL = `
id: hello
name: Hello
inputs:
  who: { type: string, default: "world" }
steps:
  - id: build
    assign:
      msg: "hi ${inputs.who}"
outputs:
  said: ${vars.msg}
`

// TestAdminTools_RegisteredNamesAndMutating asserts the factory produces the 4
// expected tools and Create/Update are mutating while List/Get are not.
func TestAdminTools_RegisteredNamesAndMutating(t *testing.T) {
	svc, _, _, _, _ := newService(t)
	tools := adminToolsByName(svc)
	for _, name := range []string{"workflow.create", "workflow.update", "workflow.list", "workflow.get"} {
		require.Contains(t, tools, name, "missing tool %s", name)
	}

	mustMut := func(name string, want bool) {
		t.Helper()
		m, ok := tools[name].(toolbus.Mutating)
		if !want {
			// list/get either don't implement Mutating or return false; both OK
			if ok {
				require.False(t, m.IsMutating(), "%s should be non-mutating", name)
			}
			return
		}
		require.True(t, ok && m.IsMutating(), "%s should be mutating", name)
	}
	mustMut("workflow.create", true)
	mustMut("workflow.update", true)
	mustMut("workflow.list", false)
	mustMut("workflow.get", false)
}

// TestAdminTools_Create_AdminHappyPath: admin role → workflow row created and
// the tool envelope reports {ok:true, version:1, published:false}.
func TestAdminTools_Create_AdminHappyPath(t *testing.T) {
	svc, repo, _, _, tid := newService(t)
	tool := adminToolsByName(svc)["workflow.create"]
	uid := uuid.New()

	input, _ := json.Marshal(map[string]any{
		"slug":     "hello",
		"name":     "Hello",
		"dsl_yaml": toolDSL,
	})
	raw, err := tool.Invoke(ctxWithRole(tid, uid, "admin"), tid, uid, input)
	require.NoError(t, err)
	env := decodeEnvelope(t, raw)
	require.Equal(t, true, env["ok"])
	require.EqualValues(t, 1, env["version"])
	require.Equal(t, false, env["published"])

	got, err := repo.Get(context.Background(), tid, "hello")
	require.NoError(t, err)
	require.Equal(t, "Hello", got.Name)
}

// TestAdminTools_Create_NonAdminRejected: a user (role!=admin) hitting
// workflow.create gets {ok:false, error:"permission_denied"} and NO row is
// written. The tool returns the envelope as a successful Go call so the LLM
// sees the explanation in the tool_result.
func TestAdminTools_Create_NonAdminRejected(t *testing.T) {
	svc, repo, _, _, tid := newService(t)
	tool := adminToolsByName(svc)["workflow.create"]
	uid := uuid.New()

	input, _ := json.Marshal(map[string]any{
		"slug": "hello", "name": "Hello", "dsl_yaml": toolDSL,
	})
	raw, err := tool.Invoke(ctxWithRole(tid, uid, "user"), tid, uid, input)
	require.NoError(t, err)
	env := decodeEnvelope(t, raw)
	require.Equal(t, false, env["ok"])
	require.Equal(t, "permission_denied", env["error"])

	_, err = repo.Get(context.Background(), tid, "hello")
	require.ErrorIs(t, err, workflow.ErrNotFound)
}

// TestAdminTools_Create_MissingClaimsRejected: when Bus.Invoke is reached
// without claims (e.g. background goroutine misuse) the tool refuses rather
// than running as "admin by accident".
func TestAdminTools_Create_MissingClaimsRejected(t *testing.T) {
	svc, _, _, _, tid := newService(t)
	tool := adminToolsByName(svc)["workflow.create"]

	input, _ := json.Marshal(map[string]any{
		"slug": "hello", "name": "Hello", "dsl_yaml": toolDSL,
	})
	raw, err := tool.Invoke(context.Background(), tid, uuid.New(), input)
	require.NoError(t, err)
	env := decodeEnvelope(t, raw)
	require.Equal(t, false, env["ok"])
	require.Equal(t, "permission_denied", env["error"])
}

// TestAdminTools_Create_TenantMismatchRejected: if the Bus passes a tenantID
// that doesn't match the Claims tenant the tool refuses — defense in depth
// against ctx plumbing regressions.
func TestAdminTools_Create_TenantMismatchRejected(t *testing.T) {
	svc, _, _, _, tid := newService(t)
	tool := adminToolsByName(svc)["workflow.create"]
	otherTenant := uuid.New()

	input, _ := json.Marshal(map[string]any{
		"slug": "hello", "name": "Hello", "dsl_yaml": toolDSL,
	})
	// Claims say tid; Invoke is called with otherTenant.
	raw, err := tool.Invoke(ctxWithRole(tid, uuid.New(), "admin"), otherTenant, uuid.New(), input)
	require.NoError(t, err)
	env := decodeEnvelope(t, raw)
	require.Equal(t, false, env["ok"])
	require.Equal(t, "permission_denied", env["error"])
}

// TestAdminTools_Create_InvalidDSL surfaces parse/validate failures as
// {ok:false, error:"invalid_dsl"} so the LLM can see what to fix.
func TestAdminTools_Create_InvalidDSL(t *testing.T) {
	svc, _, _, _, tid := newService(t)
	tool := adminToolsByName(svc)["workflow.create"]

	input, _ := json.Marshal(map[string]any{
		"slug":     "broken",
		"name":     "B",
		"dsl_yaml": "not: yaml: : :",
	})
	raw, err := tool.Invoke(ctxWithRole(tid, uuid.New(), "admin"), tid, uuid.New(), input)
	require.NoError(t, err)
	env := decodeEnvelope(t, raw)
	require.Equal(t, false, env["ok"])
	require.Equal(t, "invalid_dsl", env["error"])
}

// TestAdminTools_Update_BumpsVersionAndUnpublishes: even though our fresh row
// is unpublished, version must bump from 1 → 2 and the tool surfaces a
// requires_publish=true hint so the LLM knows to ask a human to publish.
func TestAdminTools_Update_BumpsVersionAndUnpublishes(t *testing.T) {
	svc, _, _, _, tid := newService(t)
	tools := adminToolsByName(svc)
	uid := uuid.New()
	ctx := ctxWithRole(tid, uid, "admin")

	createInput, _ := json.Marshal(map[string]any{
		"slug": "hello", "name": "Hello", "dsl_yaml": toolDSL,
	})
	_, err := tools["workflow.create"].Invoke(ctx, tid, uid, createInput)
	require.NoError(t, err)

	newDSL := strings.Replace(toolDSL, `"hi ${inputs.who}"`, `"hi v2 ${inputs.who}"`, 1)
	updInput, _ := json.Marshal(map[string]any{
		"slug": "hello", "name": "Hello v2", "dsl_yaml": newDSL,
	})
	raw, err := tools["workflow.update"].Invoke(ctx, tid, uid, updInput)
	require.NoError(t, err)
	env := decodeEnvelope(t, raw)
	require.Equal(t, true, env["ok"])
	require.EqualValues(t, 2, env["version"])
	require.Equal(t, false, env["published"])
	require.Equal(t, true, env["requires_publish"])
}

// TestAdminTools_Get_ReturnsDSLBody — agent uses this to read existing DSL
// before proposing an update, so the body must be present.
func TestAdminTools_Get_ReturnsDSLBody(t *testing.T) {
	svc, _, _, _, tid := newService(t)
	tools := adminToolsByName(svc)
	uid := uuid.New()
	ctx := ctxWithRole(tid, uid, "admin")

	createInput, _ := json.Marshal(map[string]any{
		"slug": "hello", "name": "Hello", "dsl_yaml": toolDSL,
	})
	_, err := tools["workflow.create"].Invoke(ctx, tid, uid, createInput)
	require.NoError(t, err)

	getInput, _ := json.Marshal(map[string]any{"slug": "hello"})
	raw, err := tools["workflow.get"].Invoke(ctx, tid, uid, getInput)
	require.NoError(t, err)
	env := decodeEnvelope(t, raw)
	require.Equal(t, true, env["ok"])
	require.Equal(t, "hello", env["slug"])
	require.NotEmpty(t, env["dsl_yaml"])
}

// TestAdminTools_Get_NotFound surfaces a clean error envelope rather than a
// Go error — the LLM can branch on string codes.
func TestAdminTools_Get_NotFound(t *testing.T) {
	svc, _, _, _, tid := newService(t)
	tool := adminToolsByName(svc)["workflow.get"]
	ctx := ctxWithRole(tid, uuid.New(), "admin")

	input, _ := json.Marshal(map[string]any{"slug": "no-such-thing"})
	raw, err := tool.Invoke(ctx, tid, uuid.New(), input)
	require.NoError(t, err)
	env := decodeEnvelope(t, raw)
	require.Equal(t, false, env["ok"])
	require.Equal(t, "not_found", env["error"])
}

// TestAdminTools_List_TenantScoped verifies the tool only sees the caller's
// tenant — never another tenant's workflows.
func TestAdminTools_List_TenantScoped(t *testing.T) {
	svc, _, _, _, tidA := newService(t)
	// Seed a second tenant against the same pool so both rows exist in the table.
	tidB := seedTenant(t, newPool(t))
	tools := adminToolsByName(svc)
	uid := uuid.New()

	// create one workflow under tenant A
	createInput, _ := json.Marshal(map[string]any{
		"slug": "hello", "name": "Hello", "dsl_yaml": toolDSL,
	})
	_, err := tools["workflow.create"].Invoke(
		ctxWithRole(tidA, uid, "admin"), tidA, uid, createInput,
	)
	require.NoError(t, err)

	// List from tenant B's POV → empty.
	rawB, err := tools["workflow.list"].Invoke(
		ctxWithRole(tidB, uid, "admin"), tidB, uid, json.RawMessage(`{}`),
	)
	require.NoError(t, err)
	envB := decodeEnvelope(t, rawB)
	require.Equal(t, true, envB["ok"])
	wsB, _ := envB["workflows"].([]any)
	require.Empty(t, wsB)

	// List from tenant A → 1.
	rawA, err := tools["workflow.list"].Invoke(
		ctxWithRole(tidA, uid, "admin"), tidA, uid, json.RawMessage(`{}`),
	)
	require.NoError(t, err)
	envA := decodeEnvelope(t, rawA)
	wsA, _ := envA["workflows"].([]any)
	require.Len(t, wsA, 1)
}

// TestAdminTools_List_NonAdminRejected: List is read-only but still admin-gated
// to mirror the REST surface (workflows can leak DSL structure that admins
// consider internal).
func TestAdminTools_List_NonAdminRejected(t *testing.T) {
	svc, _, _, _, tid := newService(t)
	tool := adminToolsByName(svc)["workflow.list"]
	raw, err := tool.Invoke(ctxWithRole(tid, uuid.New(), "user"), tid, uuid.New(), json.RawMessage(`{}`))
	require.NoError(t, err)
	env := decodeEnvelope(t, raw)
	require.Equal(t, false, env["ok"])
	require.Equal(t, "permission_denied", env["error"])
}
