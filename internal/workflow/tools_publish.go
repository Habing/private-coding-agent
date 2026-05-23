package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
	"github.com/yourorg/private-coding-agent/internal/workflow/template"
)

// NewProposalTools returns workflow.propose + workflow.publish for ToolBus registration.
func NewProposalTools(psvc *ProposalService) []toolbus.Tool {
	return []toolbus.Tool{
		&ProposeTool{psvc: psvc},
		&PublishTool{psvc: psvc},
	}
}

func requireAuthCtx(ctx context.Context, tenantID uuid.UUID) (*auth.Claims, []byte, bool) {
	cl := auth.FromCtx(ctx)
	if cl == nil {
		body, _ := json.Marshal(map[string]any{
			"ok": false, "error": "permission_denied",
			"detail": "missing auth context",
		})
		return nil, body, false
	}
	if cl.TenantID != tenantID {
		body, _ := json.Marshal(map[string]any{
			"ok": false, "error": "permission_denied",
			"detail": "tenant mismatch",
		})
		return nil, body, false
	}
	return cl, nil, true
}

func mapProposalErr(err error) ([]byte, error) {
	switch {
	case errors.Is(err, ErrProposalNotFound):
		body, _ := json.Marshal(map[string]any{"ok": false, "error": "not_found", "detail": err.Error()})
		return body, nil
	case errors.Is(err, ErrProposalDryRunFailed):
		body, _ := json.Marshal(map[string]any{"ok": false, "error": "dry_run_failed", "detail": err.Error()})
		return body, nil
	case errors.Is(err, ErrProposalInvalidState):
		body, _ := json.Marshal(map[string]any{"ok": false, "error": "invalid_state", "detail": err.Error()})
		return body, nil
	case errors.Is(err, ErrProposalSlugPublished):
		body, _ := json.Marshal(map[string]any{"ok": false, "error": "slug_published", "detail": err.Error()})
		return body, nil
	}
	return mapServiceErr(err)
}

func proposalToolEnvelope(p *Proposal) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"ok":           true,
		"proposal_id":  p.ID.String(),
		"slug":         p.Slug,
		"name":         p.Name,
		"source":       p.Source,
		"template_id":  p.TemplateID,
		"dry_run_ok":   p.DryRunOK,
		"dry_run_error": p.DryRunError,
		"status":       p.Status,
		"summary":      ProposalSummary(p),
	})
}

// ProposalSummary returns a short Chinese description for UI / LLM.
func ProposalSummary(p *Proposal) string {
	if p == nil {
		return ""
	}
	src := "自由生成"
	if p.TemplateID != "" {
		src = "模板 · " + p.TemplateID
	} else if len(p.Source) > len(ProposalSourceTemplatePrefix) &&
		p.Source[:len(ProposalSourceTemplatePrefix)] == ProposalSourceTemplatePrefix {
		src = "模板 · " + p.Source[len(ProposalSourceTemplatePrefix):]
	}
	dry := "模拟未通过"
	if p.DryRunOK {
		dry = "模拟通过"
	}
	return fmt.Sprintf("工作流「%s」(%s)，%s，状态=%s", p.Name, src, dry, p.Status)
}

// ----------------- workflow.propose -----------------

// ProposeTool creates a workflow proposal + dry-run. Any authenticated tenant
// user may call; members use Confirm to enter pending_approval.
type ProposeTool struct{ psvc *ProposalService }

var _ toolbus.Tool = (*ProposeTool)(nil)
var _ toolbus.Mutating = (*ProposeTool)(nil)

func (t *ProposeTool) Name() string     { return "workflow.propose" }
func (t *ProposeTool) IsMutating() bool { return true }
func (t *ProposeTool) Description() string {
	return "从模板或 DSL 创建工作流草案，自动 Dry-Run，返回 proposal_id 与摘要。发布需 workflow.publish 或 REST confirm。"
}

func (t *ProposeTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"slug":        map[string]any{"type": "string", "pattern": "^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$"},
			"name":        map[string]any{"type": "string", "minLength": 1, "maxLength": 200},
			"description": map[string]any{"type": "string", "maxLength": 2000},
			"template_id": map[string]any{"type": "string", "description": "catalog template id, e.g. llm-summarize-notify"},
			"slots":       map[string]any{"type": "object", "description": "template slot values"},
			"dsl_yaml":    map[string]any{"type": "string", "description": "full DSL for freeform path"},
			"user_message": map[string]any{
				"type":        "string",
				"description": "natural language; server classifies template + extracts slots when template_id omitted",
			},
		},
		"required":             []string{"slug", "name"},
		"additionalProperties": false,
	}
	out, _ := json.Marshal(schema)
	return out
}

