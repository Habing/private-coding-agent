// Package memory implements the persistent memory subsystem: a single
// multi-tenant table that stores Agent-/user-set memory entries, with REST
// CRUD and internal MCP tools so agents can read/write inside the ReAct loop.
//
// This slice is "basic" by design: only User-scoped entries; search uses
// keyword (ILIKE) + tag overlap + type filtering — no embeddings, no auto
// injection, no reflection. See docs/superpowers/specs/2026-05-20-slice-07-memory-design.md.
package memory

import (
	"time"

	"github.com/google/uuid"
)

// Type enumerates allowed memory.type values; a DB CHECK constraint mirrors
// these. The slice does not impose business semantics — clients pick.
const (
	TypeProfile    = "profile"
	TypePreference = "preference"
	TypeKnowledge  = "knowledge"
	TypeLesson     = "lesson"
)

// Source enumerates conventional values for memory.source. Currently free-form;
// constants document common usages.
const (
	SourceUser       = "user"
	SourceAgent      = "agent"
	SourceChat       = "chat"
	SourceReflection = "reflection"
)

// Memory is one row in the `memories` table.
type Memory struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	OwnerUserID uuid.UUID  `json:"owner_user_id"`
	Type        string     `json:"type"`
	Content     string     `json:"content"`
	Tags        []string   `json:"tags"`
	Source      string     `json:"source"`
	SourceMsgID *uuid.UUID `json:"source_msg_id,omitempty"`
	LastUsedAt  time.Time  `json:"last_used_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// CreateRequest is the body of POST /memories and the input of memory.save.
type CreateRequest struct {
	Type        string     `json:"type"`
	Content     string     `json:"content"`
	Tags        []string   `json:"tags,omitempty"`
	Source      string     `json:"source,omitempty"`
	SourceMsgID *uuid.UUID `json:"source_msg_id,omitempty"`
}

// UpdateRequest is the body of PUT /memories/{id}. Pointer fields signal
// "unchanged" when nil; Tags uses a separate convention: nil = unchanged,
// non-nil (including empty slice) = replace.
type UpdateRequest struct {
	Type    *string  `json:"type,omitempty"`
	Content *string  `json:"content,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	TagsSet bool     `json:"-"` // set by handler when client sent the field
}

// ListFilter is the input to Service.List / GET /memories.
type ListFilter struct {
	Type   string   `json:"type,omitempty"`
	Tags   []string `json:"tags,omitempty"`
	Query  string   `json:"q,omitempty"`
	Limit  int      `json:"limit,omitempty"`
	Offset int      `json:"offset,omitempty"`
}

// SearchRequest is the input to Service.Search / memory.search.
type SearchRequest struct {
	Query string   `json:"query,omitempty"`
	Type  string   `json:"type,omitempty"`
	Tags  []string `json:"tags,omitempty"`
	Limit int      `json:"limit,omitempty"`
}

// DefaultListLimit / MaxListLimit cap List & Search responses.
const (
	DefaultListLimit = 20
	MaxListLimit     = 100
)
