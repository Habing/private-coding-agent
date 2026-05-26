package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type llmConfig struct {
	BaseURL   string
	APIKey    string
	Model     string
	Mock      bool
	Timeout   time.Duration
}

func loadLLMConfig() llmConfig {
	mock := strings.EqualFold(os.Getenv("DATA_PREP_MOCK_LLM"), "true") ||
		strings.TrimSpace(os.Getenv("LLM_API_KEY")) == ""
	base := strings.TrimRight(strings.TrimSpace(env("LLM_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1")), "/")
	return llmConfig{
		BaseURL: base,
		APIKey:  strings.TrimSpace(os.Getenv("LLM_API_KEY")),
		Model:   env("LLM_MODEL", "qwen-plus"),
		Mock:    mock,
		Timeout: time.Duration(envInt("LLM_TIMEOUT_SEC", 120)) * time.Second,
	}
}

type denoiseResult struct {
	Records   []map[string]any `json:"records"`
	Changelog []string         `json:"changelog"`
}

func aiDenoiseRecords(ctx context.Context, cfg llmConfig, records []map[string]any, instructions string) (denoiseResult, error) {
	if len(records) == 0 {
		return denoiseResult{Records: records, Changelog: []string{"empty input"}}, nil
	}
	if cfg.Mock {
		return mockDenoise(records, instructions), nil
	}
	return llmDenoise(ctx, cfg, records, instructions)
}

func mockDenoise(records []map[string]any, instructions string) denoiseResult {
	seen := map[string]struct{}{}
	out := make([]map[string]any, 0, len(records))
	var changelog []string
	for i, rec := range records {
		if isNoiseRecord(rec) {
			changelog = append(changelog, fmt.Sprintf("row %d: dropped as noise (mock)", i))
			continue
		}
		id, _ := rec["id"].(string)
		if id != "" {
			if _, ok := seen[id]; ok {
				changelog = append(changelog, fmt.Sprintf("row %d: duplicate id %q (mock)", i, id))
				continue
			}
			seen[id] = struct{}{}
		}
		out = append(out, rec)
	}
	if strings.TrimSpace(instructions) != "" {
		changelog = append([]string{fmt.Sprintf("mock mode; instructions noted: %s", truncate(instructions, 120))}, changelog...)
	}
	return denoiseResult{Records: out, Changelog: changelog}
}

func isNoiseRecord(rec map[string]any) bool {
	if rec == nil || len(rec) == 0 {
		return true
	}
	if v, ok := rec["noise"]; ok {
		switch x := v.(type) {
		case bool:
			return x
		case string:
			return strings.EqualFold(x, "true") || x == "1"
		}
	}
	name, _ := rec["name"].(string)
	return strings.TrimSpace(name) == ""
}

func llmDenoise(ctx context.Context, cfg llmConfig, records []map[string]any, instructions string) (denoiseResult, error) {
	payload, err := json.Marshal(records)
	if err != nil {
		return denoiseResult{}, err
	}
	sys := `You are a data cleaning assistant. Given JSON records and user instructions, return ONLY valid JSON:
{"records":[...],"changelog":["human-readable change summaries"]}
Remove noise rows, fix obvious issues, deduplicate by id when present. Preserve semantic meaning.`
	user := fmt.Sprintf("Instructions:\n%s\n\nInput records JSON:\n%s", instructions, string(payload))

	body := map[string]any{
		"model": cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": sys},
			{"role": "user", "content": user},
		},
		"temperature": 0.1,
	}
	raw, err := postChat(ctx, cfg, body)
	if err != nil {
		return denoiseResult{}, err
	}
	text := extractAssistantContent(raw)
	text = stripJSONFence(text)
	var out denoiseResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return denoiseResult{}, fmt.Errorf("llm json parse: %w; raw=%s", err, truncate(text, 400))
	}
	if out.Records == nil {
		out.Records = []map[string]any{}
	}
	if out.Changelog == nil {
		out.Changelog = []string{"llm denoise completed"}
	}
	return out, nil
}

func postChat(ctx context.Context, cfg llmConfig, body map[string]any) (map[string]any, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	url := cfg.BaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	client := &http.Client{Timeout: cfg.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("llm http %d: %s", resp.StatusCode, truncate(string(respBody), 300))
	}
	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func extractAssistantContent(resp map[string]any) string {
	choices, _ := resp["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	first, _ := choices[0].(map[string]any)
	msg, _ := first["message"].(map[string]any)
	content, _ := msg["content"].(string)
	return content
}

func stripJSONFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
