// mockmcp is a minimal MCP server for compose E2E (slice 21b).
//
// It speaks the 2024-11-05 JSON-RPC HTTP transport with three methods:
//   - initialize  → returns protocolVersion + a trivial serverInfo
//   - tools/list  → returns one tool, "echo"
//   - tools/call  → for name="echo", echoes back "echo: <arguments.text>"
//
// Everything else returns -32601 method-not-found.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
)

const protocolVersion = "2024-11-05"

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	JSONRPC string  `json:"jsonrpc"`
	ID      any     `json:"id"`
	Result  any     `json:"result,omitempty"`
	Error   *rpcErr `json:"error,omitempty"`
}

func main() {
	port := env("MOCK_MCP_PORT", "8083")

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", handle)

	addr := ":" + port
	log.Printf("mock-mcp listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req rpcReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, nil, -32700, "parse error: "+err.Error())
		return
	}
	switch req.Method {
	case "initialize":
		writeResult(w, req.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "mock-mcp", "version": "21b"},
		})
	case "tools/list":
		writeResult(w, req.ID, map[string]any{
			"tools": []map[string]any{
				{
					"name":        "echo",
					"description": "Echo the input text back, prefixed with 'echo: '.",
					"inputSchema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"text": map[string]any{"type": "string"},
						},
						"required": []string{"text"},
					},
					"annotations": map[string]any{"destructiveHint": false},
				},
			},
		})
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeErr(w, req.ID, -32602, "invalid params: "+err.Error())
			return
		}
		if p.Name != "echo" {
			writeResult(w, req.ID, map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "unknown tool: " + p.Name},
				},
				"isError": true,
			})
			return
		}
		text, _ := p.Arguments["text"].(string)
		writeResult(w, req.ID, map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "echo: " + text},
			},
		})
	default:
		writeErr(w, req.ID, -32601, "method not found: "+req.Method)
	}
}

func writeResult(w http.ResponseWriter, id, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rpcResp{JSONRPC: "2.0", ID: id, Result: result})
}

func writeErr(w http.ResponseWriter, id any, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rpcResp{
		JSONRPC: "2.0", ID: id,
		Error: &rpcErr{Code: code, Message: msg},
	})
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}
