package reflection

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo persists memory_proposals rows.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo wires a pool. The pool must already have migrations applied.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Insert creates a proposal. Caller fills id + status + (optional) memory_id;
// created_at defaults to now() on the DB side when zero. Returns the row as
// stored (re-read so timestamps are authoritative).
func (r *Repo) Insert(ctx context.Context, p *MemoryProposal) (*MemoryProposal, error) {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	if p.Status == "" {
		p.Status = StatusPending
	}
	tags := p.Tags
	if tags == nil {
		tags = []string{}
	}
	_, err := r.pool.Exec(ctx, `
INSERT INTO memory_proposals
  (id, tenant_id, owner_user_id, session_id, type, content, tags,
   confidence, status, memory_id, decided_at, decided_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		p.ID, p.TenantID, p.OwnerUserID, p.SessionID, p.Type, p.Content, tags,
		p.Confidence, p.Status, p.MemoryID, p.DecidedAt, p.DecidedBy)
	if err != nil {
		return nil, fmt.Errorf("insert proposal: %w", err)
	}
	return r.Get(ctx, p.TenantID, p.ID)
}

// Get returns a proposal scoped to tenant. Cross-tenant reads return
// ErrProposalNotFound (no existence leak).
func (r *Repo) Get(ctx context.Context, tenantID, id uuid.UUID) (*MemoryProposal, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, tenant_id, owner_user_id, session_id, type, content, tags,
       confidence, status, memory_id, decided_at, decided_by, created_at
FROM memory_proposals WHERE id=$1 AND tenant_id=$2`, id, tenantID)
	return scanProposal(row)
}

// ListByTenant returns proposals ordered by created_at DESC.
func (r *Repo) ListByTenant(ctx context.Context, tenantID uuid.UUID, f ListFilter) ([]MemoryProposal, error) {
	if f.Status != "" && !IsValidStatus(f.Status) {
		return nil, ErrInvalidStatus
	}
	args := []any{tenantID}
	where := []string{"tenant_id=$1"}
	if f.Status != "" {
		args = append(args, f.Status)
		where = append(where, fmt.Sprintf("status=$%d", len(args)))
	}
	if f.OwnerUserID != nil {
		args = append(args, *f.OwnerUserID)
		where = append(where, fmt.Sprintf("owner_user_id=$%d", len(args)))
	}
	limit := f.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	if limit > MaxListLimit {
		limit = MaxListLimit
	}
	args = append(args, limit)
	limitArg := fmt.Sprintf("$%d", len(args))
	offsetClause := ""
	if f.Offset > 0 {
		args = append(args, f.Offset)
		offsetClause = fmt.Sprintf(" OFFSET $%d", len(args))
	}
	q := `
SELECT id, tenant_id, owner_user_id, session_id, type, content, tags,
       confidence, status, memory_id, decided_at, decided_by, created_at
FROM memory_proposals
WHERE ` + strings.Join(where, " AND ") + `
ORDER BY created_at DESC
LIMIT ` + limitArg + offsetClause

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list proposals: %w", err)
	}
	defer rows.Close()
	out := []MemoryProposal{}
	for rows.Next() {
		p, err := scanProposalRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// MarkDecided flips a pending proposal to the given target status. memoryID
// is recorded for approve / auto_approve and nil for reject. decidedBy may
// be nil for the auto_approve path. Returns ErrNotPending when the row is
// already decided, ErrProposalNotFound when nothing matched.
//
// `target` must be one of approved, auto_approved, rejected.
func (r *Repo) MarkDecided(ctx context.Context, tenantID, id uuid.UUID,
	target string, memoryID, decidedBy *uuid.UUID) (*MemoryProposal, error) {
	switch target {
	case StatusApproved, StatusAutoApproved, StatusRejected:
	default:
		return nil, fmt.Errorf("invalid target status %q", target)
	}
	tag, err := r.pool.Exec(ctx, `
UPDATE memory_proposals
SET status=$3, memory_id=$4, decided_by=$5, decided_at=now()
WHERE id=$1 AND tenant_id=$2 AND status='pending'`,
		id, tenantID, target, memoryID, decidedBy)
	if err != nil {
		return nil, fmt.Errorf("mark decided: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Either the row doesn't exist or it's no longer pending. Disambiguate
		// for the caller so admin handler can return 404 vs 409.
		_, gerr := r.Get(ctx, tenantID, id)
		if errors.Is(gerr, ErrProposalNotFound) {
			return nil, ErrProposalNotFound
		}
		return nil, ErrNotPending
	}
	return r.Get(ctx, tenantID, id)
}

func scanProposal(row pgx.Row) (*MemoryProposal, error) {
	var p MemoryProposal
	if err := row.Scan(&p.ID, &p.TenantID, &p.OwnerUserID, &p.SessionID,
		&p.Type, &p.Content, &p.Tags, &p.Confidence, &p.Status,
		&p.MemoryID, &p.DecidedAt, &p.DecidedBy, &p.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProposalNotFound
		}
		return nil, fmt.Errorf("scan proposal: %w", err)
	}
	return &p, nil
}

func scanProposalRows(rows pgx.Rows) (*MemoryProposal, error) {
	var p MemoryProposal
	if err := rows.Scan(&p.ID, &p.TenantID, &p.OwnerUserID, &p.SessionID,
		&p.Type, &p.Content, &p.Tags, &p.Confidence, &p.Status,
		&p.MemoryID, &p.DecidedAt, &p.DecidedBy, &p.CreatedAt); err != nil {
		return nil, fmt.Errorf("scan proposal row: %w", err)
	}
	return &p, nil
}
