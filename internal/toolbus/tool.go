// Package toolbus dispatches tool invocations from Agents / Workflows to
// concrete implementations (in-process Go for built-in tools).
//
// Each Tool advertises a JSON Schema for its inputs. Bus.Invoke validates
// input against the schema, hashes input/output for audit, calls Tool.Invoke,
// and records a row in tool_invocations.
package toolbus

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// Tool is the unit dispatched by Bus. Implementations must be safe for
// concurrent use.
type Tool interface {
	// Name returns the unique tool name, e.g. "fs.read".
	Name() string

	// Description is human-readable, surfaced to LLMs.
	Description() string

	// Schema returns a JSON Schema for input args. Must be OpenAI tool
	// calling compatible (i.e. usable as tools[].function.parameters).
	Schema() json.RawMessage

	// Invoke executes the tool. input is the raw JSON already validated
	// against Schema(); implementations unmarshal it into their own struct.
	// ctx carries timeout/cancellation. tenantID/userID are passed through
	// to downstream services (sandbox.Runtime / modelgw.Gateway) for
	// authorization and auditing.
	Invoke(ctx context.Context, tenantID, userID uuid.UUID,
		input json.RawMessage) (json.RawMessage, error)
}

// ToolDef is the OpenAI-tool-calling-compatible definition returned by
// Bus.ListTools.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}
