package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// Admin workflow tools expose Service.Create/Update/List/Get through the Tool
// Bus so an Agent (running under the `coding` or `workflow-authoring` profile)
// can draft and inspect workflows from a chat session. publish / delete remain
// admin-REST only — keeping a human in the loop for state changes that go live
// in the Bus.
//
// Tenant comes from the caller's JWT claims (auth.FromCtx); the tenantID arg
// passed by Bus.Invoke is the same value the middleware put on the request, so
// we cross-check the two to fail closed on any plumbing regression.

// adminToolRoleRequired is the role gate applied to every admin tool. Mirrors
// auth.RequireAdmin used on the REST surface.
const adminToolRoleRequired = "admin"

// NewAdminTools returns the 4 admin workflow tools as a slice for main.go to
// loop over and Register. svc is captured by reference.
func NewAdminTools(svc *Service) []toolbus.Tool {
	return []toolbus.Tool{
		&CreateTool{svc: svc},
		&UpdateTool{svc: svc},
		&ListTool{svc: svc},
		&GetTool{svc: svc},
	}
}

// requireAdminCtx pulls Claims out of ctx and verifies role==admin and the
// tenant matches what the Bus passed in. Returns a marshalled error envelope
// when the gate fails so the LLM gets a readable explanation rather than a
// generic schema/dispatch error.
func requireAdminCtx(ctx context.Context, tenantID uuid.UUID) (*auth.Claims, []byte, bool) {
	cl := auth.FromCtx(ctx)
	if cl == nil {
		body, _ := json.Marshal(map[string]any{
			"ok":    false,
			"error": "permission_denied",
			"detail": "missing auth context — workflow admin tools require an authenticated request",
		})
		return nil, body, false
	}
	if cl.Role != adminToolRoleRequired {
		body, _ := json.Marshal(map[string]any{
			"ok":     false,
			"error":  "permission_denied",
			"detail": "workflow admin tools require role=admin; ask the platform owner to publish on your behalf",
		})
		return nil, body, false
	}
	if cl.TenantID != tenantID {
		body, _ := json.Marshal(map[string]any{
			"ok":     false,
			"error":  "permission_denied",
			"detail": "tenant mismatch between claims and invocation",
		})
		return nil, body, false
	}
	return cl, nil, true
}

// mapServiceErr translates known sentinel errors to a stable {ok:false,error}
// envelope so the LLM can branch on string codes. Unknown errors fall through
// to a generic "internal" error with the message preserved.
func mapServiceErr(err error) ([]byte, error) {
	switch {
	case errors.Is(err, ErrNotFound):
		body, _ := json.Marshal(map[string]any{
			"ok": false, "error": "not_found", "detail": err.Error(),
		})
		return body, nil
	case errors.Is(err, ErrSlugTaken):
		body, _ := json.Marshal(map[string]any{
			"ok": false, "error": "slug_taken", "detail": err.Error(),
		})
		return body, nil
	}
	// Parse/validate failures from Service.Create/Update arrive as wrapped
	// fmt.Errorf strings; the message is the LLM's best clue to fix the DSL.
	msg := err.Error()
	body, _ := json.Marshal(map[string]any{
		"ok": false, "error": "invalid_dsl", "detail": msg,
	})
	return body, nil
}

// ----------------- workflow.create -----------------

// CreateTool drafts a new workflow row. Always created with published=false;
// publishing is admin REST.
type CreateTool struct{ svc *Service }

var _ toolbus.Tool = (*CreateTool)(nil)
var _ toolbus.Mutating = (*CreateTool)(nil)

func (t *CreateTool) Name() string         { return "workflow.create" }
func (t *CreateTool) IsMutating() bool     { return true }
func (t *CreateTool) Description() string {
	return "Draft a new Workflow DSL in the current tenant (admin only). The workflow is created unpublished — a human still has to publish it via the admin REST API. Slug must be kebab-case [a-z0-9-]; the DSL's id field must equal the slug."
}

func (t *CreateTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"slug": map[string]any{
				"type":      "string",
				"pattern":   "^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$",
				"description": "kebab-case identifier; must match the DSL's id field",
			},
			"name": map[string]any{
				"type":      "string",
				"minLength": 1,
				"maxLength": 200,
			},
			"description": map[string]any{
				"type":      "string",
				"maxLength": 2000,
			},
			"dsl_yaml": map[string]any{
				"type":      "string",
				"minLength": 1,
				"maxLength": 65536,
				"description": "Full YAML body of the workflow. See docs/WORKFLOW.md for syntax.",
			},
		},
		"required":             []string{"slug", "name", "dsl_yaml"},
		"additionalProperties": false,
	}
	out, _ := json.Marshal(schema)
	return out
}

type createToolInput struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	DSLYAML     string `json:"dsl_yaml"`
}

func (t *CreateTool) Invoke(ctx context.Context, tenantID, _ uuid.UUID,
	rawInput json.RawMessage) (json.RawMessage, error) {

	if _, body, ok := requireAdminCtx(ctx, tenantID); !ok {
		return body, nil
	}
	var in createToolInput
	if err := json.Unmarshal(rawInput, &in); err != nil {
		return nil, fmt.Errorf("unmarshal workflow.create input: %w", err)
	}
	wf, err := t.svc.Create(ctx, tenantID, in.Slug, in.Name, in.Description, in.DSLYAML)
	if err != nil {
		return mapServiceErr(err)
	}
	return json.Marshal(map[string]any{
		"ok":        true,
		"slug":      wf.Slug,
		"version":   wf.Version,
		"published": wf.Published,
	})
}

