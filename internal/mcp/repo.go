package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// slugRE constrains the public-facing identifier used in mcp.<slug>.<tool>:
// lowercase ASCII, digits, dash, underscore; must start with letter/digit;
// 1-63 chars (UNIQUE constraint covers the rest via varchar(64)).
var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// ValidateSlug returns ErrSlugInvalid when slug does not match the regex.
// Exposed so the admin handler can reject bad input before hitting the DB.
func ValidateSlug(slug string) error {
	if !slugRE.MatchString(slug) {
		return ErrSlugInvalid
	}
	return nil
}

// Repo persists mcp_servers rows. All methods are tenant-scoped except the
// boot-time ListAllEnabled used by the Manager during republish.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo wraps a pgxpool for the mcp_servers table.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// Insert creates a new mcp_servers row. The caller is responsible for
// validating ToolsCache (typically nil at create-time, refreshed shortly
// after by Manager.RegisterServer). Returns ErrSlugConflict when the
// (tenant_id, slug) UNIQUE constraint trips and ErrSlugInvalid on bad slug.
func (r *Repo) Insert(ctx context.Context, s *Server) (*Server, error) {
	if err := ValidateSlug(s.Slug); err != nil {
		return nil, err
	}
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	if s.Transport == "" {
		s.Transport = TransportHTTP
	}
	if s.AuthType == "" {
		s.AuthType = AuthTypeNone
	}
	if s.Headers == nil {
		s.Headers = map[string]string{}
	}
	if s.ToolsCache == nil {
		s.ToolsCache = []ToolSchema{}
	}
	headersJSON, err := json.Marshal(s.Headers)
	if err != nil {
		return nil, fmt.Errorf("marshal headers: %w", err)
	}
	cacheJSON, err := json.Marshal(s.ToolsCache)
	if err != nil {
		return nil, fmt.Errorf("marshal tools_cache: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
INSERT INTO mcp_servers
  (id, tenant_id, slug, name, description, url, transport, auth_type, auth_token,
   headers, enabled, tools_cache)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		s.ID, s.TenantID, s.Slug, s.Name, s.Description, s.URL,
		s.Transport, s.AuthType, s.AuthToken, headersJSON, s.Enabled, cacheJSON)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: %s", ErrSlugConflict, s.Slug)
		}
		return nil, fmt.Errorf("insert mcp_server: %w", err)
	}
	return r.Get(ctx, s.TenantID, s.ID)
}

// Get fetches one row by (tenant_id, id). Returns ErrServerNotFound on miss.
func (r *Repo) Get(ctx context.Context, tenantID, id uuid.UUID) (*Server, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, tenant_id, slug, name, description, url, transport, auth_type, auth_token,
       headers, enabled, last_seen_at, last_error, tools_cache, created_at, updated_at
FROM mcp_servers WHERE tenant_id=$1 AND id=$2`, tenantID, id)
	return scanServer(row)
}

// GetBySlug is convenience for the Manager's slug-collision check inside a
// tenant scope. Same not-found semantics as Get.
func (r *Repo) GetBySlug(ctx context.Context, tenantID uuid.UUID, slug string) (*Server, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, tenant_id, slug, name, description, url, transport, auth_type, auth_token,
       headers, enabled, last_seen_at, last_error, tools_cache, created_at, updated_at
FROM mcp_servers WHERE tenant_id=$1 AND slug=$2`, tenantID, slug)
	return scanServer(row)
}

