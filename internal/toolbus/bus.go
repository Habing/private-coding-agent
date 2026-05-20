package toolbus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/yourorg/private-coding-agent/internal/audit"
)

// Bus orchestrates tool invocation: schema-validate input, hash for audit,
// invoke the tool, persist InvocationEvent. Stateless and concurrent-safe.
type Bus struct {
	reg      *Registry
	recorder *InvocationRecorder
	schemas  map[string]*jsonschema.Schema
	audit    audit.Sink
}

// WithAuditSink wires an audit.Sink so the Bus records tool.invoke.error
// entries on failure. Success path is already captured by tool_invocations
// (status=ok); we only mirror failures to audit_log to support cross-domain
// admin queries. Returns the receiver for chaining.
func (b *Bus) WithAuditSink(s audit.Sink) *Bus {
	b.audit = s
	return b
}

// NewBus compiles each registered tool's schema once. Returns an error if any
// schema fails to compile (callers should not start the server in that case).
func NewBus(reg *Registry, recorder *InvocationRecorder) (*Bus, error) {
	schemas := map[string]*jsonschema.Schema{}
	for _, t := range reg.List() {
		s, err := CompileSchema(t.Schema())
		if err != nil {
			return nil, fmt.Errorf("toolbus: compile schema for %q: %w", t.Name(), err)
		}
		schemas[t.Name()] = s
	}
	return &Bus{reg: reg, recorder: recorder, schemas: schemas}, nil
}

// ListTools returns all registered tools as OpenAI-tool-calling-compatible defs.
func (b *Bus) ListTools(_ context.Context, _ uuid.UUID) []ToolDef {
	tools := b.reg.List()
	out := make([]ToolDef, 0, len(tools))
	for _, t := range tools {
		out = append(out, ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Schema(),
		})
	}
	return out
}

// Invoke runs the named tool with the given input. Records every call to
// tool_invocations (status=ok or error). Returns the tool's raw JSON output.
func (b *Bus) Invoke(ctx context.Context, tenantID, userID uuid.UUID,
	toolName string, input json.RawMessage) (json.RawMessage, error) {

	tool, ok := b.reg.Get(toolName)
	if !ok {
		return nil, ErrToolNotFound
	}
	schema := b.schemas[toolName]
	if err := Validate(schema, input); err != nil {
		return nil, err
	}

	inputSHA := sha256Hex(input)
	start := time.Now()
	output, callErr := tool.Invoke(ctx, tenantID, userID, input)
	dur := time.Since(start)

	event := InvocationEvent{
		TenantID:    tenantID,
		UserID:      userID,
		ToolName:    toolName,
		DurationMS:  int(dur.Milliseconds()),
		InputSHA256: inputSHA,
		OccurredAt:  time.Now(),
	}
	if callErr != nil {
		event.Status = "error"
		event.ErrorClass = classifyError(callErr)
	} else {
		event.Status = "ok"
		event.OutputSHA256 = sha256Hex(output)
	}
	b.recorder.Record(event)

	if callErr != nil && b.audit != nil {
		tid := tenantID
		uid := userID
		audit.Detached(b.audit, audit.Entry{
			OccurredAt: start,
			TenantID:   &tid, UserID: &uid,
			Action:     "tool.invoke.error",
			Target:     toolName,
			DurationMS: int(dur.Milliseconds()),
			Metadata:   map[string]any{"error_class": event.ErrorClass},
		}, nil)
	}

	return output, callErr
}

func sha256Hex(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// classifyError maps known sentinels to short stable strings for analytics.
func classifyError(err error) string {
	switch {
	case errors.Is(err, ErrInvalidArguments), errors.Is(err, ErrSandboxIDRequired):
		return "validation"
	case errors.Is(err, ErrToolNotFound):
		return "tool_not_found"
	}
	return "other"
}
