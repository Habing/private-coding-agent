// mockmcp is a minimal MCP server for compose E2E (slice 21b / P0).
//
// JSON-RPC HTTP transport (2024-11-05):
//   - initialize, tools/list, tools/call
//
// Tools: echo, fetch_status, record_event (see docs/MCP-TOOL-ROADMAP.md §8).
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
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
			"serverInfo":      map[string]any{"name": "mock-mcp", "version": "p0"},
		})
	case "tools/list":
		writeResult(w, req.ID, map[string]any{"tools": toolSchemas()})
	case "tools/call":
		handleToolCall(w, req)
	default:
		writeErr(w, req.ID, -32601, "method not found: "+req.Method)
	}
}

func handleToolCall(w http.ResponseWriter, req rpcReq) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeToolError(w, req.ID, "invalid params: "+err.Error())
		return
	}
	switch p.Name {
	case "echo":
		text, err := toolEcho(p.Arguments)
		if err != nil {
			writeToolError(w, req.ID, err.Error())
			return
		}
		writeToolText(w, req.ID, text)
	case "fetch_status":
		text, err := toolFetchStatus(p.Arguments)
		if err != nil {
			writeToolError(w, req.ID, err.Error())
			return
		}
		writeToolText(w, req.ID, text)
	case "record_event":
		payload, err := toolRecordEvent(p.Arguments)
		if err != nil {
			writeToolError(w, req.ID, err.Error())
			return
		}
		writeToolJSON(w, req.ID, payload)
	default:
		writeToolError(w, req.ID, "unknown tool: "+p.Name)
		return
	}
}

func toolEcho(args map[string]any) (string, error) {
	text, _ := args["text"].(string)
	return "echo: " + text, nil
}

// toolFetchStatus returns a plain token for workflow if/assign: "ok" | "degraded".
func toolFetchStatus(args map[string]any) (string, error) {
	scenario, _ := args["scenario"].(string)
	scenario = strings.TrimSpace(strings.ToLower(scenario))
	if scenario == "" {
		scenario = "degraded"
	}
	switch scenario {
	case "ok", "healthy":
		return "ok", nil
	default:
		return "degraded", nil
	}
}

func toolRecordEvent(args map[string]any) (map[string]any, error) {
	kind, _ := args["kind"].(string)
	if strings.TrimSpace(kind) == "" {
		kind = "inspect"
	}
	detail, _ := args["detail"].(string)
	return map[string]any{
		"recorded": true,
		"event_id": fmt.Sprintf("evt-%d", time.Now().UnixNano()),
		"kind":     kind,
		"detail":   detail,
	}, nil
}

func toolSchemas() []map[string]any {
	return []map[string]any{
		toolSchema("echo", "回显输入文本（模拟通知）", false, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
			"required": []string{"text"},
		}),
		toolSchema("fetch_status", "查询 mock 系统状态，供工作流 if 分支使用", false, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"scenario": map[string]any{
					"type":        "string",
					"description": "巡检场景：ok（正常）或 degraded（异常），默认 degraded",
				},
			},
		}),
		toolSchema("record_event", "模拟写入审计/事件记录（有副作用）", true, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"kind":   map[string]any{"type": "string"},
				"detail": map[string]any{"type": "string"},
			},
		}),
	}
}

func toolSchema(name, desc string, destructive bool, schema map[string]any) map[string]any {
	return map[string]any{
		"name":        name,
		"description": desc,
		"inputSchema": schema,
		"annotations": map[string]any{"destructiveHint": destructive},
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

func writeToolText(w http.ResponseWriter, id any, text string) {
	writeResult(w, id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	})
}

func writeToolJSON(w http.ResponseWriter, id any, payload any) {
	writeToolText(w, id, mustJSON(payload))
}

func writeToolError(w http.ResponseWriter, id any, msg string) {
	writeResult(w, id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": msg}},
		"isError": true,
	})
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":"marshal: %s"}`, err.Error())
	}
	return string(b)
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}