// List returns all rows for a tenant ordered by slug.
func (r *Repo) List(ctx context.Context, tenantID uuid.UUID) ([]Server, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, tenant_id, slug, name, description, url, transport, auth_type, auth_token,
       headers, enabled, last_seen_at, last_error, tools_cache, created_at, updated_at
FROM mcp_servers WHERE tenant_id=$1 ORDER BY slug`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list mcp_servers: %w", err)
	}
	defer rows.Close()
	out := []Server{}
	for rows.Next() {
		s, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// ListAllEnabled is used by Manager.Start to walk every tenant's enabled
// servers at boot. Result is ordered by tenant_id, slug for deterministic
// republish ordering (useful in tests).
func (r *Repo) ListAllEnabled(ctx context.Context) ([]Server, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, tenant_id, slug, name, description, url, transport, auth_type, auth_token,
       headers, enabled, last_seen_at, last_error, tools_cache, created_at, updated_at
FROM mcp_servers WHERE enabled=true ORDER BY tenant_id, slug`)
	if err != nil {
		return nil, fmt.Errorf("list enabled mcp_servers: %w", err)
	}
	defer rows.Close()
	out := []Server{}
	for rows.Next() {
		s, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// Update mutates one or more mutable fields. nil pointer = leave unchanged.
// Slug and tenant_id are immutable post-create.
func (r *Repo) Update(ctx context.Context, tenantID, id uuid.UUID,
	name, description, url, authType, authToken *string,
	headers *map[string]string, enabled *bool) (*Server, error) {

	cur, err := r.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if name != nil {
		cur.Name = *name
	}
	if description != nil {
		cur.Description = *description
	}
	if url != nil {
		cur.URL = *url
	}
	if authType != nil {
		cur.AuthType = *authType
	}
	if authToken != nil {
		cur.AuthToken = *authToken
	}
	if headers != nil {
		cur.Headers = *headers
	}
	if enabled != nil {
		cur.Enabled = *enabled
	}
	headersJSON, err := json.Marshal(cur.Headers)
	if err != nil {
		return nil, fmt.Errorf("marshal headers: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
UPDATE mcp_servers
SET name=$1, description=$2, url=$3, auth_type=$4, auth_token=$5,
    headers=$6, enabled=$7, updated_at=now()
WHERE tenant_id=$8 AND id=$9`,
		cur.Name, cur.Description, cur.URL, cur.AuthType, cur.AuthToken,
		headersJSON, cur.Enabled, tenantID, id)
	if err != nil {
		return nil, fmt.Errorf("update mcp_server: %w", err)
	}
	return r.Get(ctx, tenantID, id)
}

// UpdateToolsCache writes a fresh tools/list snapshot. Used by RefreshTools
// and after a successful create. last_error is cleared on success.
func (r *Repo) UpdateToolsCache(ctx context.Context, tenantID, id uuid.UUID,
	tools []ToolSchema, seenAt time.Time) error {
	if tools == nil {
		tools = []ToolSchema{}
	}
	cacheJSON, err := json.Marshal(tools)
	if err != nil {
		return fmt.Errorf("marshal tools_cache: %w", err)
	}
	ct, err := r.pool.Exec(ctx, `
UPDATE mcp_servers SET tools_cache=$1, last_seen_at=$2, last_error='', updated_at=now()
WHERE tenant_id=$3 AND id=$4`, cacheJSON, seenAt, tenantID, id)
	if err != nil {
		return fmt.Errorf("update tools_cache: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrServerNotFound
	}
	return nil
}

// UpdateLastSeen marks a successful heartbeat without touching tools_cache.
func (r *Repo) UpdateLastSeen(ctx context.Context, tenantID, id uuid.UUID, at time.Time) error {
	_, err := r.pool.Exec(ctx, `
UPDATE mcp_servers SET last_seen_at=$1, last_error='', updated_at=now()
WHERE tenant_id=$2 AND id=$3`, at, tenantID, id)
	if err != nil {
		return fmt.Errorf("update last_seen_at: %w", err)
	}
	return nil
}

// UpdateLastError records the latest failure (heartbeat or invoke). Does
// not clear last_seen_at — operators want to see "last good vs latest error".
func (r *Repo) UpdateLastError(ctx context.Context, tenantID, id uuid.UUID, errMsg string) error {
	// Cap stored error to keep the audit page tidy.
	if len(errMsg) > 1024 {
		errMsg = errMsg[:1024]
	}
	_, err := r.pool.Exec(ctx, `
UPDATE mcp_servers SET last_error=$1, updated_at=now()
WHERE tenant_id=$2 AND id=$3`, errMsg, tenantID, id)
	if err != nil {
		return fmt.Errorf("update last_error: %w", err)
	}
	return nil
}

// Delete drops one row. ErrServerNotFound when absent.
func (r *Repo) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM mcp_servers WHERE tenant_id=$1 AND id=$2`, tenantID, id)
	if err != nil {
		return fmt.Errorf("delete mcp_server: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrServerNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanServer(row rowScanner) (*Server, error) {
	var s Server
	var headersJSON, cacheJSON []byte
	err := row.Scan(&s.ID, &s.TenantID, &s.Slug, &s.Name, &s.Description, &s.URL,
		&s.Transport, &s.AuthType, &s.AuthToken, &headersJSON, &s.Enabled,
		&s.LastSeenAt, &s.LastError, &cacheJSON, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrServerNotFound
		}
		return nil, fmt.Errorf("scan mcp_server: %w", err)
	}
	s.Headers = map[string]string{}
	if len(headersJSON) > 0 {
		if err := json.Unmarshal(headersJSON, &s.Headers); err != nil {
			return nil, fmt.Errorf("decode headers: %w", err)
		}
	}
	s.ToolsCache = []ToolSchema{}
	if len(cacheJSON) > 0 {
		if err := json.Unmarshal(cacheJSON, &s.ToolsCache); err != nil {
			return nil, fmt.Errorf("decode tools_cache: %w", err)
		}
	}
	return &s, nil
}

// isUniqueViolation mirrors skills.isUniqueViolation: lightweight error-text
// scan so we don't pull pgconn just to read SQLSTATE.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "SQLSTATE 23505") || contains(msg, "duplicate key value")
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
