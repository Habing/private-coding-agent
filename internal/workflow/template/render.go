package template

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// RenderInput carries workflow metadata and slot values for template rendering.
type RenderInput struct {
	Slug        string
	Name        string
	Description string
	Slots       map[string]any
}

// Render produces DSL YAML for a catalog template id.
func Render(templateID string, in RenderInput) (string, error) {
	def, err := Get(templateID)
	if err != nil {
		return "", err
	}
	if in.Slug == "" || in.Name == "" {
		return "", fmt.Errorf("slug and name are required")
	}
	slots := MergeDefaults(def, in.Slots)
	if err := ValidateSlots(def, slots); err != nil {
		return "", err
	}

	var doc yamlDoc
	switch templateID {
	case "cron-notify":
		doc, err = renderCronNotify(in, slots)
	case "webhook-forward":
		doc, err = renderWebhookForward(in, slots)
	case "http-fetch-notify":
		doc, err = renderHTTPFetchNotify(in, slots)
	case "llm-summarize-notify":
		doc, err = renderLLMSummarizeNotify(in, slots)
	case "tool-chain":
		doc, err = renderToolChain(in, slots)
	default:
		return "", fmt.Errorf("template: no renderer for %q", templateID)
	}
	if err != nil {
		return "", err
	}
	if doc.ID != in.Slug {
		doc.ID = in.Slug
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return "", fmt.Errorf("marshal dsl: %w", err)
	}
	return string(out), nil
}

// yamlDoc mirrors the workflow DSL shape for marshaling (kept in this package
// to avoid an import cycle with internal/workflow).
type yamlDoc struct {
	ID          string                 `yaml:"id"`
	Name        string                 `yaml:"name"`
	Description string                 `yaml:"description,omitempty"`
	Inputs      map[string]yamlInput   `yaml:"inputs,omitempty"`
	Steps       []yamlStep             `yaml:"steps"`
	Outputs     map[string]string      `yaml:"outputs,omitempty"`
}

type yamlInput struct {
	Type    string `yaml:"type"`
	Default any    `yaml:"default,omitempty"`
}

type yamlStep struct {
	ID      string            `yaml:"id"`
	Use     string            `yaml:"use,omitempty"`
	Args    map[string]any    `yaml:"args,omitempty"`
	Assign  map[string]string `yaml:"assign,omitempty"`
	Timeout string            `yaml:"timeout,omitempty"`
	OnError string            `yaml:"on_error,omitempty"`
}

func renderCronNotify(in RenderInput, slots map[string]any) (yamlDoc, error) {
	cron := slotString(slots, "schedule_cron")
	msg := slotString(slots, "message")
	tool := slotString(slots, "notify_tool")
	args := slotObject(slots, "notify_args")
	return yamlDoc{
		ID: in.Slug, Name: in.Name, Description: in.Description,
		Inputs: map[string]yamlInput{
			"trigger_note": {Type: "string", Default: fmt.Sprintf("cron %s (trigger pending Slice 24)", cron)},
		},
		Steps: []yamlStep{
			{ID: "plan", Assign: map[string]string{"body": msg}},
			{ID: "notify", Use: tool, Args: args, OnError: "fail"},
		},
		Outputs: map[string]string{"message": "${vars.body}", "sent_via": tool},
	}, nil
}

func renderWebhookForward(in RenderInput, slots map[string]any) (yamlDoc, error) {
	path := slotString(slots, "webhook_path")
	tool := slotString(slots, "forward_tool")
	args := slotObject(slots, "forward_args")
	return yamlDoc{
		ID: in.Slug, Name: in.Name, Description: in.Description,
		Inputs: map[string]yamlInput{
			"payload": {Type: "object", Default: map[string]any{}},
			"trigger_note": {Type: "string", Default: fmt.Sprintf("webhook /%s (trigger pending Slice 24)", path)},
		},
		Steps: []yamlStep{
			{ID: "forward", Use: tool, Args: args, OnError: "fail"},
		},
		Outputs: map[string]string{"forwarded": "true"},
	}, nil
}

