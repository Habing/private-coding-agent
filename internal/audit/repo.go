package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo persists audit entries to PostgreSQL.
type Repo struct{ pool *pgxpool.Pool }

// NewRepo constructs a Repo backed by the given pgx pool.
func NewRepo(p *pgxpool.Pool) *Repo { return &Repo{pool: p} }

// Append inserts a single audit Entry into audit_log. A nil or unmarshalable
// Metadata is persisted as an empty JSON object.
func (r *Repo) Append(ctx context.Context, e Entry) error {
	meta, err := json.Marshal(e.Metadata)
	if err != nil {
		meta = []byte("{}")
	}
	_, err = r.pool.Exec(ctx, `
INSERT INTO audit_log (tenant_id, user_id, action, target, method, path, status, duration_ms, metadata)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		e.TenantID, e.UserID, e.Action, e.Target, e.Method, e.Path, e.Status, e.DurationMS, meta)
	if err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}
	return nil
}
