package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// DelegateTool implements the `agent.delegate` tool. It is registered with
// the Tool Bus by main.go and called by the LLM (through Engine.runToolCall)
// to spin a child Engine.Run under a different Profile. The child Run inherits
// the parent's sandbox_id, tenant, user and model via RunCtx; its assistant
// stream and tool calls are accumulated into a single JSON tool result and
// returned to the parent — they are NOT forwarded to the parent's yield.
type DelegateTool struct {
	engine    *Engine
	profiles  map[string]Profile
	auditSink audit.Sink

	// allowedProfiles is sorted list of known profile names; cached so the
	// schema enum is stable.
	allowedProfiles []string
}

// NewDelegateTool wires the engine reference plus the profile registry the
// tool will consult. profiles must include every profile that callers should
// be allowed to target — typically the same map passed to NewEngine.
func NewDelegateTool(engine *Engine, profiles map[string]Profile, sink audit.Sink) *DelegateTool {
	names := make([]string, 0, len(profiles))
	for n := range profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return &DelegateTool{
		engine:          engine,
		profiles:        profiles,
		auditSink:       sink,
		allowedProfiles: names,
	}
}

// Ensure DelegateTool satisfies toolbus.Tool.
var _ toolbus.Tool = (*DelegateTool)(nil)

const (
	// delegateDefaultMaxSteps is used when input.max_steps is missing or zero.
	delegateDefaultMaxSteps = 4
	// delegateMinMaxSteps / delegateMaxMaxSteps clamp the input value.
	delegateMinMaxSteps = 1
	delegateMaxMaxSteps = 8
	// delegateMaxTaskChars caps the task prompt size — must match the schema.
	delegateMaxTaskChars = 8000
)

// Name returns the canonical tool identifier.
func (t *DelegateTool) Name() string { return "agent.delegate" }

// IsMutating reports true because a child Run can dispatch any tool —
// including mutating ones — and the parent has no static view of which.
// Conservatively marking the whole delegate path as mutating means workflow
// Dry-Run short-circuits it with a mock envelope rather than executing.
func (t *DelegateTool) IsMutating() bool { return true }

// Description is surfaced to the LLM via the Tool Bus list.
func (t *DelegateTool) Description() string {
	return "Delegate a subtask to a different Agent Profile (review / research / workflow-authoring). " +
		"The child Run inherits the parent's sandbox, tenant, user and model; only its final answer is returned. " +
		"Use this when a task naturally belongs to another persona (e.g. code review) rather than your own."
}

// Schema returns the JSON schema for input arguments. The profile enum is
// built from the registry so it stays in sync with NewDelegateTool wiring.
func (t *DelegateTool) Schema() json.RawMessage {
	type prop struct {
		Type        string   `json:"type"`
		Enum        []string `json:"enum,omitempty"`
		MinLength   int      `json:"minLength,omitempty"`
		MaxLength   int      `json:"maxLength,omitempty"`
		Minimum     int      `json:"minimum,omitempty"`
		Maximum     int      `json:"maximum,omitempty"`
		Description string   `json:"description,omitempty"`
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"profile": prop{
				Type:        "string",
				Enum:        t.allowedProfiles,
				Description: "Target profile name. Must be one of the registered profiles other than your own.",
			},
			"task": prop{
				Type:        "string",
				MinLength:   1,
				MaxLength:   delegateMaxTaskChars,
				Description: "Free-form description of the subtask for the child Run.",
			},
			"max_steps": prop{
				Type:        "integer",
				Minimum:     delegateMinMaxSteps,
				Maximum:     delegateMaxMaxSteps,
				Description: "Cap on ReAct iterations inside the child Run; clamped to [1,8], default 4.",
			},
		},
		"required":             []string{"profile", "task"},
		"additionalProperties": false,
	}
	out, _ := json.Marshal(schema)
	return out
}

// delegateInput is the unmarshalled tool input.
type delegateInput struct {
	Profile  string `json:"profile"`
	Task     string `json:"task"`
	MaxSteps int    `json:"max_steps,omitempty"`
}

// delegateOutput is the JSON shape returned to the LLM. status mirrors
// {"ok", "max_steps", "error"} so the parent LLM can branch on the outcome
// without re-parsing prose.
type delegateOutput struct {
	Result       string   `json:"result"`
	SubSteps     int      `json:"sub_steps"`
	Status       string   `json:"status"`
	SubToolCalls []string `json:"sub_tool_calls"`
}

