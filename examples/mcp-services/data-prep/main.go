// mcp-data-prep — HTTP MCP server for AI file denoising (data-prep domain).
//
// Tools: load_file, ai_denoise_records, ai_denoise_file, summarize_run, write_file
package main

import (
	"context"
	"encoding/json"
	"fmt"
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

type config struct {
	Port       string
	Inbox      string
	Outbox     string
	MaxRecords int
	MaxFileMB  int64
	LLM        llmConfig
}

func main() {
	cfg := config{
		Port:       env("DATA_PREP_PORT", "8085"),
		Inbox:      env("DATA_PREP_INBOX", "/data/inbox"),
		Outbox:     env("DATA_PREP_OUTBOX", "/data/outbox"),
		MaxRecords: envInt("DATA_PREP_MAX_RECORDS", 500),
		MaxFileMB:  int64(envInt("DATA_PREP_MAX_FILE_MB", 32)),
		LLM:        loadLLMConfig(),
	}
	if cfg.LLM.Mock {
		log.Print("data-prep: LLM mock mode (set LLM_API_KEY and DATA_PREP_MOCK_LLM=false for real AI)")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleRPC(w, r, cfg)
	})

	addr := ":" + cfg.Port
	log.Printf("mcp-data-prep listening on %s inbox=%s outbox=%s", addr, cfg.Inbox, cfg.Outbox)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func handleRPC(w http.ResponseWriter, r *http.Request, cfg config) {
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
			"serverInfo":      map[string]any{"name": "mcp-data-prep", "version": "0.1.0"},
		})
	case "tools/list":
		writeResult(w, req.ID, map[string]any{"tools": toolSchemas()})
	case "tools/call":
		handleToolCall(w, req, cfg)
	default:
		writeErr(w, req.ID, -32601, "method not found: "+req.Method)
	}
}

func handleToolCall(w http.ResponseWriter, req rpcReq, cfg config) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeToolError(w, req.ID, "invalid params: "+err.Error())
		return
	}
	ctx := context.Background()
	var result any
	var err error
	switch p.Name {
	case "load_file":
		result, err = toolLoadFile(cfg, p.Arguments)
	case "ai_denoise_records":
		result, err = toolAIDenoiseRecords(ctx, cfg, p.Arguments)
	case "ai_denoise_file":
		result, err = toolAIDenoiseFile(ctx, cfg, p.Arguments)
	case "summarize_run":
		result, err = toolSummarizeRun(p.Arguments)
	case "write_file":
		result, err = toolWriteFile(cfg, p.Arguments)
	default:
		writeToolError(w, req.ID, "unknown tool: "+p.Name)
		return
	}
	if err != nil {
		writeToolError(w, req.ID, err.Error())
		return
	}
	writeToolOK(w, req.ID, result)
}

func toolLoadFile(cfg config, args map[string]any) (map[string]any, error) {
	path, _ := args["path"].(string)
	format, _ := args["format"].(string)
	full, err := safeInboxPath(cfg.Inbox, path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(full); err != nil {
		return nil, err
	}
	records, err := loadRecords(full, format)
	if err != nil {
		return nil, err
	}
	max := cfg.MaxRecords
	if v, ok := args["max_records"].(float64); ok && int(v) > 0 {
		max = int(v)
	}
	records = truncateRecords(records, max)
	return map[string]any{
		"path":    path,
		"count":   len(records),
		"records": records,
	}, nil
}

func toolAIDenoiseRecords(ctx context.Context, cfg config, args map[string]any) (map[string]any, error) {
	records, err := recordsFromArgs(args)
	if err != nil {
		return nil, err
	}
	instructions, _ := args["instructions"].(string)
	if strings.TrimSpace(instructions) == "" {
		instructions = "Remove invalid and duplicate records; normalize obvious field issues."
	}
	records = truncateRecords(records, cfg.MaxRecords)
	out, err := aiDenoiseRecords(ctx, cfg.LLM, records, instructions)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"count":     len(out.Records),
		"records":   out.Records,
		"changelog": out.Changelog,
		"mock_llm":  cfg.LLM.Mock,
	}, nil
}

