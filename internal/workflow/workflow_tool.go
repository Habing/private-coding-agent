package workflow

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// WorkflowTool is the toolbus.Tool adapter that exposes a published workflow
// as `workflow.<slug>` so any caller of the ToolBus (Agents via tool_call,
// direct /tools/invoke users, or other workflows via a `tool` node) can run
// it through the standard tool surface. The tool owns the tenant binding so
// cross-tenant invocations are refused at the boundary.
type WorkflowTool struct {
	svc      *Service
	tenantID uuid.UUID
	slug     string
	descr    string
	schema   json.RawMessage
}

// Name follows the design convention: every published workflow lives under
// the "workflow." prefix so listings can be filtered and Mutating defaults
// (conservatively true; see IsMutating) apply at a glance.
func (t *WorkflowTool) Name() string { return "workflow." + t.slug }

// Description surfaces the workflow's free-text descr to LLMs; empty descr
// falls back to a generic line at construction time (see descrOrDefault).
func (t *WorkflowTool) Description() string { return t.descr }

// Schema is generated from the DSL inputs block at publish time so it stays
// in lock-step with the workflow that backs it.
func (t *WorkflowTool) Schema() json.RawMessage { return t.schema }

// IsMutating is conservatively true: the workflow body can contain mutating
// tool nodes (fs.write, shell.exec, agent.delegate, ...), and at the tool
// surface we cannot inspect them per-call. Dry-Run within the Engine still
// short-circuits *internal* mutating steps; this flag protects callers that
// wrap workflow.<slug> in their own outer Dry-Run.
func (t *WorkflowTool) IsMutating() bool { return true }

// Invoke dispatches to Service.Invoke with dryRun=false. The tenantID passed
// by the Bus must match the tool's stored tenant — workflow.<slug> is global
// in the Bus namespace, so a cross-tenant caller would otherwise see another
// tenant's workflow under the same name.
func (t *WorkflowTool) Invoke(ctx context.Context, tenantID, userID uuid.UUID,
	input json.RawMessage) (json.RawMessage, error) {

	if tenantID != t.tenantID {
		return nil, fmt.Errorf("workflow.%s: cross-tenant invocation refused", t.slug)
	}

	var inputs map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &inputs); err != nil {
			return nil, fmt.Errorf("workflow.%s: invalid input json: %w", t.slug, err)
		}
	}
	res, err := t.svc.Invoke(ctx, tenantID, userID, t.slug, inputs, false)
	if err != nil {
		return nil, err
	}
	if res.Status != StatusOK {
		// Surface the engine's failure as a tool-call error so callers route
		// through their normal tool_error handling. The workflow_runs row
		// already carries the detail.
		return nil, fmt.Errorf("workflow.%s: %s: %s", t.slug, res.Status, res.Error)
	}
	return json.Marshal(res.Outputs)
}
