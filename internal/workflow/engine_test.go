package workflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/workflow"
)

// mockRunner is the test seam used by every engine test in this file. Each
// call is appended to calls so dispatch order can be asserted; responses can
// be primed per-tool. delay simulates an in-flight tool that should respect
// context cancellation.
type mockRunner struct {
	mu        sync.Mutex
	calls     []mockCall
	responses map[string]mockResponse
	mutating  map[string]bool
}

type mockCall struct {
	Name string
	Args json.RawMessage
}

type mockResponse struct {
	out   json.RawMessage
	err   error
	delay time.Duration
}

func newMockRunner() *mockRunner {
	return &mockRunner{
		responses: map[string]mockResponse{},
		mutating:  map[string]bool{},
	}
}

func (m *mockRunner) InvokeTool(ctx context.Context, _, _ uuid.UUID, name string, args json.RawMessage) (json.RawMessage, error) {
	m.mu.Lock()
	m.calls = append(m.calls, mockCall{Name: name, Args: append(json.RawMessage(nil), args...)})
	resp := m.responses[name]
	m.mu.Unlock()
	if resp.delay > 0 {
		select {
		case <-time.After(resp.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return resp.out, resp.err
}

func (m *mockRunner) IsMutating(name string) bool { return m.mutating[name] }

func (m *mockRunner) callNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calls))
	for i, c := range m.calls {
		out[i] = c.Name
	}
	return out
}

func mustParseValidate(t *testing.T, src string) *workflow.WorkflowDoc {
	t.Helper()
	doc, err := workflow.Parse(src)
	require.NoError(t, err)
	require.NoError(t, workflow.Validate(doc, workflow.DefaultConfig()))
	return doc
}

func newExec(slug string, inputs map[string]any, dry bool) workflow.ExecutionInput {
	return workflow.ExecutionInput{
		Slug:     slug,
		TenantID: uuid.New(),
		UserID:   uuid.New(),
		Inputs:   inputs,
		DryRun:   dry,
	}
}

// TestEngine_ToolNode_HappyPath exercises the most common shape: an assign
// sets a var, a tool node reads it via ${...}, and outputs binds the tool's
// JSON output through ${steps.<id>.output.<path>}.
func TestEngine_ToolNode_HappyPath(t *testing.T) {
	doc := mustParseValidate(t, `
id: hello
name: Hello
inputs:
  name: { type: string }
steps:
  - id: build_msg
    assign:
      greeting: "Hi ${inputs.name}"
  - id: shout
    use: echo
    args:
      text: ${vars.greeting}
outputs:
  said: ${steps.shout.output.echoed}
`)
	mr := newMockRunner()
	mr.responses["echo"] = mockResponse{out: json.RawMessage(`{"echoed":"Hi Bob"}`)}
	eng := workflow.NewEngine(mr, workflow.DefaultConfig())

	res, err := eng.Execute(context.Background(), doc, newExec("hello", map[string]any{"name": "Bob"}, false))
	require.NoError(t, err)
	require.Equal(t, workflow.StatusOK, res.Status)
	require.Equal(t, "Hi Bob", res.Outputs["said"])
	require.Equal(t, []string{"echo"}, mr.callNames())
	require.JSONEq(t, `{"text":"Hi Bob"}`, string(mr.calls[0].Args))
}

// TestEngine_ToolError_FailFast asserts on_error=fail (the default) terminates
// the run and produces StatusFailed with the wrapped error visible.
func TestEngine_ToolError_FailFast(t *testing.T) {
	doc := mustParseValidate(t, `
id: erra
name: Err
steps:
  - id: boom
    use: explode
    args: {}
`)
	mr := newMockRunner()
	mr.responses["explode"] = mockResponse{err: errors.New("kaboom")}
	eng := workflow.NewEngine(mr, workflow.DefaultConfig())

	res, _ := eng.Execute(context.Background(), doc, newExec("erra", nil, false))
	require.Equal(t, workflow.StatusFailed, res.Status)
	require.Contains(t, res.Error, "kaboom")
}

// TestEngine_ToolError_Continue checks that on_error=continue captures the
// error into ${steps.<id>.error} but lets the workflow proceed.
func TestEngine_ToolError_Continue(t *testing.T) {
	doc := mustParseValidate(t, `
id: cont
name: Continue
steps:
  - id: bad
    use: explode
    on_error: continue
    args: {}
  - id: after
    assign:
      saw: ${steps.bad.error}
outputs:
  saw: ${vars.saw}
`)
	mr := newMockRunner()
	mr.responses["explode"] = mockResponse{err: errors.New("kaboom")}
	eng := workflow.NewEngine(mr, workflow.DefaultConfig())

	res, _ := eng.Execute(context.Background(), doc, newExec("cont", nil, false))
	require.Equal(t, workflow.StatusOK, res.Status)
	require.Contains(t, res.Outputs["saw"], "kaboom")
}

