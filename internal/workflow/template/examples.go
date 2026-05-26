package template

import "fmt"

// ExampleSlots returns demo slot values suitable for preview and catalog smoke tests.
func ExampleSlots(templateID string) (map[string]any, error) {
	switch templateID {
	case "cron-notify":
		return map[string]any{
			"schedule_cron": "0 9 * * 1-5",
			"message":       "weekly report",
			"notify_tool":   "llm.chat",
			"notify_args": map[string]any{
				"model":    "default-mock:text",
				"messages": []map[string]string{{"role": "user", "content": "hi"}},
			},
		}, nil
	case "webhook-forward":
		return map[string]any{
			"webhook_path": "github",
			"forward_tool": "llm.chat",
			"forward_args": map[string]any{
				"model":    "default-mock:text",
				"messages": []map[string]string{{"role": "user", "content": "fwd"}},
			},
		}, nil
	case "http-fetch-notify":
		return map[string]any{
			"url":         "https://example.com",
			"method":      "GET",
			"notify_tool": "llm.chat",
			"notify_args": map[string]any{
				"model":    "default-mock:text",
				"messages": []map[string]string{{"role": "user", "content": "notify"}},
			},
		}, nil
	case "llm-summarize-notify":
		return map[string]any{
			"prompt":      "summarize tasks",
			"notify_tool": "llm.chat",
			"notify_args": map[string]any{
				"model":    "default-mock:text",
				"messages": []map[string]string{{"role": "user", "content": "notify"}},
			},
		}, nil
	case "tool-chain":
		return map[string]any{
			"steps": []map[string]any{
				{
					"id":  "a",
					"use": "llm.chat",
					"args": map[string]any{
						"model":    "default-mock:text",
						"messages": []map[string]string{{"role": "user", "content": "x"}},
					},
				},
			},
		}, nil
	case "mock-inspect":
		return map[string]any{
			"scenario":   "degraded",
			"alert_text": "ALERT: system degraded",
			"ok_text":    "OK: system healthy",
		}, nil
	default:
		return nil, fmt.Errorf("template: unknown id %q", templateID)
	}
}
