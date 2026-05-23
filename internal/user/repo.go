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

type CreateOIDCInput struct {
	TenantID     uuid.UUID
	Email        string
	PasswordHash string
	Name         string
	Role         Role
	OIDCIss      string
	OIDCSub      string
}

const userSelectCols = `id, tenant_id, email, password_hash, name, role, oidc_iss, oidc_sub, created_at, updated_at`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	var oidcIss, oidcSub *string
	if err := row.Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash,
		&u.Name, &u.Role, &oidcIss, &oidcSub, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan: %w", err)
	}
	if oidcIss != nil {
		u.OIDCIss = *oidcIss
	}
	if oidcSub != nil {
		u.OIDCSub = *oidcSub
	}
	return &u, nil
}

func (r *Repo) Create(ctx context.Context, in CreateInput) (*User, error) {
	if in.Role == "" {
		in.Role = RoleMember
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO users (tenant_id, email, password_hash, name, role)
VALUES ($1,$2,$3,$4,$5)
RETURNING `+userSelectCols,
		in.TenantID, in.Email, in.PasswordHash, in.Name, string(in.Role))
	return scanUser(row)
}

func (r *Repo) GetByEmail(ctx context.Context, tenantID uuid.UUID, email string) (*User, error) {
	row := r.pool.QueryRow(ctx, `
SELECT `+userSelectCols+`
FROM users WHERE tenant_id=$1 AND email=$2`, tenantID, email)
	return scanUser(row)
}

func (r *Repo) GetByOIDC(ctx context.Context, tenantID uuid.UUID, iss, sub string) (*User, error) {
	row := r.pool.QueryRow(ctx, `
SELECT `+userSelectCols+`
FROM users WHERE tenant_id=$1 AND oidc_iss=$2 AND oidc_sub=$3`, tenantID, iss, sub)
	return scanUser(row)
}

func (r *Repo) CreateOIDC(ctx context.Context, in CreateOIDCInput) (*User, error) {
	if in.Role == "" {
		in.Role = RoleMember
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO users (tenant_id, email, password_hash, name, role, oidc_iss, oidc_sub)
VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING `+userSelectCols,
		in.TenantID, in.Email, in.PasswordHash, in.Name, string(in.Role), in.OIDCIss, in.OIDCSub)
	return scanUser(row)
}

func (r *Repo) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
	row := r.pool.QueryRow(ctx, `
SELECT `+userSelectCols+`
FROM users WHERE id=$1`, id)
	return scanUser(row)
}

// FirstAdminID returns the oldest admin user for a tenant (cron trigger actor).
func (r *Repo) FirstAdminID(ctx context.Context, tenantID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
SELECT id FROM users WHERE tenant_id=$1 AND role=$2 ORDER BY created_at ASC LIMIT 1`,
		tenantID, string(RoleAdmin)).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, fmt.Errorf("first admin: %w", err)
	}
	return id, nil
}
