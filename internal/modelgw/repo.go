package modelgw

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProviderConfig 是 providers 表行映射。TenantID == nil 表示全局行。
type ProviderConfig struct {
	ID        uuid.UUID
	TenantID  *uuid.UUID
	Name      string
	Type      string
	BaseURL   string
	APIKeyEnv string
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ProviderRepo 读 providers 表。
type ProviderRepo struct {
	pool *pgxpool.Pool
}

func NewProviderRepo(pool *pgxpool.Pool) *ProviderRepo {
	return &ProviderRepo{pool: pool}
}

// ListEnabled 返回所有 enabled=true 的 provider(含 tenant 与 全局)。
func (r *ProviderRepo) ListEnabled(ctx context.Context) ([]ProviderConfig, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, tenant_id, name, type, base_url, api_key_env, enabled, created_at, updated_at
FROM providers WHERE enabled = TRUE
ORDER BY tenant_id NULLS FIRST, name`)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	defer rows.Close()

	var out []ProviderConfig
	for rows.Next() {
		var p ProviderConfig
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Name, &p.Type, &p.BaseURL,
			&p.APIKeyEnv, &p.Enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan provider: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetByName 用于测试 / 调试。返回 tenant_id IS NULL 的全局行 (按 name 应只有一条)。
func (r *ProviderRepo) GetByName(ctx context.Context, name string) (*ProviderConfig, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, tenant_id, name, type, base_url, api_key_env, enabled, created_at, updated_at
FROM providers WHERE name = $1 AND tenant_id IS NULL`, name)
	var p ProviderConfig
	if err := row.Scan(&p.ID, &p.TenantID, &p.Name, &p.Type, &p.BaseURL,
		&p.APIKeyEnv, &p.Enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProviderNotFound
		}
		return nil, fmt.Errorf("get provider: %w", err)
	}
	return &p, nil
}

// UsageRepo 写 model_usage。
type UsageRepo struct {
	pool *pgxpool.Pool
}

func NewUsageRepo(pool *pgxpool.Pool) *UsageRepo {
	return &UsageRepo{pool: pool}
}

// Insert 追加一行 model_usage。
func (r *UsageRepo) Insert(ctx context.Context, e CallEvent) error {
	_, err := r.pool.Exec(ctx, `
INSERT INTO model_usage
  (occurred_at, tenant_id, user_id, provider_id, provider_type, model,
   action, stream, status, error_class, input_tokens, output_tokens, duration_ms)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		e.OccurredAt, e.TenantID, e.UserID, e.ProviderID, e.ProviderType, e.Model,
		e.Action, e.Stream, e.Status, e.ErrorClass, e.InputTokens, e.OutputTokens, e.DurationMS)
	if err != nil {
		return fmt.Errorf("insert model_usage: %w", err)
	}
	return nil
}

// CountByTenant 测试 / 运维查询。
func (r *UsageRepo) CountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM model_usage WHERE tenant_id=$1`, tenantID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count model_usage: %w", err)
	}
	return n, nil
}
