package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/mcp"
)

// mockMCP serves enough JSON-RPC for one CallTool round-trip. responder lets
// each test shape the tools/call result (error code / IsError / content).
type mockMCP struct {
	t         *testing.T
	responder func(method string, params json.RawMessage) (any, *errEnv)
}

type errEnv struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (m *mockMCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.t.Helper()
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int64           `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	result, eErr := m.responder(req.Method, req.Params)
	w.Header().Set("Content-Type", "application/json")
	if eErr != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": req.ID,
			"error": eErr,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": req.ID, "result": result,
	})
}

func newTestTool(t *testing.T, srvURL string, tenantID uuid.UUID, schema mcp.ToolSchema) *mcp.Tool {
	t.Helper()
	client := mcp.NewClient(srvURL, mcp.AuthTypeNone, "", nil, nil)
	return mcp.NewTool(uuid.New(), "mock", tenantID, schema, client, nil)
}

func TestTool_Name_PrefixesSlugAndTool(t *testing.T) {
	tool := newTestTool(t, "http://unused", uuid.New(), mcp.ToolSchema{Name: "echo"})
	assert.Equal(t, "mcp.mock.echo", tool.Name())
}

func TestTool_Description_FallsBackWhenEmpty(t *testing.T) {
	empty := newTestTool(t, "http://unused", uuid.New(), mcp.ToolSchema{Name: "echo"})
	assert.Equal(t, "External MCP tool from mock", empty.Description())

	filled := newTestTool(t, "http://unused", uuid.New(), mcp.ToolSchema{
		Name: "echo", Description: "echoes the input verbatim",
	})
	assert.Equal(t, "echoes the input verbatim", filled.Description())
}

func TestTool_Schema_ReserializesInputSchema(t *testing.T) {
	tool := newTestTool(t, "http://unused", uuid.New(), mcp.ToolSchema{
		Name: "echo",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
		},
	})
	var got map[string]any
	require.NoError(t, json.Unmarshal(tool.Schema(), &got))
	assert.Equal(t, "object", got["type"])
}

func TestTool_Schema_DefaultsWhenNil(t *testing.T) {
	tool := newTestTool(t, "http://unused", uuid.New(), mcp.ToolSchema{Name: "echo"})
	assert.JSONEq(t, `{"type":"object"}`, string(tool.Schema()))
}

func TestTool_IsMutating_DefaultsTrueWithoutAnnotations(t *testing.T) {
	tool := newTestTool(t, "http://unused", uuid.New(), mcp.ToolSchema{Name: "echo"})
	assert.True(t, tool.IsMutating(),
		"unknown annotations should default to mutating=true (conservative)")
}

func TestTool_IsMutating_RespectsDestructiveHintFalse(t *testing.T) {
	tool := newTestTool(t, "http://unused", uuid.New(), mcp.ToolSchema{
		Name:        "echo",
		Annotations: map[string]any{"destructiveHint": false},
	})
	assert.False(t, tool.IsMutating(),
		"explicit destructiveHint=false should mark non-mutating")
}

func TestTool_IsMutating_RespectsDestructiveHintTrue(t *testing.T) {
	tool := newTestTool(t, "http://unused", uuid.New(), mcp.ToolSchema{
		Name:        "wipe",
		Annotations: map[string]any{"destructiveHint": true},
	})
	assert.True(t, tool.IsMutating())
}

func TestTool_Invoke_TenantMismatchRefused(t *testing.T) {
	owner := uuid.New()
	intruder := uuid.New()
	tool := newTestTool(t, "http://should-not-be-called", owner,
		mcp.ToolSchema{Name: "echo"})

	out, err := tool.Invoke(context.Background(), intruder, uuid.New(), nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, mcp.ErrTenantMismatch),
		"expected ErrTenantMismatch, got %v", err)
	assert.Nil(t, out)
}

func TestTool_Invoke_BadInputJSON(t *testing.T) {
	tid := uuid.New()
	tool := newTestTool(t, "http://unused", tid, mcp.ToolSchema{Name: "echo"})
	_, err := tool.Invoke(context.Background(), tid, uuid.New(),
		json.RawMessage(`{not-json}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid input json")
}

func TestTool_Invoke_HappyPath(t *testing.T) {
	tid := uuid.New()
	calls := 0
	m := &mockMCP{t: t, responder: func(method string, params json.RawMessage) (any, *errEnv) {
		calls++
		switch method {
		case "initialize":
			return map[string]any{
				"protocolVersion": mcp.ProtocolVersion,
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "mock", "version": "1"},
			}, nil
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			require.NoError(t, json.Unmarshal(params, &p))
			assert.Equal(t, "echo", p.Name)
			assert.Equal(t, "hi", p.Arguments["text"])
			return map[string]any{
				"content": []map[string]any{{"type": "text", "text": "echo: hi"}},
			}, nil
		}
		t.Fatalf("unexpected method %s", method)
		return nil, nil
	}}
	srv := httptest.NewServer(m)
	defer srv.Close()

	tool := newTestTool(t, srv.URL, tid, mcp.ToolSchema{Name: "echo"})
	out, err := tool.Invoke(context.Background(), tid, uuid.New(),
		json.RawMessage(`{"text":"hi"}`))
	require.NoError(t, err)

	var res mcp.CallToolResult
	require.NoError(t, json.Unmarshal(out, &res))
	require.Len(t, res.Content, 1)
	assert.Equal(t, "echo: hi", res.Content[0].Text)
	assert.False(t, res.IsError)
	assert.Equal(t, 1, calls, "stateless client: one call per Invoke (no initialize)")
}

func TestTool_Invoke_NilInputAccepted(t *testing.T) {
	tid := uuid.New()
	m := &mockMCP{t: t, responder: func(method string, _ json.RawMessage) (any, *errEnv) {
		if method == "tools/call" {
			return map[string]any{
				"content": []map[string]any{{"type": "text", "text": "ok"}},
			}, nil
		}
		return nil, &errEnv{Code: -32601, Message: "unknown"}
	}}
	srv := httptest.NewServer(m)
	defer srv.Close()

	tool := newTestTool(t, srv.URL, tid, mcp.ToolSchema{Name: "noop"})
	out, err := tool.Invoke(context.Background(), tid, uuid.New(), nil)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"ok"`)
}

func TestTool_Invoke_SurfacesIsErrorAsGoError(t *testing.T) {
	tid := uuid.New()
	m := &mockMCP{t: t, responder: func(method string, _ json.RawMessage) (any, *errEnv) {
		if method == "tools/call" {
			return map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "domain validation failed"},
				},
				"isError": true,
			}, nil
		}
		return nil, nil
	}}
	srv := httptest.NewServer(m)
	defer srv.Close()

	tool := newTestTool(t, srv.URL, tid, mcp.ToolSchema{Name: "echo"})
	_, err := tool.Invoke(context.Background(), tid, uuid.New(),
		json.RawMessage(`{"text":"hi"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain validation failed",
		"IsError text must reach the caller through the Go error string")
}

func TestTool_Invoke_NetworkErrorBubblesUp(t *testing.T) {
	tid := uuid.New()
	// Closed server → connection refused on first call.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	tool := newTestTool(t, srv.URL, tid, mcp.ToolSchema{Name: "echo"})
	_, err := tool.Invoke(context.Background(), tid, uuid.New(),
		json.RawMessage(`{"text":"hi"}`))
	require.Error(t, err)
}
