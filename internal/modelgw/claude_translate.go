package modelgw

import (
	"encoding/json"
	"strings"
)

// Anthropic Messages API 私有类型;不导出。

type anthropicTextBlock struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}
type anthropicToolUseBlock struct {
	Type  string          `json:"type"` // "tool_use"
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}
type anthropicToolResultBlock struct {
	Type      string `json:"type"` // "tool_result"
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

// anthropicBlock 是一个 type-tagged union。Marshal 时手动序列化对应子类型;
// 解析时用 anthropicBlockRaw 先读 type 字段。
type anthropicBlock struct {
	Text       *anthropicTextBlock
	ToolUse    *anthropicToolUseBlock
	ToolResult *anthropicToolResultBlock
}

func (b anthropicBlock) MarshalJSON() ([]byte, error) {
	switch {
	case b.Text != nil:
		return json.Marshal(b.Text)
	case b.ToolUse != nil:
		return json.Marshal(b.ToolUse)
	case b.ToolResult != nil:
		return json.Marshal(b.ToolResult)
	}
	return []byte("null"), nil
}

type anthropicMessage struct {
	Role    string           `json:"role"` // "user" / "assistant"
	Content []anthropicBlock `json:"content"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

type anthropicMessagesReq struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature *float64           `json:"temperature,omitempty"`
	TopP        *float64           `json:"top_p,omitempty"`
	Stop        []string           `json:"stop_sequences,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
}

// anthropicRespBlock 用 raw 解 type 后分发。
type anthropicRespBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicMessagesResp struct {
	ID         string               `json:"id"`
	Type       string               `json:"type"` // "message"
	Role       string               `json:"role"` // "assistant"
	Content    []anthropicRespBlock `json:"content"`
	Model      string               `json:"model"`
	StopReason string               `json:"stop_reason"`
	Usage      anthropicUsage       `json:"usage"`
}

// ToAnthropicReq 把 OpenAI ChatRequest 转 Anthropic Messages 请求。
// model 参数是裸 model 名(无 provider 前缀)。
func ToAnthropicReq(in ChatRequest, model string) anthropicMessagesReq {
	out := anthropicMessagesReq{
		Model:       model,
		Temperature: in.Temperature,
		TopP:        in.TopP,
		Stop:        in.Stop,
		Stream:      in.Stream,
	}
	if in.MaxTokens != nil {
		out.MaxTokens = *in.MaxTokens
	} else {
		out.MaxTokens = 4096
	}

	var systems []string
	for _, m := range in.Messages {
		switch m.Role {
		case RoleSystem:
			systems = append(systems, m.Content)
		case RoleTool:
			out.Messages = append(out.Messages, anthropicMessage{
				Role: "user",
				Content: []anthropicBlock{{
					ToolResult: &anthropicToolResultBlock{
						Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content,
					},
				}},
			})
		default:
			blocks := []anthropicBlock{}
			if m.Content != "" {
				blocks = append(blocks, anthropicBlock{
					Text: &anthropicTextBlock{Type: "text", Text: m.Content},
				})
			}
			for _, tc := range m.ToolCalls {
				var input json.RawMessage = []byte(tc.Function.Arguments)
				if len(input) == 0 {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, anthropicBlock{
					ToolUse: &anthropicToolUseBlock{
						Type: "tool_use", ID: tc.ID,
						Name: tc.Function.Name, Input: input,
					},
				})
			}
			out.Messages = append(out.Messages, anthropicMessage{
				Role: string(m.Role), Content: blocks,
			})
		}
	}
	if len(systems) > 0 {
		out.System = strings.Join(systems, "\n\n")
	}
	for _, t := range in.Tools {
		out.Tools = append(out.Tools, anthropicTool{
			Name: t.Function.Name, Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}
	return out
}

// FromAnthropicResp 把 Anthropic 响应转回 OpenAI ChatResponse。
// providerName 用于 ChatResponse.Model 字段 ("claude:..." 原样回显)。
func FromAnthropicResp(in anthropicMessagesResp, providerName, model string) *ChatResponse {
	msg := ChatMessage{Role: RoleAssistant}
	for _, b := range in.Content {
		switch b.Type {
		case "text":
			msg.Content += b.Text
		case "tool_use":
			args, _ := json.Marshal(json.RawMessage(b.Input))
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID: b.ID, Type: "function",
				Function: ToolCallFunc{Name: b.Name, Arguments: string(args)},
			})
		}
	}
	return &ChatResponse{
		ID: in.ID, Object: "chat.completion",
		Model: providerName + ":" + model,
		Choices: []ChatChoice{{
			Index: 0, Message: msg,
			FinishReason: mapAnthropicStopReason(in.StopReason),
		}},
		Usage: Usage{
			PromptTokens:     in.Usage.InputTokens,
			CompletionTokens: in.Usage.OutputTokens,
			TotalTokens:      in.Usage.InputTokens + in.Usage.OutputTokens,
		},
	}
}

func mapAnthropicStopReason(a string) string {
	switch a {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "stop_sequence":
		return "stop"
	}
	return "stop"
}