func toolAIDenoiseFile(ctx context.Context, cfg config, args map[string]any) (map[string]any, error) {
	path, _ := args["path"].(string)
	format, _ := args["format"].(string)
	instructions, _ := args["instructions"].(string)
	loaded, err := toolLoadFile(cfg, map[string]any{"path": path, "format": format})
	if err != nil {
		return nil, err
	}
	merged := map[string]any{
		"records":      loaded["records"],
		"instructions": instructions,
	}
	return toolAIDenoiseRecords(ctx, cfg, merged)
}

func toolSummarizeRun(args map[string]any) (map[string]any, error) {
	before, _ := args["before"].([]any)
	after, _ := args["after"].([]any)
	changelog := stringSlice(args["changelog"])
	return map[string]any{
		"before_count": len(before),
		"after_count":  len(after),
		"removed":      len(before) - len(after),
		"changelog":    changelog,
	}, nil
}

func toolWriteFile(cfg config, args map[string]any) (map[string]any, error) {
	path, _ := args["path"].(string)
	format, _ := args["format"].(string)
	records, err := recordsFromArgs(args)
	if err != nil {
		return nil, err
	}
	full, err := safeOutboxPath(cfg.Outbox, path)
	if err != nil {
		return nil, err
	}
	if err := writeRecords(full, format, records); err != nil {
		return nil, err
	}
	return map[string]any{"path": path, "count": len(records)}, nil
}

func recordsFromArgs(args map[string]any) ([]map[string]any, error) {
	raw, ok := args["records"]
	if !ok {
		return nil, fmt.Errorf("records required")
	}
	switch v := raw.(type) {
	case []map[string]any:
		return v, nil
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("records must be objects")
			}
			out = append(out, m)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("records must be an array")
	}
}

func stringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func toolSchemas() []map[string]any {
	return []map[string]any{
		toolSchema("load_file", "Read a file from inbox and parse into records[].", false, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":        map[string]any{"type": "string"},
				"format":      map[string]any{"type": "string", "enum": []string{"json", "jsonl", "csv"}},
				"max_records": map[string]any{"type": "integer"},
			},
			"required": []string{"path"},
		}),
		toolSchema("ai_denoise_records", "AI-denoise records using LLM per instructions.", false, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"records":       map[string]any{"type": "array"},
				"instructions":  map[string]any{"type": "string"},
				"model":         map[string]any{"type": "string"},
			},
			"required": []string{"records", "instructions"},
		}),
		toolSchema("ai_denoise_file", "Load inbox file and AI-denoise in one step.", false, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":         map[string]any{"type": "string"},
				"format":       map[string]any{"type": "string"},
				"instructions": map[string]any{"type": "string"},
			},
			"required": []string{"path", "instructions"},
		}),
		toolSchema("summarize_run", "Summarize before/after counts and changelog.", false, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"before":    map[string]any{"type": "array"},
				"after":     map[string]any{"type": "array"},
				"changelog": map[string]any{"type": "array"},
			},
		}),
		toolSchema("write_file", "Write records to outbox file.", true, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"format":  map[string]any{"type": "string"},
				"records": map[string]any{"type": "array"},
			},
			"required": []string{"path", "records"},
		}),
	}
}

func toolSchema(name, desc string, destructive bool, schema map[string]any) map[string]any {
	ann := map[string]any{"destructiveHint": destructive}
	return map[string]any{
		"name":        name,
		"description": desc,
		"inputSchema": schema,
		"annotations": ann,
	}
}

func writeResult(w http.ResponseWriter, id, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rpcResp{JSONRPC: "2.0", ID: id, Result: result})
}

func writeErr(w http.ResponseWriter, id any, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rpcResp{JSONRPC: "2.0", ID: id, Error: &rpcErr{Code: code, Message: msg}})
}

func writeToolOK(w http.ResponseWriter, id any, result any) {
	writeResult(w, id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": mustJSON(result)}},
	})
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

func envInt(k string, def int) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
		return def
	}
	return n
}
