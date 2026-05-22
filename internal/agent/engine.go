package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/yourorg/private-coding-agent/internal/audit"
	pcametrics "github.com/yourorg/private-coding-agent/internal/metrics"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/orchestrator"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

var tracer trace.Tracer = otel.Tracer("internal/agent")

// Gateway is the subset of *modelgw.Gateway the Engine depends on.
type Gateway interface {
	ChatCompletion(ctx context.Context, tenantID, userID uuid.UUID,
		req modelgw.ChatRequest) (*modelgw.ChatResponse, error)
	ChatCompletionStream(ctx context.Context, tenantID, userID uuid.UUID,
		req modelgw.ChatRequest, yield func(modelgw.ChatStreamChunk) error) error
}

// Bus is the subset of *toolbus.Bus the Engine depends on.
type Bus interface {
	ListTools(ctx context.Context, tenantID uuid.UUID) []toolbus.ToolDef
	Invoke(ctx context.Context, tenantID, userID uuid.UUID,
		toolName string, input json.RawMessage) (json.RawMessage, error)
}

// Engine runs a ReAct loop over a Gateway + Bus pair using a registered Profile.
type Engine struct {
	gw             Gateway
	bus            Bus
	profiles       map[string]Profile
	composer       ContextComposer
	auditSink      audit.Sink
	router         orchestrator.Router
	maxOutputBytes int
}

// NewEngine wires the engine. profiles must contain at least one entry; the
// "coding" profile is used as the default when RunInput.ProfileName is empty.
// composer composes the system-layer prefix; pass NoopComposer{} to preserve
// pre-Slice-12 behavior.
func NewEngine(gw Gateway, bus Bus, profiles map[string]Profile, composer ContextComposer) *Engine {
	if composer == nil {
		composer = NoopComposer{}
	}
	return &Engine{
		gw:             gw,
		bus:            bus,
		profiles:       profiles,
		composer:       composer,
		maxOutputBytes: DefaultMaxToolOutputBytes,
	}
}

// WithAuditSink wires an audit.Sink so the engine records skill.inject
// entries on Runs that produce a non-empty Skill set. Returns the receiver
// for chaining.
func (e *Engine) WithAuditSink(sink audit.Sink) *Engine {
	e.auditSink = sink
	return e
}

// WithRouter wires the Slice 21a orchestration router. When set, the engine
// calls Route once per Run before message assembly; a non-empty
// Decision.Hint is injected as a system message between any skill / memory
// system messages and the user history (preserving Skill priority). Pass
// nil (or skip the call) to disable routing.
func (e *Engine) WithRouter(r orchestrator.Router) *Engine {
	e.router = r
	return e
}

