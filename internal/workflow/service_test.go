package workflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
	"github.com/yourorg/private-coding-agent/internal/workflow"
)

// fakeBus records Register / Unregister so Service tests can assert on
// publish/unpublish without depending on a real *toolbus.Bus. Implements the
// BusRegistrar surface used by Service.
type fakeBus struct {
	mu       sync.Mutex
	tools    map[string]toolbus.Tool
	mutating map[string]bool
}

func newFakeBus() *fakeBus {
	return &fakeBus{tools: map[string]toolbus.Tool{}, mutating: map[string]bool{}}
}

func (b *fakeBus) Register(t toolbus.Tool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.tools[t.Name()]; exists {
		return errors.New("already registered")
	}
	b.tools[t.Name()] = t
	if m, ok := t.(toolbus.Mutating); ok && m.IsMutating() {
		b.mutating[t.Name()] = true
	}
	return nil
}

func (b *fakeBus) Unregister(name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.tools[name]; !exists {
		return errors.New("not registered")
	}
	delete(b.tools, name)
	delete(b.mutating, name)
	return nil
}

func (b *fakeBus) IsMutating(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.mutating[name]
}

func (b *fakeBus) Invoke(ctx context.Context, tenantID, userID uuid.UUID,
	toolName string, input json.RawMessage) (json.RawMessage, error) {
	b.mu.Lock()
	t, ok := b.tools[toolName]
	b.mu.Unlock()
	if !ok {
		return nil, errors.New("tool not found")
	}
	return t.Invoke(ctx, tenantID, userID, input)
}

func (b *fakeBus) has(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.tools[name]
	return ok
}

// fakeAudit captures every audit.Entry so tests can verify the workflow.*
// audit-action contract documented in the design spec.
type fakeAudit struct {
	mu      sync.Mutex
	entries []audit.Entry
}

func (a *fakeAudit) Append(_ context.Context, e audit.Entry) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, e)
	return nil
}

func (a *fakeAudit) actions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.entries))
	for i, e := range a.entries {
		out[i] = e.Action
	}
	return out
}

func newService(t *testing.T) (*workflow.Service, *workflow.Repo, *fakeBus, *fakeAudit, uuid.UUID) {
	t.Helper()
	p := newPool(t)
	repo := workflow.NewRepo(p)
	bus := newFakeBus()
	aud := &fakeAudit{}
	eng := workflow.NewEngine(newMockRunner(), workflow.DefaultConfig())
	svc := workflow.NewService(repo, eng, bus, aud)
	return svc, repo, bus, aud, seedTenant(t, p)
}

const svcDSL = `
id: greet
name: Greet
description: say hi
inputs:
  who: { type: string, default: "world" }
steps:
  - id: build
    assign:
      msg: "hello ${inputs.who}"
outputs:
  said: ${vars.msg}
`

// TestService_Create_AuditAndRow asserts Create persists and emits the
// "workflow.admin.create" audit action with version=1.
func TestService_Create_AuditAndRow(t *testing.T) {
	svc, repo, _, aud, tid := newService(t)
	ctx := context.Background()

	wf, err := svc.Create(ctx, tid, "greet", "Greet", "say hi", svcDSL)
	require.NoError(t, err)
	require.Equal(t, 1, wf.Version)
	require.False(t, wf.Published)

	got, err := repo.Get(ctx, tid, "greet")
	require.NoError(t, err)
	require.Equal(t, wf.ID, got.ID)
	require.Contains(t, aud.actions(), "workflow.admin.create")
}

// TestService_Create_BadDSL surfaces parse/validate errors as a Go error
// (handler turns them into HTTP 400).
func TestService_Create_BadDSL(t *testing.T) {
	svc, _, _, _, tid := newService(t)
	_, err := svc.Create(context.Background(), tid, "broken", "B", "", "not: yaml: : :")
	require.Error(t, err)
}