// Invoke runs the delegation. Errors that should be visible to the LLM
// (depth exceeded, unknown profile, empty task) are returned as
// delegateOutput with status="error" rather than as a tool error — the parent
// LLM gets to read and react. Truly fatal internal errors (marshal failure)
// surface as a regular Go error.
func (t *DelegateTool) Invoke(ctx context.Context, tenantID, userID uuid.UUID,
	rawInput json.RawMessage) (json.RawMessage, error) {

	start := time.Now()

	var in delegateInput
	if err := json.Unmarshal(rawInput, &in); err != nil {
		return marshalDelegateErr("invalid input: " + err.Error())
	}
	if in.Task == "" {
		return marshalDelegateErr("task is required")
	}
	if utf8.RuneCountInString(in.Task) > delegateMaxTaskChars {
		return marshalDelegateErr(fmt.Sprintf("task too long (max %d chars)", delegateMaxTaskChars))
	}

	subProfile, ok := t.profiles[in.Profile]
	if !ok {
		return marshalDelegateErr(fmt.Sprintf("unknown profile %q", in.Profile))
	}

	parentRC := RunCtxFromCtx(ctx)
	if parentRC.DelegateDepth >= MaxDelegateDepth {
		return marshalDelegateErr(fmt.Sprintf("delegate depth exceeded (max=%d)", MaxDelegateDepth))
	}

	maxSteps := in.MaxSteps
	if maxSteps <= 0 {
		maxSteps = delegateDefaultMaxSteps
	}
	if maxSteps < delegateMinMaxSteps {
		maxSteps = delegateMinMaxSteps
	}
	if maxSteps > delegateMaxMaxSteps {
		maxSteps = delegateMaxMaxSteps
	}

	t.recordStart(ctx, tenantID, userID, parentRC, in.Profile, len(in.Task), maxSteps)

	childIn := RunInput{
		TenantID:    tenantID,
		UserID:      userID,
		Model:       parentRC.Model,
		ProfileName: in.Profile,
		MaxSteps:    maxSteps,
		SandboxID:   parentRC.SandboxID,
		Messages: []modelgw.ChatMessage{
			{Role: modelgw.RoleUser, Content: in.Task},
		},
	}
	// Inherit the parent's depth tracker bumped by one. Engine.Run will
	// re-stamp ctx using childIn.SandboxID/Model, but DelegateDepth must come
	// from us — RunInput has no field for it on purpose (delegation depth is
	// engine-internal, not a request parameter).
	childCtx := WithRunCtx(ctx, RunCtx{
		SandboxID:     parentRC.SandboxID,
		Model:         parentRC.Model,
		ProfileName:   in.Profile,
		DelegateDepth: parentRC.DelegateDepth + 1,
	})

	var (
		finalText    string
		subSteps     int
		toolCallNames []string
	)
	collect := func(ev Event) error {
		if ev.Step > subSteps {
			subSteps = ev.Step
		}
		switch ev.Kind {
		case EventFinal:
			finalText = ev.Text
		case EventToolCall:
			toolCallNames = append(toolCallNames, ev.ToolName)
		case EventAssistantMessage:
			// If the run ends without an explicit final event (it shouldn't,
			// but be defensive), remember the last assistant content as a
			// fallback for max_steps cases.
			if ev.Text != "" {
				finalText = ev.Text
			}
		}
		return nil
	}

	status := "ok"
	runErr := t.engine.Run(childCtx, childIn, collect)
	if runErr != nil {
		switch {
		case errors.Is(runErr, ErrMaxStepsExceeded):
			status = "max_steps"
			if finalText == "" {
				finalText = ErrMaxStepsExceeded.Error()
			}
		default:
			status = "error"
			finalText = runErr.Error()
		}
	}

	out := delegateOutput{
		Result:       finalText,
		SubSteps:     subSteps,
		Status:       status,
		SubToolCalls: toolCallNames,
	}
	t.recordComplete(ctx, tenantID, userID, parentRC, in.Profile, subSteps, status, toolCallNames, time.Since(start))

	body, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("marshal delegate output: %w", err)
	}
	// Keep the sub-profile reference live to silence lint warnings about the
	// looked-up Profile being unused — the registry hit itself is the
	// validation we needed.
	_ = subProfile
	return body, nil
}

func marshalDelegateErr(msg string) (json.RawMessage, error) {
	body, err := json.Marshal(delegateOutput{
		Result:       msg,
		Status:       "error",
		SubToolCalls: []string{},
	})
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (t *DelegateTool) recordStart(ctx context.Context, tenantID, userID uuid.UUID,
	parentRC RunCtx, subProfile string, taskChars, maxSteps int) {
	if t.auditSink == nil {
		return
	}
	tid := tenantID
	uid := userID
	parentProfile := profileNameFromCtx(ctx)
	audit.Detached(t.auditSink, audit.Entry{
		OccurredAt: time.Now(),
		TenantID:   &tid, UserID: &uid,
		Action: "agent.delegate.start",
		Target: subProfile,
		Metadata: map[string]any{
			"parent_profile": parentProfile,
			"sub_profile":    subProfile,
			"task_chars":     taskChars,
			"max_steps":      maxSteps,
			"depth":          parentRC.DelegateDepth + 1,
		},
	}, nil)
}

func (t *DelegateTool) recordComplete(ctx context.Context, tenantID, userID uuid.UUID,
	parentRC RunCtx, subProfile string, subSteps int, status string,
	toolCalls []string, dur time.Duration) {
	if t.auditSink == nil {
		return
	}
	tid := tenantID
	uid := userID
	parentProfile := profileNameFromCtx(ctx)
	audit.Detached(t.auditSink, audit.Entry{
		OccurredAt: time.Now(),
		TenantID:   &tid, UserID: &uid,
		Action: "agent.delegate.complete",
		Target: subProfile,
		Metadata: map[string]any{
			"parent_profile": parentProfile,
			"sub_profile":    subProfile,
			"sub_steps":      subSteps,
			"status":         status,
			"sub_tool_calls": toolCalls,
			"duration_ms":    dur.Milliseconds(),
			"depth":          parentRC.DelegateDepth + 1,
		},
	}, nil)
}

// profileNameFromCtx reads the parent profile name out of RunCtx so audit
// entries can attribute the delegation to its caller.
func profileNameFromCtx(ctx context.Context) string {
	return RunCtxFromCtx(ctx).ProfileName
}
