package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("user not found")

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

type CreateInput struct {
	TenantID     uuid.UUID
	Email        string
	PasswordHash string
	Name         string
	Role         Role
}

func (r *Repo) Create(ctx context.Context, in CreateInput) (*User, error) {
	if in.Role == "" {
		in.Role = RoleMember
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO users (tenant_id, email, password_hash, name, role)
VALUES ($1,$2,$3,$4,$5)
RETURNING id, tenant_id, email, password_hash, name, role, created_at, updated_at`,
		in.TenantID, in.Email, in.PasswordHash, in.Name, string(in.Role))

	var u User
	if err := row.Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash,
		&u.Name, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, fmt.Errorf("insert: %w", err)
	}
	return &u, nil
}

func (r *Repo) GetByEmail(ctx context.Context, tenantID uuid.UUID, email string) (*User, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, tenant_id, email, password_hash, name, role, created_at, updated_at
FROM users WHERE tenant_id=$1 AND email=$2`, tenantID, email)

	var u User
	if err := row.Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash,
		&u.Name, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan: %w", err)
	}
	return &u, nil
}

func (r *Repo) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, tenant_id, email, password_hash, name, role, created_at, updated_at
FROM users WHERE id=$1`, id)

	var u User
	if err := row.Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash,
		&u.Name, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan: %w", err)
	}
	return &u, nil
}
