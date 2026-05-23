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

// Proposal sentinel errors.
var (
	ErrProposalNotFound      = errors.New("workflow: proposal not found")
	ErrProposalInvalidState  = errors.New("workflow: proposal invalid state for operation")
	ErrProposalDryRunFailed  = errors.New("workflow: proposal dry-run not ok")
	ErrProposalSlugPublished = errors.New("workflow: slug already published")
)

// ProposalRepo persists workflow_proposals rows.
type ProposalRepo struct {
	pool *pgxpool.Pool
}

// NewProposalRepo wires a proposal repo against an existing pool.
func NewProposalRepo(pool *pgxpool.Pool) *ProposalRepo { return &ProposalRepo{pool: pool} }

// Insert creates a new proposal row in draft status.
func (r *ProposalRepo) Insert(ctx context.Context, p Proposal) (*Proposal, error) {
	slots := p.SlotsJSON
	if len(slots) == 0 {
		slots = []byte("{}")
	}
	if !json.Valid(slots) {
		return nil, fmt.Errorf("slots_json invalid")
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO workflow_proposals (
    tenant_id, session_id, created_by, slug, name, description, dsl_yaml,
    source, template_id, slots_json, dry_run_ok, dry_run_output_json, dry_run_error, status
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10,$11,$12,NULLIF($13,''),$14)
RETURNING id, tenant_id, session_id, created_by, slug, name, description, dsl_yaml,
          source, COALESCE(template_id,''), slots_json, dry_run_ok, dry_run_output_json,
          COALESCE(dry_run_error,''), status, published_at, decided_by, created_at, updated_at`,
		p.TenantID, p.SessionID, p.CreatedBy, p.Slug, p.Name, p.Description, p.DSLYAML,
		p.Source, p.TemplateID, slots, p.DryRunOK, p.DryRunOutputJSON, p.DryRunError, p.Status)
	return scanProposal(row)
}

// Get fetches a proposal scoped to tenant.
func (r *ProposalRepo) Get(ctx context.Context, tenantID, id uuid.UUID) (*Proposal, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, tenant_id, session_id, created_by, slug, name, description, dsl_yaml,
       source, COALESCE(template_id,''), slots_json, dry_run_ok, dry_run_output_json,
       COALESCE(dry_run_error,''), status, published_at, decided_by, created_at, updated_at
FROM workflow_proposals WHERE tenant_id=$1 AND id=$2`, tenantID, id)
	return scanProposal(row)
}

// UpdateDryRun stamps dry-run results on a proposal still in draft/pending.
func (r *ProposalRepo) UpdateDryRun(ctx context.Context, tenantID, id uuid.UUID,
	ok bool, outputs json.RawMessage, errText string) error {
	if len(outputs) > 0 && !json.Valid(outputs) {
		return fmt.Errorf("dry_run_output_json invalid")
	}
	ct, err := r.pool.Exec(ctx, `
UPDATE workflow_proposals
   SET dry_run_ok=$3, dry_run_output_json=$4, dry_run_error=NULLIF($5,''), updated_at=now()
 WHERE tenant_id=$1 AND id=$2 AND status IN ('draft','pending_approval')`,
		tenantID, id, ok, outputs, errText)
	if err != nil {
		return fmt.Errorf("update proposal dry_run: %w", err)
	}
	if strings.HasSuffix(ct.String(), " 0") {
		return ErrProposalNotFound
	}
	return nil
}

// List returns proposals for a tenant ordered by created_at desc.
func (r *ProposalRepo) List(ctx context.Context, tenantID uuid.UUID, f ProposalListFilter) ([]Proposal, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	status := f.Status
	if status != "" && !ValidProposalStatus(status) {
		return nil, fmt.Errorf("invalid status %q", status)
	}

	rows, err := r.pool.Query(ctx, `
SELECT id, tenant_id, session_id, created_by, slug, name, description, dsl_yaml,
       source, COALESCE(template_id,''), slots_json, dry_run_ok, dry_run_output_json,
       COALESCE(dry_run_error,''), status, published_at, decided_by, created_at, updated_at
FROM workflow_proposals
WHERE tenant_id=$1 AND ($2='' OR status=$2)
ORDER BY created_at DESC
LIMIT $3 OFFSET $4`, tenantID, status, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list proposals: %w", err)
	}
	defer rows.Close()

	var out []Proposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// SetStatus updates proposal status and optional decided_by / published_at.
func (r *ProposalRepo) SetStatus(ctx context.Context, tenantID, id uuid.UUID,
	status string, decidedBy *uuid.UUID) error {
	var publishedAt *time.Time
	if status == ProposalPublished {
		now := time.Now().UTC()
		publishedAt = &now
	}
	ct, err := r.pool.Exec(ctx, `
UPDATE workflow_proposals
   SET status=$3, decided_by=$4, published_at=$5, updated_at=now()
 WHERE tenant_id=$1 AND id=$2`,
		tenantID, id, status, decidedBy, publishedAt)
	if err != nil {
		return fmt.Errorf("update proposal status: %w", err)
	}
	if strings.HasSuffix(ct.String(), " 0") {
		return ErrProposalNotFound
	}
	return nil
}

type proposalScanner interface {
	Scan(dest ...any) error
}

func scanProposal(row proposalScanner) (*Proposal, error) {
	var p Proposal
	var templateID string
	var dryRunErr string
	err := row.Scan(
		&p.ID, &p.TenantID, &p.SessionID, &p.CreatedBy, &p.Slug, &p.Name, &p.Description, &p.DSLYAML,
		&p.Source, &templateID, &p.SlotsJSON, &p.DryRunOK, &p.DryRunOutputJSON,
		&dryRunErr, &p.Status, &p.PublishedAt, &p.DecidedBy, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProposalNotFound
		}
		return nil, fmt.Errorf("scan proposal: %w", err)
	}
	p.TemplateID = templateID
	p.DryRunError = dryRunErr
	return &p, nil
}
