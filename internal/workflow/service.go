package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// BusStepRunner adapts *toolbus.Bus to the engine's StepRunner interface so
// production wiring doesn't need a custom shim per call site. Tests inject a
// mock runner directly.
type BusStepRunner struct {
	Bus interface {
		Invoke(ctx context.Context, tenantID, userID uuid.UUID,
			toolName string, input json.RawMessage) (json.RawMessage, error)
		IsMutating(name string) bool
	}
}

// InvokeTool forwards to bus.Invoke; the name change is only to match the
// StepRunner contract.
func (r BusStepRunner) InvokeTool(ctx context.Context, tenantID, userID uuid.UUID,
	name string, args json.RawMessage) (json.RawMessage, error) {
	return r.Bus.Invoke(ctx, tenantID, userID, name, args)
}

// IsMutating delegates to the Bus's per-name lookup.
func (r BusStepRunner) IsMutating(name string) bool { return r.Bus.IsMutating(name) }

// BusRegistrar is the slice of *toolbus.Bus that Service depends on. Keeping
// it as an interface (rather than passing *Bus directly) lets the service
// tests fake registration without spinning up a full Bus + schema cache.
type BusRegistrar interface {
	Register(t toolbus.Tool) error
	Unregister(name string) error
	IsMutating(name string) bool
	Invoke(ctx context.Context, tenantID, userID uuid.UUID,
		toolName string, input json.RawMessage) (json.RawMessage, error)
}

// Service is the central entry point for the Workflow Engine. It coordinates
// Repo (persistence) + Engine (execution) + ToolBus (publish/unpublish) and
// owns the audit + run-log instrumentation so the admin handler and the
// workflow.<slug> Bus tool share one execution path.
type Service struct {
	repo   *Repo
	engine *Engine
	bus    BusRegistrar
	audit  audit.Sink
}

// NewService wires a Service. bus may be nil for unit tests that exercise only
// CRUD + Invoke (publishing requires a real bus).
func NewService(repo *Repo, engine *Engine, bus BusRegistrar, sink audit.Sink) *Service {
	return &Service{repo: repo, engine: engine, bus: bus, audit: sink}
}

// WithAuditSink swaps the audit sink after construction. Allows main.go to
// build the service before the audit sink is ready and then late-bind.
func (s *Service) WithAuditSink(sink audit.Sink) *Service {
	if s != nil {
		s.audit = sink
	}
	return s
}

// InvokeResult is the unified shape returned by admin Invoke and the
// workflow.<slug> Bus tool. RunID lets callers cross-reference the
// workflow_runs row for trace + audit.
type InvokeResult struct {
	RunID      uuid.UUID      `json:"run_id"`
	Status     string         `json:"status"`
	Outputs    map[string]any `json:"outputs,omitempty"`
	Error      string         `json:"error,omitempty"`
	Steps      int            `json:"steps"`
	DryRun     bool           `json:"dry_run"`
	StartedAt  time.Time      `json:"started_at"`
	DurationMS int            `json:"duration_ms"`
}

// Create persists a new workflow row. Slug uniqueness is per-tenant; on
// collision returns ErrSlugTaken (caller maps to HTTP 409).
func (s *Service) Create(ctx context.Context, tenantID uuid.UUID, slug, name, descr, dsl string) (*Workflow, error) {
	if _, err := s.parseValidate(dsl, slug); err != nil {
		return nil, err
	}
	wf, err := s.repo.Create(ctx, tenantID, slug, name, descr, dsl)
	if err != nil {
		return nil, err
	}
	s.auditAdmin(tenantID, slug, "workflow.admin.create",
		map[string]any{"version": wf.Version, "id": wf.ID.String()})
	return wf, nil
}

// Get returns a single workflow scoped to the tenant. ErrNotFound on miss.
func (s *Service) Get(ctx context.Context, tenantID uuid.UUID, slug string) (*Workflow, error) {
	return s.repo.Get(ctx, tenantID, slug)
}

// List returns the tenant's workflows (without dsl_yaml; caller can GET the
// detail endpoint for full body).
func (s *Service) List(ctx context.Context, tenantID uuid.UUID) ([]Workflow, error) {
	return s.repo.List(ctx, tenantID)
}

