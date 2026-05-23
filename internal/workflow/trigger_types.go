package workflow

import (
	"time"

	"github.com/google/uuid"
)

// TriggerKind identifies how a workflow trigger fires.
type TriggerKind string

const (
	TriggerKindCron    TriggerKind = "cron"
	TriggerKindWebhook TriggerKind = "webhook"
)

// TriggerSpec is one entry in the DSL `triggers:` block. Cron and webhook are
// mutually exclusive; parse/validate enforces exactly one per entry.
type TriggerSpec struct {
	ID       string         `yaml:"id"`
	Cron     string         `yaml:"cron,omitempty"`
	Timezone string         `yaml:"timezone,omitempty"`
	Webhook  map[string]any `yaml:"webhook,omitempty"`
	Inputs   map[string]any `yaml:"inputs,omitempty"`
}

// WorkflowTrigger is a persisted trigger row synced from DSL on publish.
type WorkflowTrigger struct {
	ID            uuid.UUID   `json:"id"`
	TenantID      uuid.UUID   `json:"tenant_id"`
	WorkflowID    uuid.UUID   `json:"workflow_id"`
	TriggerID     string      `json:"trigger_id"`
	Kind          TriggerKind `json:"kind"`
	CronExpr      string      `json:"cron_expr,omitempty"`
	Timezone      string      `json:"timezone"`
	WebhookToken  string      `json:"webhook_token,omitempty"`
	DefaultInputs []byte      `json:"default_inputs,omitempty"`
	Enabled       bool        `json:"enabled"`
	NextRunAt     *time.Time  `json:"next_run_at,omitempty"`
	LastRunAt     *time.Time  `json:"last_run_at,omitempty"`
	LastStatus    string      `json:"last_status,omitempty"`
	LastError     string      `json:"last_error,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}
