package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// Repo persists memory entries.
type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Insert creates a row. Caller stamps the UUID and timestamps (or relies on
// DB defaults). When embedding is non-nil, it is written to the `embedding`
// column; nil leaves the column NULL (row will be invisible to vector search).
// Returns a re-read row so timestamps are authoritative.
func (r *Repo) Insert(ctx context.Context, m *Memory, embedding []float32) (*Memory, error) {
	tags := m.Tags
	if tags == nil {
		tags = []string{}
	}
	var embArg any
	if embedding != nil {
		embArg = pgvector.NewVector(embedding)
	}
	_, err := r.pool.Exec(ctx, `
INSERT INTO memories (id, tenant_id, owner_user_id, type, content, tags, source, source_msg_id, embedding)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		m.ID, m.TenantID, m.OwnerUserID, m.Type, m.Content, tags, m.Source, m.SourceMsgID, embArg)
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
// matched the (tenant, owner, id) triple. When embedding is non-nil it is
// written too; nil leaves the column untouched (i.e. callers that didn't
// change Content should pass nil).
func (r *Repo) Update(ctx context.Context, tenantID, ownerUserID, id uuid.UUID,
	req UpdateRequest, embedding []float32) (*Memory, error) {
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
	if embedding != nil {
		args = append(args, pgvector.NewVector(embedding))
		sets = append(sets, fmt.Sprintf("embedding=$%d", len(args)))
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

// SearchKeyword runs the legacy ILIKE + tag + type filter and touches
// last_used_at on hits. Returns ordered by created_at DESC. Score is 0.
func (r *Repo) SearchKeyword(ctx context.Context, tenantID, ownerUserID uuid.UUID, req SearchRequest) ([]SearchResult, error) {
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
	out := []SearchResult{}
	ids := []uuid.UUID{}
	for rows.Next() {
		m, err := scanMemoryRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, SearchResult{Memory: *m})
		ids = append(ids, m.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.touchIDs(ctx, tenantID, ownerUserID, ids); err != nil {
		return nil, err
	}
	return out, nil
}

// SearchVector runs cosine similarity over `embedding` (ascending distance
// = descending similarity), applies the same optional type/tag filters, and
// touches last_used_at on hits. Rows without an embedding are skipped.
//
// Score is `1 - (embedding <=> qvec)` i.e. cosine similarity in [-1, 1]
// (practically [0, 1] for L2-normalized vectors).
func (r *Repo) SearchVector(ctx context.Context, tenantID, ownerUserID uuid.UUID,
	qVec []float32, req SearchRequest) ([]SearchResult, error) {
	if len(qVec) == 0 {
		return nil, fmt.Errorf("empty query vector")
	}
	qArg := pgvector.NewVector(qVec)
	args := []any{tenantID, ownerUserID, qArg}
	where := []string{"tenant_id=$1", "owner_user_id=$2", "embedding IS NOT NULL"}
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
       last_used_at, created_at, updated_at,
       1 - (embedding <=> $3) AS score
FROM memories
WHERE ` + strings.Join(where, " AND ") + `
ORDER BY embedding <=> $3 ASC
LIMIT ` + fmt.Sprintf("$%d", len(args))

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	defer rows.Close()
	out := []SearchResult{}
	ids := []uuid.UUID{}
	for rows.Next() {
		var m Memory
		var score float64
		if err := rows.Scan(&m.ID, &m.TenantID, &m.OwnerUserID, &m.Type, &m.Content,
			&m.Tags, &m.Source, &m.SourceMsgID, &m.LastUsedAt, &m.CreatedAt, &m.UpdatedAt,
			&score); err != nil {
			return nil, fmt.Errorf("scan vector row: %w", err)
		}
		out = append(out, SearchResult{Memory: m, Score: score})
		ids = append(ids, m.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.touchIDs(ctx, tenantID, ownerUserID, ids); err != nil {
		return nil, err
	}
	return out, nil
}

// FindSimilar returns the single nearest neighbour to qVec whose cosine
// similarity is at least threshold; ErrMemoryNotFound when no row clears the
// bar. Used by Create-path dedup.
func (r *Repo) FindSimilar(ctx context.Context, tenantID, ownerUserID uuid.UUID,
	qVec []float32, threshold float64) (*Memory, float64, error) {
	if len(qVec) == 0 {
		return nil, 0, fmt.Errorf("empty query vector")
	}
	qArg := pgvector.NewVector(qVec)
	row := r.pool.QueryRow(ctx, `
SELECT id, tenant_id, owner_user_id, type, content, tags, source, source_msg_id,
       last_used_at, created_at, updated_at,
       1 - (embedding <=> $3) AS score
FROM memories
WHERE tenant_id=$1 AND owner_user_id=$2 AND embedding IS NOT NULL
ORDER BY embedding <=> $3 ASC
LIMIT 1`, tenantID, ownerUserID, qArg)
	var m Memory
	var score float64
	if err := row.Scan(&m.ID, &m.TenantID, &m.OwnerUserID, &m.Type, &m.Content,
		&m.Tags, &m.Source, &m.SourceMsgID, &m.LastUsedAt, &m.CreatedAt, &m.UpdatedAt,
		&score); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, ErrMemoryNotFound
		}
		return nil, 0, fmt.Errorf("find similar: %w", err)
	}
	if score < threshold {
		return nil, score, ErrMemoryNotFound
	}
	return &m, score, nil
}

// TouchLastUsed bumps last_used_at on a single row. Silent no-op if id is
// missing (Service has already returned the row from a prior query).
func (r *Repo) TouchLastUsed(ctx context.Context, tenantID, ownerUserID, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
UPDATE memories SET last_used_at=now()
WHERE id=$1 AND tenant_id=$2 AND owner_user_id=$3`, id, tenantID, ownerUserID)
	if err != nil {
		return fmt.Errorf("touch last_used_at: %w", err)
	}
	return nil
}

func (r *Repo) touchIDs(ctx context.Context, tenantID, ownerUserID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := r.pool.Exec(ctx, `
UPDATE memories SET last_used_at=now()
WHERE id = ANY($1) AND tenant_id=$2 AND owner_user_id=$3`,
		ids, tenantID, ownerUserID); err != nil {
		return fmt.Errorf("touch last_used_at: %w", err)
	}
	return nil
}

// memoryRowLite is a minimal row for tenant-wide re-embed scans.
type memoryRowLite struct {
	ID          uuid.UUID
	OwnerUserID uuid.UUID
	Content     string
}

// ListByTenant returns memories for a tenant ordered by id (stable pagination).
func (r *Repo) ListByTenant(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]memoryRowLite, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := r.pool.Query(ctx, `
SELECT id, owner_user_id, content
FROM memories
WHERE tenant_id=$1
ORDER BY id ASC
LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list memories by tenant: %w", err)
	}
	defer rows.Close()
	out := make([]memoryRowLite, 0, limit)
	for rows.Next() {
		var row memoryRowLite
		if err := rows.Scan(&row.ID, &row.OwnerUserID, &row.Content); err != nil {
			return nil, fmt.Errorf("scan memory lite: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpdateEmbedding overwrites the embedding column for one row (admin re-embed).
func (r *Repo) UpdateEmbedding(ctx context.Context, tenantID, ownerUserID, id uuid.UUID, embedding []float32) error {
	if len(embedding) == 0 {
		return fmt.Errorf("empty embedding")
	}
	tag, err := r.pool.Exec(ctx, `
UPDATE memories SET embedding=$4, updated_at=now()
WHERE id=$1 AND tenant_id=$2 AND owner_user_id=$3`,
		id, tenantID, ownerUserID, pgvector.NewVector(embedding))
	if err != nil {
		return fmt.Errorf("update embedding: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrMemoryNotFound
	}
	return nil
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
