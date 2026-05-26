package connectors

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const httpFetchSettingsID = "global"

// HTTPFetchRepo persists the admin-editable http.fetch allowlist.
type HTTPFetchRepo struct {
	pool *pgxpool.Pool
}

// NewHTTPFetchRepo wraps a pgx pool for http_fetch_settings.
func NewHTTPFetchRepo(pool *pgxpool.Pool) *HTTPFetchRepo {
	return &HTTPFetchRepo{pool: pool}
}

// GetAllowHosts loads the stored allowlist. ok is false when no row exists.
func (r *HTTPFetchRepo) GetAllowHosts(ctx context.Context) (hosts []string, ok bool, err error) {
	err = r.pool.QueryRow(ctx,
		`SELECT allow_hosts FROM http_fetch_settings WHERE id = $1`,
		httpFetchSettingsID,
	).Scan(&hosts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("http_fetch_settings get: %w", err)
	}
	return hosts, true, nil
}

// UpsertAllowHosts saves the allowlist and returns the normalized hosts written.
func (r *HTTPFetchRepo) UpsertAllowHosts(ctx context.Context, hosts []string) ([]string, error) {
	norm := NormalizeAllowHosts(hosts)
	_, err := r.pool.Exec(ctx, `
INSERT INTO http_fetch_settings (id, allow_hosts, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (id) DO UPDATE
SET allow_hosts = EXCLUDED.allow_hosts, updated_at = now()`,
		httpFetchSettingsID, norm)
	if err != nil {
		return nil, fmt.Errorf("http_fetch_settings upsert: %w", err)
	}
	return norm, nil
}
