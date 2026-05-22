package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// DefaultTimeout is the per-call HTTP timeout when the caller does not provide
// one. The Manager overrides this from config (PCA_MCP_INVOKE_TIMEOUT etc.).
const DefaultTimeout = 30 * time.Second

// Client speaks JSON-RPC 2.0 over HTTP to a single MCP server. It is safe for
// concurrent use and stateless across calls: every CallTool re-issues
// initialize first so transient server restarts do not require client-side
// session tracking.
type Client struct {
	URL        string
	AuthType   string
	AuthToken  string
	Headers    map[string]string
	HTTPClient *http.Client

	idSeq atomic.Int64
}

// NewClient builds a Client with sensible HTTP defaults. The URL is the
// server's JSON-RPC endpoint (typically a single POST endpoint per the MCP
// HTTP transport). httpClient may be nil to use a default 30s-timeout one.
func NewClient(url, authType, authToken string, headers map[string]string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultTimeout}
	}
	// Copy headers so the caller can mutate their map without affecting us.
	hcopy := make(map[string]string, len(headers))
	for k, v := range headers {
		hcopy[k] = v
	}
	return &Client{
		URL:        url,
		AuthType:   authType,
		AuthToken:  authToken,
		Headers:    hcopy,
		HTTPClient: httpClient,
	}
}

// Initialize performs the MCP handshake. Servers that reply with a different
// protocolVersion are still accepted — the client records what the server
// said but does not enforce strict equality, mirroring the spec's
// version-tolerant guidance.
func (c *Client) Initialize(ctx context.Context) (*InitializeResult, error) {
	params := map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    ClientName,
			"version": ClientVersion,
		},
	}
	var res InitializeResult
	if err := c.call(ctx, "initialize", params, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// ListTools fetches the server's tool catalogue. Pagination via "nextCursor"
// is not implemented in 21b: real-world MCP servers fit their full list in
// one response, and the cache layer can refresh on demand.
func (c *Client) ListTools(ctx context.Context) ([]ToolSchema, error) {
	var res struct {
		Tools []ToolSchema `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", map[string]any{}, &res); err != nil {
		return nil, err
	}
	return res.Tools, nil
}

// CallTool invokes a named tool. args may be nil; the spec allows omitting
// arguments entirely but most servers prefer an empty object. Tool-level
// failures (CallToolResult.IsError) are returned in the result, not as a Go
// error — the Agent loop needs to surface them as tool_result with error flag,
// not as a tool_call transport failure.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*CallToolResult, error) {
	if args == nil {
		args = map[string]any{}
	}
	params := map[string]any{
		"name":      name,
		"arguments": args,
	}
	var res CallToolResult
	if err := c.call(ctx, "tools/call", params, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Ping wraps Initialize as a liveness probe; the result is discarded. The
// heartbeat goroutine in Manager calls Ping to update last_seen_at /
// last_error without touching tools_cache.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Initialize(ctx)
	return err
}

// call is the shared JSON-RPC plumbing. It does not retry — transient
// failures bubble up to the Manager which decides whether to mark the server
// degraded or fail the admin action.
func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	id := c.idSeq.Add(1)
	body, err := json.Marshal(jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return fmt.Errorf("mcp: marshal %s request: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mcp: new %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	if c.AuthType == AuthTypeBearer && c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("mcp: %s request: %w", method, err)
	}
	defer resp.Body.Close()

	// Drain at most 1 MiB to keep a buggy server from exhausting memory.
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("mcp: read %s response: %w", method, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Surface the HTTP error verbatim — many MCP servers return useful
		// error text out-of-band of the JSON-RPC envelope on auth failures.
		snippet := string(respBody)
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		return fmt.Errorf("mcp: %s http %d: %s", method, resp.StatusCode, snippet)
	}

	var env jsonRPCResponse
	if err := json.Unmarshal(respBody, &env); err != nil {
		return fmt.Errorf("mcp: decode %s envelope: %w (body=%q)", method, err, string(respBody))
	}
	if env.Error != nil {
		return env.Error
	}
	if len(env.Result) == 0 {
		return fmt.Errorf("mcp: %s: empty result", method)
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return fmt.Errorf("mcp: decode %s result: %w", method, err)
	}
	return nil
}
