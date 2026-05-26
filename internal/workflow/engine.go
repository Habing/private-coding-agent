package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"github.com/yourorg/private-coding-agent/internal/workflow/expr"
)

var tracer = otel.Tracer("internal/workflow")

// errMaxSteps is the sentinel that bubbles up from runStep when the per-run
// MaxSteps cap is hit. Execute maps it to StatusMaxSteps; nothing else in the
// engine matches against the value, so it stays unexported.
var errMaxSteps = errors.New("workflow: max steps exceeded")

// StepRunner is the seam between the Engine and the Tool Bus. Production wires
// service.go's BusStepRunner; tests inject a mock to assert dispatch order and
// the Dry-Run mock contract without spinning a real Bus.
type StepRunner interface {
	InvokeTool(ctx context.Context, tenantID, userID uuid.UUID,
		name string, args json.RawMessage) (json.RawMessage, error)
	IsMutating(name string) bool
}

// Engine executes a parsed WorkflowDoc against a StepRunner. One Engine is
// shared across calls; each Execute keeps per-run state in a local runState so
// concurrent invocations cannot stomp each other.
type Engine struct {
	runner StepRunner
	cfg    Config
}

// NewEngine wires a runner and config. Zero-value Config falls back to
// DefaultConfig() so callers that don't care about caps get safe defaults.
func NewEngine(runner StepRunner, cfg Config) *Engine {
	if cfg.MaxSteps == 0 {
		cfg = DefaultConfig()
	}
	return &Engine{runner: runner, cfg: cfg}
}

// Config returns the engine's bounds. Service reuses these caps when
// re-validating DSL on Publish/Invoke so a single tunable controls both layers.
func (e *Engine) Config() Config { return e.cfg }

// Execute walks the DSL tree. It returns a populated ExecutionResult under all
// non-system failure modes (tool error, expression error, MaxSteps cap, parent
// ctx timeout); the second return is reserved for "engine could not even try"
// situations (nil doc) and currently always returns nil so Service.Invoke can
// rely on the result + status being meaningful.
func (e *Engine) Execute(parentCtx context.Context, doc *WorkflowDoc, in ExecutionInput) (ExecutionResult, error) {
	if doc == nil {
		return ExecutionResult{Status: StatusFailed, Error: "nil doc"}, nil
	}

	ctx, span := tracer.Start(parentCtx, "workflow.execute",
		trace.WithAttributes(
			attribute.String("workflow.slug", in.Slug),
			attribute.Bool("workflow.dry_run", in.DryRun),
		))
	defer span.End()

	st := newRunState(doc, in, e.cfg)
	runErr := e.runSteps(ctx, st, doc.Steps)

	res := ExecutionResult{Steps: st.stepCount}
	switch {
	case runErr == nil:
		outs, err := st.resolveOutputs(doc.Outputs)
		if err != nil {
			res.Status = StatusFailed
			res.Error = "outputs: " + err.Error()
		} else {
			res.Outputs = outs
			res.Status = StatusOK
		}
	case errors.Is(runErr, errMaxSteps):
		res.Status = StatusMaxSteps
		res.Error = runErr.Error()
	case errors.Is(parentCtx.Err(), context.DeadlineExceeded):
		res.Status = StatusTimeout
		res.Error = runErr.Error()
	case errors.Is(parentCtx.Err(), context.Canceled):
		res.Status = StatusCancelled
		res.Error = runErr.Error()
	default:
		res.Status = StatusFailed
		res.Error = runErr.Error()
	}
	span.SetAttributes(attribute.String("workflow.status", res.Status),
		attribute.Int("workflow.steps", res.Steps))
	return res, nil
}

func (e *Engine) runSteps(ctx context.Context, st *runState, steps []Step) error {
	for i := range steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.runStep(ctx, st, &steps[i]); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) runStep(ctx context.Context, st *runState, s *Step) error {
	if isLeafKind(s.Kind) {
		if !st.incSteps() {
			return errMaxSteps
		}
	}
	if st.in.StepEvents != nil {
		ev := StepEvent{
			Kind:      "step_start",
			StepID:    s.ID,
			StepKind:  s.Kind,
			Tool:      s.Use,
			Timestamp: time.Now(),
		}
		// Block briefly so SSE/UI can highlight each step; channel is buffered in invokeStream.
		select {
		case st.in.StepEvents <- ev:
		case <-time.After(2 * time.Second):
			// drop only if receiver is stuck
		}
	}
	ctx, span := tracer.Start(ctx, "workflow.step",
		trace.WithAttributes(
			attribute.String("step.id", s.ID),
			attribute.String("step.kind", string(s.Kind)),
			attribute.Bool("step.dry_run", st.in.DryRun),
		))
	defer span.End()

	switch s.Kind {
	case NodeTool:
		err := e.runTool(ctx, st, s)
		e.emitStepComplete(st, s, err)
		return err
	case NodeAssign:
		err := e.runAssign(st, s)
		e.emitStepComplete(st, s, err)
		return err
	case NodeIf:
		err := e.runIf(ctx, st, s)
		e.emitStepComplete(st, s, err)
		return err
	case NodeForeach:
		err := e.runForeach(ctx, st, s)
		e.emitStepComplete(st, s, err)
		return err
	case NodeParallel:
		err := e.runParallel(ctx, st, s)
		e.emitStepComplete(st, s, err)
		return err
	case NodeWait:
		err := e.runWait(ctx, s)
		e.emitStepComplete(st, s, err)
		return err
	default:
		err := fmt.Errorf("step %s: unknown kind %q", s.ID, s.Kind)
		e.emitStepComplete(st, s, err)
		return err
	}
}

