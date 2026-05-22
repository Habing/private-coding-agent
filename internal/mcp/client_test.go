package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockServer wraps an httptest.Server with a handler that dispatches by
// jsonRPCRequest.Method. Each test installs its own table-of-responses.
type mockServer struct {
	t         *testing.T
	srv       *httptest.Server
	calls     atomic.Int64
	lastAuth  atomic.Value // string
	lastBody  atomic.Value // []byte
	handler   func(req jsonRPCRequest) (any, *jsonRPCError, int) // result, error, http status (0 = 200)
}

func newMockServer(t *testing.T, handler func(jsonRPCRequest) (any, *jsonRPCError, int)) *mockServer {
	t.Helper()
	ms := &mockServer{t: t, handler: handler}
	ms.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ms.calls.Add(1)
		ms.lastAuth.Store(r.Header.Get("Authorization"))
		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		result, rpcErr, status := ms.handler(req)
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		env := jsonRPCResponse{JSONRPC: "2.0", ID: req.ID}
		if rpcErr != nil {
			env.Error = rpcErr
		} else if result != nil {
			raw, _ := json.Marshal(result)
			env.Result = raw
		}
		_ = json.NewEncoder(w).Encode(env)
	}))
	t.Cleanup(ms.srv.Close)
	return ms
}

func TestClient_Initialize_OK(t *testing.T) {
	ms := newMockServer(t, func(req jsonRPCRequest) (any, *jsonRPCError, int) {
		require.Equal(t, "initialize", req.Method)
		return InitializeResult{
			ProtocolVersion: ProtocolVersion,
			Capabilities:    map[string]any{"tools": map[string]any{}},
			ServerInfo:      ServerInfo{Name: "mock", Version: "1.0"},
		}, nil, 0
	})

	c := NewClient(ms.srv.URL, AuthTypeNone, "", nil, nil)
	res, err := c.Initialize(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ProtocolVersion, res.ProtocolVersion)
	assert.Equal(t, "mock", res.ServerInfo.Name)
	assert.NotNil(t, res.Capabilities)
}

func TestClient_BearerAuth_HeaderSet(t *testing.T) {
	ms := newMockServer(t, func(req jsonRPCRequest) (any, *jsonRPCError, int) {
		return InitializeResult{ProtocolVersion: ProtocolVersion}, nil, 0
	})

	c := NewClient(ms.srv.URL, AuthTypeBearer, "secret123", nil, nil)
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Bearer secret123", ms.lastAuth.Load())
}

func TestClient_NoAuth_NoHeader(t *testing.T) {
	ms := newMockServer(t, func(req jsonRPCRequest) (any, *jsonRPCError, int) {
		return InitializeResult{ProtocolVersion: ProtocolVersion}, nil, 0
	})

	c := NewClient(ms.srv.URL, AuthTypeNone, "ignored", nil, nil)
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "", ms.lastAuth.Load())
}

