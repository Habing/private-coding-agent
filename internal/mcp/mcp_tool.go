package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/yourorg/private-coding-agent/internal/audit"
	pcametrics "github.com/yourorg/private-coding-agent/internal/metrics"
)

// Tool is the toolbus.Tool adapter that exposes one MCP server's tool under
// `mcp.<slug>.<tool>`. Like WorkflowTool, it owns the tenant binding so the
// Bus's global tool namespace cannot be used by another tenant to call into
// this tool — Invoke refuses with ErrTenantMismatch on a mismatch.
type Tool struct {
	serverID   uuid.UUID
	serverSlug string
	tenantID   uuid.UUID
	schema     ToolSchema
	client     *Client
	audit      audit.Sink
}

// NewTool builds a Tool wrapping one ToolSchema from a specific server. The
// caller (Manager) keeps one Client per server (URL/auth are stable) and
// passes the same pointer to every Tool spawned from that server.
func NewTool(serverID uuid.UUID, serverSlug string, tenantID uuid.UUID,
	schema ToolSchema, client *Client, sink audit.Sink) *Tool {
	return &Tool{
		serverID:   serverID,
		serverSlug: serverSlug,
		tenantID:   tenantID,
		schema:     schema,
		client:     client,
		audit:      sink,
	}
}

// Name implements toolbus.Tool — global identifier seen by ListTools and
// Bus.Invoke. The "mcp." prefix lets the Workflow Engine and Agent loops
// filter MCP-backed tools at a glance.
func (t *Tool) Name() string { return "mcp." + t.serverSlug + "." + t.schema.Name }

// Description is the server-supplied free-text; empty descriptions fall back
// to a generic line so LLMs always see something.
func (t *Tool) Description() string {
	if t.schema.Description == "" {
		return "External MCP tool from " + t.serverSlug
	}
	return t.schema.Description
}

// Schema reserializes the InputSchema map into JSON. If reserialization fails
// (should not happen for well-formed maps), we return an empty object so the
// Bus schema-compile step accepts it; the server will reject malformed args
// at Invoke time anyway.
func (t *Tool) Schema() json.RawMessage {
	if t.schema.InputSchema == nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	raw, err := json.Marshal(t.schema.InputSchema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return raw
}

// IsMutating consults annotations.destructiveHint. MCP spec calls this an
// optional hint, so default-true is the conservative choice: the WebUI
// flags an unknown tool as mutating until the server tells us otherwise.
func (t *Tool) IsMutating() bool {
	if t.schema.Annotations == nil {
		return true
	}
	if v, ok := t.schema.Annotations["destructiveHint"].(bool); ok {
		return v
	}
	return true
}

// Invoke forwards args to the server's tools/call. Cross-tenant invocations
// are refused with ErrTenantMismatch. Tool-level failures (CallToolResult.
// IsError) are surfaced as a Go error so the Agent loop routes them through
// tool_error rather than tool_result.
func (t *Tool) Invoke(ctx context.Context, tenantID, userID uuid.UUID,
	input json.RawMessage) (json.RawMessage, error) {

	start := time.Now()
	if tenantID != t.tenantID {
		t.recordMetric(ctx, "tenant_mismatch")
		return nil, fmt.Errorf("%w: tool=%s", ErrTenantMismatch, t.Name())
	}

	var args map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			t.recordMetric(ctx, "bad_input")
			return nil, fmt.Errorf("%s: invalid input json: %w", t.Name(), err)
		}
	}
	res, err := t.client.CallTool(ctx, t.schema.Name, args)
	dur := time.Since(start)
	if err != nil {
		t.recordMetric(ctx, "error")
		t.recordDuration(ctx, dur)
		t.auditInvoke(tenantID, userID, dur, err.Error(), false)
		return nil, err
	}
	if res.IsError {
		t.recordMetric(ctx, "tool_error")
		t.recordDuration(ctx, dur)
		// IsError carries server-side validation/business errors; bubble them
		// up so the Agent gets a tool_error event with the text content.
		msg := errorMessageFromContent(res.Content)
		t.auditInvoke(tenantID, userID, dur, msg, true)
		return nil, fmt.Errorf("%s: %s", t.Name(), msg)
	}

	out, err := json.Marshal(res)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal result: %w", t.Name(), err)
	}
	t.recordMetric(ctx, "success")
	t.recordDuration(ctx, dur)
	t.auditInvoke(tenantID, userID, dur, "", false)
	return out, nil
}

func (t *Tool) recordMetric(ctx context.Context, outcome string) {
	if pcametrics.MCPInvocationsTotal == nil {
		return
	}
	pcametrics.MCPInvocationsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("server", t.serverSlug),
		attribute.String("tool", t.schema.Name),
		attribute.String("outcome", outcome),
	))
}

func (t *Tool) recordDuration(ctx context.Context, dur time.Duration) {
	if pcametrics.MCPInvocationDuration == nil {
		return
	}
	pcametrics.MCPInvocationDuration.Record(ctx, dur.Seconds(), metric.WithAttributes(
		attribute.String("server", t.serverSlug),
		attribute.String("tool", t.schema.Name),
	))
}

func (t *Tool) auditInvoke(tenantID, userID uuid.UUID, dur time.Duration, errMsg string, isToolErr bool) {
	if t.audit == nil {
		return
	}
	tid := tenantID
	uid := userID
	meta := map[string]any{
		"server_id":  t.serverID.String(),
		"latency_ms": dur.Milliseconds(),
		"tool":       t.schema.Name,
	}
	if errMsg != "" {
		meta["error"] = errMsg
		meta["is_tool_error"] = isToolErr
	}
	audit.Detached(t.audit, audit.Entry{
		OccurredAt: time.Now(),
		TenantID:   &tid, UserID: &uid,
		Action:     "mcp.tool.invoke",
		Target:     t.Name(),
		DurationMS: int(dur.Milliseconds()),
		Metadata:   meta,
	}, nil)
}

// errorMessageFromContent extracts a human-readable message from the content
// blocks returned with IsError=true. MCP servers typically place the failure
// reason in the first text block; we concatenate up to 3 to give the Agent
// enough context without ballooning audit metadata.
func errorMessageFromContent(blocks []ContentBlock) string {
	out := ""
	for i, b := range blocks {
		if i >= 3 {
			break
		}
		if b.Text == "" {
			continue
		}
		if out != "" {
			out += "; "
		}
		out += b.Text
	}
	if out == "" {
		return "tool returned isError=true with no text content"
	}
	return out
}
