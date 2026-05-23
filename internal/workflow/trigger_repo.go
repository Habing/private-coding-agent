package workflow

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TriggerRepo persists workflow_triggers rows synced from DSL on publish.
type TriggerRepo struct {
	pool *pgxpool.Pool
}

// NewTriggerRepo wires a trigger repo against the same pool as Repo.
func NewTriggerRepo(repo *Repo) *TriggerRepo {
	return &TriggerRepo{pool: repo.pool}
}

// ListByWorkflow returns triggers for a workflow ordered by trigger_id.
func (r *TriggerRepo) ListByWorkflow(ctx context.Context, tenantID, workflowID uuid.UUID) ([]WorkflowTrigger, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, tenant_id, workflow_id, trigger_id, kind, COALESCE(cron_expr,''), timezone,
       COALESCE(webhook_token,''), default_inputs, enabled, next_run_at, last_run_at,
       COALESCE(last_status,''), COALESCE(last_error,''), created_at, updated_at
FROM workflow_triggers
WHERE tenant_id=$1 AND workflow_id=$2
ORDER BY trigger_id`, tenantID, workflowID)
	if err != nil {
		return nil, fmt.Errorf("list triggers: %w", err)
	}
	defer rows.Close()
	out := []WorkflowTrigger{}
	for rows.Next() {
		tr, err := scanTrigger(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *tr)
	}
	return out, rows.Err()
}

// DisableAllForWorkflow sets enabled=false for every trigger on a workflow.
func (r *TriggerRepo) DisableAllForWorkflow(ctx context.Context, tenantID, workflowID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
UPDATE workflow_triggers
   SET enabled=false, updated_at=now()
 WHERE tenant_id=$1 AND workflow_id=$2`, tenantID, workflowID)
	if err != nil {
		return fmt.Errorf("disable triggers: %w", err)
	}
	return nil
}

// GetByWebhookToken loads a trigger row by its public webhook token.
func (r *TriggerRepo) GetByWebhookToken(ctx context.Context, token string) (*WorkflowTrigger, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, tenant_id, workflow_id, trigger_id, kind, COALESCE(cron_expr,''), timezone,
       COALESCE(webhook_token,''), default_inputs, enabled, next_run_at, last_run_at,
       COALESCE(last_status,''), COALESCE(last_error,''), created_at, updated_at
FROM workflow_triggers
WHERE webhook_token=$1`, token)
	return scanTrigger(row)
}

type triggerScanner interface {
	Scan(dest ...any) error
}

func scanTrigger(row triggerScanner) (*WorkflowTrigger, error) {
	var tr WorkflowTrigger
	var kind string
	err := row.Scan(&tr.ID, &tr.TenantID, &tr.WorkflowID, &tr.TriggerID, &kind,
		&tr.CronExpr, &tr.Timezone, &tr.WebhookToken, &tr.DefaultInputs, &tr.Enabled,
		&tr.NextRunAt, &tr.LastRunAt, &tr.LastStatus, &tr.LastError,
		&tr.CreatedAt, &tr.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan trigger: %w", err)
	}
	tr.Kind = TriggerKind(kind)
	return &tr, nil
}

func generateWebhookToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func marshalDefaultInputs(inputs map[string]any) ([]byte, error) {
	if inputs == nil {
		return []byte("{}"), nil
	}
	raw, err := json.Marshal(inputs)
	if err != nil {
		return nil, fmt.Errorf("marshal default_inputs: %w", err)
	}
	return raw, nil
}

func (r *TriggerRepo) upsertTrigger(ctx context.Context, tenantID, workflowID uuid.UUID,
	spec TriggerSpec, existing map[string]*WorkflowTrigger, now time.Time) error {

	inputsJSON, err := marshalDefaultInputs(spec.Inputs)
	if err != nil {
		return err
	}

	var kind TriggerKind
	var cronExpr, webhookToken string
	var nextRun *time.Time

	if spec.Cron != "" {
		kind = TriggerKindCron
		cronExpr = spec.Cron
		tz := spec.Timezone
		if tz == "" {
			tz = "UTC"
		}
		nextRun, err = nextCronRun(cronExpr, tz, now)
		if err != nil {
			return fmt.Errorf("trigger %s: %w", spec.ID, err)
		}
	} else {
		kind = TriggerKindWebhook
		if prev, ok := existing[spec.ID]; ok && prev.WebhookToken != "" {
			webhookToken = prev.WebhookToken
		} else {
			webhookToken, err = generateWebhookToken()
			if err != nil {
				return err
			}
		}
	}

	_, err = r.pool.Exec(ctx, `
INSERT INTO workflow_triggers (
    tenant_id, workflow_id, trigger_id, kind, cron_expr, timezone,
    webhook_token, default_inputs, enabled, next_run_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,true,$9,now())
ON CONFLICT (workflow_id, trigger_id) DO UPDATE SET
    kind=EXCLUDED.kind,
    cron_expr=EXCLUDED.cron_expr,
    timezone=EXCLUDED.timezone,
    webhook_token=COALESCE(NULLIF(workflow_triggers.webhook_token,''), EXCLUDED.webhook_token),
    default_inputs=EXCLUDED.default_inputs,
    enabled=true,
    next_run_at=EXCLUDED.next_run_at,
    updated_at=now()`,
		tenantID, workflowID, spec.ID, string(kind), nullIfEmpty(cronExpr), nullIfTimezone(spec),
		webhookToken, inputsJSON, nextRun)
	if err != nil {
		return fmt.Errorf("upsert trigger %s: %w", spec.ID, err)
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullIfTimezone(spec TriggerSpec) string {
	if spec.Timezone != "" {
		return spec.Timezone
	}
	return "UTC"
}
