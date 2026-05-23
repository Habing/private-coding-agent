package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

// SyncTriggersFromDoc upserts DSL triggers for a published workflow and disables
// rows removed from the doc.
func (r *TriggerRepo) SyncTriggersFromDoc(ctx context.Context, tenantID, workflowID uuid.UUID, doc *WorkflowDoc) error {
	if doc == nil {
		return fmt.Errorf("nil doc")
	}
	existing, err := r.ListByWorkflow(ctx, tenantID, workflowID)
	if err != nil {
		return err
	}
	byID := map[string]*WorkflowTrigger{}
	for i := range existing {
		byID[existing[i].TriggerID] = &existing[i]
	}

	now := time.Now().UTC()
	want := map[string]bool{}
	for _, spec := range doc.Triggers {
		want[spec.ID] = true
		if err := r.upsertTrigger(ctx, tenantID, workflowID, spec, byID, now); err != nil {
			return err
		}
	}

	for _, prev := range existing {
		if want[prev.TriggerID] {
			continue
		}
		if _, err := r.pool.Exec(ctx, `
UPDATE workflow_triggers SET enabled=false, updated_at=now()
 WHERE tenant_id=$1 AND workflow_id=$2 AND trigger_id=$3`,
			tenantID, workflowID, prev.TriggerID); err != nil {
			return fmt.Errorf("disable removed trigger %s: %w", prev.TriggerID, err)
		}
	}
	return nil
}

// nextCronRun returns the next fire time after from, interpreted in timezone.
func nextCronRun(expr, timezone string, from time.Time) (*time.Time, error) {
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return nil, fmt.Errorf("parse cron: %w", err)
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("load timezone: %w", err)
	}
	next := sched.Next(from.In(loc)).UTC()
	return &next, nil
}
