package template

import (
	"regexp"
	"strings"
)

// Classify picks the best-matching template for a user message, or "" if none.
// v1 uses keyword rules; Slice 19b+ may add embedding scores.
func Classify(userMessage string) string {
	msg := strings.ToLower(strings.TrimSpace(userMessage))
	switch {
	case strings.Contains(msg, "webhook") || strings.Contains(msg, "回调"):
		return "webhook-forward"
	case strings.Contains(msg, "每周") || strings.Contains(msg, "定时") || strings.Contains(msg, "cron"):
		return "cron-notify"
	case strings.Contains(msg, "http") || strings.Contains(msg, "拉取") || strings.Contains(msg, "fetch"):
		return "http-fetch-notify"
	case strings.Contains(msg, "摘要") || strings.Contains(msg, "summarize") || strings.Contains(msg, "总结"):
		return "llm-summarize-notify"
	case strings.Contains(msg, "工具链") || strings.Contains(msg, "顺序") || strings.Contains(msg, "chain"):
		return "tool-chain"
	default:
		return ""
	}
}

var cronRe = regexp.MustCompile(`(?i)(\d+\s+\d+\s+\*\s+\*\s+[0-9\-]+|\d+\s+\d+\s+\*\s+\*\s+\*)`)

// ExtractSlots fills template slots from natural language (rule-based v1).
func ExtractSlots(templateID, userMessage string, slug, name string) (map[string]any, error) {
	_ = slug
	_ = name
	msg := strings.TrimSpace(userMessage)
	switch templateID {
	case "cron-notify":
		cron := "0 9 * * 1-5"
		if m := cronRe.FindString(msg); m != "" {
			cron = m
		}
		return map[string]any{
			"schedule_cron": cron,
			"message":       msg,
			"notify_tool":   "llm.chat",
			"notify_args": map[string]any{
				"model": "default-mock:text",
				"messages": []map[string]string{
					{"role": "user", "content": msg},
				},
			},
		}, nil
	case "webhook-forward":
		path := "incoming"
		if i := strings.Index(msg, "/"); i >= 0 {
			path = strings.Fields(msg[i+1:])[0]
		}
		return map[string]any{
			"webhook_path":  path,
			"forward_tool":  "llm.chat",
			"forward_args": map[string]any{
				"model": "default-mock:text",
				"messages": []map[string]string{
					{"role": "user", "content": "forward webhook payload"},
				},
			},
		}, nil
	case "http-fetch-notify":
		url := "https://example.com"
		if strings.Contains(msg, "http") {
			for _, tok := range strings.Fields(msg) {
				if strings.HasPrefix(tok, "http") {
					url = strings.Trim(tok, ".,;")
					break
				}
			}
		}
		return map[string]any{
			"url":         url,
			"method":      "GET",
			"notify_tool": "llm.chat",
			"notify_args": map[string]any{
				"model": "default-mock:text",
				"messages": []map[string]string{
					{"role": "user", "content": "notify fetch result"},
				},
			},
		}, nil
	case "llm-summarize-notify":
		return map[string]any{
			"prompt":      msg,
			"model":       "default-mock:text",
			"notify_tool": "llm.chat",
			"notify_args": map[string]any{
				"model": "default-mock:text",
				"messages": []map[string]string{
					{"role": "user", "content": "notification: " + msg},
				},
			},
		}, nil
	case "tool-chain":
		return map[string]any{
			"steps": []map[string]any{
				{
					"id": "step1", "use": "llm.chat",
					"args": map[string]any{
						"model": "default-mock:text",
						"messages": []map[string]string{{"role": "user", "content": msg}},
					},
				},
			},
		}, nil
	default:
		return nil, nil
	}
}

// ClassifyAndExtract runs Classify then ExtractSlots when a template matches.
func ClassifyAndExtract(userMessage, slug, name string) (templateID string, slots map[string]any, ok bool) {
	templateID = Classify(userMessage)
	if templateID == "" {
		return "", nil, false
	}
	s, err := ExtractSlots(templateID, userMessage, slug, name)
	if err != nil {
		return "", nil, false
	}
	return templateID, s, true
}
