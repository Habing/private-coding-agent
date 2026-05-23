package workflow_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/workflow"
)

func seedUser(t *testing.T, p *pgxpool.Pool, tenantID uuid.UUID, role string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := p.Exec(context.Background(), `
INSERT INTO users (id, tenant_id, email, password_hash, role)
VALUES ($1,$2,$3,'hash',$4)`,
		id, tenantID, id.String()+"@example.com", role)
	require.NoError(t, err)
	return id
}

func newProposalService(t *testing.T) (*workflow.ProposalService, *workflow.Service, *fakeBus, *fakeAudit, uuid.UUID, uuid.UUID, *pgxpool.Pool) {
	t.Helper()
	p := newPool(t)
	repo := workflow.NewRepo(p)
	bus := newFakeBus()
	aud := &fakeAudit{}
	eng := workflow.NewEngine(newMockRunner(), workflow.DefaultConfig())
	svc := workflow.NewService(repo, eng, bus, aud)
	pRepo := workflow.NewProposalRepo(p)
	psvc := workflow.NewProposalService(pRepo, svc, aud)
	tid := seedTenant(t, p)
	uid := seedUser(t, p, tid, "admin")
	return psvc, svc, bus, aud, tid, uid, p
}

func TestProposalService_CreateFromTemplate_DryRun(t *testing.T) {
	psvc, _, _, aud, tid, uid, _ := newProposalService(t)
	ctx := context.Background()

	prop, err := psvc.CreateFromTemplate(ctx, tid, uid,
		"tool-chain", "nl-chain", "NL Chain", "from template",
		map[string]any{
			"steps": []map[string]any{
				{"id": "echo", "use": "llm.chat", "args": map[string]any{
					"model": "default-mock:text",
					"messages": []map[string]string{{"role": "user", "content": "ping"}},
				}},
			},
		}, nil)
	require.NoError(t, err)
	require.Equal(t, workflow.ProposalDraft, prop.Status)
	require.True(t, prop.DryRunOK, "dry_run_error=%q", prop.DryRunError)
	require.Equal(t, "template:tool-chain", prop.Source)
	require.Contains(t, aud.actions(), "workflow.proposal.create")
}

func TestProposalService_ConfirmAdmin_Publishes(t *testing.T) {
	psvc, _, bus, _, tid, uid, _ := newProposalService(t)
	ctx := context.Background()

	prop, err := psvc.Create(ctx, tid, uid, workflow.CreateProposalInput{
		Slug: "greet", Name: "NL Greet", Description: "test", DSLYAML: svcDSL,
	})
	require.NoError(t, err)
	require.True(t, prop.DryRunOK)

	got, err := psvc.Confirm(ctx, tid, uid, prop.ID, true)
	require.NoError(t, err)
	require.Equal(t, workflow.ProposalPublished, got.Status)
	require.True(t, bus.has("workflow.greet"))
}

func TestProposalService_ConfirmMember_PendingApproval(t *testing.T) {
	psvc, _, bus, _, tid, adminID, p := newProposalService(t)
	memberID := seedUser(t, p, tid, "member")
	ctx := context.Background()

	prop, err := psvc.Create(ctx, tid, adminID, workflow.CreateProposalInput{
		Slug: "pending-wf", Name: "Pending", DSLYAML: replaceDSLID(svcDSL, "pending-wf"),
	})
	require.NoError(t, err)

	got, err := psvc.Confirm(ctx, tid, memberID, prop.ID, false)
	require.NoError(t, err)
	require.Equal(t, workflow.ProposalPendingApproval, got.Status)

	approved, err := psvc.Approve(ctx, tid, adminID, prop.ID)
	require.NoError(t, err)
	require.Equal(t, workflow.ProposalPublished, approved.Status)
	require.True(t, bus.has("workflow.pending-wf"))
}

func TestProposalService_Reject(t *testing.T) {
	psvc, _, _, _, tid, uid, _ := newProposalService(t)
	ctx := context.Background()

	prop, err := psvc.Create(ctx, tid, uid, workflow.CreateProposalInput{
		Slug: "reject-me", Name: "Reject", DSLYAML: replaceDSLID(svcDSL, "reject-me"),
	})
	require.NoError(t, err)

	require.NoError(t, psvc.Reject(ctx, tid, uid, prop.ID))
	got, err := psvc.Get(ctx, tid, prop.ID)
	require.NoError(t, err)
	require.Equal(t, workflow.ProposalRejected, got.Status)
}

func TestProposalService_Create_PublishedSlugBlocked(t *testing.T) {
	psvc, svc, _, _, tid, uid, _ := newProposalService(t)
	ctx := context.Background()

	_, err := svc.Create(ctx, tid, "live-slug", "Live", "", replaceDSLID(svcDSL, "live-slug"))
	require.NoError(t, err)
	require.NoError(t, svc.Publish(ctx, tid, "live-slug"))

	_, err = psvc.Create(ctx, tid, uid, workflow.CreateProposalInput{
		Slug: "live-slug", Name: "Live", DSLYAML: replaceDSLID(svcDSL, "live-slug"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, workflow.ErrProposalSlugPublished)
}

func replaceDSLID(dsl, slug string) string {
	return strings.Replace(dsl, "id: greet", "id: "+slug, 1)
}
