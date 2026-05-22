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

	w.Header().Set("Content-Type", "application/json")

	resp := pickDeterministicResponse(req.Messages)
	if resp.kind == "tool_call" {
		writeToolCall(w, req.Model, resp.callID, resp.toolName, resp.toolArgs)
		return
	}
	writeFinal(w, req.Model, resp.text)
}

// mockResponse is the canned reaction produced by pickDeterministicResponse.
// Either kind == "final" with text, or kind == "tool_call" with toolName +
// toolArgs (+ callID).
type mockResponse struct {
	kind     string // "final" or "tool_call"
	text     string
	toolName string
	toolArgs string
	callID   string
}

// pickDeterministicResponse centralises mock-provider dispatch. New markers
// (skills, tenant skills, delegate parent/sub …) plug in here so the chat
// and streamChat code paths stay in lockstep.
//
// Priority (highest to lowest):
//  0a. orchestrator hint marker — set on a system message by Slice 21a's
//      router. Returns a canned final so the E2E suite can prove the hint
//      made it from the rule engine into the LLM prompt.
//  0b. reflection marker — set on the Reflector's system prompt; returns a
//      canned JSON array so the worker has something deterministic to parse.
//  1. tenant-skill marker — DB-backed skill round-trip.
//  2. delegate sub marker — set on the review profile's system prompt; signals
//     "this Run is the child of an agent.delegate". Returns the canonical
//     final string the E2E suite asserts on.
//  3. delegate parent marker — token in the user message; first turn returns
//     a tool_call for agent.delegate(review,…); after the tool result comes
//     back, returns the parent's final answer that includes the sub result.
//  4. skill marker — FS skill round-trip.
//  5. last message is a tool observation — "done" final.
//  6. last user message asks to list workspace — fs.list tool_call.
//  7. fallback: "hello from mock" final.
func pickDeterministicResponse(msgs []mockMessage) mockResponse {
	var last mockMessage
	if n := len(msgs); n > 0 {
		last = msgs[n-1]
	}

	if hasOrchestratorHint(msgs) {
		return mockResponse{kind: "final", text: "orchestrator-hint-ok"}
	}
	if hasReflectionMarker(msgs) {
		return mockResponse{
			kind: "final",
			text: `[{"type":"preference","content":"E2E test prefers golang generics","tags":["golang","e2e"],"confidence":0.5}]`,
		}
	}
	if hasTenantSkillMarker(msgs) {
		return mockResponse{kind: "final", text: "tenant-skill-marker-ok"}
	}
	// Sub-Run check runs BEFORE tool-message check: a child Run will only
	// ever see system + user messages on its first call (the parent's tool
	// observation lives in the parent's history, not the child's), so this
	// ordering is safe — and protects us if a future change ever surfaces
	// tool messages into the child somehow.
	if hasDelegateSubMarker(msgs) {
		return mockResponse{kind: "final", text: "delegate-sub-marker-ok"}
	}
	if hasDelegateParentMarker(msgs) {
		// Second turn: the role=tool observation is back, time to finalise.
		if hasToolMessage(msgs) {
			return mockResponse{
				kind: "final",
				text: "delegate-parent-final: " + summariseDelegateResult(msgs),
			}
		}
		// First turn: emit a tool_call asking the review profile to look it over.
		return mockResponse{
			kind:     "tool_call",
			callID:   "call_delegate_1",
			toolName: "agent.delegate",
			toolArgs: `{"profile":"review","task":"E2E delegate sub-task — please review"}`,
		}
	}
	if hasSkillMarker(msgs) {
		return mockResponse{kind: "final", text: "skill-marker-ok"}
	}
	if hasWorkflowMarker(msgs) {
		if hasToolMessage(msgs) {
			return mockResponse{kind: "final", text: "workflow-tool-final: ok"}
		}
		return mockResponse{
			kind:     "tool_call",
			callID:   "call_workflow_1",
			toolName: "workflow.e2e-demo",
			toolArgs: `{"name":"World"}`,
		}
	}

	switch {
	case last.Role == "tool":
		return mockResponse{kind: "final", text: "done"}
	case last.Role == "user" && containsAny(strings.ToLower(last.Content), "list", "ls", "files", "workspace"):
		path := sandboxIDFromMessages(msgs)
		return mockResponse{
			kind:     "tool_call",
			callID:   "call_mock_1",
			toolName: "fs.list",
			toolArgs: fmt.Sprintf(`{"sandbox_id":%q,"path":"/workspace"}`, path),
		}
	default:
		return mockResponse{kind: "final", text: "hello from mock"}
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

// delegateParentMarker rides on the user message that kicks off a delegate
// chain in E2E step 50. Triggers a tool_call agent.delegate(...) on the
// parent's first turn.
const delegateParentMarker = "E2E_DELEGATE_PARENT_V1"

// delegateSubMarker is embedded in the review profile's system prompt so the
// child Run (which never sees the parent's user message) can still be
// identified and produce the canonical sub-final string.
const delegateSubMarker = "E2E_DELEGATE_SUB_V1"

// workflowMarker rides on the user message that kicks off the Slice 19 E2E.
// First turn emits a tool_call workflow.e2e-demo; second turn (after the tool
// observation) closes out with the canonical final string.
const workflowMarker = "E2E_WORKFLOW_V1"

// reflectionMarker is the token Reflector embeds in its system prompt. Kept in
// sync with reflection.ReflectionMarker (we can't import the package — this
// is the leaf binary).
const reflectionMarker = "REFLECTION_TASK_V1"

// orchestratorHintMarker is the literal substring Slice 21a's rule engine
// embeds in the routing hint system message for E2E step 62. Coupled to the
// hint text in deploy/compose/docker-compose.yml's orchestrator rules YAML.
const orchestratorHintMarker = "ORCHESTRATOR_E2E_HINT_DELIVERED"

func hasOrchestratorHint(msgs []mockMessage) bool {
	for _, m := range msgs {
		if m.Role == "system" && strings.Contains(m.Content, orchestratorHintMarker) {
			return true
		}
	}
	return false
}

func hasReflectionMarker(msgs []mockMessage) bool {
	for _, m := range msgs {
		if m.Role == "system" && strings.Contains(m.Content, reflectionMarker) {
			return true
		}
	}
	return false
}

func hasWorkflowMarker(msgs []mockMessage) bool {
	for _, m := range msgs {
		if m.Role == "user" && strings.Contains(m.Content, workflowMarker) {
			return true
		}
	}
	return false
}

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

func hasDelegateParentMarker(msgs []mockMessage) bool {
	for _, m := range msgs {
		if m.Role == "user" && strings.Contains(m.Content, delegateParentMarker) {
			return true
		}
	}
	return false
}

func hasDelegateSubMarker(msgs []mockMessage) bool {
	for _, m := range msgs {
		if m.Role == "system" && strings.Contains(m.Content, delegateSubMarker) {
			return true
		}
	}
	return false
}

func hasToolMessage(msgs []mockMessage) bool {
	for _, m := range msgs {
		if m.Role == "tool" {
			return true
		}
	}
	return false
}

// summariseDelegateResult extracts the embedded sub-final string from the
// tool observation. The delegate tool returns JSON like
// {"result":"delegate-sub-marker-ok",...}; rather than parse it, we just look
// for the canonical sub-marker and echo it back so the E2E assertion has a
// single string to match. Falls back to a fixed placeholder.
func summariseDelegateResult(msgs []mockMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "tool" && strings.Contains(msgs[i].Content, "delegate-sub-marker-ok") {
			return "delegate-sub-marker-ok"
		}
	}
	return "ok"
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
// "hello from mock" is split into a few chunks to exercise the streaming code
// path; everything else is emitted as a single chunk for simplicity.
func streamTextDeltas(send func(map[string]any), model, text string) {
	if text == "" {
		return
	}
	if text == "hello from mock" {
		for _, c := range []string{"hello ", "from ", "mock"} {
			send(map[string]any{
				"id": "mock-1", "object": "chat.completion.chunk", "model": model,
				"choices": []map[string]any{{"index": 0,
					"delta": map[string]any{"content": c}}},
			})
		}
		return
	}
	send(map[string]any{
		"id": "mock-1", "object": "chat.completion.chunk", "model": model,
		"choices": []map[string]any{{"index": 0,
			"delta": map[string]any{"content": text}}},
	})
}

func streamChat(w http.ResponseWriter, model string, msgs []mockMessage) {
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

	resp := pickDeterministicResponse(msgs)
	var finish string
	if resp.kind == "tool_call" {
		finish = "tool_calls"
		send(map[string]any{
			"id": "mock-1", "object": "chat.completion.chunk", "model": model,
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{
				"tool_calls": []map[string]any{{
					"index": 0, "id": resp.callID, "type": "function",
					"function": map[string]any{"name": resp.toolName, "arguments": resp.toolArgs},
				}},
			}}},
		})
	} else {
		finish = "stop"
		streamTextDeltas(send, model, resp.text)
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
