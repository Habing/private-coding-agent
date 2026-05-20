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

// ProviderConfig 是 providers 表行映射。
type ProviderConfig struct {
	ID        uuid.UUID
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

// ListEnabled 返回所有 enabled=true 的 provider。
func (r *ProviderRepo) ListEnabled(ctx context.Context) ([]ProviderConfig, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, name, type, base_url, api_key_env, enabled, created_at, updated_at
FROM providers WHERE enabled = TRUE
ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	defer rows.Close()

	var out []ProviderConfig
	for rows.Next() {
		var p ProviderConfig
		if err := rows.Scan(&p.ID, &p.Name, &p.Type, &p.BaseURL,
			&p.APIKeyEnv, &p.Enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan provider: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetByName 用于测试 / 调试。
func (r *ProviderRepo) GetByName(ctx context.Context, name string) (*ProviderConfig, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, name, type, base_url, api_key_env, enabled, created_at, updated_at
FROM providers WHERE name = $1`, name)
	var p ProviderConfig
	if err := row.Scan(&p.ID, &p.Name, &p.Type, &p.BaseURL,
		&p.APIKeyEnv, &p.Enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProviderNotFound
		}
		return nil, fmt.Errorf("get provider: %w", err)
	}
	return &p, nil
}