// Update rewrites the DSL + metadata, bumping version. The previous published
// state is force-cleared by the Repo so callers must Re-Publish to put the new
// version back into the Bus — prevents silent replacement of a live tool.
func (s *Service) Update(ctx context.Context, tenantID uuid.UUID, slug, name, descr, dsl string) (*Workflow, error) {
	if _, err := s.parseValidate(dsl, slug); err != nil {
		return nil, err
	}
	// Read pre-state so we know whether to drop the Bus registration.
	prev, err := s.repo.Get(ctx, tenantID, slug)
	if err != nil {
		return nil, err
	}
	wf, err := s.repo.Update(ctx, tenantID, slug, name, descr, dsl)
	if err != nil {
		return nil, err
	}
	if prev.Published && s.bus != nil {
		// Repo already flipped published=false; mirror in the Bus.
		_ = s.bus.Unregister("workflow." + slug)
	}
	s.auditAdmin(tenantID, slug, "workflow.admin.update",
		map[string]any{"version_new": wf.Version, "was_published": prev.Published})
	return wf, nil
}

// Delete drops a workflow row. If it was published, the Bus registration is
// unregistered first so /tools queries become consistent immediately.
func (s *Service) Delete(ctx context.Context, tenantID uuid.UUID, slug string) error {
	prev, err := s.repo.Get(ctx, tenantID, slug)
	if err != nil {
		return err
	}
	if prev.Published && s.bus != nil {
		_ = s.bus.Unregister("workflow." + slug)
	}
	if err := s.repo.Delete(ctx, tenantID, slug); err != nil {
		return err
	}
	s.auditAdmin(tenantID, slug, "workflow.admin.delete",
		map[string]any{"was_published": prev.Published})
	return nil
}

// Publish validates DSL again (defense in depth — DSL on disk may be stale
// against a code-level validation tightening) and registers a workflow.<slug>
// tool into the Bus. Returns ErrNotFound if the row is missing.
func (s *Service) Publish(ctx context.Context, tenantID uuid.UUID, slug string) error {
	wf, doc, err := s.loadParsed(ctx, tenantID, slug)
	if err != nil {
		return err
	}
	if err := s.registerTool(tenantID, wf, doc); err != nil {
		return err
	}
	if err := s.repo.SetPublished(ctx, tenantID, slug, true); err != nil {
		return err
	}
	s.auditAdmin(tenantID, slug, "workflow.admin.publish",
		map[string]any{"version": wf.Version})
	return nil
}

// Unpublish removes the Bus registration and clears the published flag.
// Returns ErrNotFound if the row is missing; the Bus removal is best-effort
// (an absent registration is not an error from the caller's POV).
func (s *Service) Unpublish(ctx context.Context, tenantID uuid.UUID, slug string) error {
	wf, err := s.repo.Get(ctx, tenantID, slug)
	if err != nil {
		return err
	}
	if s.bus != nil {
		_ = s.bus.Unregister("workflow." + slug)
	}
	if err := s.repo.SetPublished(ctx, tenantID, slug, false); err != nil {
		return err
	}
	s.auditAdmin(tenantID, slug, "workflow.admin.unpublish",
		map[string]any{"version": wf.Version})
	return nil
}