// ----------------- workflow.update -----------------

// UpdateTool rewrites a draft. Always forces published=false on the row (Repo
// behavior); the agent must remind the user to re-publish.
type UpdateTool struct{ svc *Service }

var _ toolbus.Tool = (*UpdateTool)(nil)
var _ toolbus.Mutating = (*UpdateTool)(nil)

func (t *UpdateTool) Name() string         { return "workflow.update" }
func (t *UpdateTool) IsMutating() bool     { return true }
func (t *UpdateTool) Description() string {
	return "Replace a Workflow's name/description/dsl_yaml (admin only). Version is bumped; if the workflow was published it is force-unpublished so the live tool is not silently swapped — a human must re-publish via admin REST."
}

func (t *UpdateTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"slug": map[string]any{
				"type":    "string",
				"pattern": "^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$",
			},
			"name": map[string]any{
				"type":      "string",
				"minLength": 1,
				"maxLength": 200,
			},
			"description": map[string]any{
				"type":      "string",
				"maxLength": 2000,
			},
			"dsl_yaml": map[string]any{
				"type":      "string",
				"minLength": 1,
				"maxLength": 65536,
			},
		},
		"required":             []string{"slug", "name", "dsl_yaml"},
		"additionalProperties": false,
	}
	out, _ := json.Marshal(schema)
	return out
}

func (t *UpdateTool) Invoke(ctx context.Context, tenantID, _ uuid.UUID,
	rawInput json.RawMessage) (json.RawMessage, error) {

	if _, body, ok := requireAdminCtx(ctx, tenantID); !ok {
		return body, nil
	}
	var in createToolInput // same field shape as create
	if err := json.Unmarshal(rawInput, &in); err != nil {
		return nil, fmt.Errorf("unmarshal workflow.update input: %w", err)
	}
	wf, err := t.svc.Update(ctx, tenantID, in.Slug, in.Name, in.Description, in.DSLYAML)
	if err != nil {
		return mapServiceErr(err)
	}
	return json.Marshal(map[string]any{
		"ok":               true,
		"slug":             wf.Slug,
		"version":          wf.Version,
		"published":        wf.Published,
		"requires_publish": true,
	})
}

// ----------------- workflow.list -----------------

// ListTool returns the tenant's workflows (without DSL body — too verbose for a
// tool result). Read-only but still admin-gated to mirror the REST surface.
type ListTool struct{ svc *Service }

var _ toolbus.Tool = (*ListTool)(nil)

func (t *ListTool) Name() string         { return "workflow.list" }
func (t *ListTool) Description() string {
	return "List the current tenant's workflows (admin only). Returns slug, name, version, published status; not the DSL body — use workflow.get for that."
}

func (t *ListTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
	out, _ := json.Marshal(schema)
	return out
}

type listToolEntry struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Version     int    `json:"version"`
	Published   bool   `json:"published"`
}

func (t *ListTool) Invoke(ctx context.Context, tenantID, _ uuid.UUID,
	_ json.RawMessage) (json.RawMessage, error) {

	if _, body, ok := requireAdminCtx(ctx, tenantID); !ok {
		return body, nil
	}
	wfs, err := t.svc.List(ctx, tenantID)
	if err != nil {
		return mapServiceErr(err)
	}
	out := make([]listToolEntry, 0, len(wfs))
	for _, w := range wfs {
		out = append(out, listToolEntry{
			Slug: w.Slug, Name: w.Name, Description: w.Description,
			Version: w.Version, Published: w.Published,
		})
	}
	return json.Marshal(map[string]any{
		"ok":        true,
		"workflows": out,
	})
}

// ----------------- workflow.get -----------------

// GetTool returns one workflow including its DSL body so the agent can
// re-render it before proposing an update.
type GetTool struct{ svc *Service }

var _ toolbus.Tool = (*GetTool)(nil)

func (t *GetTool) Name() string         { return "workflow.get" }
func (t *GetTool) Description() string {
	return "Fetch one workflow including its DSL body (admin only). Use this before workflow.update so you have the current DSL to modify."
}

func (t *GetTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"slug": map[string]any{
				"type":    "string",
				"pattern": "^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$",
			},
		},
		"required":             []string{"slug"},
		"additionalProperties": false,
	}
	out, _ := json.Marshal(schema)
	return out
}

type getToolInput struct {
	Slug string `json:"slug"`
}

func (t *GetTool) Invoke(ctx context.Context, tenantID, _ uuid.UUID,
	rawInput json.RawMessage) (json.RawMessage, error) {

	if _, body, ok := requireAdminCtx(ctx, tenantID); !ok {
		return body, nil
	}
	var in getToolInput
	if err := json.Unmarshal(rawInput, &in); err != nil {
		return nil, fmt.Errorf("unmarshal workflow.get input: %w", err)
	}
	wf, err := t.svc.Get(ctx, tenantID, in.Slug)
	if err != nil {
		return mapServiceErr(err)
	}
	return json.Marshal(map[string]any{
		"ok":          true,
		"slug":        wf.Slug,
		"name":        wf.Name,
		"description": wf.Description,
		"dsl_yaml":    wf.DSLYAML,
		"version":     wf.Version,
		"published":   wf.Published,
	})
}
