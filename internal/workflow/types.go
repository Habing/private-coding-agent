// Package workflow implements the Slice 19 Workflow Engine: a YAML DSL, a DAG
// executor, and a publish-as-Tool adapter that registers each published
// workflow into the Tool Bus as workflow.<slug>.
package workflow

import (
	"time"

	"github.com/google/uuid"
)

// NodeKind enumerates the v1 DSL node kinds. The parser infers the kind from
// which field is populated (use → tool; assign → assign; etc.); user-visible
// YAML never spells out a "kind:" field.
type NodeKind string

const (
	NodeTool     NodeKind = "tool"
	NodeAssign   NodeKind = "assign"
	NodeIf       NodeKind = "if"
	NodeForeach  NodeKind = "foreach"
	NodeParallel NodeKind = "parallel"
	NodeWait     NodeKind = "wait"
)

// OnError* are the v1 step-error policies. retry/rollback are deferred.
const (
	OnErrorFail     = "fail"
	OnErrorContinue = "continue"
)

// Execution-result statuses surfaced to callers + persisted to workflow_runs.
const (
	StatusOK        = "ok"
	StatusFailed    = "failed"
	StatusMaxSteps  = "max_steps"
	StatusTimeout   = "timeout"
	StatusCancelled = "cancelled"
)

// InputSpec is one entry in the DSL `inputs:` block. Type is a JSON-schema-ish
// primitive name (string/int/number/bool/object/array); validate.go also accepts
// a freeform Schema map for explicit array/object item descriptors.
type InputSpec struct {
	Type    string         `yaml:"type"`
	Default any            `yaml:"default,omitempty"`
	Schema  map[string]any `yaml:"schema,omitempty"`
}

// Step is the union of all v1 node kinds; parse.go infers Kind by scanning
// which fields the YAML populated.
type Step struct {
	ID      string `yaml:"id"`
	Kind    NodeKind

	// tool
	Use     string         `yaml:"use,omitempty"`
	Args    map[string]any `yaml:"args,omitempty"`
	Timeout time.Duration  `yaml:"-"`
	TimeoutRaw string      `yaml:"timeout,omitempty"`
	OnError string         `yaml:"on_error,omitempty"`

	// assign
	Assign map[string]string `yaml:"assign,omitempty"`

	// if
	If   string `yaml:"if,omitempty"`
	Then []Step `yaml:"then,omitempty"`
	Else []Step `yaml:"else,omitempty"`

	// foreach
	Foreach string `yaml:"foreach,omitempty"`
	As      string `yaml:"as,omitempty"`
	Steps   []Step `yaml:"steps,omitempty"`

	// parallel
	Parallel [][]Step `yaml:"parallel,omitempty"`

	// wait
	Wait    string        `yaml:"wait,omitempty"`
	WaitDur time.Duration `yaml:"-"`
}

// WorkflowDoc is the parsed in-memory shape of a DSL file.
type WorkflowDoc struct {
	ID          string               `yaml:"id"`
	Name        string               `yaml:"name"`
	Version     int                  `yaml:"version,omitempty"`
	Description string               `yaml:"description,omitempty"`
	Inputs      map[string]InputSpec `yaml:"inputs,omitempty"`
	Triggers    []TriggerSpec        `yaml:"triggers,omitempty"`
	Steps       []Step               `yaml:"steps"`
	Outputs     map[string]string    `yaml:"outputs,omitempty"`
}

// StepResult is a per-step record held in Engine state; expressions reference
// it through ${steps.<id>.output[.path...]} / ${steps.<id>.error}.
type StepResult struct {
	Output any    `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// ExecutionInput packages everything Engine.Execute needs in one struct so the
// signature stays stable as fields accrue (e.g. tracer attributes).
type ExecutionInput struct {
	Slug     string
	TenantID uuid.UUID
	UserID   uuid.UUID
	Inputs   map[string]any
	DryRun   bool
}

// ExecutionResult is what Engine.Execute returns; Service.Invoke persists most
// of these into the workflow_runs row.
type ExecutionResult struct {
	Outputs map[string]any
	Status  string
	Error   string
	Steps   int
}

// Config bounds the executor. Values exposed here are the v1 defaults; see
// ADR-76. Engine.NewEngine accepts a Config for tests to inject smaller caps.
type Config struct {
	MaxSteps           int
	MaxParallelFanout  int
	MaxNestingDepth    int
	DefaultStepTimeout time.Duration
}

// DefaultConfig returns the production bounds from ADR-76.
func DefaultConfig() Config {
	return Config{
		MaxSteps:           200,
		MaxParallelFanout:  8,
		MaxNestingDepth:    8,
		DefaultStepTimeout: 60 * time.Second,
	}
}