// Invoke is the single execution path. Both the admin invoke endpoint and the
// workflow.<slug> Bus tool funnel through here so the workflow_runs row, the
// audit pair, and the Engine call stay in lock-step regardless of entry.
func (s *Service) Invoke(ctx context.Context, tenantID, userID uuid.UUID,
	slug string, inputs map[string]any, dryRun bool) (*InvokeResult, error) {

	wf, doc, err := s.loadParsed(ctx, tenantID, slug)
	if err != nil {
		return nil, err
	}

	inputsJSON, err := json.Marshal(orEmptyMap(inputs))
	if err != nil {
		return nil, fmt.Errorf("marshal inputs: %w", err)
	}

	runID, err := s.repo.CreateRun(ctx, Run{
		TenantID: tenantID, UserID: userID, WorkflowID: wf.ID,
		VersionAtRun: wf.Version, DryRun: dryRun, Status: "running",
		Inputs: inputsJSON,
	})
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}

	s.auditInvokeStart(tenantID, userID, slug, runID, dryRun, inputs)

	started := time.Now()
	res, _ := s.engine.Execute(ctx, doc, ExecutionInput{
		Slug: slug, TenantID: tenantID, UserID: userID,
		Inputs: inputs, DryRun: dryRun,
	})
	duration := time.Since(started)

	var outputsJSON []byte
	if res.Outputs != nil {
		outputsJSON, _ = json.Marshal(res.Outputs)
	}

	// Use a detached context for the final write so a client disconnect doesn't
	// leave the row stuck in "running". Mirrors the audit.Detached pattern.
	finCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if ferr := s.repo.FinishRun(finCtx, runID, res.Status, outputsJSON, res.Error, int(duration.Milliseconds())); ferr != nil {
		slog.Warn("workflow: finish run", "run_id", runID, "err", ferr)
	}

	s.auditInvokeComplete(tenantID, userID, slug, runID, dryRun, res, duration)

	return &InvokeResult{
		RunID: runID, Status: res.Status, Outputs: res.Outputs,
		Error: res.Error, Steps: res.Steps, DryRun: dryRun,
		StartedAt: started, DurationMS: int(duration.Milliseconds()),
	}, nil
}

// ListRuns returns the most-recent N runs of a workflow (looked up by slug
// first so callers don't have to know workflow_id).
func (s *Service) ListRuns(ctx context.Context, tenantID uuid.UUID, slug string, limit int) ([]Run, error) {
	wf, err := s.repo.Get(ctx, tenantID, slug)
	if err != nil {
		return nil, err
	}
	return s.repo.ListRuns(ctx, tenantID, wf.ID, limit)
}

// RepublishAll re-registers every published workflow into the Bus on boot.
// Errors on individual workflows are logged and skipped so one bad row can't
// block startup. Called once from main.go after the Bus is constructed.
func (s *Service) RepublishAll(ctx context.Context) error {
	rows, err := s.repo.ListPublished(ctx)
	if err != nil {
		return fmt.Errorf("list published: %w", err)
	}
	for i := range rows {
		wf := &rows[i]
		doc, err := s.parseValidate(wf.DSLYAML, wf.Slug)
		if err != nil {
			slog.Warn("workflow: republish skip — validate failed",
				"slug", wf.Slug, "tenant", wf.TenantID, "err", err)
			continue
		}
		if err := s.registerTool(wf.TenantID, wf, doc); err != nil {
			slog.Warn("workflow: republish skip — bus register failed",
				"slug", wf.Slug, "tenant", wf.TenantID, "err", err)
		}
	}
	return nil
}

// ----------------- helpers -----------------

// loadParsed reads, parses, and validates the DSL for a workflow. Used by
// Publish + Invoke so any DSL corruption surfaces as a clean error before we
// touch external state.
func (s *Service) loadParsed(ctx context.Context, tenantID uuid.UUID, slug string) (*Workflow, *WorkflowDoc, error) {
	wf, err := s.repo.Get(ctx, tenantID, slug)
	if err != nil {
		return nil, nil, err
	}
	doc, err := s.parseValidate(wf.DSLYAML, slug)
	if err != nil {
		return nil, nil, err
	}
	return wf, doc, nil
}

// parseValidate wraps Parse + Validate; slug is asserted to match doc.ID so
// admins can't smuggle a DSL whose internal id differs from the URL slug.
func (s *Service) parseValidate(dsl, slug string) (*WorkflowDoc, error) {
	doc, err := Parse(dsl)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if err := Validate(doc, s.engine.Config()); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	if doc.ID != slug {
		return nil, fmt.Errorf("dsl id %q does not match slug %q", doc.ID, slug)
	}
	return doc, nil
}

