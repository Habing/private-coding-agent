package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestToolsList_HasThreeTools(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	handle(rec, req)

	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range resp.Result.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"echo", "fetch_status", "record_event"} {
		if !names[want] {
			t.Fatalf("missing tool %q: %v", want, names)
		}
	}
}

func TestFetchStatus_Degraded(t *testing.T) {
	out := callTool(t, "fetch_status", map[string]any{})
	text := toolText(out)
	if text != "degraded" {
		t.Fatalf("got %q", text)
	}
}

func TestEcho_PlainText(t *testing.T) {
	out := callTool(t, "echo", map[string]any{"text": "hi"})
	if toolText(out) != "echo: hi" {
		t.Fatalf("got %v", out)
	}
}

func TestRecordEvent_Recorded(t *testing.T) {
	out := callTool(t, "record_event", map[string]any{"kind": "test"})
	var payload map[string]any
	decodeToolText(t, out, &payload)
	if payload["recorded"] != true {
		t.Fatalf("got %v", payload)
	}
}

func callTool(t *testing.T, name string, args map[string]any) map[string]any {
	t.Helper()
	params, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": json.RawMessage(params)}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	handle(rec, req)
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	res, _ := resp["result"].(map[string]any)
	return res
}

func toolText(result map[string]any) string {
	content, _ := result["content"].([]any)
	block, _ := content[0].(map[string]any)
	text, _ := block["text"].(string)
	return text
}

func decodeToolText(t *testing.T, result map[string]any, dest any) {
	t.Helper()
	if err := json.Unmarshal([]byte(toolText(result)), dest); err != nil {
		t.Fatal(err)
	}
}
