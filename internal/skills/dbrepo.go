package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBSkill is the persisted form of a tenant-scoped Skill. The same skill_key
// namespace as the filesystem registry: a DB row with skill_key=X shadows the
// filesystem skill with id=X for that tenant.
type DBSkill struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	SkillKey    string    `json:"skill_key"`
	Description string    `json:"description"`
	Body        string    `json:"body,omitempty"`
	ContentHash string    `json:"content_hash"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ToSkill produces the in-memory shape used by Resolver/Injector. The version
// is the 12-hex prefix of content_hash, matching the filesystem registry.
func (s *DBSkill) ToSkill() *Skill {
	ver := s.ContentHash
	if len(ver) > 12 {
		ver = ver[:12]
	}
	return &Skill{
		Document: Document{
			ID:          s.SkillKey,
			Description: s.Description,
			Body:        s.Body,
			SourcePath:  fmt.Sprintf("db://tenant/%s/%s", s.TenantID.String(), s.SkillKey),
		},
		Version:   ver,
		CharCount: len(s.Body),
	}
}

// DBRepo persists tenant-scoped skills and profile-skill bindings.
type DBRepo struct {
	pool *pgxpool.Pool
}

func NewDBRepo(pool *pgxpool.Pool) *DBRepo { return &DBRepo{pool: pool} }

// HashBody computes the content hash stored alongside a DBSkill row.
func HashBody(body string) string {
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:])
}

// Insert persists a new DBSkill row. The caller stamps id; (tenant_id,
// skill_key) must be unique or the insert returns ErrSkillKeyConflict.
func (r *DBRepo) Insert(ctx context.Context, s *DBSkill) (*DBSkill, error) {
	s.ContentHash = HashBody(s.Body)
	_, err := r.pool.Exec(ctx, `
INSERT INTO skills (id, tenant_id, skill_key, description, body, content_hash, enabled)
VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		s.ID, s.TenantID, s.SkillKey, s.Description, s.Body, s.ContentHash, s.Enabled)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: %s", ErrSkillKeyConflict, s.SkillKey)
		}
		return nil, fmt.Errorf("insert skill: %w", err)
	}
	return r.GetByKey(ctx, s.TenantID, s.SkillKey)
}

// GetByKey returns a tenant-scoped skill by skill_key. ErrSkillNotFound on miss.
func (r *DBRepo) GetByKey(ctx context.Context, tenantID uuid.UUID, key string) (*DBSkill, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, tenant_id, skill_key, description, body, content_hash, enabled, created_at, updated_at
FROM skills WHERE tenant_id=$1 AND skill_key=$2`, tenantID, key)
	return scanSkill(row)
}

// List returns all DB skills for a tenant ordered by skill_key. Bodies are
// included; admin handler controls when to omit them in the JSON response.
func (r *DBRepo) List(ctx context.Context, tenantID uuid.UUID) ([]DBSkill, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, tenant_id, skill_key, description, body, content_hash, enabled, created_at, updated_at
FROM skills WHERE tenant_id=$1 ORDER BY skill_key`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	defer rows.Close()
	out := []DBSkill{}
	for rows.Next() {
		s, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// ListEnabled returns only enabled rows for a tenant; used by Resolver.
func (r *DBRepo) ListEnabled(ctx context.Context, tenantID uuid.UUID) ([]DBSkill, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, tenant_id, skill_key, description, body, content_hash, enabled, created_at, updated_at
FROM skills WHERE tenant_id=$1 AND enabled=true ORDER BY skill_key`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list enabled skills: %w", err)
	}
	defer rows.Close()
	out := []DBSkill{}
	for rows.Next() {
		s, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// Update partially mutates description/body/enabled. Any nil pointer leaves
// the column unchanged. content_hash is recomputed when body is supplied.
func (r *DBRepo) Update(ctx context.Context, tenantID uuid.UUID, key string,
	description, body *string, enabled *bool) (*DBSkill, error) {
	cur, err := r.GetByKey(ctx, tenantID, key)
	if err != nil {
		return nil, err
	}
	if description != nil {
		cur.Description = *description
	}
	if body != nil {
		cur.Body = *body
		cur.ContentHash = HashBody(*body)
	}
	if enabled != nil {
		cur.Enabled = *enabled
	}
	_, err = r.pool.Exec(ctx, `
UPDATE skills SET description=$1, body=$2, content_hash=$3, enabled=$4, updated_at=now()
WHERE tenant_id=$5 AND skill_key=$6`,
		cur.Description, cur.Body, cur.ContentHash, cur.Enabled, tenantID, key)
	if err != nil {
		return nil, fmt.Errorf("update skill: %w", err)
	}
	return r.GetByKey(ctx, tenantID, key)
}

// Delete removes a tenant skill. Returns ErrSkillNotFound when missing.
func (r *DBRepo) Delete(ctx context.Context, tenantID uuid.UUID, key string) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM skills WHERE tenant_id=$1 AND skill_key=$2`, tenantID, key)
	if err != nil {
		return fmt.Errorf("delete skill: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrSkillNotFound
	}
	return nil
}

// GetForProfile returns the ordered skill_keys bound to (tenant, profile).
// Empty slice = no override; resolver falls back to in-code Profile.SkillIDs.
func (r *DBRepo) GetForProfile(ctx context.Context, tenantID uuid.UUID, profile string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
SELECT skill_key FROM tenant_profile_skills
WHERE tenant_id=$1 AND profile_name=$2 ORDER BY sort_order, skill_key`, tenantID, profile)
	if err != nil {
		return nil, fmt.Errorf("list profile skills: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// SetForProfile replaces the binding for (tenant, profile) atomically.
// Pass an empty slice to clear (resolver then falls back to in-code Profile).
func (r *DBRepo) SetForProfile(ctx context.Context, tenantID uuid.UUID, profile string, keys []string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx,
		`DELETE FROM tenant_profile_skills WHERE tenant_id=$1 AND profile_name=$2`,
		tenantID, profile); err != nil {
		return fmt.Errorf("clear profile skills: %w", err)
	}
	for i, k := range keys {
		if k == "" {
			continue
		}
		_, err := tx.Exec(ctx, `
INSERT INTO tenant_profile_skills (tenant_id, profile_name, skill_key, sort_order)
VALUES ($1,$2,$3,$4)`, tenantID, profile, k, i)
		if err != nil {
			return fmt.Errorf("insert profile skill: %w", err)
		}
	}
	return tx.Commit(ctx)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSkill(row rowScanner) (*DBSkill, error) {
	var s DBSkill
	err := row.Scan(&s.ID, &s.TenantID, &s.SkillKey, &s.Description, &s.Body,
		&s.ContentHash, &s.Enabled, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSkillNotFound
		}
		return nil, fmt.Errorf("scan skill: %w", err)
	}
	return &s, nil
}

func isUniqueViolation(err error) bool {
	// pgx maps unique violation to SQLSTATE 23505; we keep this dep-light by
	// scanning the error string rather than importing pgconn just for this.
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
