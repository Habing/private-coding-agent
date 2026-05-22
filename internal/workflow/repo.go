package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sentinel errors. Handler maps these to HTTP statuses (404 / 409).
var (
	ErrNotFound  = errors.New("workflow: not found")
	ErrSlugTaken = errors.New("workflow: slug already used in tenant")
)

// Workflow is the persisted row. dsl_yaml is the canonical source of truth;
// version is monotonic per (tenant_id, slug) — no history table in v1.
type Workflow struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	Slug        string     `json:"slug"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	DSLYAML     string     `json:"dsl_yaml,omitempty"`
	Version     int        `json:"version"`
	Published   bool       `json:"published"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Run is one execution log row. inputs_json is what the caller passed;
// outputs_json is the workflow `outputs:` map after expression resolution.
type Run struct {
	ID           uuid.UUID  `json:"id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	UserID       uuid.UUID  `json:"user_id"`
	WorkflowID   uuid.UUID  `json:"workflow_id"`
	VersionAtRun int        `json:"version_at_run"`
	DryRun       bool       `json:"dry_run"`
	Status       string     `json:"status"`
	Inputs       []byte     `json:"inputs_json,omitempty"`
	Outputs      []byte     `json:"outputs_json,omitempty"`
	ErrorText    string     `json:"error_text,omitempty"`
	DurationMS   int        `json:"duration_ms"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

// Repo persists Workflow + Run rows. Single-tenant safety is enforced by
// every method taking a tenantID and filtering on it; callers must pass the
// tenant from the request claims.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo wires a fresh repo against an existing pool.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// Create inserts a new workflow row with version=1, published=false.
// Returns ErrSlugTaken on UNIQUE violation.
func (r *Repo) Create(ctx context.Context, tenantID uuid.UUID, slug, name, descr, dsl string) (*Workflow, error) {
	row := r.pool.QueryRow(ctx, `
INSERT INTO workflows (tenant_id, slug, name, description, dsl_yaml, version, published)
VALUES ($1,$2,$3,$4,$5,1,false)
RETURNING id, tenant_id, slug, name, description, dsl_yaml, version, published, published_at, created_at, updated_at`,
		tenantID, slug, name, descr, dsl)
	w, err := scanWorkflow(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: %s", ErrSlugTaken, slug)
		}
		return nil, fmt.Errorf("insert workflow: %w", err)
	}
	return w, nil
}

