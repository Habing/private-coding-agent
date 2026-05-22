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
	Mutating    bool            `json:"mutating"`
}

// Mutating is an optional interface tools implement to flag side-effecting
// behavior. The Workflow Engine consults this during Dry-Run to short-circuit
// mutating tools with a mock JSON envelope instead of dispatching them. Tools
// that do not implement Mutating are treated as non-mutating.
type Mutating interface {
	IsMutating() bool
}

// IsMutating returns true iff the named tool implements Mutating and reports
// true. Used by Engine to decide whether to invoke or mock during Dry-Run.
// Unknown tool names return false (the unknown-tool error surfaces later in
// Bus.Invoke during the real call path).
func (b *Bus) IsMutating(name string) bool {
	t, ok := b.reg.Get(name)
	if !ok {
		return false
	}
	m, ok := t.(Mutating)
	if !ok {
		return false
	}
	return m.IsMutating()
}
