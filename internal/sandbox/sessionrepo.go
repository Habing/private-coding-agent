package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionRepo persists sandbox metadata in PostgreSQL.
type SessionRepo struct {
	pool *pgxpool.Pool
}

func NewSessionRepo(pool *pgxpool.Pool) *SessionRepo {
	return &SessionRepo{pool: pool}
}

// Insert creates a new sandbox row in status=pending. The Sandbox argument
// supplies the immutable fields; CreatedAt/UpdatedAt are stamped by the DB
// and NOT written back to the struct.
func (r *SessionRepo) Insert(ctx context.Context, sb *Sandbox) error {
	labels, _ := json.Marshal(map[string]string{})
	_, err := r.pool.Exec(ctx, `
INSERT INTO sandbox_sessions
  (id, tenant_id, owner_user_id, project_id, container_id, image, status,
   network_mode, cpus, memory_mb, pids_limit, labels)
VALUES ($1,$2,$3,$4,NULL,$5,$6,$7,$8,$9,$10,$11)`,
		sb.ID, sb.TenantID, sb.OwnerUserID, sb.ProjectID,
		sb.Image, string(sb.Status), string(sb.Network),
		sb.Resources.CPUs, sb.Resources.MemoryMB, sb.Resources.PIDsLimit,
		labels)
	if err != nil {
		return fmt.Errorf("insert sandbox: %w", err)
	}
	return nil
}

// SetContainerID transitions a pending sandbox to running with its container_id.
// Returns an error if the sandbox is not in 'pending' status (i.e., the caller
// must not call this twice or against a destroyed sandbox).
func (r *SessionRepo) SetContainerID(ctx context.Context, id uuid.UUID, containerID string) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE sandbox_sessions
SET container_id=$2, status='running', updated_at=now()
WHERE id=$1 AND status='pending'`, id, containerID)
	if err != nil {
		return fmt.Errorf("update container_id: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("set container_id: sandbox %s not in pending status", id)
	}
	return nil
}

// UpdateStatus changes status (and stamps updated_at; sets destroyed_at when
// status transitions to destroyed/failed terminal states).
func (r *SessionRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status Status) error {
	terminal := status == StatusDestroyed || status == StatusFailed
	if terminal {
		_, err := r.pool.Exec(ctx, `
UPDATE sandbox_sessions
SET status=$2, updated_at=now(), destroyed_at=now()
WHERE id=$1`, id, string(status))
		if err != nil {
			return fmt.Errorf("update status terminal: %w", err)
		}
		return nil
	}
	_, err := r.pool.Exec(ctx, `
UPDATE sandbox_sessions
SET status=$2, updated_at=now()
WHERE id=$1`, id, string(status))
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	return nil
}

// Get returns a sandbox by id scoped to tenantID. Returns ErrSandboxNotFound
// when the row doesn't exist or belongs to another tenant.
func (r *SessionRepo) Get(ctx context.Context, tenantID, id uuid.UUID) (*Sandbox, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, tenant_id, owner_user_id, project_id, image, status, network_mode,
       cpus, memory_mb, pids_limit, created_at, updated_at, destroyed_at
FROM sandbox_sessions
WHERE id=$1 AND tenant_id=$2`, id, tenantID)

	var sb Sandbox
	var network string
	var status string
	var destroyedAt *time.Time
	if err := row.Scan(&sb.ID, &sb.TenantID, &sb.OwnerUserID, &sb.ProjectID,
		&sb.Image, &status, &network,
		&sb.Resources.CPUs, &sb.Resources.MemoryMB, &sb.Resources.PIDsLimit,
		&sb.CreatedAt, &sb.UpdatedAt, &destroyedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSandboxNotFound
		}
		return nil, fmt.Errorf("scan sandbox: %w", err)
	}
	sb.Status = Status(status)
	sb.Network = NetworkMode(network)
	sb.DestroyedAt = destroyedAt
	return &sb, nil
}

// GetContainerID returns the container_id (may be empty for pending) for
// internal use that doesn't need full Sandbox load. Scoped to tenantID.
func (r *SessionRepo) GetContainerID(ctx context.Context, tenantID, id uuid.UUID) (string, error) {
	var cid *string
	err := r.pool.QueryRow(ctx,
		`SELECT container_id FROM sandbox_sessions WHERE id=$1 AND tenant_id=$2`,
		id, tenantID).Scan(&cid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrSandboxNotFound
		}
		return "", fmt.Errorf("get container_id: %w", err)
	}
	if cid == nil {
		return "", nil
	}
	return *cid, nil
}

// ListActive returns sandboxes not in terminal status. Used by Reconciler.
func (r *SessionRepo) ListActive(ctx context.Context) ([]*Sandbox, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, tenant_id, owner_user_id, project_id, image, status, network_mode,
       cpus, memory_mb, pids_limit, created_at, updated_at, destroyed_at
FROM sandbox_sessions
WHERE status IN ('pending','running','destroying')`)
	if err != nil {
		return nil, fmt.Errorf("query active: %w", err)
	}
	defer rows.Close()

	var out []*Sandbox
	for rows.Next() {
		var sb Sandbox
		var network, status string
		var destroyedAt *time.Time
		if err := rows.Scan(&sb.ID, &sb.TenantID, &sb.OwnerUserID, &sb.ProjectID,
			&sb.Image, &status, &network,
			&sb.Resources.CPUs, &sb.Resources.MemoryMB, &sb.Resources.PIDsLimit,
			&sb.CreatedAt, &sb.UpdatedAt, &destroyedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		sb.Status = Status(status)
		sb.Network = NetworkMode(network)
		sb.DestroyedAt = destroyedAt
		out = append(out, &sb)
	}
	return out, rows.Err()
}
