package audit

import (
	"time"

	"github.com/google/uuid"
)

// Entry represents a single audit log record.
type Entry struct {
	OccurredAt time.Time
	TenantID   *uuid.UUID
	UserID     *uuid.UUID
	Action     string
	Target     string
	Method     string
	Path       string
	Status     int
	DurationMS int
	Metadata   map[string]any
}
