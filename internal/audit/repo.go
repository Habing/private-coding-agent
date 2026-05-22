package audit

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

// Repo persists audit entries to PostgreSQL.
type Repo struct{ pool *pgxpool.Pool }

// NewRepo constructs a Repo backed by the given pgx pool.
func NewRepo(p *pgxpool.Pool) *Repo { return &Repo{pool: p} }

// Append inserts a single audit Entry into audit_log, chaining its SHA-256
// hash to the previous row's entry_hash to make the table tamper-evident
// (Slice 22a). All writers across goroutines and replicas serialize on a
// transaction-scoped advisory lock so the chain cannot fork under concurrent
// load. OccurredAt is truncated to microsecond precision so the value the
// hash is computed over matches what PostgreSQL's timestamptz stores.
func (r *Repo) Append(ctx context.Context, e Entry) error {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now()
	}
	e.OccurredAt = e.OccurredAt.Truncate(time.Microsecond)

	meta, err := json.Marshal(e.Metadata)
	if err != nil {
		meta = []byte("{}")
		e.Metadata = map[string]any{}
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtext('audit_log'))`); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}

	var prev []byte
	err = tx.QueryRow(ctx,
		`SELECT entry_hash FROM audit_log ORDER BY id DESC LIMIT 1`).Scan(&prev)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read prev hash: %w", err)
	}
	if len(prev) != HashSize {
		prev = ZeroHash()
	}
	h := Hash(prev, e)

	if _, err := tx.Exec(ctx, `
INSERT INTO audit_log (occurred_at, tenant_id, user_id, action, target, method, path, status, duration_ms, metadata, prev_hash, entry_hash)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		e.OccurredAt, e.TenantID, e.UserID, e.Action, e.Target, e.Method, e.Path,
		e.Status, e.DurationMS, meta, prev, h[:]); err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Default and maximum LIMIT values for List. Mirrors the contract used by the
// memory package so callers can predict cap behavior without reading source.
const (
	DefaultListLimit = 50
	MaxListLimit     = 200
)

// ListFilter narrows audit.List results. TenantID is mandatory and is set by
// the handler from auth claims — Repo never returns cross-tenant rows. Action
// is treated as a prefix (e.g. "auth.login" matches both
// "auth.login.success" and "auth.login.failure"). From/To bound occurred_at
// inclusively. MinStatus/MaxStatus bound the HTTP status column inclusively
// (0/0 means "unbounded").
type ListFilter struct {
	TenantID  uuid.UUID
	Action    string
	UserID    *uuid.UUID
	From, To  *time.Time
	MinStatus int
	MaxStatus int
	Limit     int
	Offset    int
}

// List returns audit entries matching f scoped to f.TenantID, newest first.
// total is the unpaginated count of matching rows so the caller can render
// pagination controls without a second round trip.
func (r *Repo) List(ctx context.Context, f ListFilter) ([]Entry, int, error) {
	if f.TenantID == uuid.Nil {
		return nil, 0, fmt.Errorf("audit.List: TenantID required")
	}
	args := []any{f.TenantID}
	where := []string{"tenant_id=$1"}
	if f.Action != "" {
		args = append(args, f.Action+"%")
		where = append(where, fmt.Sprintf("action LIKE $%d", len(args)))
	}
	if f.UserID != nil {
		args = append(args, *f.UserID)
		where = append(where, fmt.Sprintf("user_id=$%d", len(args)))
	}
	if f.From != nil {
		args = append(args, *f.From)
		where = append(where, fmt.Sprintf("occurred_at >= $%d", len(args)))
	}
	if f.To != nil {
		args = append(args, *f.To)
		where = append(where, fmt.Sprintf("occurred_at <= $%d", len(args)))
	}
	if f.MinStatus > 0 {
		args = append(args, f.MinStatus)
		where = append(where, fmt.Sprintf("status >= $%d", len(args)))
	}
	if f.MaxStatus > 0 {
		args = append(args, f.MaxStatus)
		where = append(where, fmt.Sprintf("status <= $%d", len(args)))
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit: %w", err)
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
SELECT occurred_at, tenant_id, user_id, action, target, method, path, status, duration_ms, metadata
FROM audit_log
WHERE ` + whereSQL + `
ORDER BY occurred_at DESC, id DESC
LIMIT ` + limitArg + offsetClause

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query audit: %w", err)
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var meta []byte
		if err := rows.Scan(&e.OccurredAt, &e.TenantID, &e.UserID, &e.Action, &e.Target,
			&e.Method, &e.Path, &e.Status, &e.DurationMS, &meta); err != nil {
			return nil, 0, fmt.Errorf("scan audit: %w", err)
		}
		if len(meta) > 0 {
			_ = json.Unmarshal(meta, &e.Metadata)
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}
