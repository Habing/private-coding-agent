package workflow_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/workflow"
)

const triggersDSL = `
id: trig-flow
name: Triggers
triggers:
  - id: every-minute
    cron: "* * * * *"
    timezone: UTC
    inputs:
      channel: team
  - id: inbound
    webhook: {}
steps:
  - id: a
    wait: 1ms
outputs:
  ok: "true"
`

func TestTriggerRepo_SyncOnPublish(t *testing.T) {
	p := newPool(t)
	repo := workflow.NewRepo(p)
	triggers := workflow.NewTriggerRepo(repo)
	ctx := context.Background()
	tid := seedTenant(t, p)

	w, err := repo.Create(ctx, tid, "trig-flow", "Triggers", "", triggersDSL)
	require.NoError(t, err)

	doc, err := workflow.Parse(triggersDSL)
	require.NoError(t, err)
	require.NoError(t, workflow.Validate(doc, workflow.DefaultConfig()))

	require.NoError(t, triggers.SyncTriggersFromDoc(ctx, tid, w.ID, doc))

	rows, err := triggers.ListByWorkflow(ctx, tid, w.ID)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	byID := map[string]workflow.WorkflowTrigger{}
	for _, r := range rows {
		byID[r.TriggerID] = r
		require.True(t, r.Enabled)
	}
	require.Equal(t, workflow.TriggerKindCron, byID["every-minute"].Kind)
	require.NotNil(t, byID["every-minute"].NextRunAt)
	require.Equal(t, workflow.TriggerKindWebhook, byID["inbound"].Kind)
	require.NotEmpty(t, byID["inbound"].WebhookToken)

	token := byID["inbound"].WebhookToken
	require.NoError(t, triggers.DisableAllForWorkflow(ctx, tid, w.ID))
	rows, err = triggers.ListByWorkflow(ctx, tid, w.ID)
	require.NoError(t, err)
	for _, r := range rows {
		require.False(t, r.Enabled)
	}

	require.NoError(t, triggers.SyncTriggersFromDoc(ctx, tid, w.ID, doc))
	got, err := triggers.GetByWebhookToken(ctx, token)
	require.NoError(t, err)
	require.Equal(t, token, got.WebhookToken)
	require.True(t, got.Enabled)
}

func TestTriggerRepo_SyncRemovesStaleTrigger(t *testing.T) {
	p := newPool(t)
	repo := workflow.NewRepo(p)
	triggers := workflow.NewTriggerRepo(repo)
	ctx := context.Background()
	tid := seedTenant(t, p)

	w, err := repo.Create(ctx, tid, "trig-flow", "Triggers", "", triggersDSL)
	require.NoError(t, err)

	doc, err := workflow.Parse(triggersDSL)
	require.NoError(t, err)
	require.NoError(t, triggers.SyncTriggersFromDoc(ctx, tid, w.ID, doc))

	doc.Triggers = doc.Triggers[:1]
	require.NoError(t, triggers.SyncTriggersFromDoc(ctx, tid, w.ID, doc))

	rows, err := triggers.ListByWorkflow(ctx, tid, w.ID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, r := range rows {
		if r.TriggerID == "inbound" {
			require.False(t, r.Enabled)
		} else {
			require.True(t, r.Enabled)
		}
	}
}
