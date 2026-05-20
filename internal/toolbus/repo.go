package toolbus

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InvocationEvent 是 tool_invocations 表行映射。
type InvocationEvent struct {
	OccurredAt   time.Time
	TenantID     uuid.UUID
	UserID       uuid.UUID
	ToolName     string
	Status       string // "ok" | "error"
	ErrorClass   string
	DurationMS   int
	InputSHA256  string
	OutputSHA256 string
}

// InvocationRepo 写 tool_invocations。
type InvocationRepo struct {
	pool *pgxpool.Pool
}

func NewInvocationRepo(pool *pgxpool.Pool) *InvocationRepo {
	return &InvocationRepo{pool: pool}
}

// Insert 追加一行 tool_invocations。
func (r *InvocationRepo) Insert(ctx context.Context, e InvocationEvent) error {
	_, err := r.pool.Exec(ctx, `
INSERT INTO tool_invocations
  (occurred_at, tenant_id, user_id, tool_name, status, error_class,
   duration_ms, input_sha256, output_sha256)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		e.OccurredAt, e.TenantID, e.UserID, e.ToolName, e.Status, e.ErrorClass,
		e.DurationMS, e.InputSHA256, e.OutputSHA256)
	if err != nil {
		return fmt.Errorf("insert tool_invocations: %w", err)
	}
	return nil
}

// CountByTenant 测试 / 运维查询。
func (r *InvocationRepo) CountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM tool_invocations WHERE tenant_id=$1`, tenantID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count tool_invocations: %w", err)
	}
	return n, nil
}