func TestClient_CustomHeaders(t *testing.T) {
	got := make(chan map[string]string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := map[string]string{
			"X-Tenant": r.Header.Get("X-Tenant"),
			"X-Org":    r.Header.Get("X-Org"),
		}
		got <- h
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      1,
			Result:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"m","version":"1"}}`),
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, AuthTypeNone, "", map[string]string{
		"X-Tenant": "acme",
		"X-Org":    "infra",
	}, nil)
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)
	h := <-got
	assert.Equal(t, "acme", h["X-Tenant"])
	assert.Equal(t, "infra", h["X-Org"])
}

func TestClient_ListTools_OK(t *testing.T) {
	ms := newMockServer(t, func(req jsonRPCRequest) (any, *jsonRPCError, int) {
		require.Equal(t, "tools/list", req.Method)
		return map[string]any{
			"tools": []ToolSchema{
				{
					Name:        "echo",
					Description: "Echoes its input back.",
					InputSchema: map[string]any{
						"type":       "object",
						"properties": map[string]any{"text": map[string]any{"type": "string"}},
						"required":   []any{"text"},
					},
					Annotations: map[string]any{"destructiveHint": false},
				},
			},
		}, nil, 0
	})

	c := NewClient(ms.srv.URL, AuthTypeNone, "", nil, nil)
	tools, err := c.ListTools(context.Background())
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "echo", tools[0].Name)
	assert.Equal(t, false, tools[0].Annotations["destructiveHint"])
}

func TestClient_CallTool_OK(t *testing.T) {
	ms := newMockServer(t, func(req jsonRPCRequest) (any, *jsonRPCError, int) {
		require.Equal(t, "tools/call", req.Method)
		p, ok := req.Params.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "echo", p["name"])
		args := p["arguments"].(map[string]any)
		return CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: "echo: " + args["text"].(string)}},
		}, nil, 0
	})

	c := NewClient(ms.srv.URL, AuthTypeNone, "", nil, nil)
	res, err := c.CallTool(context.Background(), "echo", map[string]any{"text": "hi"})
	require.NoError(t, err)
	require.Len(t, res.Content, 1)
	assert.Equal(t, "echo: hi", res.Content[0].Text)
	assert.False(t, res.IsError)
}

func TestClient_CallTool_NilArgsBecomesEmptyObject(t *testing.T) {
	ms := newMockServer(t, func(req jsonRPCRequest) (any, *jsonRPCError, int) {
		p := req.Params.(map[string]any)
		args, ok := p["arguments"].(map[string]any)
		require.True(t, ok, "arguments must be an object even when caller passes nil")
		assert.Empty(t, args)
		return CallToolResult{Content: []ContentBlock{{Type: "text", Text: "ok"}}}, nil, 0
	})
	c := NewClient(ms.srv.URL, AuthTypeNone, "", nil, nil)
	_, err := c.CallTool(context.Background(), "ping", nil)
	require.NoError(t, err)
}

func TestClient_Ping_DelegatesToInitialize(t *testing.T) {
	ms := newMockServer(t, func(req jsonRPCRequest) (any, *jsonRPCError, int) {
		assert.Equal(t, "initialize", req.Method)
		return InitializeResult{ProtocolVersion: ProtocolVersion}, nil, 0
	})
	c := NewClient(ms.srv.URL, AuthTypeNone, "", nil, nil)
	require.NoError(t, c.Ping(context.Background()))
	assert.Equal(t, int64(1), ms.calls.Load())
}

func TestClient_RPCError_Surfaces(t *testing.T) {
	ms := newMockServer(t, func(req jsonRPCRequest) (any, *jsonRPCError, int) {
		return nil, &jsonRPCError{Code: JSONRPCErrMethodNotFound, Message: "method not found"}, 0
	})
	c := NewClient(ms.srv.URL, AuthTypeNone, "", nil, nil)
	_, err := c.ListTools(context.Background())
	require.Error(t, err)
	var rpcErr *jsonRPCError
	require.True(t, errors.As(err, &rpcErr))
	assert.Equal(t, JSONRPCErrMethodNotFound, rpcErr.Code)
}

func TestClient_HTTPError_Surfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, AuthTypeBearer, "wrong", nil, nil)
	_, err := c.Initialize(context.Background())
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "401"), err.Error())
}

func TestClient_ContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than the context to force a deadline exceeded.
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, AuthTypeNone, "", nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.Initialize(ctx)
	require.Error(t, err)
}

func TestClient_BadJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, AuthTypeNone, "", nil, nil)
	_, err := c.Initialize(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode")
}

func TestClient_EmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, AuthTypeNone, "", nil, nil)
	_, err := c.Initialize(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty result")
}

func TestClient_DefaultTimeoutApplied(t *testing.T) {
	c := NewClient("http://example.invalid", AuthTypeNone, "", nil, nil)
	require.NotNil(t, c.HTTPClient)
	assert.Equal(t, DefaultTimeout, c.HTTPClient.Timeout)
}

func TestClient_HeadersCopiedFromConstructor(t *testing.T) {
	src := map[string]string{"X-One": "1"}
	c := NewClient("http://example.invalid", AuthTypeNone, "", src, nil)
	src["X-One"] = "2"           // mutate after construction
	src["X-Two"] = "added"        // add after construction
	assert.Equal(t, "1", c.Headers["X-One"])
	_, exists := c.Headers["X-Two"]
	assert.False(t, exists)
}
