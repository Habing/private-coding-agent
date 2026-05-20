package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// Gateway is the subset of *modelgw.Gateway the Engine depends on.
type Gateway interface {
	ChatCompletion(ctx context.Context, tenantID, userID uuid.UUID,
		req modelgw.ChatRequest) (*modelgw.ChatResponse, error)
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
	maxOutputBytes int
}

// NewEngine wires the engine. profiles must contain at least one entry; the
// "coding" profile is used as the default when RunInput.ProfileName is empty.
func NewEngine(gw Gateway, bus Bus, profiles map[string]Profile) *Engine {
	return &Engine{
		gw:             gw,
		bus:            bus,
		profiles:       profiles,
		maxOutputBytes: DefaultMaxToolOutputBytes,
	}
}

// Run drives the ReAct loop. Each event is emitted via yield(); if yield returns
// an error the loop aborts and returns that error. The final return value is nil
// on a clean stop, or a sentinel error on max steps / LLM failure.
func (e *Engine) Run(ctx context.Context, in RunInput, yield func(Event) error) error {
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

	// Build conversation: system prompt first, then caller-provided messages.
	messages := make([]modelgw.ChatMessage, 0, len(in.Messages)+1)
	if profile.SystemPrompt != "" {
		messages = append(messages, modelgw.ChatMessage{
			Role:    modelgw.RoleSystem,
			Content: profile.SystemPrompt,
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
		req := modelgw.ChatRequest{
			Model:    in.Model,
			Messages: messages,
			Tools:    modelTools,
		}
		resp, err := e.gw.ChatCompletion(ctx, in.TenantID, in.UserID, req)
		if err != nil {
			_ = yield(Event{Kind: EventError, Step: step, Text: err.Error(), FinishReason: "llm_error"})
			return fmt.Errorf("%w: %v", ErrLLMFailed, err)
		}
		if len(resp.Choices) == 0 {
			_ = yield(Event{Kind: EventError, Step: step, Text: "empty choices", FinishReason: "llm_error"})
			return ErrLLMFailed
		}
		choice := resp.Choices[0]
		assistant := choice.Message
		if err := yield(Event{
			Kind:         EventAssistantMessage,
			Step:         step,
			Text:         assistant.Content,
			ToolCalls:    assistant.ToolCalls,
			FinishReason: choice.FinishReason,
		}); err != nil {
			return err
		}

		if choice.FinishReason == "tool_calls" && len(assistant.ToolCalls) > 0 {
			messages = append(messages, assistant)
			for _, call := range assistant.ToolCalls {
				messages = append(messages, e.runToolCall(ctx, in, step, call, allowed, yield))
			}
			continue
		}

		if err := yield(Event{
			Kind:         EventFinal,
			Step:         step,
			Text:         assistant.Content,
			FinishReason: choice.FinishReason,
		}); err != nil {
			return err
		}
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
