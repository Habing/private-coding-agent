// Package session implements the Session Orchestrator: session lifecycle,
// append-only message persistence, REST CRUD, and a WebSocket channel that
// streams agent.Engine events to the client while writing each one to the
// messages table.
package session

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Status enumerates allowed session.status values.
const (
	StatusActive   = "active"
	StatusArchived = "archived"
)

// Role enumerates allowed message.role values.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
	RoleSystem    = "system"
)

// DefaultProfile is used when CreateRequest.Profile is empty.
const DefaultProfile = "coding"

// Session is the persisted metadata for one conversation.
type Session struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	OwnerUserID uuid.UUID `json:"owner_user_id"`
	Title       string    `json:"title"`
	Model       string    `json:"model"`
	Profile     string    `json:"profile"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Message is one append-only row in the conversation log.
// ToolCalls / Metadata are JSONB columns; nil for messages that don't use them.
type Message struct {
	ID         uuid.UUID       `json:"id"`
	SessionID  uuid.UUID       `json:"session_id"`
	TenantID   uuid.UUID       `json:"tenant_id"`
	Seq        int64           `json:"seq"`
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

// CreateRequest is the body of POST /sessions.
type CreateRequest struct {
	Model   string `json:"model"`
	Profile string `json:"profile"`
	Title   string `json:"title"`
}

// WebSocket frame type constants — shared by client and server payloads.
const (
	FrameTypeUserMessage = "user_message"
	FrameTypePing        = "ping"
	FrameTypePong        = "pong"
	FrameTypeEvent       = "event"
	FrameTypeDone        = "done"
	FrameTypeError       = "error"
)
