package template

import "fmt"

// SlotSpec describes one template parameter shown to users and Agents.
type SlotSpec struct {
	Name            string   `json:"name"`
	Type            string   `json:"type"` // string | object | array
	Required        bool     `json:"required"`
	Description     string   `json:"description"`
	Default         any      `json:"default,omitempty"`
	ToolPicker      string   `json:"tool_picker,omitempty"` // notify | forward
	SuggestedTools  []string `json:"suggested_tools,omitempty"`
}

// Definition is a built-in workflow template (Path C).
type Definition struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Slots       []SlotSpec `json:"slots"`
}

var catalog = []Definition{
	{
		ID:          "cron-notify",
		Name:        "定时通知",
		Description: "按 cron 表达式触发后发送通知（v1 触发器占位，手动 invoke 可测）",
		Slots: []SlotSpec{
			{Name: "schedule_cron", Type: "string", Required: true, Description: "Cron 表达式，如 0 9 * * 1-5"},
			{Name: "message", Type: "string", Required: true, Description: "通知正文"},
			{Name: "notify_tool", Type: "string", Required: true, Default: "llm.chat", ToolPicker: "notify",
				SuggestedTools: []string{"mcp.slack.post_message", "mcp.slack.post", "llm.chat"},
				Description: "通知工具（MCP 连接器或 llm.chat）"},
			{Name: "notify_args", Type: "object", Required: true, Description: "传给 notify_tool 的 args"},
		},
	},
	{
		ID:          "webhook-forward",
		Name:        "Webhook 转发",
		Description: "接收 Webhook 后转发到指定工具（v1 触发器占位）",
		Slots: []SlotSpec{
			{Name: "webhook_path", Type: "string", Required: true, Description: "Webhook 路径片段"},
			{Name: "forward_tool", Type: "string", Required: true, ToolPicker: "forward",
				SuggestedTools: []string{"mcp.slack.post_message", "mcp.github.create_issue", "llm.chat"},
				Description: "转发目标工具（MCP 或 llm.chat）"},
			{Name: "forward_args", Type: "object", Required: true, Description: "转发 args；可用 ${inputs.payload}"},
		},
	},
	{
		ID:          "http-fetch-notify",
		Name:        "HTTP 拉取并通知",
		Description: "用 LLM 处理拉取意图并发送通知（v1 用 llm.chat 占位 HTTP）",
		Slots: []SlotSpec{
			{Name: "url", Type: "string", Required: true, Description: "HTTP URL"},
			{Name: "method", Type: "string", Required: false, Default: "GET", Description: "HTTP 方法"},
			{Name: "notify_tool", Type: "string", Required: true, Default: "llm.chat", ToolPicker: "notify",
				SuggestedTools: []string{"mcp.slack.post_message", "http.fetch", "llm.chat"}},
			{Name: "notify_args", Type: "object", Required: true, Description: "通知工具 args"},
		},
	},
	{
		ID:          "llm-summarize-notify",
		Name:        "LLM 摘要并通知",
		Description: "用大模型生成摘要，再调用通知工具",
		Slots: []SlotSpec{
			{Name: "prompt", Type: "string", Required: true, Description: "摘要提示词"},
			{Name: "model", Type: "string", Required: false, Default: "default-mock:text"},
			{Name: "notify_tool", Type: "string", Required: true, Default: "llm.chat", ToolPicker: "notify",
				SuggestedTools: []string{"mcp.slack.post_message", "http.fetch", "llm.chat"}},
			{Name: "notify_args", Type: "object", Required: true, Description: "通知工具 args"},
		},
	},
	{
		ID:          "tool-chain",
		Name:        "工具链",
		Description: "顺序执行 2–3 个 ToolBus 工具",
		Slots: []SlotSpec{
			{Name: "steps", Type: "array", Required: true, Description: "数组项：{id, use, args}"},
		},
	},
	{
		ID:          "mock-inspect",
		Name:        "Mock 状态巡检",
		Description: "P0 验证：e2e-mock 查状态、分支告警或正常 echo（需 slug=e2e-mock）",
		Slots: []SlotSpec{
			{Name: "scenario", Type: "string", Required: false, Default: "degraded",
				Description: "ok | degraded"},
			{Name: "alert_text", Type: "string", Required: false,
				Default: "ALERT: system degraded", Description: "异常分支 echo 文案"},
			{Name: "ok_text", Type: "string", Required: false,
				Default: "OK: system healthy", Description: "正常分支 echo 文案"},
		},
	},
}

// List returns a copy of the built-in template catalog.
func List() []Definition {
	out := make([]Definition, len(catalog))
	copy(out, catalog)
	return out
}

// Get returns one template by id.
func Get(id string) (Definition, error) {
	for _, d := range catalog {
		if d.ID == id {
			return d, nil
		}
	}
	return Definition{}, fmt.Errorf("template: unknown id %q", id)
}

// IDs returns all template ids.
func IDs() []string {
	out := make([]string, len(catalog))
	for i, d := range catalog {
		out[i] = d.ID
	}
	return out
}
