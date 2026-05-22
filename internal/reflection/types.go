// Package reflection extracts memory candidates from archived sessions via
// an asynchronous worker pool. Each ReflectionJob (tenant + user + session)
// fans out to the Reflector which calls the model gateway, parses a JSON
// array of proposals, and persists them. High-confidence proposals are
// auto-approved (creating a memory row inline through memory.Service.Create
// so vector dedup applies); the rest sit in `memory_proposals` for admin
// review through /admin/memory-proposals.
package reflection

import (
	"time"

	"github.com/google/uuid"
)

// Status enumerates allowed memory_proposals.status values.
const (
	StatusPending      = "pending"
	StatusApproved     = "approved"
	StatusAutoApproved = "auto_approved"
	StatusRejected     = "rejected"
)

// Allowed proposal types mirror memory.Type* — kept as constants here so the
// package does not import memory in repo code.
const (
	TypeProfile    = "profile"
	TypePreference = "preference"
	TypeKnowledge  = "knowledge"
	TypeLesson     = "lesson"
)

// MemoryProposal is one row of memory_proposals.
type MemoryProposal struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	OwnerUserID uuid.UUID  `json:"owner_user_id"`
	SessionID   *uuid.UUID `json:"session_id,omitempty"`
	Type        string     `json:"type"`
	Content     string     `json:"content"`
	Tags        []string   `json:"tags"`
	Confidence  float32    `json:"confidence"`
	Status      string     `json:"status"`
	MemoryID    *uuid.UUID `json:"memory_id,omitempty"`
	DecidedAt   *time.Time `json:"decided_at,omitempty"`
	DecidedBy   *uuid.UUID `json:"decided_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ReflectionJob is what session.ArchiveSession hands to the worker.
type ReflectionJob struct {
	TenantID  uuid.UUID
	UserID    uuid.UUID
	SessionID uuid.UUID
}

// ListFilter is the input to Repo.ListByTenant. Empty Status returns all.
type ListFilter struct {
	Status      string
	OwnerUserID *uuid.UUID
	Limit       int
	Offset      int
}

// Defaults for list paging.
const (
	DefaultListLimit = 20
	MaxListLimit     = 100
)

// IsValidType reports whether t is one of the four canonical memory types.
func IsValidType(t string) bool {
	switch t {
	case TypeProfile, TypePreference, TypeKnowledge, TypeLesson:
		return true
	}
	return false
}

// IsValidStatus reports whether s is one of the four canonical statuses.
func IsValidStatus(s string) bool {
	switch s {
	case StatusPending, StatusApproved, StatusAutoApproved, StatusRejected:
		return true
	}
	return false
}