// Profiles returns the registered profiles as a name-sorted slice copy. Used
// by the agent handler to back GET /agent/profiles and by callers wiring
// agent.delegate.
func (e *Engine) Profiles() []Profile {
	out := make([]Profile, 0, len(e.profiles))
	for _, p := range e.profiles {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Run drives the ReAct loop. Each event is emitted via yield(); if yield returns
// an error the loop aborts and returns that error. The final return value is nil
// on a clean stop, or a sentinel error on max steps / LLM failure.
func (e *Engine) Run(ctx context.Context, in RunInput, yield func(Event) error) (runErr error) {
	if len(in.Messages) == 0 {
		return ErrEmptyMessages
	}
	profileName := in.ProfileName
	if profileName == "" {
		profileName = "coding"
	}
	profile, ok := e.profiles[profileName]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownProfile, profileName)
	}
	maxSteps := in.MaxSteps
	if maxSteps <= 0 {
		maxSteps = profile.MaxSteps
	}
	if maxSteps <= 0 {
		maxSteps = 16
	}

	// Propagate per-Run state (sandbox / model / profile / delegate depth) via
	// ctx so engine-internal tools — currently only agent.delegate — can read
	// it without needing the *RunInput. DelegateDepth carries over from the
	// caller's RunCtx; the delegate tool bumps it before invoking Engine.Run
	// for the child, so a depth-1 RunCtx flowing through here means "this is
	// the child Run." Sandbox / Model / ProfileName always come from in.
	parentRC := RunCtxFromCtx(ctx)
	ctx = WithRunCtx(ctx, RunCtx{
		SandboxID:     in.SandboxID,
		Model:         in.Model,
		ProfileName:   profileName,
		DelegateDepth: parentRC.DelegateDepth,
	})

	ctx, runSpan := tracer.Start(ctx, "agent.run",
		trace.WithAttributes(
			attribute.String("agent.model", in.Model),
			attribute.String("agent.profile", profileName),
			attribute.Int("agent.max_steps", maxSteps),
		))
	defer func() {
		if runErr != nil {
			runSpan.RecordError(runErr)
			runSpan.SetStatus(codes.Error, runErr.Error())
		}
		runSpan.End()
	}()

	// Build conversation: composer-built system prefix, then caller messages.
	sysMsgs, meta, err := e.composer.ComposeSystem(ctx, ComposeInput{
		TenantID:          in.TenantID,
		UserID:            in.UserID,
		Profile:           profile,
		RunSkillIDs:       in.SkillIDs,
		SessionSkillIDs:   in.SessionSkillIDs,
		MemorySection:     in.MemorySection,
		MemoryIDs:         in.MemoryIDs,
		MemoryCharCount:   in.MemoryCharCount,
		MemoryTruncated:   in.MemoryTruncated,
	})
	if err != nil {
		return fmt.Errorf("compose system: %w", err)
	}
	if len(meta.MemoryIDs) > 0 {
		e.recordMemoryInject(ctx, in, meta)
	}
	if len(meta.SkillIDs) > 0 {
		runSpan.SetAttributes(
			attribute.StringSlice("agent.skill_ids", meta.SkillIDs),
			attribute.Int("agent.skill_chars", meta.CharCount),
			attribute.Bool("agent.skill_truncated", meta.Truncated),
		)
		e.recordSkillInject(ctx, in, meta)
	}
	// Slice 21a: route the Run through the orchestrator (no-op when nil or
	// disabled). The Decision is computed unconditionally so callers can later
	// emit audit / metric even on no_match; only injection is gated by
	// InjectHintEnabled + non-empty Hint.
	routeDecision := e.routeIfEnabled(ctx, profileName, in.Messages)
	messages := make([]modelgw.ChatMessage, 0, len(sysMsgs)+len(in.Messages)+2)
	if in.SandboxID != uuid.Nil {
		messages = append(messages, modelgw.ChatMessage{
			Role:    modelgw.RoleSystem,
			Content: fmt.Sprintf("Current sandbox_id: %s", in.SandboxID),
		})
	}
	messages = append(messages, sysMsgs...)
	if routeDecision.Hint != "" && e.router != nil && e.router.InjectHintEnabled() {
		messages = append(messages, modelgw.ChatMessage{
			Role:    modelgw.RoleSystem,
			Content: routeDecision.Hint,
		})
	}
	messages = append(messages, in.Messages...)

	// Resolve and convert tools once per Run.
	busTools := e.bus.ListTools(ctx, in.TenantID)
	modelTools := BuildModelTools(busTools, profile.ToolAllowlist)
	allowed := map[string]struct{}{}
	for _, n := range profile.ToolAllowlist {
		allowed[n] = struct{}{}
	}

	for step := 1; step <= maxSteps; step++ {
		stepCtx, stepSpan := tracer.Start(ctx, "agent.step",
			trace.WithAttributes(attribute.Int("agent.step_index", step)))

		req := modelgw.ChatRequest{
			Model:    in.Model,
			Messages: messages,
			Tools:    modelTools,
		}
		var accum streamAccum
		err := e.gw.ChatCompletionStream(stepCtx, in.TenantID, in.UserID, req,
			func(chunk modelgw.ChatStreamChunk) error {
				if delta := accum.apply(chunk); delta != "" {
					if err := yield(Event{
						Kind: EventAssistantDelta,
						Step: step,
						Text: delta,
					}); err != nil {
						return err
					}
				}
				return nil
			})
		if err != nil {
			_ = yield(Event{Kind: EventError, Step: step, Text: err.Error(), FinishReason: "llm_error"})
			stepSpan.RecordError(err)
			stepSpan.SetStatus(codes.Error, err.Error())
			stepSpan.End()
			return fmt.Errorf("%w: %v", ErrLLMFailed, err)
		}
		assistant := accum.message()
		finishReason := accum.finishReason
		if finishReason == "" {
			finishReason = "stop"
		}
		stepSpan.SetAttributes(attribute.String("agent.finish_reason", finishReason))
		if err := yield(Event{
			Kind:         EventAssistantMessage,
			Step:         step,
			Text:         assistant.Content,
			ToolCalls:    assistant.ToolCalls,
			FinishReason: finishReason,
		}); err != nil {
			stepSpan.End()
			return err
		}

		if finishReason == "tool_calls" && len(assistant.ToolCalls) > 0 {
			messages = append(messages, assistant)
			for _, call := range assistant.ToolCalls {
				messages = append(messages, e.runToolCall(stepCtx, in, step, call, allowed, yield))
			}
			stepSpan.End()
			continue
		}

		if err := yield(Event{
			Kind:         EventFinal,
			Step:         step,
			Text:         assistant.Content,
			FinishReason: finishReason,
		}); err != nil {
			stepSpan.End()
			return err
		}
		stepSpan.End()
		return nil
	}

	_ = yield(Event{Kind: EventError, Step: maxSteps, Text: ErrMaxStepsExceeded.Error(), FinishReason: "max_steps"})
	return ErrMaxStepsExceeded
}

