package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo persists memory entries.
type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Insert creates a row. Caller stamps the UUID and timestamps (or relies on
// DB defaults). Returns a re-read row so timestamps are authoritative.
func (r *Repo) Insert(ctx context.Context, m *Memory) (*Memory, error) {
	tags := m.Tags
	if tags == nil {
		tags = []string{}
	}
	_, err := r.pool.Exec(ctx, `
INSERT INTO memories (id, tenant_id, owner_user_id, type, content, tags, source, source_msg_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		m.ID, m.TenantID, m.OwnerUserID, m.Type, m.Content, tags, m.Source, m.SourceMsgID)
	if err != nil {
		return nil, fmt.Errorf("insert memory: %w", err)
	}
	return r.Get(ctx, m.TenantID, m.OwnerUserID, m.ID)
}

// Get returns a row scoped to tenant + owner. Cross-tenant or cross-owner
// reads return ErrMemoryNotFound (no existence leak).
func (r *Repo) Get(ctx context.Context, tenantID, ownerUserID, id uuid.UUID) (*Memory, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, tenant_id, owner_user_id, type, content, tags, source, source_msg_id,
       last_used_at, created_at, updated_at
FROM memories
WHERE id=$1 AND tenant_id=$2 AND owner_user_id=$3`, id, tenantID, ownerUserID)
	return scanMemory(row)
}

// List returns memories matching the filter, newest first.
func (r *Repo) List(ctx context.Context, tenantID, ownerUserID uuid.UUID, f ListFilter) ([]Memory, error) {
	args := []any{tenantID, ownerUserID}
	where := []string{"tenant_id=$1", "owner_user_id=$2"}
	if f.Type != "" {
		args = append(args, f.Type)
		where = append(where, fmt.Sprintf("type=$%d", len(args)))
	}
	if len(f.Tags) > 0 {
		args = append(args, f.Tags)
		where = append(where, fmt.Sprintf("tags && $%d", len(args)))
	}
	if f.Query != "" {
		args = append(args, "%"+f.Query+"%")
		where = append(where, fmt.Sprintf("content ILIKE $%d", len(args)))
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
SELECT id, tenant_id, owner_user_id, type, content, tags, source, source_msg_id,
       last_used_at, created_at, updated_at
FROM memories
WHERE ` + strings.Join(where, " AND ") + `
ORDER BY created_at DESC
LIMIT ` + limitArg + offsetClause

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query memories: %w", err)
	}
	defer rows.Close()
	out := []Memory{}
	for rows.Next() {
		m, err := scanMemoryRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// Update applies a partial update. Returns ErrMemoryNotFound when nothing
// matched the (tenant, owner, id) triple.
func (r *Repo) Update(ctx context.Context, tenantID, ownerUserID, id uuid.UUID, req UpdateRequest) (*Memory, error) {
	sets := []string{"updated_at=now()"}
	args := []any{}
	if req.Type != nil {
		args = append(args, *req.Type)
		sets = append(sets, fmt.Sprintf("type=$%d", len(args)))
	}
	if req.Content != nil {
		args = append(args, *req.Content)
		sets = append(sets, fmt.Sprintf("content=$%d", len(args)))
	}
	if req.TagsSet {
		tags := req.Tags
		if tags == nil {
			tags = []string{}
		}
		args = append(args, tags)
		sets = append(sets, fmt.Sprintf("tags=$%d", len(args)))
	}
	args = append(args, id, tenantID, ownerUserID)
	q := `UPDATE memories SET ` + strings.Join(sets, ", ") +
		fmt.Sprintf(" WHERE id=$%d AND tenant_id=$%d AND owner_user_id=$%d", len(args)-2, len(args)-1, len(args))
	tag, err := r.pool.Exec(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("update memory: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrMemoryNotFound
	}
	return r.Get(ctx, tenantID, ownerUserID, id)
}

// Delete removes a memory. Returns ErrMemoryNotFound when nothing matched.
func (r *Repo) Delete(ctx context.Context, tenantID, ownerUserID, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
DELETE FROM memories WHERE id=$1 AND tenant_id=$2 AND owner_user_id=$3`,
		id, tenantID, ownerUserID)
	if err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrMemoryNotFound
	}
	return nil
}

// Search runs the keyword + tag + type filter and touches last_used_at on hits.
// Returns ordered by created_at DESC (consistent with List).
func (r *Repo) Search(ctx context.Context, tenantID, ownerUserID uuid.UUID, req SearchRequest) ([]Memory, error) {
	args := []any{tenantID, ownerUserID}
	where := []string{"tenant_id=$1", "owner_user_id=$2"}
	if req.Query != "" {
		args = append(args, "%"+req.Query+"%")
		where = append(where, fmt.Sprintf("content ILIKE $%d", len(args)))
	}
	if req.Type != "" {
		args = append(args, req.Type)
		where = append(where, fmt.Sprintf("type=$%d", len(args)))
	}
	if len(req.Tags) > 0 {
		args = append(args, req.Tags)
		where = append(where, fmt.Sprintf("tags && $%d", len(args)))
	}
	limit := req.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	if limit > MaxListLimit {
		limit = MaxListLimit
	}
	args = append(args, limit)

	q := `
SELECT id, tenant_id, owner_user_id, type, content, tags, source, source_msg_id,
       last_used_at, created_at, updated_at
FROM memories
WHERE ` + strings.Join(where, " AND ") + `
ORDER BY created_at DESC
LIMIT ` + fmt.Sprintf("$%d", len(args))

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	defer rows.Close()
	out := []Memory{}
	ids := []uuid.UUID{}
	for rows.Next() {
		m, err := scanMemoryRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
		ids = append(ids, m.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(ids) > 0 {
		if _, err := r.pool.Exec(ctx, `
UPDATE memories SET last_used_at=now()
WHERE id = ANY($1) AND tenant_id=$2 AND owner_user_id=$3`, ids, tenantID, ownerUserID); err != nil {
			return nil, fmt.Errorf("touch last_used_at: %w", err)
		}
	}
	return out, nil
}

// scanMemory consumes a QueryRow and returns the row or ErrMemoryNotFound.
func scanMemory(row pgx.Row) (*Memory, error) {
	var m Memory
	if err := row.Scan(&m.ID, &m.TenantID, &m.OwnerUserID, &m.Type, &m.Content, &m.Tags,
		&m.Source, &m.SourceMsgID, &m.LastUsedAt, &m.CreatedAt, &m.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMemoryNotFound
		}
		return nil, fmt.Errorf("scan memory: %w", err)
	}
	return &m, nil
}

// scanMemoryRows scans a row from a pgx.Rows iteration.
func scanMemoryRows(rows pgx.Rows) (*Memory, error) {
	var m Memory
	if err := rows.Scan(&m.ID, &m.TenantID, &m.OwnerUserID, &m.Type, &m.Content, &m.Tags,
		&m.Source, &m.SourceMsgID, &m.LastUsedAt, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, fmt.Errorf("scan memory: %w", err)
	}
	return &m, nil
}