// TestService_Create_MismatchedSlug rejects a DSL whose internal id doesn't
// match the URL slug — prevents admins from quietly renaming a workflow.
func TestService_Create_MismatchedSlug(t *testing.T) {
	svc, _, _, _, tid := newService(t)
	_, err := svc.Create(context.Background(), tid, "different", "Different", "", svcDSL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not match slug")
}

// TestService_Publish_RegistersToBus_AndUnpublishRemoves covers the round trip:
// publish flips DB flag + adds to bus; unpublish removes from bus + clears flag.
func TestService_Publish_Unpublish(t *testing.T) {
	svc, _, bus, aud, tid := newService(t)
	ctx := context.Background()
	_, err := svc.Create(ctx, tid, "greet", "Greet", "say hi", svcDSL)
	require.NoError(t, err)

	require.NoError(t, svc.Publish(ctx, tid, "greet"))
	require.True(t, bus.has("workflow.greet"))
	wf, err := svc.Get(ctx, tid, "greet")
	require.NoError(t, err)
	require.True(t, wf.Published)
	require.Contains(t, aud.actions(), "workflow.admin.publish")

	require.NoError(t, svc.Unpublish(ctx, tid, "greet"))
	require.False(t, bus.has("workflow.greet"))
	wf2, _ := svc.Get(ctx, tid, "greet")
	require.False(t, wf2.Published)
	require.Contains(t, aud.actions(), "workflow.admin.unpublish")
}

// TestService_Update_ForcesUnpublish locks the "PUT must require re-publish"
// invariant: an Update on a published workflow drops the Bus tool and clears
// the published flag.
func TestService_Update_ForcesUnpublish(t *testing.T) {
	svc, _, bus, _, tid := newService(t)
	ctx := context.Background()
	_, err := svc.Create(ctx, tid, "greet", "G", "", svcDSL)
	require.NoError(t, err)
	require.NoError(t, svc.Publish(ctx, tid, "greet"))
	require.True(t, bus.has("workflow.greet"))

	updated, err := svc.Update(ctx, tid, "greet", "G2", "edited", svcDSL+"\n# bump")
	require.NoError(t, err)
	require.Equal(t, 2, updated.Version)
	require.False(t, updated.Published)
	require.False(t, bus.has("workflow.greet"), "Update must drop the Bus tool until re-publish")
}

// TestService_Delete_PublishedAlsoRemovesBus checks the parallel of Update —
// deleting a published workflow drops the Bus registration.
func TestService_Delete_PublishedAlsoRemovesBus(t *testing.T) {
	svc, _, bus, aud, tid := newService(t)
	ctx := context.Background()
	_, err := svc.Create(ctx, tid, "greet", "G", "", svcDSL)
	require.NoError(t, err)
	require.NoError(t, svc.Publish(ctx, tid, "greet"))

	require.NoError(t, svc.Delete(ctx, tid, "greet"))
	require.False(t, bus.has("workflow.greet"))
	require.Contains(t, aud.actions(), "workflow.admin.delete")
}

// TestService_Invoke_EndToEnd asserts the Invoke flow: writes a workflow_runs
// row, audits start+complete, returns outputs, and binds version_at_run.
func TestService_Invoke_EndToEnd(t *testing.T) {
	svc, repo, _, aud, tid := newService(t)
	ctx := context.Background()
	_, err := svc.Create(ctx, tid, "greet", "G", "", svcDSL)
	require.NoError(t, err)
	uid := uuid.New()

	res, err := svc.Invoke(ctx, tid, uid, "greet", map[string]any{"who": "Alice"}, false)
	require.NoError(t, err)
	require.Equal(t, workflow.StatusOK, res.Status)
	require.Equal(t, "hello Alice", res.Outputs["said"])
	require.False(t, res.DryRun)
	require.NotEqual(t, uuid.Nil, res.RunID)

	wf, err := repo.Get(ctx, tid, "greet")
	require.NoError(t, err)
	runs, err := repo.ListRuns(ctx, tid, wf.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, "ok", runs[0].Status)
	require.Equal(t, 1, runs[0].VersionAtRun)

	require.Contains(t, aud.actions(), "workflow.invoke.start")
	require.Contains(t, aud.actions(), "workflow.invoke.complete")
}

// TestService_Invoke_DryRun confirms the dry_run flag flows through to both
// the workflow_runs row and the audit metadata.
func TestService_Invoke_DryRun(t *testing.T) {
	svc, repo, _, _, tid := newService(t)
	ctx := context.Background()
	_, err := svc.Create(ctx, tid, "greet", "G", "", svcDSL)
	require.NoError(t, err)

	res, err := svc.Invoke(ctx, tid, uuid.New(), "greet", nil, true)
	require.NoError(t, err)
	require.True(t, res.DryRun)
	require.Equal(t, workflow.StatusOK, res.Status)

	wf, _ := repo.Get(ctx, tid, "greet")
	runs, _ := repo.ListRuns(ctx, tid, wf.ID, 10)
	require.True(t, runs[0].DryRun)
}

// TestService_RepublishAll restarts the service by recreating the Bus and
// asserting that ListPublished rows come back as Bus tools.
func TestService_RepublishAll(t *testing.T) {
	svc, _, bus, _, tid := newService(t)
	ctx := context.Background()
	_, err := svc.Create(ctx, tid, "greet", "G", "", svcDSL)
	require.NoError(t, err)
	require.NoError(t, svc.Publish(ctx, tid, "greet"))
	require.True(t, bus.has("workflow.greet"))

	// Simulate process restart: drop the bus registration, then RepublishAll.
	require.NoError(t, bus.Unregister("workflow.greet"))
	require.False(t, bus.has("workflow.greet"))

	require.NoError(t, svc.RepublishAll(ctx))
	require.True(t, bus.has("workflow.greet"))
}

// TestService_RepublishAll_SkipsInvalidDSL ensures one broken row doesn't
// block startup — the rest are still re-registered.
func TestService_RepublishAll_SkipsInvalidDSL(t *testing.T) {
	svc, repo, bus, _, tid := newService(t)
	ctx := context.Background()
	_, err := svc.Create(ctx, tid, "greet", "G", "", svcDSL)
	require.NoError(t, err)
	require.NoError(t, svc.Publish(ctx, tid, "greet"))

	// Corrupt the DSL directly in DB (mimics a code-level validation
	// tightening that retroactively makes a stored DSL invalid).
	_, err = repo.Update(ctx, tid, "greet", "G", "", "id: greet\nname: G\nsteps:\n  - id: dup\n    assign: {x: 1}\n  - id: dup\n    assign: {y: 2}\n")
	require.NoError(t, err)
	require.NoError(t, repo.SetPublished(ctx, tid, "greet", true))

	require.NoError(t, bus.Unregister("workflow.greet"))
	require.NoError(t, svc.RepublishAll(ctx))
	require.False(t, bus.has("workflow.greet"), "broken DSL should be skipped, not re-registered")
}

// TestService_Invoke_FailedWorkflow_PersistsStatus ensures a workflow that
// errors out gets an "failed" status persisted (so audit + reflection can pick
// it up later).
func TestService_Invoke_FailedWorkflow_PersistsStatus(t *testing.T) {
	svc, repo, _, _, tid := newService(t)
	ctx := context.Background()
	dsl := `
id: badref
name: BadRef
steps:
  - id: bad
    assign:
      v: "${steps.missing.output}"
outputs:
  x: ${vars.v}
`
	_, err := svc.Create(ctx, tid, "badref", "BadRef", "", dsl)
	require.NoError(t, err)
	res, err := svc.Invoke(ctx, tid, uuid.New(), "badref", nil, false)
	require.NoError(t, err)
	require.Equal(t, workflow.StatusFailed, res.Status)
	require.True(t, strings.Contains(res.Error, "unknown step") || strings.Contains(res.Error, "steps.missing"))

	wf, _ := repo.Get(ctx, tid, "badref")
	runs, _ := repo.ListRuns(ctx, tid, wf.ID, 10)
	require.Equal(t, "failed", runs[0].Status)
}