// TestEngine_StepTimeout enforces per-step timeout via context.WithTimeout
// inside runTool; the resulting error surfaces as StatusFailed.
func TestEngine_StepTimeout(t *testing.T) {
	doc := mustParseValidate(t, `
id: slow
name: Slow
steps:
  - id: hang
    use: sleeper
    timeout: 30ms
    args: {}
`)
	mr := newMockRunner()
	mr.responses["sleeper"] = mockResponse{delay: 200 * time.Millisecond, out: json.RawMessage(`{}`)}
	eng := workflow.NewEngine(mr, workflow.DefaultConfig())

	start := time.Now()
	res, _ := eng.Execute(context.Background(), doc, newExec("slow", nil, false))
	elapsed := time.Since(start)
	require.Equal(t, workflow.StatusFailed, res.Status)
	require.Less(t, elapsed, 150*time.Millisecond, "should have aborted near timeout, not waited full delay")
}

// TestEngine_IfBranch_TrueAndFalse runs the same DSL twice with different
// inputs to verify if dispatches into Then on truthy and Else on falsy.
func TestEngine_IfBranch_TrueAndFalse(t *testing.T) {
	doc := mustParseValidate(t, `
id: gate
name: Gate
inputs:
  ok: { type: bool }
steps:
  - id: pick
    if: "${inputs.ok}"
    then:
      - id: yes_branch
        use: yes_tool
        args: {}
    else:
      - id: no_branch
        use: no_tool
        args: {}
`)
	for _, tc := range []struct {
		name     string
		input    bool
		expected string
	}{
		{"true", true, "yes_tool"},
		{"false", false, "no_tool"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mr := newMockRunner()
			mr.responses["yes_tool"] = mockResponse{out: json.RawMessage(`{}`)}
			mr.responses["no_tool"] = mockResponse{out: json.RawMessage(`{}`)}
			eng := workflow.NewEngine(mr, workflow.DefaultConfig())
			res, _ := eng.Execute(context.Background(), doc, newExec("gate", map[string]any{"ok": tc.input}, false))
			require.Equal(t, workflow.StatusOK, res.Status, "error=%s", res.Error)
			require.Equal(t, []string{tc.expected}, mr.callNames())
		})
	}
}

// TestEngine_Foreach iterates a list input, asserting each iteration's tool
// call receives the bound vars.<as> value.
func TestEngine_Foreach(t *testing.T) {
	doc := mustParseValidate(t, `
id: each
name: Each
inputs:
  items: { type: array }
steps:
  - id: loop
    foreach: "${inputs.items}"
    as: item
    steps:
      - id: visit
        use: visitor
        args:
          arg: "${vars.item}"
`)
	mr := newMockRunner()
	mr.responses["visitor"] = mockResponse{out: json.RawMessage(`{}`)}
	eng := workflow.NewEngine(mr, workflow.DefaultConfig())

	res, _ := eng.Execute(context.Background(), doc, newExec("each", map[string]any{"items": []any{"a", "b", "c"}}, false))
	require.Equal(t, workflow.StatusOK, res.Status)
	require.Len(t, mr.calls, 3)
	require.JSONEq(t, `{"arg":"a"}`, string(mr.calls[0].Args))
	require.JSONEq(t, `{"arg":"b"}`, string(mr.calls[1].Args))
	require.JSONEq(t, `{"arg":"c"}`, string(mr.calls[2].Args))
	require.Equal(t, 3, res.Steps, "3 visit nodes; foreach itself is control-flow and doesn't count")
}

// TestEngine_Parallel_HappyPath checks that two branches both run and the
// workflow waits for both. We don't assert call ORDER because parallel.
func TestEngine_Parallel_HappyPath(t *testing.T) {
	doc := mustParseValidate(t, `
id: par
name: Par
steps:
  - id: fan
    parallel:
      - - id: a1
          use: alpha
          args: {}
      - - id: b1
          use: beta
          args: {}
`)
	mr := newMockRunner()
	mr.responses["alpha"] = mockResponse{out: json.RawMessage(`{}`)}
	mr.responses["beta"] = mockResponse{out: json.RawMessage(`{}`)}
	eng := workflow.NewEngine(mr, workflow.DefaultConfig())

	res, _ := eng.Execute(context.Background(), doc, newExec("par", nil, false))
	require.Equal(t, workflow.StatusOK, res.Status)
	names := mr.callNames()
	require.ElementsMatch(t, []string{"alpha", "beta"}, names)
}