// registerTool builds a WorkflowTool from a parsed doc and swaps it into the
// Bus. Idempotent: Unregister first, ignore "not registered", then Register.
func (s *Service) registerTool(tenantID uuid.UUID, wf *Workflow, doc *WorkflowDoc) error {
	if s.bus == nil {
		return nil
	}
	schema, err := SchemaFromInputs(doc.Inputs)
	if err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	tool := &WorkflowTool{
		svc:      s,
		tenantID: tenantID,
		slug:     wf.Slug,
		descr:    descrOrDefault(wf, doc),
		schema:   schema,
	}
	_ = s.bus.Unregister(tool.Name()) // best effort; first publish has nothing to remove
	return s.bus.Register(tool)
}

func descrOrDefault(wf *Workflow, doc *WorkflowDoc) string {
	if wf.Description != "" {
		return wf.Description
	}
	if doc.Description != "" {
		return doc.Description
	}
	return "已发布的工作流：" + wf.Slug
}

func orEmptyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// ----------------- audit -----------------

func (s *Service) auditAdmin(tenantID uuid.UUID, slug, action string, meta map[string]any) {
	if s.audit == nil {
		return
	}
	tid := tenantID
	audit.Detached(s.audit, audit.Entry{
		TenantID: &tid, Action: action, Target: slug, Metadata: meta,
	}, nil)
}

func (s *Service) auditInvokeStart(tenantID, userID uuid.UUID, slug string, runID uuid.UUID, dryRun bool, inputs map[string]any) {
	if s.audit == nil {
		return
	}
	tid := tenantID
	uid := userID
	keys := make([]string, 0, len(inputs))
	for k := range inputs {
		keys = append(keys, k)
	}
	audit.Detached(s.audit, audit.Entry{
		TenantID: &tid, UserID: &uid,
		Action: "workflow.invoke.start", Target: slug,
		Metadata: map[string]any{
			"run_id":      runID.String(),
			"dry_run":     dryRun,
			"inputs_keys": keys,
		},
	}, nil)
}

func (s *Service) auditInvokeComplete(tenantID, userID uuid.UUID, slug string, runID uuid.UUID, dryRun bool, res ExecutionResult, dur time.Duration) {
	if s.audit == nil {
		return
	}
	tid := tenantID
	uid := userID
	audit.Detached(s.audit, audit.Entry{
		TenantID: &tid, UserID: &uid,
		Action: "workflow.invoke.complete", Target: slug,
		DurationMS: int(dur.Milliseconds()),
		Metadata: map[string]any{
			"run_id":   runID.String(),
			"dry_run":  dryRun,
			"status":   res.Status,
			"steps":    res.Steps,
			"has_err":  res.Error != "",
		},
	}, nil)
}

// SchemaFromInputs builds a JSON Schema "object" descriptor from the DSL
// inputs block so the published workflow.<slug> tool can advertise it to LLMs
// the same way other Bus tools do.
func SchemaFromInputs(inputs map[string]InputSpec) (json.RawMessage, error) {
	properties := map[string]any{}
	required := []string{}
	for k, spec := range inputs {
		prop := map[string]any{}
		if spec.Type != "" {
			prop["type"] = jsonSchemaType(spec.Type)
		}
		// User-supplied raw schema fragments override (e.g. items, properties)
		for k2, v := range spec.Schema {
			prop[k2] = v
		}
		properties[k] = prop
		if spec.Default == nil {
			required = append(required, k)
		}
	}
	out := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	// Always declare additionalProperties=true so unknown inputs flow through
	// (workflows can read them via ${inputs.<key>} even without a declaration).
	out["additionalProperties"] = true
	if len(required) > 0 {
		out["required"] = required
	}
	return json.Marshal(out)
}

// jsonSchemaType maps the DSL's compact type names to canonical JSON-schema
// type strings; falls back to "string" for unknown so the schema stays valid.
func jsonSchemaType(t string) string {
	switch t {
	case "int":
		return "integer"
	case "number", "float":
		return "number"
	case "bool", "boolean":
		return "boolean"
	case "object":
		return "object"
	case "array":
		return "array"
	default:
		return "string"
	}
}

// ensure errors import stays meaningful even if Repo.Get error paths change.
var _ = errors.Is