type proposeToolInput struct {
	Slug        string         `json:"slug"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	TemplateID  string         `json:"template_id"`
	Slots       map[string]any `json:"slots"`
	DSLYAML     string         `json:"dsl_yaml"`
	UserMessage string         `json:"user_message"`
}

func (t *ProposeTool) Invoke(ctx context.Context, tenantID, userID uuid.UUID,
	rawInput json.RawMessage) (json.RawMessage, error) {

	cl, body, ok := requireAuthCtx(ctx, tenantID)
	if !ok {
		return body, nil
	}
	_ = cl

	var in proposeToolInput
	if err := json.Unmarshal(rawInput, &in); err != nil {
		return nil, fmt.Errorf("unmarshal workflow.propose input: %w", err)
	}

	var prop *Proposal
	var err error

	switch {
	case in.TemplateID != "" || (in.DSLYAML == "" && in.UserMessage != ""):
		tid, slots, fromNL := in.TemplateID, in.Slots, false
		if tid == "" && in.UserMessage != "" {
			var okClassify bool
			tid, slots, okClassify = template.ClassifyAndExtract(in.UserMessage, in.Slug, in.Name)
			if !okClassify {
				body, _ := json.Marshal(map[string]any{
					"ok": false, "error": "no_template_match",
					"detail": "could not classify user_message to a template; pass template_id or dsl_yaml",
				})
				return body, nil
			}
			fromNL = true
		}
		if slots == nil {
			slots = map[string]any{}
		}
		if fromNL && in.Description == "" {
			in.Description = in.UserMessage
		}
		prop, err = t.psvc.CreateFromTemplate(ctx, tenantID, userID, tid, in.Slug, in.Name, in.Description, slots, nil)
	case in.DSLYAML != "":
		prop, err = t.psvc.Create(ctx, tenantID, userID, CreateProposalInput{
			Slug: in.Slug, Name: in.Name, Description: in.Description, DSLYAML: in.DSLYAML,
		})
	default:
		body, _ := json.Marshal(map[string]any{
			"ok": false, "error": "invalid_input",
			"detail": "provide template_id, dsl_yaml, or user_message",
		})
		return body, nil
	}
	if err != nil {
		return mapProposalErr(err)
	}
	return proposalToolEnvelope(prop)
}

// ----------------- workflow.publish -----------------

// PublishTool confirms a proposal (admin) and registers workflow.<slug> on the Bus.
type PublishTool struct{ psvc *ProposalService }

var _ toolbus.Tool = (*PublishTool)(nil)
var _ toolbus.Mutating = (*PublishTool)(nil)

func (t *PublishTool) Name() string     { return "workflow.publish" }
func (t *PublishTool) IsMutating() bool { return true }
func (t *PublishTool) Description() string {
	return "确认并发布工作流草案（仅 admin）。input 含 proposal_id；要求 dry_run_ok=true。"
}

func (t *PublishTool) Schema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"proposal_id": map[string]any{"type": "string", "format": "uuid"},
		},
		"required":             []string{"proposal_id"},
		"additionalProperties": false,
	}
	out, _ := json.Marshal(schema)
	return out
}

type publishToolInput struct {
	ProposalID string `json:"proposal_id"`
}

func (t *PublishTool) Invoke(ctx context.Context, tenantID, userID uuid.UUID,
	rawInput json.RawMessage) (json.RawMessage, error) {

	if _, body, ok := requireAdminCtx(ctx, tenantID); !ok {
		return body, nil
	}
	var in publishToolInput
	if err := json.Unmarshal(rawInput, &in); err != nil {
		return nil, fmt.Errorf("unmarshal workflow.publish input: %w", err)
	}
	pid, err := uuid.Parse(in.ProposalID)
	if err != nil {
		body, _ := json.Marshal(map[string]any{"ok": false, "error": "invalid_input", "detail": "proposal_id must be uuid"})
		return body, nil
	}
	prop, err := t.psvc.Confirm(ctx, tenantID, userID, pid, true)
	if err != nil {
		return mapProposalErr(err)
	}
	return json.Marshal(map[string]any{
		"ok":          true,
		"proposal_id": prop.ID.String(),
		"slug":        prop.Slug,
		"status":      prop.Status,
		"published":   prop.Status == ProposalPublished,
		"summary":     ProposalSummary(prop),
	})
}
