package reflection

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrJobNotFound = errors.New("reflection: job not found")
)

const (
	JobStatusPending    = "pending"
	JobStatusProcessing = "processing"
	JobStatusCompleted  = "completed"
	JobStatusFailed     = "failed"
)

// JobRow is a durable reflection_jobs record.
type JobRow struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	UserID     uuid.UUID
	SessionID  uuid.UUID
	Status     string
	Attempts   int
	MaxAttempts int
	NextRunAt  time.Time
	LastError  string
}

// JobRepo persists reflection work items so restarts do not lose archive jobs.
type JobRepo struct {
	pool *pgxpool.Pool
}

// NewJobRepo wires a pool with migrations applied.
func NewJobRepo(pool *pgxpool.Pool) *JobRepo {
	return &JobRepo{pool: pool}
}

// Enqueue inserts a pending job for an archived session. Duplicate session_id
// is ignored (returns false, nil) so re-archive is idempotent.
func (r *JobRepo) Enqueue(ctx context.Context, tenantID, userID, sessionID uuid.UUID, maxAttempts int) (uuid.UUID, bool, error) {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
INSERT INTO reflection_jobs (tenant_id, user_id, session_id, status, max_attempts, next_run_at)
VALUES ($1,$2,$3,$4,$5, now())
ON CONFLICT (session_id) DO NOTHING
RETURNING id`,
		tenantID, userID, sessionID, JobStatusPending, maxAttempts).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, fmt.Errorf("enqueue reflection job: %w", err)
	}
	return id, true, nil
}

// ListDue returns jobs ready to run (pending + next_run_at <= now).
func (r *JobRepo) ListDue(ctx context.Context, limit int) ([]JobRow, error) {
	if limit <= 0 || limit > 64 {
		limit = 32
	}
	rows, err := r.pool.Query(ctx, `
SELECT id, tenant_id, user_id, session_id, status, attempts, max_attempts, next_run_at, COALESCE(last_error,'')
FROM reflection_jobs
WHERE status = $1 AND next_run_at <= now() AND attempts < max_attempts
ORDER BY next_run_at ASC
LIMIT $2`, JobStatusPending, limit)
	if err != nil {
		return nil, fmt.Errorf("list due reflection jobs: %w", err)
	}
	defer rows.Close()
	out := make([]JobRow, 0, limit)
	for rows.Next() {
		var j JobRow
		if err := rows.Scan(&j.ID, &j.TenantID, &j.UserID, &j.SessionID, &j.Status,
			&j.Attempts, &j.MaxAttempts, &j.NextRunAt, &j.LastError); err != nil {
			return nil, fmt.Errorf("scan reflection job: %w", err)
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// MarkProcessing bumps updated_at before work starts (best-effort guard).
func (r *JobRepo) MarkProcessing(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE reflection_jobs SET status=$2, updated_at=now() WHERE id=$1 AND status=$3`,
		id, JobStatusProcessing, JobStatusPending)
	if err != nil {
		return fmt.Errorf("mark processing: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrJobNotFound
	}
	return nil
}

// MarkCompleted sets terminal success.
func (r *JobRepo) MarkCompleted(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE reflection_jobs SET status=$2, last_error=NULL, updated_at=now() WHERE id=$1`,
		id, JobStatusCompleted)
	if err != nil {
		return fmt.Errorf("mark completed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrJobNotFound
	}
	return nil
}

// MarkFailed schedules retry with exponential backoff or marks terminal failed.
func (r *JobRepo) MarkFailed(ctx context.Context, id uuid.UUID, jobErr error, retryBase time.Duration) error {
	if retryBase <= 0 {
		retryBase = time.Minute
	}
	var attempts, maxAttempts int
	err := r.pool.QueryRow(ctx, `
SELECT attempts, max_attempts FROM reflection_jobs WHERE id=$1`, id).Scan(&attempts, &maxAttempts)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrJobNotFound
		}
		return fmt.Errorf("load job for failure: %w", err)
	}
	attempts++
	errText := ""
	if jobErr != nil {
		errText = jobErr.Error()
		if len(errText) > 2000 {
			errText = errText[:2000]
		}
	}
	status := JobStatusPending
	next := time.Now()
	if attempts >= maxAttempts {
		status = JobStatusFailed
	} else {
		shift := attempts - 1
		if shift > 10 {
			shift = 10
		}
		delay := time.Duration(float64(retryBase) * math.Pow(2, float64(shift)))
		if delay > time.Hour {
			delay = time.Hour
		}
		next = time.Now().Add(delay)
	}
	_, err = r.pool.Exec(ctx, `
UPDATE reflection_jobs
   SET status=$2, attempts=$3, next_run_at=$4, last_error=$5, updated_at=now()
 WHERE id=$1`,
		id, status, attempts, next, errText)
	if err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	return nil
}

// ExpireStalePending rejects proposals older than ttlDays (0 skips).
func (r *JobRepo) ExpireStalePendingProposals(ctx context.Context, ttlDays int) (int64, error) {
	if ttlDays <= 0 {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx, `
UPDATE memory_proposals
   SET status=$1, decided_at=now()
 WHERE status=$2 AND created_at < now() - ($3 || ' days')::interval`,
		StatusRejected, StatusPending, fmt.Sprintf("%d", ttlDays))
	if err != nil {
		return 0, fmt.Errorf("expire stale proposals: %w", err)
	}
	return tag.RowsAffected(), nil
}