// TestEngine_Parallel_FirstErrorCancelsSiblings primes branch "fast" to error
// quickly while "slow" tries to sleep; the slow branch's context should be
// canceled by the errgroup, so the run finishes well before slow's full delay.
func TestEngine_Parallel_FirstErrorCancelsSiblings(t *testing.T) {
	doc := mustParseValidate(t, `
id: parerr
name: ParErr
steps:
  - id: fan
    parallel:
      - - id: fast
          use: fast
          args: {}
      - - id: slow
          use: slow
          timeout: 5s
          args: {}
`)
	mr := newMockRunner()
	mr.responses["fast"] = mockResponse{err: errors.New("blew up")}
	mr.responses["slow"] = mockResponse{delay: 500 * time.Millisecond, out: json.RawMessage(`{}`)}
	eng := workflow.NewEngine(mr, workflow.DefaultConfig())

	start := time.Now()
	res, _ := eng.Execute(context.Background(), doc, newExec("parerr", nil, false))
	elapsed := time.Since(start)
	require.Equal(t, workflow.StatusFailed, res.Status)
	require.Contains(t, res.Error, "blew up")
	require.Less(t, elapsed, 400*time.Millisecond, "sibling cancellation should have shortened the run")
}

// TestEngine_Wait_RespectsCtx ensures a wait node returns promptly when the
// outer context is canceled; the result is StatusCancelled.
func TestEngine_Wait_RespectsCtx(t *testing.T) {
	doc := mustParseValidate(t, `
id: nap
name: Nap
steps:
  - id: sleep
    wait: 5s
`)
	eng := workflow.NewEngine(newMockRunner(), workflow.DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	res, _ := eng.Execute(ctx, doc, newExec("nap", nil, false))
	elapsed := time.Since(start)
	require.Equal(t, workflow.StatusCancelled, res.Status)
	require.Less(t, elapsed, 200*time.Millisecond)
}

// TestEngine_DryRun_MutatingMocked locks the v1 Dry-Run contract: mutating
// tools are short-circuited with a stable envelope and IsMutating()==false
// tools still go through the runner.
func TestEngine_DryRun_MutatingMocked(t *testing.T) {
	doc := mustParseValidate(t, `
id: dry
name: Dry
steps:
  - id: write
    use: fs.write
    args:
      path: /tmp/x
      content: hi
  - id: read_after
    use: fs.read
    args:
      path: /tmp/x
`)
	mr := newMockRunner()
	mr.mutating["fs.write"] = true
	// fs.read should still be called for real even in dry-run.
	mr.responses["fs.read"] = mockResponse{out: json.RawMessage(`{"content":"hi"}`)}
	eng := workflow.NewEngine(mr, workflow.DefaultConfig())

	res, _ := eng.Execute(context.Background(), doc, newExec("dry", nil, true))
	require.Equal(t, workflow.StatusOK, res.Status)
	require.Equal(t, []string{"fs.read"}, mr.callNames(), "fs.write must not hit the runner under dry_run")
}

// TestEngine_DryRun_NonMutatingRunsForReal verifies that under DryRun a tool
// that does NOT advertise IsMutating still gets dispatched normally.
func TestEngine_DryRun_NonMutatingRunsForReal(t *testing.T) {
	doc := mustParseValidate(t, `
id: dryro
name: DryReadOnly
steps:
  - id: r
    use: fs.read
    args: { path: /etc/hosts }
`)
	mr := newMockRunner()
	mr.responses["fs.read"] = mockResponse{out: json.RawMessage(`{"content":"x"}`)}
	eng := workflow.NewEngine(mr, workflow.DefaultConfig())
	res, _ := eng.Execute(context.Background(), doc, newExec("dryro", nil, true))
	require.Equal(t, workflow.StatusOK, res.Status)
	require.Equal(t, []string{"fs.read"}, mr.callNames())
}

// TestEngine_MaxStepsCap caps MaxSteps below what the workflow needs, so the
// foreach body trips the limit and the engine reports StatusMaxSteps.
func TestEngine_MaxStepsCap(t *testing.T) {
	doc := mustParseValidate(t, `
id: cap
name: Cap
inputs:
  list: { type: array }
steps:
  - id: loop
    foreach: "${inputs.list}"
    as: x
    steps:
      - id: tick
        use: noop
        args:
          v: "${vars.x}"
`)
	mr := newMockRunner()
	mr.responses["noop"] = mockResponse{out: json.RawMessage(`{}`)}
	cfg := workflow.DefaultConfig()
	cfg.MaxSteps = 2
	eng := workflow.NewEngine(mr, cfg)

	items := []any{1, 2, 3, 4, 5}
	res, _ := eng.Execute(context.Background(), doc, newExec("cap", map[string]any{"list": items}, false))
	require.Equal(t, workflow.StatusMaxSteps, res.Status)
	require.LessOrEqual(t, len(mr.calls), 2)
}

// TestEngine_StepCount_Accounting confirms leaf steps (tool/assign/wait) count
// while control-flow wrappers (if/foreach/parallel) do not.
func TestEngine_StepCount_Accounting(t *testing.T) {
	doc := mustParseValidate(t, `
id: cnt
name: Count
steps:
  - id: a
    assign:
      x: 1
  - id: gate
    if: "true"
    then:
      - id: b
        assign:
          y: 2
  - id: pause
    wait: 1ms
`)
	eng := workflow.NewEngine(newMockRunner(), workflow.DefaultConfig())
	res, _ := eng.Execute(context.Background(), doc, newExec("cnt", nil, false))
	require.Equal(t, workflow.StatusOK, res.Status)
	require.Equal(t, 3, res.Steps, "2 assigns + 1 wait; if doesn't count")
}

// TestEngine_OutputsExpression_PreservesType verifies that a single
// ${path} output template keeps its native type (here: a JSON number).
func TestEngine_OutputsExpression_PreservesType(t *testing.T) {
	doc := mustParseValidate(t, `
id: typ
name: Type
steps:
  - id: x
    use: producer
    args: {}
outputs:
  count: ${steps.x.output.n}
`)
	mr := newMockRunner()
	mr.responses["producer"] = mockResponse{out: json.RawMessage(`{"n":42}`)}
	eng := workflow.NewEngine(mr, workflow.DefaultConfig())
	res, _ := eng.Execute(context.Background(), doc, newExec("typ", nil, false))
	require.Equal(t, workflow.StatusOK, res.Status)
	// JSON unmarshal into any → float64
	got, ok := res.Outputs["count"].(float64)
	require.True(t, ok, "want float64, got %T", res.Outputs["count"])
	require.Equal(t, 42.0, got)
}

// TestEngine_UnknownStepKind verifies that a step with an unrecognized Kind
// (impossible via Parse, but guards against future refactors) fails clean.
func TestEngine_UnknownStepKind(t *testing.T) {
	doc := &workflow.WorkflowDoc{
		ID:   "weird",
		Name: "Weird",
		Steps: []workflow.Step{
			{ID: "x", Kind: workflow.NodeKind("bogus")},
		},
	}
	eng := workflow.NewEngine(newMockRunner(), workflow.DefaultConfig())
	res, _ := eng.Execute(context.Background(), doc, newExec("weird", nil, false))
	require.Equal(t, workflow.StatusFailed, res.Status)
	require.Contains(t, res.Error, "unknown kind")
}

// TestEngine_NilDoc isolates the one Go-error return path: nil document.
func TestEngine_NilDoc(t *testing.T) {
	eng := workflow.NewEngine(newMockRunner(), workflow.DefaultConfig())
	res, err := eng.Execute(context.Background(), nil, newExec("nope", nil, false))
	require.NoError(t, err)
	require.Equal(t, workflow.StatusFailed, res.Status)
}

// Sanity test: a tool whose response is non-JSON text gets surfaced as a
// plain string into ${steps.<id>.output} so downstream templates can read it.
func TestEngine_ToolReturnsNonJSON(t *testing.T) {
	doc := mustParseValidate(t, `
id: nj
name: NonJSON
steps:
  - id: raw
    use: emit
    args: {}
outputs:
  v: ${steps.raw.output}
`)
	mr := newMockRunner()
	mr.responses["emit"] = mockResponse{out: json.RawMessage("not-json")}
	eng := workflow.NewEngine(mr, workflow.DefaultConfig())
	res, _ := eng.Execute(context.Background(), doc, newExec("nj", nil, false))
	require.Equal(t, workflow.StatusOK, res.Status)
	require.Equal(t, "not-json", res.Outputs["v"])
}

// ensure unused import guard for fmt if reformulations drop usages
var _ = fmt.Sprint
