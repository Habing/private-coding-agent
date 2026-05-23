package workflow

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

// CronTriggerClaim is a due cron row reserved for one scheduler tick.
type CronTriggerClaim struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	WorkflowID    uuid.UUID
	WorkflowSlug  string
	TriggerID     string
	CronExpr      string
	Timezone      string
	DefaultInputs []byte
}

// WebhookTriggerContext joins a webhook trigger with its parent workflow.
type WebhookTriggerContext struct {
	Trigger   WorkflowTrigger
	Slug      string
	Published bool
}

// ClaimDueCron locks due cron triggers, advances next_run_at from now(), and
// returns claims. Missed schedules fire at most once per tick (no backlog).
func (r *TriggerRepo) ClaimDueCron(ctx context.Context, limit int) ([]CronTriggerClaim, error) {
	if limit <= 0 || limit > 64 {
		limit = 32
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim tx: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
SELECT t.id, t.tenant_id, t.workflow_id, w.slug, t.trigger_id,
       COALESCE(t.cron_expr,''), t.timezone, t.default_inputs
FROM workflow_triggers t
JOIN workflows w ON w.id = t.workflow_id
WHERE t.kind = 'cron' AND t.enabled AND w.published
  AND t.next_run_at IS NOT NULL AND t.next_run_at <= now()
ORDER BY t.next_run_at ASC
LIMIT $1
FOR UPDATE OF t SKIP LOCKED`, limit)
	if err != nil {
		return nil, fmt.Errorf("select due cron: %w", err)
	}
	defer rows.Close()

	now := time.Now().UTC()
	claims := []CronTriggerClaim{}
	for rows.Next() {
		var c CronTriggerClaim
		if err := rows.Scan(&c.ID, &c.TenantID, &c.WorkflowID, &c.WorkflowSlug, &c.TriggerID,
			&c.CronExpr, &c.Timezone, &c.DefaultInputs); err != nil {
			return nil, fmt.Errorf("scan due cron: %w", err)
		}
		claims = append(claims, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	valid := make([]CronTriggerClaim, 0, len(claims))
	for _, c := range claims {
		next, err := nextCronRun(c.CronExpr, c.Timezone, now)
		if err != nil {
			slog.Warn("workflow: skip due cron claim", "trigger_id", c.TriggerID, "err", err)
			if _, uerr := tx.Exec(ctx, `
UPDATE workflow_triggers SET last_error=$2, last_status='failed', updated_at=now()
 WHERE id=$1`, c.ID, truncateErr(err)); uerr != nil {
				return nil, uerr
			}
			continue
		}
		if _, err := tx.Exec(ctx, `
UPDATE workflow_triggers SET next_run_at=$2, updated_at=now() WHERE id=$1`,
			c.ID, next); err != nil {
			return nil, fmt.Errorf("advance next_run_at: %w", err)
		}
		valid = append(valid, c)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim tx: %w", err)
	}
	return valid, nil
}

func truncateErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 2000 {
		return s[:2000]
	}
	return s
}

// RecordCronRun stamps last_run_at / status after invoke completes.
func (r *TriggerRepo) RecordCronRun(ctx context.Context, id uuid.UUID, status, errText string) error {
	_, err := r.pool.Exec(ctx, `
UPDATE workflow_triggers
   SET last_run_at=now(), last_status=$2, last_error=$3, updated_at=now()
 WHERE id=$1`, id, status, nullIfEmptyString(errText))
	if err != nil {
		return fmt.Errorf("record cron run: %w", err)
	}
	return nil
}

func nullIfEmptyString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// GetByWorkflowTrigger loads one trigger row for a workflow.
func (r *TriggerRepo) GetByWorkflowTrigger(ctx context.Context, tenantID, workflowID uuid.UUID, triggerID string) (*WorkflowTrigger, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, tenant_id, workflow_id, trigger_id, kind, COALESCE(cron_expr,''), timezone,
       COALESCE(webhook_token,''), default_inputs, enabled, next_run_at, last_run_at,
       COALESCE(last_status,''), COALESCE(last_error,''), created_at, updated_at
FROM workflow_triggers
WHERE tenant_id=$1 AND workflow_id=$2 AND trigger_id=$3`,
		tenantID, workflowID, triggerID)
	return scanTrigger(row)
}

// GetWebhookTrigger resolves a public webhook token to trigger + workflow.
func (r *TriggerRepo) GetWebhookTrigger(ctx context.Context, token string) (*WebhookTriggerContext, error) {
	row := r.pool.QueryRow(ctx, `
SELECT t.id, t.tenant_id, t.workflow_id, t.trigger_id, t.kind, COALESCE(t.cron_expr,''), t.timezone,
       COALESCE(t.webhook_token,''), t.default_inputs, t.enabled, t.next_run_at, t.last_run_at,
       COALESCE(t.last_status,''), COALESCE(t.last_error,''), t.created_at, t.updated_at,
       w.slug, w.published
FROM workflow_triggers t
JOIN workflows w ON w.id = t.workflow_id
WHERE t.webhook_token=$1 AND t.kind = 'webhook'`, token)
	var tr WorkflowTrigger
	var kind, slug string
	var published bool
	err := row.Scan(&tr.ID, &tr.TenantID, &tr.WorkflowID, &tr.TriggerID, &kind,
		&tr.CronExpr, &tr.Timezone, &tr.WebhookToken, &tr.DefaultInputs, &tr.Enabled,
		&tr.NextRunAt, &tr.LastRunAt, &tr.LastStatus, &tr.LastError,
		&tr.CreatedAt, &tr.UpdatedAt, &slug, &published)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan webhook trigger: %w", err)
	}
	tr.Kind = TriggerKind(kind)
	return &WebhookTriggerContext{Trigger: tr, Slug: slug, Published: published}, nil
}

// RotateWebhookToken replaces the webhook token for one trigger row.
func (r *TriggerRepo) RotateWebhookToken(ctx context.Context, tenantID, workflowID uuid.UUID, triggerID string) (string, error) {
	token, err := generateWebhookToken()
	if err != nil {
		return "", err
	}
	tag, err := r.pool.Exec(ctx, `
UPDATE workflow_triggers
   SET webhook_token=$4, updated_at=now()
 WHERE tenant_id=$1 AND workflow_id=$2 AND trigger_id=$3 AND kind='webhook'`,
		tenantID, workflowID, triggerID, token)
	if err != nil {
		return "", fmt.Errorf("rotate webhook token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	return token, nil
}

