package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionRepo persists session metadata.
type SessionRepo struct {
	pool *pgxpool.Pool
}

func NewSessionRepo(pool *pgxpool.Pool) *SessionRepo {
	return &SessionRepo{pool: pool}
}

// Create inserts a new active session. Caller stamps the UUID.
func (r *SessionRepo) Create(ctx context.Context, s *Session) error {
	_, err := r.pool.Exec(ctx, `
INSERT INTO sessions (id, tenant_id, owner_user_id, title, model, profile, status)
VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		s.ID, s.TenantID, s.OwnerUserID, s.Title, s.Model, s.Profile, s.Status)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// Get returns a session by id scoped to tenant + owner. Cross-tenant or
// cross-owner reads return ErrSessionNotFound (no existence leak).
func (r *SessionRepo) Get(ctx context.Context, tenantID, ownerUserID, id uuid.UUID) (*Session, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, tenant_id, owner_user_id, title, model, profile, status, created_at, updated_at
FROM sessions
WHERE id=$1 AND tenant_id=$2 AND owner_user_id=$3`, id, tenantID, ownerUserID)
	var s Session
	if err := row.Scan(&s.ID, &s.TenantID, &s.OwnerUserID, &s.Title, &s.Model, &s.Profile,
		&s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("scan session: %w", err)
	}
	return &s, nil
}

// List returns sessions owned by ownerUserID under tenantID, newest first.
func (r *SessionRepo) List(ctx context.Context, tenantID, ownerUserID uuid.UUID) ([]Session, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, tenant_id, owner_user_id, title, model, profile, status, created_at, updated_at
FROM sessions
WHERE tenant_id=$1 AND owner_user_id=$2
ORDER BY created_at DESC`, tenantID, ownerUserID)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.TenantID, &s.OwnerUserID, &s.Title, &s.Model,
			&s.Profile, &s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Archive marks status='archived'. No-op if already archived; returns
// ErrSessionNotFound when the row doesn't exist or belongs elsewhere.
func (r *SessionRepo) Archive(ctx context.Context, tenantID, ownerUserID, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE sessions
SET status='archived', updated_at=now()
WHERE id=$1 AND tenant_id=$2 AND owner_user_id=$3`, id, tenantID, ownerUserID)
	if err != nil {
		return fmt.Errorf("archive session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// MessageRepo persists append-only conversation messages.
type MessageRepo struct {
	pool *pgxpool.Pool
}

func NewMessageRepo(pool *pgxpool.Pool) *MessageRepo {
	return &MessageRepo{pool: pool}
}

// Append inserts a message. Caller stamps ID + Seq. Seq must be monotonically
// increasing per session_id (UNIQUE constraint enforces this).
func (r *MessageRepo) Append(ctx context.Context, m *Message) error {
	var toolCalls, metadata any
	if len(m.ToolCalls) > 0 {
		toolCalls = []byte(m.ToolCalls)
	}
	if len(m.Metadata) > 0 {
		metadata = []byte(m.Metadata)
	}
	_, err := r.pool.Exec(ctx, `
INSERT INTO messages (id, session_id, tenant_id, seq, role, content, tool_call_id, tool_calls, metadata)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		m.ID, m.SessionID, m.TenantID, m.Seq, m.Role, m.Content,
		m.ToolCallID, toolCalls, metadata)
	if err != nil {
		return fmt.Errorf("append message: %w", err)
	}
	return nil
}

// List returns all messages for a session in seq order. Scoped to tenant for
// cross-tenant safety.
func (r *MessageRepo) List(ctx context.Context, tenantID, sessionID uuid.UUID) ([]Message, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, session_id, tenant_id, seq, role, content, tool_call_id, tool_calls, metadata, created_at
FROM messages
WHERE session_id=$1 AND tenant_id=$2
ORDER BY seq ASC`, sessionID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var m Message
		var toolCalls, metadata []byte
		if err := rows.Scan(&m.ID, &m.SessionID, &m.TenantID, &m.Seq, &m.Role, &m.Content,
			&m.ToolCallID, &toolCalls, &metadata, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if len(toolCalls) > 0 {
			m.ToolCalls = json.RawMessage(toolCalls)
		}
		if len(metadata) > 0 {
			m.Metadata = json.RawMessage(metadata)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// NextSeq returns the next monotonically-increasing seq for sessionID.
// Note: not atomic across concurrent callers; the (session_id, seq) UNIQUE
// constraint protects integrity. Service layer serializes SendMessage per
// session so concurrent appends to the same session are not allowed.
func (r *MessageRepo) NextSeq(ctx context.Context, sessionID uuid.UUID) (int64, error) {
	var seq int64
	err := r.pool.QueryRow(ctx, `
SELECT COALESCE(MAX(seq), 0) + 1 FROM messages WHERE session_id=$1`, sessionID).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("next seq: %w", err)
	}
	return seq, nil
}
