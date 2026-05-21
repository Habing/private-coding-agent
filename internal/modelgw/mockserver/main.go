// Command mockserver runs a minimal OpenAI-compatible HTTP server for E2E tests.
// It returns canned responses without calling any real model backend.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

// embedDim is the fixed vector width returned by the mock embeddings
// endpoint. Must match internal/memory.EmbeddingDim and the DB column.
const embedDim = 1536

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
		streamChat(w, req.Model, req.Messages)
		return
	}

	// Simple state machine: look at the last message in the conversation.
	var last mockMessage
	if n := len(req.Messages); n > 0 {
		last = req.Messages[n-1]
	}

	w.Header().Set("Content-Type", "application/json")

	// Skills E2E hooks: a literal marker in any system message proves the
	// SkillComposer injected the body into the run. The tenant marker is
	// distinct so the e2e suite can tell DB skills apart from FS skills.
	if hasTenantSkillMarker(req.Messages) {
		writeFinal(w, req.Model, "tenant-skill-marker-ok")
		return
	}
	if hasSkillMarker(req.Messages) {
		writeFinal(w, req.Model, "skill-marker-ok")
		return
	}

	switch {
	case last.Role == "tool":
		// We've already executed a tool — return a final answer.
		writeFinal(w, req.Model, "done")
	case last.Role == "user" && containsAny(strings.ToLower(last.Content), "list", "ls", "files", "workspace"):
		// User asked to list — issue a fs.list tool_call (sandbox from system inject or message).
		path := sandboxIDFromMessages(req.Messages)
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

// skillMarker is the literal token embedded in skills/e2e/e2e-marker/SKILL.md.
// Coupled to that file by intent: tests rely on the chain registry → resolver
// → composer → modelgw → mockserver to surface the marker end-to-end.
const skillMarker = "E2E_SKILL_MARKER_V1"

// tenantSkillMarker is the literal token DB-backed Skills inject in e2e.
// Distinct from skillMarker so the test can prove the resolver picked up the
// DB row (and not just a stale FS skill).
const tenantSkillMarker = "E2E_TENANT_SKILL_V1"

func hasSkillMarker(msgs []mockMessage) bool {
	for _, m := range msgs {
		if m.Role == "system" && strings.Contains(m.Content, skillMarker) {
			return true
		}
	}
	return false
}

func hasTenantSkillMarker(msgs []mockMessage) bool {
	for _, m := range msgs {
		if m.Role == "system" && strings.Contains(m.Content, tenantSkillMarker) {
			return true
		}
	}
	return false
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// sandboxIDFromMessages prefers the slice-14 system inject line, then scans
// the last user message for a UUID token.
func sandboxIDFromMessages(msgs []mockMessage) string {
	for _, m := range msgs {
		if m.Role != "system" {
			continue
		}
		if id := parseInjectedSandboxID(m.Content); id != "" {
			return id
		}
	}
	if n := len(msgs); n > 0 && msgs[n-1].Role == "user" {
		return extractSandbox(msgs[n-1].Content)
	}
	return ""
}

func parseInjectedSandboxID(s string) string {
	const prefix = "Current sandbox_id:"
	idx := strings.Index(s, prefix)
	if idx < 0 {
		return ""
	}
	return extractSandbox(strings.TrimSpace(s[idx+len(prefix):]))
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

// streamTextDeltas emits content chunks that concatenate to text exactly.
func streamTextDeltas(send func(map[string]any), model, text string) {
	switch text {
	case "hello from mock":
		for _, c := range []string{"hello ", "from ", "mock"} {
			send(map[string]any{
				"id": "mock-1", "object": "chat.completion.chunk", "model": model,
				"choices": []map[string]any{{"index": 0,
					"delta": map[string]any{"content": c}}},
			})
		}
		return
	case "skill-marker-ok", "tenant-skill-marker-ok", "done":
		send(map[string]any{
			"id": "mock-1", "object": "chat.completion.chunk", "model": model,
			"choices": []map[string]any{{"index": 0,
				"delta": map[string]any{"content": text}}},
		})
		return
	default:
		if text != "" {
			send(map[string]any{
				"id": "mock-1", "object": "chat.completion.chunk", "model": model,
				"choices": []map[string]any{{"index": 0,
					"delta": map[string]any{"content": text}}},
			})
		}
	}
}

func streamChat(w http.ResponseWriter, model string, msgs []mockMessage) {
	w.Header().Set("Content-Type", "text/event-stream")
	fl := w.(http.Flusher)
	send := func(payload map[string]any) {
		b, _ := json.Marshal(payload)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		fl.Flush()
	}

	var last mockMessage
	if n := len(msgs); n > 0 {
		last = msgs[n-1]
	}

	send(map[string]any{
		"id": "mock-1", "object": "chat.completion.chunk", "model": model,
		"choices": []map[string]any{{"index": 0,
			"delta": map[string]any{"role": "assistant"}}},
	})

	var text string
	var finish string
	var toolName, toolArgs, toolID string

	switch {
	case hasTenantSkillMarker(msgs):
		text, finish = "tenant-skill-marker-ok", "stop"
	case hasSkillMarker(msgs):
		text, finish = "skill-marker-ok", "stop"
	case last.Role == "tool":
		text, finish = "done", "stop"
	case last.Role == "user" && containsAny(strings.ToLower(last.Content), "list", "ls", "files", "workspace"):
		path := sandboxIDFromMessages(msgs)
		toolID, toolName = "call_mock_1", "fs.list"
		toolArgs = fmt.Sprintf(`{"sandbox_id":%q,"path":"/workspace"}`, path)
		finish = "tool_calls"
	default:
		text, finish = "hello from mock", "stop"
	}

	if finish == "tool_calls" {
		send(map[string]any{
			"id": "mock-1", "object": "chat.completion.chunk", "model": model,
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{
				"tool_calls": []map[string]any{{
					"index": 0, "id": toolID, "type": "function",
					"function": map[string]any{"name": toolName, "arguments": toolArgs},
				}},
			}}},
		})
	} else {
		streamTextDeltas(send, model, text)
	}

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
	for i, in := range req.Input {
		data = append(data, map[string]any{
			"index": i, "object": "embedding",
			"embedding": deterministicVec(in, embedDim),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list", "data": data, "model": req.Model,
		"usage": map[string]int{"prompt_tokens": 1, "total_tokens": 1},
	})
}

// deterministicVec builds a unit-length float vector seeded by sha256(input).
// Properties exploited by tests:
//   - same input always returns the same vector (dedup is testable)
//   - different inputs produce different vectors (vector ranking is non-trivial)
//   - L2-normalized → cosine similarity = dot product, bounded in [-1, 1]
func deterministicVec(s string, dim int) []float64 {
	out := make([]float64, dim)
	var sum float64
	// Repeatedly hash (seed || counter) to fill the slice with 8-byte floats.
	seed := sha256.Sum256([]byte(s))
	var ctr uint32
	for i := 0; i < dim; i++ {
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], ctr)
		h := sha256.Sum256(append(seed[:], buf[:]...))
		// Map first 8 bytes to a float64 in [-1, 1) via uint64 → fraction.
		u := binary.BigEndian.Uint64(h[:8])
		f := float64(u)/float64(math.MaxUint64)*2 - 1
		out[i] = f
		sum += f * f
		ctr++
	}
	norm := math.Sqrt(sum)
	if norm == 0 {
		return out
	}
	for i := range out {
		out[i] /= norm
	}
	return out
}
