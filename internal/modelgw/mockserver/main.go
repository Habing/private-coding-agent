// Command mockserver runs a minimal OpenAI-compatible HTTP server for E2E tests.
// It returns canned responses without calling any real model backend.
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	addr := ":8081"
	if v := os.Getenv("PORT"); v != "" {
		addr = ":" + v
	}
	http.HandleFunc("/v1/chat/completions", chat)
	http.HandleFunc("/v1/embeddings", embed)
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	slog.Info("mock-provider listening", "addr", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		slog.Error("mock-provider exited", "err", err.Error())
		os.Exit(1)
	}
}

type mockMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
}

func chat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model    string        `json:"model"`
		Stream   bool          `json:"stream"`
		Messages []mockMessage `json:"messages"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Stream {
		streamChat(w, req.Model)
		return
	}

	// Simple state machine: look at the last message in the conversation.
	var last mockMessage
	if n := len(req.Messages); n > 0 {
		last = req.Messages[n-1]
	}

	w.Header().Set("Content-Type", "application/json")

	switch {
	case last.Role == "tool":
		// We've already executed a tool — return a final answer.
		writeFinal(w, req.Model, "done")
	case last.Role == "user" && containsAny(strings.ToLower(last.Content), "list", "ls"):
		// User asked to list — issue a fs.list tool_call.
		path := extractSandbox(last.Content)
		writeToolCall(w, req.Model, "call_mock_1", "fs.list",
			fmt.Sprintf(`{"sandbox_id":%q,"path":"/workspace"}`, path))
	default:
		writeFinal(w, req.Model, "hello from mock")
	}
}

func writeFinal(w http.ResponseWriter, model, text string) {
	resp := map[string]any{
		"id": "mock-1", "object": "chat.completion",
		"created": time.Now().Unix(), "model": model,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role": "assistant", "content": text,
			},
			"finish_reason": "stop",
		}},
		"usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 4, "total_tokens": 9},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func writeToolCall(w http.ResponseWriter, model, callID, name, argsJSON string) {
	resp := map[string]any{
		"id": "mock-1", "object": "chat.completion",
		"created": time.Now().Unix(), "model": model,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []map[string]any{{
					"id":   callID,
					"type": "function",
					"function": map[string]any{
						"name":      name,
						"arguments": argsJSON,
					},
				}},
			},
			"finish_reason": "tool_calls",
		}},
		"usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 4, "total_tokens": 9},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// extractSandbox pulls a UUID-looking sandbox id from a free-form message.
// Returns empty string if not found.
func extractSandbox(s string) string {
	for _, tok := range strings.Fields(s) {
		tok = strings.Trim(tok, ".,\"'`")
		if len(tok) == 36 && strings.Count(tok, "-") == 4 {
			return tok
		}
	}
	return ""
}

func streamChat(w http.ResponseWriter, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	fl := w.(http.Flusher)
	send := func(payload map[string]any) {
		b, _ := json.Marshal(payload)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		fl.Flush()
	}
	send(map[string]any{
		"id": "mock-1", "object": "chat.completion.chunk", "model": model,
		"choices": []map[string]any{{"index": 0,
			"delta": map[string]any{"role": "assistant"}}},
	})
	for _, c := range []string{"hello ", "from ", "mock"} {
		send(map[string]any{
			"id": "mock-1", "object": "chat.completion.chunk", "model": model,
			"choices": []map[string]any{{"index": 0,
				"delta": map[string]any{"content": c}}},
		})
	}
	finish := "stop"
	send(map[string]any{
		"id": "mock-1", "object": "chat.completion.chunk", "model": model,
		"choices": []map[string]any{{"index": 0,
			"delta": map[string]any{}, "finish_reason": finish}},
		"usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8},
	})
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	fl.Flush()
}

func embed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	data := make([]map[string]any, 0, len(req.Input))
	for i := range req.Input {
		data = append(data, map[string]any{
			"index": i, "object": "embedding",
			"embedding": []float64{0.1, 0.2, 0.3},
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list", "data": data, "model": req.Model,
		"usage": map[string]int{"prompt_tokens": 1, "total_tokens": 1},
	})
}
