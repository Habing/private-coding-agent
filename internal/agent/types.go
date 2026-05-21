// Package agent implements the Agent Engine: a ReAct-style loop that drives an
// LLM through Tool Bus invocations until it produces a final answer.
//
// Engine.Run takes a RunInput (model + messages + profile) and emits a stream
// of Event values via a yield callback. Each loop step calls the Model Gateway
// for the next assistant message; if the assistant requested tool calls, the
// engine dispatches them through the Tool Bus and feeds observations back to
// the LLM as role=tool messages. The loop ends on finish_reason!=tool_calls or
// when MaxSteps is exceeded.
package agent

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

// RunInput is the request payload for Engine.Run.
type RunInput struct {
	TenantID    uuid.UUID
	UserID      uuid.UUID
	Model       string                // e.g. "default-mock:gpt-4o"
	Messages    []modelgw.ChatMessage // user/system/assistant; engine prepends system from profile
	ProfileName string                // looked up in Engine.profiles; empty falls back to "coding"
	MaxSteps    int                   // 0 -> profile default

	// SkillIDs is an explicit override from the caller (POST /agent/run body).
	// Wins over SessionSkillIDs and Profile.SkillIDs.
	SkillIDs []string `json:"skill_ids,omitempty"`

	// SessionSkillIDs is populated by the session service from sessions.skill_ids.
	// Never bound from JSON; transport-only field.
	SessionSkillIDs []string `json:"-"`
}

// EventKind enumerates the event types emitted by Engine.Run.
type EventKind string

const (
	EventAssistantDelta   EventKind = "assistant_delta"   // incremental text chunk while LLM streams
	EventAssistantMessage EventKind = "assistant_message"
	EventToolCall         EventKind = "tool_call"
	EventToolResult       EventKind = "tool_result"
	EventFinal            EventKind = "final"
	EventError            EventKind = "error"
)

// Event is one entry in the engine's output stream. Fields are populated based
// on Kind; unused fields are zero values.
type Event struct {
	Kind         EventKind          `json:"kind"`
	Step         int                `json:"step"`
	Text         string             `json:"text,omitempty"`           // assistant content / final content / error message
	ToolCallID   string             `json:"tool_call_id,omitempty"`   // tool_call / tool_result
	ToolName     string             `json:"tool_name,omitempty"`      // tool_call / tool_result
	ToolInput    json.RawMessage    `json:"tool_input,omitempty"`     // tool_call
	ToolOutput   json.RawMessage    `json:"tool_output,omitempty"`    // tool_result (truncated form if needed)
	ToolError    string             `json:"tool_error,omitempty"`     // tool_result when downstream failed
	Truncated    bool               `json:"truncated,omitempty"`      // tool_result
	OriginalSize int                `json:"original_size,omitempty"`  // tool_result when truncated
	FinishReason string             `json:"finish_reason,omitempty"`  // final / assistant_message
	ToolCalls    []modelgw.ToolCall `json:"tool_calls,omitempty"`     // assistant_message when LLM requested calls
}