// runToolCall executes a single tool_call, emits tool_call + tool_result events,
// and returns the role=tool message to append to the conversation (always — even
// on parse / unknown / invocation errors, so the LLM can self-correct).
func (e *Engine) runToolCall(ctx context.Context, in RunInput, step int,
	call modelgw.ToolCall, allowed map[string]struct{}, yield func(Event) error) modelgw.ChatMessage {

	name := call.Function.Name
	args := json.RawMessage(call.Function.Arguments)

	// Validate arguments are JSON; if not, feed the error back to the LLM.
	if len(args) == 0 || !json.Valid(args) {
		errMsg := fmt.Sprintf("invalid tool_call arguments: not valid JSON: %q", call.Function.Arguments)
		_ = yield(Event{
			Kind: EventToolCall, Step: step,
			ToolCallID: call.ID, ToolName: name,
			ToolInput: args,
		})
		_ = yield(Event{
			Kind: EventToolResult, Step: step,
			ToolCallID: call.ID, ToolName: name,
			ToolError: errMsg,
		})
		return toolErrorMessage(call.ID, errMsg)
	}

	if _, ok := allowed[name]; !ok {
		errMsg := fmt.Sprintf("tool %q is not allowed or does not exist", name)
		_ = yield(Event{
			Kind: EventToolCall, Step: step,
			ToolCallID: call.ID, ToolName: name, ToolInput: args,
		})
		_ = yield(Event{
			Kind: EventToolResult, Step: step,
			ToolCallID: call.ID, ToolName: name, ToolError: errMsg,
		})
		return toolErrorMessage(call.ID, errMsg)
	}

	_ = yield(Event{
		Kind: EventToolCall, Step: step,
		ToolCallID: call.ID, ToolName: name, ToolInput: args,
	})

	out, invErr := e.bus.Invoke(ctx, in.TenantID, in.UserID, name, args)
	if invErr != nil {
		errMsg := invErr.Error()
		// Map unknown tool from bus as a non-fatal feedback to LLM too.
		if errors.Is(invErr, toolbus.ErrToolNotFound) {
			errMsg = fmt.Sprintf("tool %q not found", name)
		}
		_ = yield(Event{
			Kind: EventToolResult, Step: step,
			ToolCallID: call.ID, ToolName: name, ToolError: errMsg,
		})
		return toolErrorMessage(call.ID, errMsg)
	}

	truncated, isTrunc := TruncateToolOutput(out, e.maxOutputBytes)
	evt := Event{
		Kind: EventToolResult, Step: step,
		ToolCallID: call.ID, ToolName: name,
		ToolOutput: truncated,
		Truncated:  isTrunc,
	}
	if isTrunc {
		evt.OriginalSize = len(out)
	}
	_ = yield(evt)

	return modelgw.ChatMessage{
		Role:       modelgw.RoleTool,
		ToolCallID: call.ID,
		Name:       name,
		Content:    string(truncated),
	}
}

// routeIfEnabled invokes the orchestrator router (if wired and enabled) and
// returns its Decision. A nil router or disabled engine yields a zero
// Decision. The latest user message is extracted from msgs; if the run has
// no user message yet (delegate sub-Run, recovery flow…), UserContent is "".
func (e *Engine) routeIfEnabled(ctx context.Context, profileName string, msgs []modelgw.ChatMessage) orchestrator.Decision {
	if e.router == nil {
		return orchestrator.Decision{}
	}
	return e.router.Route(ctx, orchestrator.RouteInput{
		Profile:     profileName,
		UserContent: lastUserContent(msgs),
	})
}

// lastUserContent walks msgs in reverse and returns the content of the most
// recent user message. Returns "" if none.
func lastUserContent(msgs []modelgw.ChatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == modelgw.RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}

func (e *Engine) recordSkillInject(ctx context.Context, in RunInput, meta ComposeMeta) {
	if pcametrics.SkillInjectionsTotal != nil {
		pcametrics.SkillInjectionsTotal.Add(ctx, 1,
			metric.WithAttributes(attribute.Bool("truncated", meta.Truncated)))
	}
	if pcametrics.SkillInjectedChars != nil {
		pcametrics.SkillInjectedChars.Record(ctx, int64(meta.CharCount))
	}
	if e.auditSink == nil {
		return
	}
	tid := in.TenantID
	uid := in.UserID
	audit.Detached(e.auditSink, audit.Entry{
		OccurredAt: time.Now(),
		TenantID:   &tid, UserID: &uid,
		Action: "skill.inject",
		Metadata: map[string]any{
			"skill_ids": meta.SkillIDs,
			"chars":     meta.CharCount,
			"truncated": meta.Truncated,
		},
	}, nil)
}

func (e *Engine) recordMemoryInject(ctx context.Context, in RunInput, meta ComposeMeta) {
	if e.auditSink == nil {
		return
	}
	tid := in.TenantID
	uid := in.UserID
	audit.Detached(e.auditSink, audit.Entry{
		OccurredAt: time.Now(),
		TenantID:   &tid, UserID: &uid,
		Action: "memory.inject",
		Metadata: map[string]any{
			"memory_ids": meta.MemoryIDs,
			"chars":      meta.MemoryCharCount,
			"truncated":  meta.MemoryTruncated,
		},
	}, nil)
}

func toolErrorMessage(callID, errMsg string) modelgw.ChatMessage {
	body, _ := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: errMsg})
	return modelgw.ChatMessage{
		Role:       modelgw.RoleTool,
		ToolCallID: callID,
		Content:    string(body),
	}
}
