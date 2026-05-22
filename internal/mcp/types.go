// Package mcp connects external MCP (Model Context Protocol) servers to the
// in-process ToolBus so admins can register HTTP MCP endpoints and have their
// advertised tools appear as `mcp.<slug>.<tool>` candidates for Agent runs.
//
// Slice 21b only implements the 2024-11-05 minimal subset over HTTP:
// initialize + tools/list + tools/call. Stdio transport, prompts, resources,
// notifications, and sampling are out of scope.
package mcp

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ProtocolVersion is the MCP wire version this client speaks. Servers that
// negotiate a different version are still accepted as long as initialize
// succeeds; the field is round-tripped verbatim in the request.
const ProtocolVersion = "2024-11-05"

// ClientName / ClientVersion identify this client in initialize.clientInfo so
// MCP servers can log who is calling. Bumped on protocol-relevant changes.
const (
	ClientName    = "private-coding-agent"
	ClientVersion = "21b"
)

// Transport types. Only "http" is supported in 21b; stdio is reserved.
const (
	TransportHTTP = "http"
)

// AuthType variants for outbound MCP requests.
const (
	AuthTypeNone   = "none"
	AuthTypeBearer = "bearer"
)

// Server is the persisted row in mcp_servers. JSON-tagged for admin REST so
// the handler can encode/decode without an intermediate DTO. AuthToken is
// redacted to "***" when serialized by the admin layer (handler is responsible
// for redaction; Server.MarshalJSON itself does not redact).
type Server struct {
	ID          uuid.UUID         `json:"id"`
	TenantID    uuid.UUID         `json:"tenant_id"`
	Slug        string            `json:"slug"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	URL         string            `json:"url"`
	Transport   string            `json:"transport"`
	AuthType    string            `json:"auth_type"`
	AuthToken   string            `json:"auth_token,omitempty"`
	Headers     map[string]string `json:"headers"`
	Enabled     bool              `json:"enabled"`
	LastSeenAt  *time.Time        `json:"last_seen_at,omitempty"`
	LastError   string            `json:"last_error"`
	ToolsCache  []ToolSchema      `json:"tools_cache"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// ServerInfo is the MCP server identification block returned by initialize.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the payload of a successful MCP initialize response.
// Capabilities is intentionally kept as a free-form map: the spec lists
// experimental and version-specific flags this client does not inspect.
type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      ServerInfo     `json:"serverInfo"`
}

// ToolSchema mirrors a tools/list entry. InputSchema is left as a generic map
// so callers can re-serialize it into ToolDef.Parameters without losing fields
// the JSON-Schema compiler does not require (e.g. additionalProperties=false,
// vendor extensions). Annotations carries optional hints like destructiveHint
// which the manager translates into Mutating bool.
type ToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

// ContentBlock is one element of tools/call result.content. Only text blocks
// are surfaced to the Agent in 21b; image/resource blocks are passed through
// untouched (the calling Agent decides how to handle them).
type ContentBlock struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	Data     string         `json:"data,omitempty"`
	MIMEType string         `json:"mimeType,omitempty"`
	Resource map[string]any `json:"resource,omitempty"`
}

// CallToolResult is what tools/call returns under "result". IsError signals
// tool-level failure (validation, business logic) — distinct from JSON-RPC
// transport errors which surface as a non-nil error from Client methods.
type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// jsonRPCRequest is the envelope sent on every method call. ID is the monotonic
// sequence number per Client; it lets callers correlate streamed responses
// later if HTTP+SSE transport is added.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCResponse is the envelope received. Either Result or Error is set,
// never both, per the spec. Result is delayed-decoded so each method can
// unmarshal into its own typed result struct.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError is the error object per JSON-RPC 2.0. Data is server-specific
// and forwarded verbatim through the Go error string for diagnostics.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implements error so the Client can return *jsonRPCError directly when
// the remote side reports a method-level failure (-32601 unsupported, etc.).
func (e *jsonRPCError) Error() string {
	return e.Message
}

// JSONRPC error codes used by this package. The full list lives in the
// JSON-RPC 2.0 spec; we only name the ones the client surface acts on.
const (
	JSONRPCErrMethodNotFound = -32601
)