// Get fetches a workflow by (tenant, slug). ErrNotFound on miss.
func (r *Repo) Get(ctx context.Context, tenantID uuid.UUID, slug string) (*Workflow, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, tenant_id, slug, name, description, dsl_yaml, version, published, published_at, created_at, updated_at
FROM workflows WHERE tenant_id=$1 AND slug=$2`, tenantID, slug)
	return scanWorkflow(row)
}

// List returns the tenant's workflows ordered by slug. dsl_yaml is omitted
// from the rows (the handler can decide via ?include=dsl whether to follow
// up with a Get for the full text).
func (r *Repo) List(ctx context.Context, tenantID uuid.UUID) ([]Workflow, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, tenant_id, slug, name, description, '' AS dsl_yaml, version, published, published_at, created_at, updated_at
FROM workflows WHERE tenant_id=$1 ORDER BY slug`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}
	defer rows.Close()
	out := []Workflow{}
	for rows.Next() {
		w, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

// Update replaces name/description/dsl_yaml, bumps version, and forces
// published=false so the caller must re-publish to re-register into the Bus.
func (r *Repo) Update(ctx context.Context, tenantID uuid.UUID, slug, name, descr, dsl string) (*Workflow, error) {
	row := r.pool.QueryRow(ctx, `
UPDATE workflows
   SET name=$3, description=$4, dsl_yaml=$5,
       version=version+1, published=false, published_at=NULL,
       updated_at=now()
 WHERE tenant_id=$1 AND slug=$2
 RETURNING id, tenant_id, slug, name, description, dsl_yaml, version, published, published_at, created_at, updated_at`,
		tenantID, slug, name, descr, dsl)
	return scanWorkflow(row)
}

// SetPublished flips the published flag (and stamps published_at on true).
// Returns ErrNotFound when no row matches.
func (r *Repo) SetPublished(ctx context.Context, tenantID uuid.UUID, slug string, published bool) error {
	var tag string
	if published {
		ct, err := r.pool.Exec(ctx, `
UPDATE workflows SET published=true, published_at=now(), updated_at=now()
 WHERE tenant_id=$1 AND slug=$2`, tenantID, slug)
		if err != nil {
			return fmt.Errorf("publish workflow: %w", err)
		}
		tag = ct.String()
	} else {
		ct, err := r.pool.Exec(ctx, `
UPDATE workflows SET published=false, published_at=NULL, updated_at=now()
 WHERE tenant_id=$1 AND slug=$2`, tenantID, slug)
		if err != nil {
			return fmt.Errorf("unpublish workflow: %w", err)
		}
		tag = ct.String()
	}
	if strings.HasSuffix(tag, " 0") {
		return ErrNotFound
	}
	return nil
}

// Delete removes a workflow row (cascade deletes workflow_runs). Returns
// ErrNotFound if no row matched.
func (r *Repo) Delete(ctx context.Context, tenantID uuid.UUID, slug string) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM workflows WHERE tenant_id=$1 AND slug=$2`, tenantID, slug)
	if err != nil {
		return fmt.Errorf("delete workflow: %w", err)
	}
	if strings.HasSuffix(ct.String(), " 0") {
		return ErrNotFound
	}
	return nil
}

// ListPublished returns ALL tenants' published workflows. Used at server
// boot by Service.RepublishAll to put each one back into the Tool Bus.
func (r *Repo) ListPublished(ctx context.Context) ([]Workflow, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, tenant_id, slug, name, description, dsl_yaml, version, published, published_at, created_at, updated_at
FROM workflows WHERE published=true ORDER BY tenant_id, slug`)
	if err != nil {
		return nil, fmt.Errorf("list published: %w", err)
	}
	defer rows.Close()
	out := []Workflow{}
	for rows.Next() {
		w, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

// CreateRun inserts a pending workflow_runs row and returns its id. Status
// is whatever the caller passes (typically "running"); Service updates it
// later via FinishRun.
func (r *Repo) CreateRun(ctx context.Context, run Run) (uuid.UUID, error) {
	inputs := run.Inputs
	if len(inputs) == 0 {
		inputs = []byte("{}")
	}
	if !json.Valid(inputs) {
		return uuid.Nil, fmt.Errorf("inputs_json invalid")
	}
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
INSERT INTO workflow_runs (tenant_id, user_id, workflow_id, version_at_run, dry_run, status, inputs_json)
VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		run.TenantID, run.UserID, run.WorkflowID, run.VersionAtRun, run.DryRun, run.Status, inputs).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert run: %w", err)
	}
	return id, nil
}

// FinishRun stamps a terminal status, outputs, error and duration on an
// existing run row. The two-step write makes audit and trace easier to
// correlate (run row exists before the first audit `start` is written).
func (r *Repo) FinishRun(ctx context.Context, id uuid.UUID, status string, outputs []byte, errText string, durationMS int) error {
	if len(outputs) > 0 && !json.Valid(outputs) {
		return fmt.Errorf("outputs_json invalid")
	}
	_, err := r.pool.Exec(ctx, `
UPDATE workflow_runs
   SET status=$2, outputs_json=$3, error_text=$4, duration_ms=$5, finished_at=now()
 WHERE id=$1`, id, status, outputs, errText, durationMS)
	if err != nil {
		return fmt.Errorf("update run: %w", err)
	}
	return nil
}

// ListRuns returns the most-recent N runs of a workflow (descending by start
// time). Caller must already know workflow_id; admin handler looks it up by
// slug first.
func (r *Repo) ListRuns(ctx context.Context, tenantID, workflowID uuid.UUID, limit int) ([]Run, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
SELECT id, tenant_id, user_id, workflow_id, version_at_run, dry_run, status,
       inputs_json, outputs_json, COALESCE(error_text,''), COALESCE(duration_ms,0),
       started_at, finished_at
FROM workflow_runs WHERE tenant_id=$1 AND workflow_id=$2
ORDER BY started_at DESC LIMIT $3`, tenantID, workflowID, limit)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var run Run
		if err := rows.Scan(&run.ID, &run.TenantID, &run.UserID, &run.WorkflowID,
			&run.VersionAtRun, &run.DryRun, &run.Status,
			&run.Inputs, &run.Outputs, &run.ErrorText, &run.DurationMS,
			&run.StartedAt, &run.FinishedAt); err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanWorkflow(row rowScanner) (*Workflow, error) {
	var w Workflow
	err := row.Scan(&w.ID, &w.TenantID, &w.Slug, &w.Name, &w.Description, &w.DSLYAML,
		&w.Version, &w.Published, &w.PublishedAt, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan workflow: %w", err)
	}
	return &w, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") || strings.Contains(msg, "duplicate key value")
}