func (e *Engine) emitStepComplete(st *runState, s *Step, err error) {
	if st == nil || st.in.StepEvents == nil {
		return
	}
	status := "ok"
	var msg string
	if err != nil {
		status = "error"
		msg = err.Error()
	}
	ev := StepEvent{
		Kind:      "step_complete",
		StepID:    s.ID,
		StepKind:  s.Kind,
		Tool:      s.Use,
		Status:    status,
		Error:     msg,
		Timestamp: time.Now(),
	}
	if err == nil {
		if r, ok := st.stepResult(s.ID); ok {
			ev.Output = r.Output
		}
	}
	select {
	case st.in.StepEvents <- ev:
	case <-time.After(2 * time.Second):
		// drop only if receiver is stuck
	}
}

// runTool resolves args, dispatches via StepRunner (or short-circuits with a
// mock envelope during Dry-Run when IsMutating==true), records the result into
// the per-run scope so downstream ${steps.<id>.output...} works.
func (e *Engine) runTool(ctx context.Context, st *runState, s *Step) error {
	args, err := st.resolveArgs(s.Args)
	if err != nil {
		return e.recordStepErr(st, s, fmt.Errorf("step %s: %w", s.ID, err))
	}

	if st.in.DryRun && e.runner.IsMutating(s.Use) {
		envelope := map[string]any{
			"dry_run": true,
			"tool":    s.Use,
			"input":   args,
		}
		st.setStepResult(s.ID, expr.StepResult{Output: envelope})
		return nil
	}

	inputJSON, err := json.Marshal(args)
	if err != nil {
		return e.recordStepErr(st, s, fmt.Errorf("step %s: marshal args: %w", s.ID, err))
	}

	timeout := s.Timeout
	if timeout == 0 {
		timeout = e.cfg.DefaultStepTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	outJSON, callErr := e.runner.InvokeTool(callCtx, st.in.TenantID, st.in.UserID, s.Use, inputJSON)
	if callErr != nil {
		return e.recordStepErr(st, s, fmt.Errorf("step %s: %w", s.ID, callErr))
	}

	var outAny any
	if len(outJSON) > 0 {
		if err := json.Unmarshal(outJSON, &outAny); err != nil {
			// Tool returned non-JSON — surface as opaque string so workflows
			// can still bind it through ${steps.<id>.output}.
			outAny = string(outJSON)
		}
	}
	st.setStepResult(s.ID, expr.StepResult{Output: outAny})
	return nil
}

// recordStepErr writes the error into the step record so expressions can read
// ${steps.<id>.error}. on_error=continue stops propagation here; otherwise the
// error bubbles out.
func (e *Engine) recordStepErr(st *runState, s *Step, err error) error {
	st.setStepResult(s.ID, expr.StepResult{Error: err.Error()})
	if s.OnError == OnErrorContinue {
		return nil
	}
	return err
}

func (e *Engine) runAssign(st *runState, s *Step) error {
	for k, exprStr := range s.Assign {
		v, err := st.resolve(exprStr)
		if err != nil {
			return fmt.Errorf("step %s.assign.%s: %w", s.ID, k, err)
		}
		st.setVar(k, v)
	}
	return nil
}

// runIf resolves the `if:` template (users write `if: ${expr}` so a raw
// EvalBool that expects "expr" would mis-parse the ${} wrapper) and then
// coerces the resolved value to bool using the canonical Truthy rules.
func (e *Engine) runIf(ctx context.Context, st *runState, s *Step) error {
	v, err := st.resolve(s.If)
	if err != nil {
		return fmt.Errorf("step %s.if: %w", s.ID, err)
	}
	if expr.Truthy(v) {
		return e.runSteps(ctx, st, s.Then)
	}
	return e.runSteps(ctx, st, s.Else)
}

// runForeach evaluates the source expression to a list and iterates serially,
// binding each item to vars[s.As]. Parallel iteration is intentionally out of
// scope for v1 — users can wrap inner body in a `parallel:` block.
func (e *Engine) runForeach(ctx context.Context, st *runState, s *Step) error {
	src, err := st.resolve(s.Foreach)
	if err != nil {
		return fmt.Errorf("step %s.foreach: %w", s.ID, err)
	}
	list, ok := toList(src)
	if !ok {
		return fmt.Errorf("step %s.foreach: expected list, got %T", s.ID, src)
	}
	for _, item := range list {
		if err := ctx.Err(); err != nil {
			return err
		}
		st.setVar(s.As, item)
		if err := e.runSteps(ctx, st, s.Steps); err != nil {
			return err
		}
	}
	return nil
}

// runParallel uses errgroup so the first error cancels siblings and Wait()
// returns that original error. The shared runState is locked internally by
// each helper, so the branches may freely read/write vars (with the documented
// caveat that concurrent assigns to the same key race).
func (e *Engine) runParallel(ctx context.Context, st *runState, s *Step) error {
	g, gctx := errgroup.WithContext(ctx)
	for _, branch := range s.Parallel {
		branch := branch
		g.Go(func() error {
			return e.runSteps(gctx, st, branch)
		})
	}
	return g.Wait()
}

func (e *Engine) runWait(ctx context.Context, s *Step) error {
	timer := time.NewTimer(s.WaitDur)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func isLeafKind(k NodeKind) bool {
	switch k {
	case NodeTool, NodeAssign, NodeWait:
		return true
	}
	return false
}

// toList accepts []any or []string (yaml may decode either way depending on
// item type). Returns the items as a []any so the iteration helper is uniform.
func toList(v any) ([]any, bool) {
	switch x := v.(type) {
	case []any:
		return x, true
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out, true
	}
	return nil, false
}

// ------------- runState -------------

// runState is the per-Execute mutable scratchpad. All shared maps are guarded
// by mu so parallel branches don't race; the lock is held only while reading
// or mutating maps, never across user code (tool invocations, time.Sleep).
type runState struct {
	in        ExecutionInput
	cfg       Config
	inputs    map[string]any
	mu        sync.RWMutex
	vars      map[string]any
	steps     map[string]expr.StepResult
	stepCount int
}

func newRunState(doc *WorkflowDoc, in ExecutionInput, cfg Config) *runState {
	return &runState{
		in:     in,
		cfg:    cfg,
		inputs: materializeInputs(doc.Inputs, in.Inputs),
		vars:   map[string]any{},
		steps:  map[string]expr.StepResult{},
	}
}

// materializeInputs merges caller-supplied inputs with DSL defaults. Inputs
// not declared in the DSL pass through unchanged so workflows can accept ad
// hoc keys without re-declaring them; declared inputs without a value fall
// back to Default if one was specified.
func materializeInputs(specs map[string]InputSpec, given map[string]any) map[string]any {
	out := map[string]any{}
	for k, spec := range specs {
		if v, ok := given[k]; ok {
			out[k] = v
			continue
		}
		if spec.Default != nil {
			out[k] = spec.Default
		}
	}
	for k, v := range given {
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	return out
}

func (st *runState) scope() expr.Scope {
	return expr.Scope{
		Inputs: st.inputs,
		Vars:   st.vars,
		Steps:  st.steps,
	}
}

func (st *runState) resolve(tmpl string) (any, error) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return expr.Resolve(tmpl, st.scope())
}

// resolveArgs walks an args map, resolving any string values as templates and
// recursing into nested maps/slices so ${...} can appear at any depth.
func (st *runState) resolveArgs(args map[string]any) (map[string]any, error) {
	if args == nil {
		return nil, nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		rv, err := st.resolveAny(v)
		if err != nil {
			return nil, fmt.Errorf("args.%s: %w", k, err)
		}
		out[k] = rv
	}
	return out, nil
}

func (st *runState) resolveAny(v any) (any, error) {
	switch x := v.(type) {
	case string:
		return st.resolve(x)
	case map[string]any:
		return st.resolveArgs(x)
	case map[any]any:
		m := make(map[string]any, len(x))
		for k, val := range x {
			ks, ok := k.(string)
			if !ok {
				ks = fmt.Sprint(k)
			}
			rv, err := st.resolveAny(val)
			if err != nil {
				return nil, err
			}
			m[ks] = rv
		}
		return m, nil
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			rv, err := st.resolveAny(item)
			if err != nil {
				return nil, err
			}
			out[i] = rv
		}
		return out, nil
	default:
		return v, nil
	}
}

func (st *runState) resolveOutputs(outs map[string]string) (map[string]any, error) {
	if len(outs) == 0 {
		return nil, nil
	}
	res := make(map[string]any, len(outs))
	for k, tmpl := range outs {
		v, err := st.resolve(tmpl)
		if err != nil {
			return nil, fmt.Errorf("outputs.%s: %w", k, err)
		}
		res[k] = v
	}
	return res, nil
}

func (st *runState) setVar(k string, v any) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.vars[k] = v
}

func (st *runState) setStepResult(id string, r expr.StepResult) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.steps[id] = r
}

func (st *runState) stepResult(id string) (expr.StepResult, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	r, ok := st.steps[id]
	return r, ok
}

// incSteps bumps the leaf-node counter and reports whether the cap was hit.
// Returns false the moment the post-increment count exceeds MaxSteps so the
// caller can return errMaxSteps before dispatching the offending step.
func (st *runState) incSteps() bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.stepCount++
	return st.stepCount <= st.cfg.MaxSteps
}
