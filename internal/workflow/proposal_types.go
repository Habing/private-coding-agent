package workflow

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Proposal lifecycle statuses for NL workflow authoring (Slice 19b).
const (
	ProposalDraft            = "draft"
	ProposalPendingApproval  = "pending_approval"
	ProposalConfirmed        = "confirmed"
	ProposalPublished        = "published"
	ProposalRejected         = "rejected"
)

// ProposalSourceFreeform marks DSL produced by workflow-authoring / manual YAML.
const ProposalSourceFreeform = "freeform"

// ProposalSourceTemplatePrefix is prepended to template_id in the source column.
const ProposalSourceTemplatePrefix = "template:"

// Proposal is a draft workflow awaiting dry-run review and optional publish.
type Proposal struct {
	ID               uuid.UUID       `json:"id"`
	TenantID         uuid.UUID       `json:"tenant_id"`
	SessionID        *uuid.UUID      `json:"session_id,omitempty"`
	CreatedBy        uuid.UUID       `json:"created_by"`
	Slug             string          `json:"slug"`
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	DSLYAML          string          `json:"dsl_yaml"`
	Source           string          `json:"source"`
	TemplateID       string          `json:"template_id,omitempty"`
	SlotsJSON        json.RawMessage `json:"slots_json,omitempty"`
	DryRunOK         bool            `json:"dry_run_ok"`
	DryRunOutputJSON json.RawMessage `json:"dry_run_output_json,omitempty"`
	DryRunError      string          `json:"dry_run_error,omitempty"`
	Status           string          `json:"status"`
	PublishedAt      *time.Time      `json:"published_at,omitempty"`
	DecidedBy        *uuid.UUID      `json:"decided_by,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// ValidProposalStatus reports whether s is a known proposal status or empty (no filter).
func ValidProposalStatus(s string) bool {
	switch s {
	case "", ProposalDraft, ProposalPendingApproval, ProposalConfirmed, ProposalPublished, ProposalRejected:
		return true
	default:
		return false
	}
}

// ProposalListFilter scopes admin list queries.
type ProposalListFilter struct {
	Status string
	Limit  int
	Offset int
}

// CreateProposalInput bundles fields for ProposalService.Create.
type CreateProposalInput struct {
	Slug        string
	Name        string
	Description string
	DSLYAML     string
	Source      string
	TemplateID  string
	Slots       map[string]any
	SessionID   *uuid.UUID
}