func renderHTTPFetchNotify(in RenderInput, slots map[string]any) (yamlDoc, error) {
	url := slotString(slots, "url")
	method := slotString(slots, "method")
	if method == "" {
		method = "GET"
	}
	tool := slotString(slots, "notify_tool")
	args := slotObject(slots, "notify_args")
	prompt := fmt.Sprintf("Summarize an HTTP %s response from %s for notification.", method, url)
	return yamlDoc{
		ID: in.Slug, Name: in.Name, Description: in.Description,
		Steps: []yamlStep{
			{
				ID: "fetch_plan", Use: "llm.chat",
				Args: map[string]any{
					"model": "default-mock:text",
					"messages": []map[string]string{
						{"role": "user", "content": prompt},
					},
				},
			},
			{ID: "pick", Assign: map[string]string{"summary": "${steps.fetch_plan.output}"}},
			{ID: "notify", Use: tool, Args: args, OnError: "fail"},
		},
		Outputs: map[string]string{"summary": "${vars.summary}"},
	}, nil
}

func renderLLMSummarizeNotify(in RenderInput, slots map[string]any) (yamlDoc, error) {
	prompt := slotString(slots, "prompt")
	model := slotString(slots, "model")
	if model == "" {
		model = "default-mock:text"
	}
	tool := slotString(slots, "notify_tool")
	args := slotObject(slots, "notify_args")
	return yamlDoc{
		ID: in.Slug, Name: in.Name, Description: in.Description,
		Steps: []yamlStep{
			{
				ID: "summarize", Use: "llm.chat",
				Args: map[string]any{
					"model": model,
					"messages": []map[string]string{{"role": "user", "content": prompt}},
				},
			},
			{ID: "pick", Assign: map[string]string{"summary": "${steps.summarize.output}"}},
			{ID: "notify", Use: tool, Args: args, OnError: "fail"},
		},
		Outputs: map[string]string{"summary": "${vars.summary}"},
	}, nil
}

func renderToolChain(in RenderInput, slots map[string]any) (yamlDoc, error) {
	raw, ok := slots["steps"]
	if !ok {
		return yamlDoc{}, fmt.Errorf("slot steps is required")
	}
	items, err := parseStepChain(raw)
	if err != nil {
		return yamlDoc{}, err
	}
	if len(items) < 1 || len(items) > 3 {
		return yamlDoc{}, fmt.Errorf("tool-chain requires 1-3 steps, got %d", len(items))
	}
	steps := make([]yamlStep, 0, len(items))
	for _, it := range items {
		steps = append(steps, yamlStep{ID: it.id, Use: it.use, Args: it.args, OnError: "fail"})
	}
	last := items[len(items)-1].id
	return yamlDoc{
		ID: in.Slug, Name: in.Name, Description: in.Description,
		Steps:   steps,
		Outputs: map[string]string{"last": fmt.Sprintf("${steps.%s.output}", last)},
	}, nil
}

type chainItem struct {
	id   string
	use  string
	args map[string]any
}

func parseStepChain(raw any) ([]chainItem, error) {
	switch arr := raw.(type) {
	case []any:
		out := make([]chainItem, 0, len(arr))
		for i, el := range arr {
			m, ok := el.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("steps[%d]: expected object", i)
			}
			id := slotString(m, "id")
			use := slotString(m, "use")
			if id == "" || use == "" {
				return nil, fmt.Errorf("steps[%d]: id and use required", i)
			}
			args, _ := m["args"].(map[string]any)
			if args == nil {
				args = map[string]any{}
			}
			out = append(out, chainItem{id: id, use: use, args: args})
		}
		return out, nil
	case []map[string]any:
		tmp := make([]any, len(arr))
		for i, m := range arr {
			tmp[i] = m
		}
		return parseStepChain(tmp)
	default:
		return nil, fmt.Errorf("slot steps: expected array")
	}
}

func slotString(slots map[string]any, key string) string {
	if slots == nil {
		return ""
	}
	v, ok := slots[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func slotObject(slots map[string]any, key string) map[string]any {
	if slots == nil {
		return map[string]any{}
	}
	v, ok := slots[key]
	if !ok {
		return map[string]any{}
	}
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
